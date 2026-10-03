package admin

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"time"
)

var clineClient = &http.Client{Timeout: 30 * time.Second}

// handleClineOAuthStart begins a Cline device-login on the embedded Cline
// proxy and forwards the verification URL / user code to the dashboard.
func (a *Server) handleClineOAuthStart(w http.ResponseWriter, r *http.Request) {
	if a.clineURL == "" {
		writeAdminError(w, http.StatusServiceUnavailable, "cline_unavailable", "embedded Cline proxy is disabled (-cline-port 0)")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, a.clineURL+"/admin/api/oauth/start", bytes.NewReader(nil))
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "cline_request_failed", err.Error())
		return
	}
	resp, err := clineClient.Do(req)
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "cline_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// handleClineOAuthStatus polls a Cline device-login session.
func (a *Server) handleClineOAuthStatus(w http.ResponseWriter, r *http.Request) {
	if a.clineURL == "" {
		writeAdminError(w, http.StatusServiceUnavailable, "cline_unavailable", "embedded Cline proxy is disabled (-cline-port 0)")
		return
	}
	sessionID := url.QueryEscape(r.URL.Query().Get("sessionId"))
	if sessionID == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "sessionId is required")
		return
	}
	resp, err := clineClient.Get(a.clineURL + "/admin/api/oauth/status?sessionId=" + sessionID)
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "cline_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// handleClineAccounts proxies the embedded proxy's account list (email,
// status, local usage counters) for the Config tab.
func (a *Server) handleClineAccounts(w http.ResponseWriter, r *http.Request) {
	a.clineProxyJSON(w, r, http.MethodGet, "/admin/api/accounts", nil)
}

// handleClineAccountDelete removes one signed-in Cline account.
func (a *Server) handleClineAccountDelete(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	a.clineProxyJSON(w, r, http.MethodPost, "/admin/api/accounts/delete", body)
}

func (a *Server) clineProxyJSON(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	if a.clineURL == "" {
		writeAdminError(w, http.StatusServiceUnavailable, "cline_unavailable", "embedded Cline proxy is disabled (-cline-port 0)")
		return
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, a.clineURL+path, reader)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "cline_request_failed", err.Error())
		return
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := clineClient.Do(req)
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "cline_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
}
