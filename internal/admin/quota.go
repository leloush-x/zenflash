package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"zenflash-llm/internal/httpx"
)

// handleAntigravityQuota returns live fetchAvailableModels quota meters.
func (a *Server) handleAntigravityQuota(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	quotas := a.manager.AntigravityQuota(ctx)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"antigravity": quotas})
}

// handleQuota returns a unified quota snapshot for all three OAuth pools:
//   - antigravity: live remainingFraction meters from fetchAvailableModels
//   - codex: local 30-day usage (OpenAI exposes no remaining quota) + models
//   - cline: local usage counters from the embedded proxy
func (a *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	antigravity := a.manager.AntigravityQuota(ctx)

	var codex any
	if a.codex != nil {
		codex = map[string]any{
			"accounts":         a.codex.Accounts(),
			"models":           a.codex.Models(),
			"window":           "last 30 days, local ZenFlash requests only",
			"remaining_note":   "OpenAI does not expose remaining monthly quota via API",
			"manage_usage_url": "https://chatgpt.com/#settings/Usage",
		}
	} else {
		codex = map[string]any{"accounts": []any{}, "models": []any{}}
	}

	cline := a.fetchClineQuota(ctx)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"antigravity": antigravity,
		"codex":       codex,
		"cline":       cline,
		"fetched_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *Server) fetchClineQuota(ctx context.Context) any {
	if a.clineURL == "" {
		return map[string]any{"available": false, "reason": "embedded Cline proxy is disabled (-cline-port 0)", "accounts": []any{}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.clineURL+"/admin/api/accounts", nil)
	if err != nil {
		return map[string]any{"available": false, "reason": err.Error(), "accounts": []any{}}
	}
	resp, err := clineClient.Do(req)
	if err != nil {
		return map[string]any{"available": false, "reason": "cline unreachable: " + err.Error(), "accounts": []any{}}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return map[string]any{"available": false, "reason": "cline HTTP " + resp.Status, "accounts": []any{}}
	}
	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			Accounts []any `json:"accounts"`
			Total    int   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Fall back to raw shape if the proxy changes envelope.
		var raw any
		_ = json.Unmarshal(body, &raw)
		return map[string]any{"available": true, "accounts": raw}
	}
	return map[string]any{"available": true, "accounts": parsed.Data.Accounts, "total": parsed.Data.Total}
}

// handleCatalogRefresh forces one live model catalog pass so /v1/models and
// the dashboard adapt immediately without waiting for the background tick.
func (a *Server) handleCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	a.manager.RefreshModelsNow(ctx)
	res := a.manager.Resources()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"refreshed": true, "models": res.Models, "fetched_at": res.Models.UpdatedAt.UTC().Format(time.RFC3339)})
}
