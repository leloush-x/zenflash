package gateway

import (
	"context"
	"net/http"
	"time"

	"zenflash-llm/internal/buildinfo"
	"zenflash-llm/internal/config"
	"zenflash-llm/internal/httpx"
	modelcatalog "zenflash-llm/internal/models"
	"zenflash-llm/internal/opencode"
)

type healthResponse struct {
	Status  string        `json:"status"`
	Ready   bool          `json:"ready"`
	Version string        `json:"version"`
	Models  healthModels  `json:"models"`
	Keys    healthKeys    `json:"keys"`
	Proxies healthProxies `json:"proxies"`
	Issues  []string      `json:"issues,omitempty"`
}

type healthModels struct {
	Status            string     `json:"status"`
	Total             int        `json:"total"`
	Exposed           int        `json:"exposed"`
	Zen               int        `json:"zen"`
	Go                int        `json:"go"`
	LastRefresh       *time.Time `json:"last_refresh,omitempty"`
	StaleAfterSeconds int        `json:"stale_after_seconds"`
	CacheSource       string     `json:"cache_source,omitempty"`
	Stale             bool       `json:"stale"`
}

type healthKeys struct {
	Zen         int  `json:"zen"`
	Go          int  `json:"go"`
	Codex       int  `json:"codex,omitempty"`
	Antigravity int  `json:"antigravity,omitempty"`
	Total       int  `json:"total"`
	Anonymous   bool `json:"anonymous"`
}

type healthProxies struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
}

func (g *Gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	_, models := g.availableModels()
	proxyTotal, proxyHealthy := g.transports.healthCounts()
	zenKeys, goKeys := g.zenNodes.Len(), g.goNodes.Len()
	codexKeys := g.codexPool().Len()
	antigravityKeys := g.antigravityPool().Len()
	staleAfter := max(2*time.Duration(g.cfg.Models.RefreshSeconds)*time.Second, time.Minute)

	modelStatus := "ready"
	var lastRefresh *time.Time
	issues := make([]string, 0, 3)
	if models.UpdatedAt.IsZero() {
		modelStatus = "pending"
		issues = append(issues, "model_catalog_pending")
	} else {
		updatedAt := models.UpdatedAt.UTC()
		lastRefresh = &updatedAt
		if models.Exposed == 0 {
			modelStatus = "empty"
			issues = append(issues, "model_catalog_empty")
		} else if models.Stale || time.Since(models.UpdatedAt) > staleAfter {
			modelStatus = "stale"
			issues = append(issues, "model_catalog_stale")
		}
	}
	if zenKeys+goKeys+codexKeys+antigravityKeys == 0 && !g.cfg.Anonymous {
		issues = append(issues, "no_upstream_keys")
	}
	if proxyHealthy == 0 {
		issues = append(issues, "no_healthy_proxies")
	}

	status := "ok"
	if len(issues) > 0 {
		status = "degraded"
	}
	blocking := modelStatus == "pending" || modelStatus == "empty" || proxyHealthy == 0
	httpStatus := http.StatusOK
	ready := !blocking
	if blocking {
		httpStatus = http.StatusServiceUnavailable
		if modelStatus == "pending" {
			status = "starting"
		}
	}
	httpx.WriteJSON(w, httpStatus, healthResponse{
		Status:  status,
		Ready:   ready,
		Version: buildinfo.Version,
		Models: healthModels{
			Status:            modelStatus,
			Total:             models.Total,
			Exposed:           models.Exposed,
			Zen:               models.Zen,
			Go:                models.Go,
			LastRefresh:       lastRefresh,
			StaleAfterSeconds: int(staleAfter / time.Second),
			CacheSource:       models.CacheSource,
			Stale:             models.Stale,
		},
		Keys: healthKeys{Zen: zenKeys, Go: goKeys, Codex: codexKeys, Antigravity: antigravityKeys, Total: zenKeys + goKeys + codexKeys + antigravityKeys, Anonymous: g.cfg.Anonymous},
		Proxies: healthProxies{
			Total:     proxyTotal,
			Healthy:   proxyHealthy,
			Unhealthy: proxyTotal - proxyHealthy,
		},
		Issues: issues,
	})
}

