package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"zenflash-llm/internal/config"
	"zenflash-llm/internal/httpx"
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
	if wasHealthy || zenMoved+goMoved > 0 {
		g.logger.Warn("proxy became unavailable", "component", "proxy", "event", "proxy_unavailable", "proxy", config.RedactURL(proxy.name), "zen_keys_moved", zenMoved, "go_keys_moved", goMoved)
	}
	return zenMoved, goMoved
}

func (g *Gateway) restoreProxy(proxy *proxyTransport) (zenMoved, goMoved int) {
	if proxy == nil {
		return 0, 0
	}
	zenMoved = g.zenNodes.RestoreProxy(proxy.index)
	goMoved = g.goNodes.RestoreProxy(proxy.index)
	if zenMoved+goMoved > 0 {
		g.logger.Info("proxy connectivity restored", "component", "proxy", "event", "proxy_restored", "proxy", config.RedactURL(proxy.name), "zen_keys_moved", zenMoved, "go_keys_moved", goMoved)
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
		var zen, goModels []string
		var capabilities modelcatalog.Capabilities
		var capabilitiesErr error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); zen = g.refreshZen(ctx) }()
		go func() { defer wg.Done(); goModels = g.refreshTier(ctx, g.cfg.Upstream.Go, g.goNodes) }()
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
		if goModels != nil {
			if capabilities.Protocols == nil {
				capabilities.Protocols = map[config.Tier]map[string]wire.Protocol{config.TierZen: {}, config.TierGo: {}}
			}
			if capabilities.Protocols[config.TierGo] == nil {
				capabilities.Protocols[config.TierGo] = map[string]wire.Protocol{}
			}
			for _, model := range goModels {
				if _, ok := capabilities.Protocols[config.TierGo][model]; !ok {
					capabilities.Protocols[config.TierGo][model] = wire.Chat
				}
			}
		}
		if zen != nil || goModels != nil {
			g.catalog.ReplaceWithCapabilities(zen, goModels, capabilities.Protocols, capabilities.Unsupported, capabilities.Metadata)
			if ctx.Err() == nil {
				if err := g.catalog.SaveCache(); err != nil {
					g.logger.Warn("model catalog cache write failed", "component", "models", "event", "catalog_cache_write_failed", "error", err)
				}
				g.probeModelEfforts(ctx)
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

var effortProbeValues = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

type effortIndex struct {
	Models map[string]effortModelInfo `json:"models"`
}

type effortModelInfo struct {
	Efforts []string `json:"efforts"`
	Error   string   `json:"error,omitempty"`
}

func (g *Gateway) probeModelEfforts(ctx context.Context) {
	if !g.cfg.Anonymous || g.cfg.Upstream.Zen == "" {
		return
	}
	cachePath := g.catalog.CachePath()
	if cachePath == "" {
		return
	}
	indexPath := strings.TrimSuffix(cachePath, ".models.catalog.json") + ".models.effort_index.json"
	existing := effortIndex{Models: map[string]effortModelInfo{}}
	if b, err := os.ReadFile(indexPath); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	var ids []string
	for _, id := range g.catalog.List() {
		if !g.catalog.IsFreeModel(id) {
			continue
		}
		if info, ok := existing.Models[id]; ok && len(info.Efforts) > 0 {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	client := &http.Client{Timeout: 20 * time.Second}
	for _, proxy := range g.transports.items {
		if proxy != nil && proxy.healthy.Load() {
			client = proxy.client
			break
		}
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			info := probeModelEffort(ctx, client, g.cfg.Upstream.Zen, id)
			mu.Lock()
			existing.Models[id] = info
			mu.Unlock()
			g.logger.Info("model effort probed", "component", "models", "event", "effort_probe", "model", id, "efforts", len(info.Efforts), "error", info.Error)
		}()
	}
	wg.Wait()
	if len(existing.Models) == 0 {
		return
	}
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	tmp := indexPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err == nil {
		_ = os.Rename(tmp, indexPath)
	}
}

func probeModelEffort(ctx context.Context, client *http.Client, base, id string) effortModelInfo {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var efforts []string
	var lastErr string
	for _, effort := range effortProbeValues {
		payload := map[string]any{
			"model":            id,
			"messages":         []map[string]string{{"role": "user", "content": "reply ok"}},
			"max_tokens":       1,
			"reasoning_effort": effort,
			"stream":           true,
			"stream_options":   map[string]any{"include_usage": true},
			"tools":            effortProbeTools(),
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			return effortModelInfo{Error: err.Error()}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+anonymousZenKey)
		req.Header.Set("User-Agent", httpx.UserAgent())
		req.Header.Set("x-opencode-client", "cli")
		req.Header.Set("x-opencode-session", "ses_000000000000AAAAAAAAAAAAAA")
		req.Header.Set("x-session-affinity", "ses_000000000000AAAAAAAAAAAAAA")
		req.Header.Set("X-Session-Id", "ses_000000000000AAAAAAAAAAAAAA")
		req.Header.Set("x-opencode-request", "zenflash_effort_probe")
		req.Header.Set("x-opencode-project", "zenflash-llm")
		resp, err := client.Do(req)
		if err == nil {
			var body []byte
			_, body, err = readAllResponse(resp)
			resp.Body.Close()
			if err == nil {
				s := string(body)
				if resp.StatusCode/100 == 2 && !strings.Contains(s, `"error"`) {
					efforts = append(efforts, effort)
					continue
				}
				lastErr = parseEffortError(s)
				if lastErr == "" {
					lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
				if allowed := parseAllowedEfforts(s); len(allowed) > 0 {
					return effortModelInfo{Efforts: allowed}
				}
				if strings.Contains(s, `allowed values: []`) || strings.Contains(s, `Supported values: []`) {
					return effortModelInfo{Efforts: []string{"none"}}
				}
				if !strings.Contains(s, "reasoning_effort") && !strings.Contains(s, "reasoning") {
					lastErr = parseEffortError(s)
					return effortModelInfo{Efforts: []string{"none"}, Error: lastErr}
				}
				continue
			}
		}
		if err != nil {
			lastErr = err.Error()
			break
		}
	}
	if len(efforts) == 0 {
		return effortModelInfo{Efforts: []string{"none"}, Error: lastErr}
	}
	return effortModelInfo{Efforts: efforts, Error: lastErr}
}

func effortProbeTools() []map[string]any {
	var tools []map[string]any
	for _, name := range []string{"bash", "edit", "glob", "grep", "read"} {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": "Agent tool " + name,
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		})
	}
	return tools
}

func parseAllowedEfforts(s string) []string {
	if i := strings.Index(s, `allowed values: `); i >= 0 {
		rest := s[i+len(`allowed values: `):]
		if j := strings.Index(rest, "]"); j > 0 {
			var allowed []string
			if err := json.Unmarshal([]byte(rest[:j+1]), &allowed); err == nil {
				return allowed
			}
		}
	}
	if i := strings.Index(s, `Supported values: [`); i >= 0 {
		rest := s[i+len(`Supported values: [`):]
		if j := strings.Index(rest, "]"); j > 0 {
			var allowed []string
			if err := json.Unmarshal([]byte("["+rest[:j+1]), &allowed); err == nil {
				return allowed
			}
		}
	}
	return nil
}

func parseEffortError(s string) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		if v.Error.Message != "" {
			return v.Error.Message
		}
		if v.Message != "" {
			return v.Message
		}
	}
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

// readAllResponse reads a small upstream body without pulling in extra helpers.
func readAllResponse(resp *http.Response) (string, []byte, error) {
	if resp.Body == nil {
		return "", nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", nil, err
	}
	return "", b, nil
}
