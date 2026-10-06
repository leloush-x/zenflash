package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"zenflash-llm/internal/antigravity"
	"zenflash-llm/internal/codex"
	"zenflash-llm/internal/config"
	modelcatalog "zenflash-llm/internal/models"
	wire "zenflash-llm/internal/protocol"
)

const (
	proxyHealthCheckURL      = "https://cloudflare.com/cdn-cgi/trace"
	proxyHealthCheckInterval = 15 * time.Minute
	proxyHealthCheckTimeout  = 10 * time.Second
)

// syncProxyResult updates proxy health from real traffic. Only timeouts and
// connection refusals mark a proxy unavailable. Other errors and 4xx/5xx
// responses trigger a neutral URL check without being treated as proxy failure.
func (g *Gateway) syncProxyResult(ctx context.Context, proxy *proxyTransport, status int, err error) bool {
	if proxy == nil {
		return false
	}
	if isProxyFailure(err) {
		g.rebindFailedProxy(proxy)
		g.verifyProxyAfterError(ctx, proxy, status)
		return true
	}
	if status >= 200 && status < 400 {
		wasHealthy := proxy.healthy.Swap(true)
		if !wasHealthy {
			g.restoreProxy(proxy)
		}
		return false
	}
	if err != nil {
		g.verifyProxyAfterError(ctx, proxy, status)
		return false
	}
	if status >= 400 && status < 600 {
		g.verifyProxyAfterError(ctx, proxy, status)
	}
	return false
}

func (g *Gateway) verifyProxyAfterError(ctx context.Context, proxy *proxyTransport, status int) {
	if !proxy.checking.CompareAndSwap(false, true) {
		return
	}
	// The client request may finish or be cancelled while the verification is
	// running. Keep its values but give the proxy check an independent timeout.
	checkCtx := context.WithoutCancel(ctx)
	go func() {
		result := g.transports.checkClaimedProxy(checkCtx, proxy, proxyHealthCheckURL, proxyHealthCheckTimeout)
		g.applyProxyHealthResult(result, "upstream HTTP response", status)
	}()
}

func (g *Gateway) rebindFailedProxy(proxy *proxyTransport) (zenMoved, goMoved int) {
	if proxy == nil {
		return 0, 0
	}
	wasHealthy := proxy.healthy.Swap(false)
	return g.rebindUnavailableProxy(proxy, wasHealthy)
}

func (g *Gateway) rebindUnavailableProxy(proxy *proxyTransport, wasHealthy bool) (zenMoved, goMoved int) {
	zenMoved = g.zenNodes.RebindProxy(proxy.index)
	goMoved = g.goNodes.RebindProxy(proxy.index)
	codexMoved := 0
	if pool := g.codexPool(); pool != nil {
		codexMoved = pool.RebindProxy(proxy.index)
	}
	antigravityMoved := 0
	if pool := g.antigravityPool(); pool != nil {
		antigravityMoved = pool.RebindProxy(proxy.index)
	}
	if wasHealthy || zenMoved+goMoved+codexMoved+antigravityMoved > 0 {
		g.logger.Warn("proxy became unavailable", "component", "proxy", "event", "proxy_unavailable", "proxy", config.RedactURL(proxy.name), "zen_keys_moved", zenMoved, "go_keys_moved", goMoved, "codex_keys_moved", codexMoved, "antigravity_keys_moved", antigravityMoved)
	}
	return zenMoved, goMoved
}

func (g *Gateway) restoreProxy(proxy *proxyTransport) (zenMoved, goMoved int) {
	if proxy == nil {
		return 0, 0
	}
	zenMoved = g.zenNodes.RestoreProxy(proxy.index)
	goMoved = g.goNodes.RestoreProxy(proxy.index)
	codexMoved := 0
	if pool := g.codexPool(); pool != nil {
		codexMoved = pool.RestoreProxy(proxy.index)
	}
	antigravityMoved := 0
	if pool := g.antigravityPool(); pool != nil {
		antigravityMoved = pool.RestoreProxy(proxy.index)
	}
	if zenMoved+goMoved+codexMoved+antigravityMoved > 0 {
		g.logger.Info("proxy connectivity restored", "component", "proxy", "event", "proxy_restored", "proxy", config.RedactURL(proxy.name), "zen_keys_moved", zenMoved, "go_keys_moved", goMoved, "codex_keys_moved", codexMoved, "antigravity_keys_moved", antigravityMoved)
	}
	return zenMoved, goMoved
}

