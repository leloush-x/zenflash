package admin

import (
	"encoding/json"
	html "html"
	"io"
	"net/http"
	"strings"

	"zenflash-llm/internal/httpx"
)

// handleOAuthLoginStart creates a browser sign-in session and returns its link.
func (a *Server) handleOAuthLoginStart(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var input struct {
		Provider    string `json:"provider"`
		RedirectURI string `json:"redirect_uri"`
	}
	_ = json.Unmarshal(body, &input)
	if strings.TrimSpace(input.Provider) == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "provider is required")
		return
	}
	sessionID, authURL, err := a.manager.StartBrowserLogin(r.Context(), strings.TrimSpace(input.Provider), strings.TrimSpace(input.RedirectURI))
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "login_start_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"session_id": sessionID, "auth_url": authURL})
}

// handleOAuthLoginComplete exchanges a pasted callback URL for credentials.
func (a *Server) handleOAuthLoginComplete(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var input struct {
		SessionID   string `json:"session_id"`
		CallbackURL string `json:"callback_url"`
	}
	_ = json.Unmarshal(body, &input)
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.CallbackURL) == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "session_id and callback_url are required")
		return
	}
	account, err := a.manager.CompleteBrowserLogin(r.Context(), strings.TrimSpace(input.SessionID), strings.TrimSpace(input.CallbackURL))
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "login_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"account": account, "accounts": a.manager.OAuthAccounts()})
}

// handleOAuthCallback serves custom-registered public redirects
// (e.g. https://host/oauth-callback). Official loopback callbacks never reach
// here; those codes are pasted back through login/complete instead.
func (a *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code, state := r.URL.Query().Get("code"), r.URL.Query().Get("state")
	if code == "" || state == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<!doctype html><body style="font-family:sans-serif;text-align:center;padding:40px"><h2>Sign-in link is incomplete.</h2><p>Start again from the dashboard.</p></body>`))
		return
	}
	account, err := a.manager.CompleteBrowserLoginByState(r.Context(), code, state)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<!doctype html><body style="font-family:sans-serif;text-align:center;padding:40px"><h2>Sign-in could not be completed.</h2><p>` + html.EscapeString(err.Error()) + `</p></body>`))
		return
	}
	name := account.Email
	if name == "" {
		name = account.ID
	}
	_, _ = w.Write([]byte(`<!doctype html><body style="font-family:sans-serif;text-align:center;padding:40px"><h2>Signed in as ` + html.EscapeString(name) + `.</h2><p>You can close this tab and return to the dashboard.</p></body>`))
}
