package admin

import (
	"net/http"
	"strings"

	"zenflash-llm/internal/httpx"
	modelcatalog "zenflash-llm/internal/models"
)

// handleFlags lists every admin model flag (raw ID, deprecated, updated_at).
// Read-only; no secrets.
func (a *Server) handleFlags(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ds := a.durable
	a.mu.Unlock()
	if ds == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"flags": []any{}, "persisted": false})
		return
	}
	flags := ds.FlagList(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"flags": flags, "persisted": true})
}

// handlePutFlag toggles one raw model ID deprecated. The change is written to
// Postgres, applies to the in-memory cache immediately, and therefore affects
// /v1/models and every inference route at once. Requests never wait on DB
// reads afterwards.
func (a *Server) handlePutFlag(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ds := a.durable
	a.mu.Unlock()
	if ds == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "flags_unavailable", "Postgres is not configured; set DATABASE_URL to use model toggles")
		return
	}
	var input struct {
		ID         string `json:"id"`
		Deprecated bool   `json:"deprecated"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id := strings.TrimSpace(input.ID)
	if id != "" {
		if raw, _, ok := modelcatalog.SplitTierPrefix(id); ok {
			id = raw
		}
	}
	if id == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	if err := ds.SetDeprecated(r.Context(), id, input.Deprecated); err != nil {
		writeAdminError(w, http.StatusBadGateway, "flag_update_failed", "could not update model flag in Postgres")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deprecated": input.Deprecated, "flags": ds.FlagList(r.Context())})
}
