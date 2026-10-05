package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"zenflash-llm/internal/httpx"
)

func (a *Server) handleCodexLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := a.codex.StartLogin(req.ClientID)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "codex_login_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (a *Server) handleCodexLoginComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallbackURL string `json:"callback_url"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	account, err := a.codex.CompleteLogin(ctx, req.CallbackURL)
	if err != nil {
		// Cloudflare turns upstream 502 responses into its own HTML error page,
		// hiding the useful OAuth error from the admin UI. This is a failed
		// callback submission, so return the diagnostic as a normal client error.
		writeAdminError(w, http.StatusUnprocessableEntity, "codex_login_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "connected", "account": account})
}

func (a *Server) handleCodexAccounts(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"accounts": a.codex.Accounts()})
}

func (a *Server) handleCodexAccountDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := a.codex.DeleteAccount(strings.TrimSpace(req.ID)); err != nil {
		writeAdminError(w, http.StatusNotFound, "account_not_found", "Codex account was not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *Server) handleCodexModels(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": a.codex.Models()})
}

func (a *Server) handleCodexModelRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := a.codex.RefreshModels(ctx); err != nil {
		writeAdminError(w, http.StatusBadGateway, "codex_model_refresh_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": a.codex.Models()})
}

func (a *Server) handleCodexUsage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"window": "last 30 days, local ZenFlash requests only", "accounts": a.codex.Accounts(), "manage_usage_url": "https://chatgpt.com/#settings/Usage"})
}