func (g *Gateway) StartProxyHealthChecks(ctx context.Context) {
	check := func() {
		results := g.transports.CheckHealth(ctx, proxyHealthCheckURL, proxyHealthCheckTimeout)
		for _, result := range results {
			g.applyProxyHealthResult(result, "scheduled health check", 0)
		}
	}
	go func() {
		ticker := time.NewTicker(proxyHealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check()
			}
		}
	}()
}

func (g *Gateway) applyProxyHealthResult(result proxyHealthResult, source string, upstreamStatus int) {
	if result.err == nil {
		if !result.wasHealthy {
			g.restoreProxy(result.proxy)
		}
		g.logger.Debug("proxy health check passed", "component", "proxy", "event", "health_check_passed", "source", source, "upstream_status", upstreamStatus, "proxy", config.RedactURL(result.proxy.name))
		return
	}
	if !result.failed {
		g.logger.Debug("proxy health check was inconclusive", "component", "proxy", "event", "health_check_inconclusive", "source", source, "upstream_status", upstreamStatus, "proxy", config.RedactURL(result.proxy.name), "error", result.err)
		return
	}
	if g.transports.hasHealthy() {
		zenMoved, goMoved := g.rebindUnavailableProxy(result.proxy, result.wasHealthy)
		if result.wasHealthy || zenMoved+goMoved > 0 {
			g.logger.Warn("proxy health check failed", "component", "proxy", "event", "health_check_failed", "source", source, "upstream_status", upstreamStatus, "proxy", config.RedactURL(result.proxy.name), "zen_keys_moved", zenMoved, "go_keys_moved", goMoved, "error", result.err)
			return
		}
	}
	g.logger.Debug("proxy health check is still failing", "component", "proxy", "event", "health_check_still_failing", "source", source, "upstream_status", upstreamStatus, "proxy", config.RedactURL(result.proxy.name), "error", result.err)
}