func (g *Gateway) availableModels() ([]modelcatalog.Route, modelcatalog.CatalogSnapshot) {
	return g.catalog.AvailableModelsWithAntigravity(g.zenNodes.Len() > 0, g.goNodes.Len() > 0 && g.hasGoKeys(), g.codexPool().Len() > 0, g.antigravityPool().Len() > 0, g.cfg.Anonymous)
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	// Dynamic adapt: ?refresh=1 / ?fresh=1 / Cache-Control: no-cache forces one
	// live catalog pass before serving, so newly linked accounts and upstream
	// model changes appear immediately instead of waiting for the next tick.
	if r != nil {
		q := r.URL.Query()
		want := q.Get("refresh") == "1" || q.Get("fresh") == "1" || q.Get("live") == "1"
		if !want && r.Header.Get("Cache-Control") == "no-cache" {
			want = true
		}
		if want {
			ctx := r.Context()
			if _, ok := ctx.Deadline(); !ok {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
				defer cancel()
			}
			g.RefreshModelsNow(ctx)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	now := time.Now().Unix()
	routes, _ := g.availableModels()
	// Usable-tier view: ?free=1 keeps anonymous-eligible free models plus
	// linked-account tiers (Codex service accounts, Antigravity free-tier
	// quota). Pure paid upstream models stay hidden. OpenCode's anonymous
	// free lane is currently restricted upstream, so account tiers are what
	// actually serve.
	if r != nil && (r.URL.Query().Get("free") == "1" || r.URL.Query().Get("free") == "true") {
		kept := routes[:0]
		for _, route := range routes {
			if g.catalog.IsFreeModel(route.ID) || route.Tier == config.TierCodex || route.Tier == config.TierAntigravity {
				kept = append(kept, route)
			}
		}
		routes = kept
	}
	data := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		model := route.ID
		raw := model
		if stripped, _, ok := modelcatalog.SplitTierPrefix(model); ok {
			raw = stripped
		}
		deprecated := g.Deprecated(raw)
		// Tier-scoped metadata: the advertised context window must match
		// the tier that will serve the request (anonymous ⇒ Zen).
		md := g.catalog.MetadataForTier(model, route.Tier)
		mdMap := map[string]any{}
		if md.ContextWindow > 0 {
			mdMap["context_window"] = md.ContextWindow
		}
		if md.MaxInput > 0 {
			mdMap["max_input"] = md.MaxInput
		}
		if md.MaxOutput > 0 {
			mdMap["max_output"] = md.MaxOutput
		}
		if md.Reasoning {
			mdMap["reasoning"] = true
		}
		if md.ReasoningEfforts != nil {
			mdMap["reasoning_efforts"] = md.ReasoningEfforts
		}
		if md.ToolCall {
			mdMap["tool_call"] = true
		}
		if md.StructuredOutput {
			mdMap["structured_output"] = true
		}
		if md.InputModalities != nil {
			mdMap["input_modalities"] = md.InputModalities
		}
		if md.OutputModalities != nil {
			mdMap["output_modalities"] = md.OutputModalities
		}
		entry := map[string]any{
			"id": model, "object": "model", "created": now, "owned_by": "opencode",
			"metadata": mdMap,
		}
		if deprecated {
			entry["deprecated"] = true
		}
		entry["provider"] = opencode.ProviderLabel(route.Tier, g.cfg.Upstream.Go)
		entry["route_protocol"] = route.Protocol
		if md.ReasoningEfforts != nil {
			entry["reasoning_efforts"] = md.ReasoningEfforts
		}
		// Top-level OpenAI-standard fields: discovery clients (jcode, Pi, …)
		// read context/reasoning at the top level of each model entry.
		if md.ContextWindow > 0 {
			entry["context_window"] = md.ContextWindow
			entry["context_length"] = md.ContextWindow
		}
		if md.MaxInput > 0 {
			entry["max_input"] = md.MaxInput
		}
		if md.MaxOutput > 0 {
			entry["max_output"] = md.MaxOutput
		}
		if md.Reasoning {
			entry["reasoning"] = true
			entry["supports_reasoning"] = true
		}
		if md.ToolCall {
			entry["tool_call"] = true
		}
		if md.StructuredOutput {
			entry["structured_output"] = true
		}
		data = append(data, entry)
		// Collision aliases: the same raw ID advertised on both opencode
		// (zen) and cline (go) is listed once per tier as opencode/<id> and
		// cline/<id> so clients can pin a provider. The bare ID is kept for
		// backward compatibility and follows prefer-order routing.
		tiers := g.catalog.TiersForModel(model)
		hasZen, hasGoCatalog := false, false
		for _, tr := range tiers {
			if tr == config.TierZen {
				hasZen = true
			} else if tr == config.TierGo {
				hasGoCatalog = true
			}
		}
		if hasZen && hasGoCatalog {
			aliasTiers := make([]config.Tier, 0, 2)
			if g.zenNodes.Len() > 0 || g.cfg.Anonymous {
				aliasTiers = append(aliasTiers, config.TierZen)
			}
			if g.goNodes.Len() > 0 && g.hasGoKeys() {
				aliasTiers = append(aliasTiers, config.TierGo)
			}
			for _, aliasTier := range aliasTiers {
				amd := g.catalog.MetadataForTier(model, aliasTier)
				amdMap := map[string]any{}
				if amd.ContextWindow > 0 {
					amdMap["context_window"] = amd.ContextWindow
				}
				if amd.MaxInput > 0 {
					amdMap["max_input"] = amd.MaxInput
				}
				if amd.MaxOutput > 0 {
					amdMap["max_output"] = amd.MaxOutput
				}
				if amd.Reasoning {
					amdMap["reasoning"] = true
				}
				if amd.ReasoningEfforts != nil {
					amdMap["reasoning_efforts"] = amd.ReasoningEfforts
				}
				if amd.ToolCall {
					amdMap["tool_call"] = true
				}
				if amd.StructuredOutput {
					amdMap["structured_output"] = true
				}
				alias := map[string]any{
					"id": opencode.AliasID(aliasTier, model), "object": "model", "created": now, "owned_by": "opencode",
					"metadata": amdMap,
				}
				if deprecated {
					alias["deprecated"] = true
				}
				if aliasTier == config.TierZen {
					alias["provider"] = "opencode"
				} else {
					alias["provider"] = "cline"
				}
				alias["route_protocol"] = route.ProtocolFor(aliasTier)
				if amd.ReasoningEfforts != nil {
					alias["reasoning_efforts"] = amd.ReasoningEfforts
				}
				if amd.ContextWindow > 0 {
					alias["context_window"] = amd.ContextWindow
					alias["context_length"] = amd.ContextWindow
				}
				if amd.MaxInput > 0 {
					alias["max_input"] = amd.MaxInput
				}
				if amd.MaxOutput > 0 {
					alias["max_output"] = amd.MaxOutput
				}
				if amd.Reasoning {
					alias["reasoning"] = true
					alias["supports_reasoning"] = true
				}
				if amd.ToolCall {
					alias["tool_call"] = true
				}
				if amd.StructuredOutput {
					alias["structured_output"] = true
				}
				data = append(data, alias)
			}
		}
	}
	// Deprecated entries stay visible but sink to the bottom so clients and
	// the dashboard stop surfacing them first. Separate backing arrays keep
	// the stable active/deprecated partition from overwriting itself.
	kept := make([]map[string]any, 0, len(data))
	pinned := make([]map[string]any, 0, len(data))
	for _, entry := range data {
		if entry["deprecated"] == true {
			pinned = append(pinned, entry)
		} else {
			kept = append(kept, entry)
		}
	}
	data = append(kept, pinned...)
	// Working-only view: ?working=1 live-probes each entry through the
	// production inference path and keeps entries that answer 2xx. Opt-in
	// and slower (one tiny reply per model); disabled models are removed
	// before probing, and the default listing shape is unchanged.
	if r != nil && (r.URL.Query().Get("working") == "1" || r.URL.Query().Get("working") == "true") {
		data = g.filterWorkingModels(r, data)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}
