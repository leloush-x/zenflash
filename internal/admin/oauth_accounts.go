package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"zenflash-llm/internal/httpx"
)

// handleOAuthAccounts lists stored Codex + Antigravity credentials for Settings.
func (a *Server) handleOAuthAccounts(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"accounts": a.manager.OAuthAccounts()})
}

// handleOAuthImport validates a pasted refresh token and stores it.
func (a *Server) handleOAuthImport(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var input struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(body, &input); err != nil || strings.TrimSpace(input.RefreshToken) == "" {
			writeAdminError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		account, err := a.manager.ImportOAuthToken(ctx, provider, strings.TrimSpace(input.RefreshToken))
		if err != nil {
			writeAdminError(w, http.StatusBadGateway, "import_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"account": account, "accounts": a.manager.OAuthAccounts()})
	}
}

// handleOAuthDelete removes one stored credential.
func (a *Server) handleOAuthDelete(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var input struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &input)
		if strings.TrimSpace(input.ID) == "" {
			writeAdminError(w, http.StatusBadRequest, "invalid_request", "id is required")
			return
		}
		if err := a.manager.DeleteOAuthAccount(provider, strings.TrimSpace(input.ID)); err != nil {
			writeAdminError(w, http.StatusNotFound, "delete_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"removed": true, "accounts": a.manager.OAuthAccounts()})
	}
}

// handleOAuthRefresh forces an immediate stale-token refresh for both providers.
func (a *Server) handleOAuthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	a.manager.RefreshOAuthAccounts(ctx)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"refreshed": true, "accounts": a.manager.OAuthAccounts()})
}