func (g *Gateway) StartModelRefresh(ctx context.Context) {
	refresh := func() {
		var zen, goModels, codexModels, antigravityModels []string
		var capabilities modelcatalog.Capabilities
		var capabilitiesErr error
		var wg sync.WaitGroup
		wg.Add(5)
		go func() { defer wg.Done(); zen = g.refreshZen(ctx) }()
		go func() { defer wg.Done(); goModels = g.refreshTier(ctx, g.cfg.Upstream.Go, g.goNodes) }()
		go func() { defer wg.Done(); codexModels = g.refreshCodexModels(ctx) }()
		go func() { defer wg.Done(); antigravityModels = g.refreshAntigravityModels(ctx) }()
		go func() {
			defer wg.Done()
			capabilityCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			capabilities, capabilitiesErr = g.refreshProtocolCapabilities(capabilityCtx)
		}()
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
		if capabilitiesErr != nil {
			g.logger.Warn("OpenCode capability catalog refresh failed", "component", "models", "event", "capability_refresh_failed", "error", capabilitiesErr)
		}
		if capabilities.Protocols == nil {
			capabilities.Protocols = map[config.Tier]map[string]wire.Protocol{config.TierZen: {}, config.TierGo: {}, config.TierCodex: {}, config.TierAntigravity: {}}
		}
		if goModels != nil {
			if capabilities.Protocols[config.TierGo] == nil {
				capabilities.Protocols[config.TierGo] = map[string]wire.Protocol{}
			}
			for _, model := range goModels {
				if _, ok := capabilities.Protocols[config.TierGo][model]; !ok {
					capabilities.Protocols[config.TierGo][model] = wire.Chat
				}
			}
		}
		if antigravityModels != nil {
			if capabilities.Protocols[config.TierAntigravity] == nil {
				capabilities.Protocols[config.TierAntigravity] = map[string]wire.Protocol{}
			}
			for _, model := range antigravityModels {
				if _, ok := capabilities.Protocols[config.TierAntigravity][model]; !ok {
					capabilities.Protocols[config.TierAntigravity][model] = wire.Chat
				}
			}
		}
		if codexModels != nil {
			if capabilities.Protocols == nil {
				capabilities.Protocols = map[config.Tier]map[string]wire.Protocol{config.TierZen: {}, config.TierGo: {}, config.TierCodex: {}, config.TierAntigravity: {}}
			}
			if capabilities.Protocols[config.TierCodex] == nil {
				capabilities.Protocols[config.TierCodex] = map[string]wire.Protocol{}
			}
			for _, model := range codexModels {
				if _, ok := capabilities.Protocols[config.TierCodex][model]; !ok {
					capabilities.Protocols[config.TierCodex][model] = wire.Responses
				}
			}
		}
		if zen != nil || goModels != nil || codexModels != nil || antigravityModels != nil {
			g.catalog.ReplaceWithCapabilities(zen, goModels, codexModels, antigravityModels, capabilities.Protocols, capabilities.Unsupported, capabilities.Metadata)
			if ctx.Err() == nil {
				if err := g.catalog.SaveCache(); err != nil {
					g.logger.Warn("model catalog cache write failed", "component", "models", "event", "catalog_cache_write_failed", "error", err)
				}
			}
			g.logger.Info("model catalog refreshed", "component", "models", "event", "catalog_refreshed", "models", len(g.catalog.List()))
		}
	}
	go func() {
		refresh()
		ticker := time.NewTicker(time.Duration(g.cfg.Models.RefreshSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func (g *Gateway) refreshProtocolCapabilities(ctx context.Context) (modelcatalog.Capabilities, error) {
	if g.transports == nil || len(g.transports.items) == 0 {
		return modelcatalog.FetchCapabilities(ctx, &http.Client{Timeout: 30 * time.Second}, modelcatalog.CapabilitiesURL)
	}
	var lastErr error
	for _, proxy := range g.transports.items {
		if proxy == nil || !proxy.healthy.Load() {
			continue
		}
		capabilities, err := modelcatalog.FetchCapabilities(ctx, proxy.client, modelcatalog.CapabilitiesURL)
		if err == nil {
			return capabilities, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no healthy proxy available for OpenCode capability catalog")
	}
	return modelcatalog.Capabilities{}, lastErr
}

func (g *Gateway) refreshZen(ctx context.Context) []string {
	if models := g.refreshTier(ctx, g.cfg.Upstream.Zen, g.zenNodes); models != nil {
		return models
	}
	if !g.cfg.Anonymous {
		return nil
	}
	return g.refreshAnonymousTier(ctx, g.cfg.Upstream.Zen)
}

func (g *Gateway) refreshAnonymousTier(ctx context.Context, base string) []string {
	cursor := g.anonymous.CursorFor("")
	limit := g.anonymous.Len()
	for attempt := 1; attempt <= limit; attempt++ {
		node := cursor.Next()
		if node == nil {
			break
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		models, status, err := modelcatalog.FetchModels(refreshCtx, node.proxy.client, base, anonymousZenKey)
		g.syncProxyResult(refreshCtx, node.proxy, status, err)
		cancel()
		if err == nil {
			g.anonymous.MarkSuccess(node)
			return models
		}
		g.anonymous.MarkFailure(node, nil, err)
		g.logger.Debug("anonymous model catalog refresh attempt failed", "component", "models", "event", "anonymous_refresh_attempt_failed", "upstream", config.RedactURL(base), "attempt", attempt, "proxy", config.RedactURL(node.proxy.name), "error", err)
	}
	g.logger.Warn("anonymous model catalog refresh failed", "component", "models", "event", "anonymous_refresh_failed", "upstream", config.RedactURL(base))
	return nil
}

func (g *Gateway) refreshTier(ctx context.Context, base string, nodes *nodePool) []string {
	cursor := nodes.Cursor()
	for attempt := 0; attempt < g.cfg.Retry.MaxAttempts; attempt++ {
		node := cursor.Next()
		if node == nil {
			return nil
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		proxy := nodes.Proxy(node)
		if proxy == nil {
			cancel()
			return nil
		}
		models, status, err := modelcatalog.FetchModels(refreshCtx, proxy.client, base, node.key)
		g.syncProxyResult(refreshCtx, proxy, status, err)
		cancel()
		if err == nil {
			nodes.MarkSuccess(node)
			return models
		}
		nodes.MarkFailure(node, nil, err)
		g.logger.Debug("model catalog refresh attempt failed", "component", "models", "event", "refresh_attempt_failed", "upstream", config.RedactURL(base), "attempt", attempt+1, "error", err)
	}
	g.logger.Warn("model catalog refresh failed", "component", "models", "event", "refresh_failed", "upstream", config.RedactURL(base))
	return nil
}

// refreshCodexModels lists models from the Codex backend using the same
// node/proxy rotation as the other tiers. It returns nil when Codex is not
// configured so the catalog keeps the previous snapshot.
func (g *Gateway) refreshCodexModels(ctx context.Context) []string {
	nodes := g.codexPool()
	if nodes == nil || nodes.Len() == 0 {
		return nil
	}
	base := g.codexBase()
	cursor := nodes.Cursor()
	for attempt := 0; attempt < g.cfg.Retry.MaxAttempts; attempt++ {
		node := cursor.Next()
		if node == nil {
			return nil
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		proxy := nodes.Proxy(node)
		if proxy == nil {
			cancel()
			return nil
		}
		models, status, err := codex.FetchModels(refreshCtx, proxy.client, base, codex.Credential{AccessToken: node.key, AccountID: node.accountID})
		g.syncProxyResult(refreshCtx, proxy, status, err)
		cancel()
		if err == nil {
			nodes.MarkSuccess(node)
			return models
		}
		nodes.MarkFailure(node, nil, err)
		g.logger.Debug("codex model catalog refresh attempt failed", "component", "models", "event", "codex_refresh_attempt_failed", "upstream", config.RedactURL(base), "attempt", attempt+1, "error", err)
	}
	g.logger.Warn("codex model catalog refresh failed", "component", "models", "event", "codex_refresh_failed", "upstream", config.RedactURL(base))
	return nil
}

// StartCodexTokenRefresh periodically exchanges stale stored Codex tokens
// for fresh ones and rebuilds the node pool when the set changes. Static
// codex_keys are left untouched; only tokens written by the login flow have
// a refresh token and can be renewed.
func (g *Gateway) StartCodexTokenRefresh(ctx context.Context) {
	go func() {
		g.refreshCodexTokens(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.refreshCodexTokens(ctx)
			}
		}
	}()
}

func (g *Gateway) refreshCodexTokens(ctx context.Context) {
	client := &http.Client{Timeout: 60 * time.Second}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, changed, err := codex.RefreshStale(ctx, client, codex.DefaultConfig(), g.codexAuthPath)
	if err != nil {
		g.logger.Warn("codex token refresh failed", "component", "codex", "event", "codex_token_refresh_failed", "error", err)
		return
	}
	if !changed {
		return
	}
	g.logger.Info("codex tokens refreshed", "component", "codex", "event", "codex_tokens_refreshed")
	nodes, err := newCodexNodePool(g.codexCredentials(), g.transports, time.Duration(g.cfg.Performance.FailureCooldownSeconds)*time.Second)
	if err == nil {
		g.codexNodes.Store(nodes)
	}
}

// refreshAntigravityModels lists models via Cloud Code fetchAvailableModels.
func (g *Gateway) refreshAntigravityModels(ctx context.Context) []string {
	nodes := g.antigravityPool()
	if nodes == nil || nodes.Len() == 0 {
		return nil
	}
	cursor := nodes.Cursor()
	for attempt := 0; attempt < g.cfg.Retry.MaxAttempts; attempt++ {
		node := cursor.Next()
		if node == nil {
			return nil
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		proxy := nodes.Proxy(node)
		if proxy == nil {
			cancel()
			return nil
		}
		models, err := antigravity.FetchModels(refreshCtx, proxy.client, node.key, node.accountID)
		status := 0
		if err == nil {
			status = 200
		}
		g.syncProxyResult(refreshCtx, proxy, status, err)
		cancel()
		if err == nil {
			nodes.MarkSuccess(node)
			return models
		}
		nodes.MarkFailure(node, nil, err)
		g.logger.Debug("antigravity model catalog refresh attempt failed", "component", "models", "event", "antigravity_refresh_attempt_failed", "attempt", attempt+1, "error", err)
	}
	g.logger.Warn("antigravity model catalog refresh failed", "component", "models", "event", "antigravity_refresh_failed")
	return nil
}

// StartAntigravityTokenRefresh periodically renews stored Antigravity bearers.
func (g *Gateway) StartAntigravityTokenRefresh(ctx context.Context) {
	go func() {
		g.refreshAntigravityTokens(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.refreshAntigravityTokens(ctx)
			}
		}
	}()
}

func (g *Gateway) refreshAntigravityTokens(ctx context.Context) {
	client := &http.Client{Timeout: 60 * time.Second}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, changed, err := antigravity.RefreshStale(ctx, client, g.antigravityAuthPath)
	if err != nil {
		g.logger.Warn("antigravity token refresh failed", "component", "antigravity", "event", "antigravity_token_refresh_failed", "error", err)
		return
	}
	if !changed {
		return
	}
	g.logger.Info("antigravity tokens refreshed", "component", "antigravity", "event", "antigravity_tokens_refreshed")
	nodes, err := newCodexNodePool(g.antigravityCredentials(), g.transports, time.Duration(g.cfg.Performance.FailureCooldownSeconds)*time.Second)
	if err == nil {
		g.antigravityNodes.Store(nodes)
	}
}
