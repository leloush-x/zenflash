package admin

import (
	"net/http"

	"zenflash-llm/internal/httpx"
	"zenflash-llm/internal/store"
)

// SetStore attaches the optional durable store for status reporting.
func (a *Server) SetStore(s *store.Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.durable = s
}

// handleStorage reports Postgres/file-sync status for the dashboard.
// Additive read-only route; existing routes and shapes are unchanged.
// Never exposes DATABASE_URL or key material, only counts and timestamps.
func (a *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ds := a.durable
	a.mu.Unlock()
	if ds == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "mode": "file",
			"note": "DATABASE_URL is unset; state persists in files beside config.json",
		})
		return
	}
	st := ds.Status(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled": st.Enabled, "mode": "postgres", "reachable": st.Reachable,
		"keys_cached": st.Keys, "slots": st.Slots, "last_sync": st.LastSync,
	})
}
