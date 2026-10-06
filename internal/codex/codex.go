// Package codex implements the Codex (ChatGPT backend) upstream auth flow:
// PKCE OAuth login, form-encoded token refresh, file token storage, and the
// model listing used for catalog discovery. It mirrors the Codex CLI and
// codex2api credential lifecycle.
package codex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"zenflash-llm/internal/store"
)

// tokenStore is the optional Postgres home for login tokens. When nil,
// tokens live only in the JSON file beside the config.
var tokenStore *store.Store

// SetStore attaches the Postgres token home. Nil restores file-only mode.
func SetStore(s *store.Store) {
	tokenStore = s
}

func codexAccountID(t TokenData, i int) string {
	if t.AccountID != "" {
		return t.AccountID
	}
	if t.Email != "" {
		return t.Email
	}
	return fmt.Sprintf("account-%d", i)
}

const (
	DefaultCodexBase  = "https://chatgpt.com/backend-api/codex"
	DefaultRefreshURL = "https://auth.openai.com/oauth/token"
	DefaultClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	AuthorizeURL      = "https://auth.openai.com/oauth/authorize"
	RedirectURI       = "http://localhost:1455/auth/callback"
	AuthorizeScopes   = "openid profile email offline_access"
	RefreshScopes     = "openid profile email"
	UserAgent         = "codex-cli/0.155.0"
	ClientVersion     = "0.155.0"
	RefreshHours      = 8
)

type Config struct {
	CodexBase  string
	RefreshURL string
	ClientID   string
}

func DefaultConfig() Config {
	return Config{CodexBase: DefaultCodexBase, RefreshURL: DefaultRefreshURL, ClientID: DefaultClientID}
}

func (c Config) refreshURL() string {
	if strings.TrimSpace(c.RefreshURL) != "" {
		return strings.TrimSpace(c.RefreshURL)
	}
	return DefaultRefreshURL
}

func (c Config) clientID() string {
	if strings.TrimSpace(c.ClientID) != "" {
		return strings.TrimSpace(c.ClientID)
	}
	return DefaultClientID
}

// TokenData is one account's credential set persisted beside the config.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	LastRefresh  string    `json:"last_refresh"`
}

type Credential struct {
	AccessToken string
	AccountID   string
}

func Credentials(stored []TokenData, staticKeys []string) []Credential {
	out := make([]Credential, 0, len(stored)+len(staticKeys))
	for _, t := range stored {
		if strings.TrimSpace(t.AccessToken) != "" {
			out = append(out, Credential{AccessToken: strings.TrimSpace(t.AccessToken), AccountID: strings.TrimSpace(t.AccountID)})
		}
	}
	for _, k := range staticKeys {
		if strings.TrimSpace(k) != "" {
			out = append(out, Credential{AccessToken: strings.TrimSpace(k)})
		}
	}
	return out
}

func AuthPath(configPath string) string {
	if configPath == "" {
		return "codex-auth.json"
	}
	return configPath + ".codex-auth.json"
}

func LoadTokens(path string) ([]TokenData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return restoreTokens(path), nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return restoreTokens(path), nil
	}
	var tokens []TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return tokens, nil
}

// restoreTokens reloads login tokens from Postgres when the file is gone
// (fresh host, wiped volume) and rewrites the file so later reads stay fast.
func restoreTokens(path string) []TokenData {
	if tokenStore == nil {
		return nil
	}
	raw := tokenStore.OAuthList(context.Background(), "codex")
	if len(raw) == 0 {
		return nil
	}
	var tokens []TokenData
	for _, b := range raw {
		var one TokenData
		if json.Unmarshal(b, &one) == nil {
			tokens = append(tokens, one)
		}
	}
	if len(tokens) > 0 {
		_ = writeTokensFile(path, tokens)
	}
	return tokens
}

func writeTokensFile(path string, tokens []TokenData) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func SaveTokens(path string, tokens []TokenData) error {
	if err := writeTokensFile(path, tokens); err != nil {
		return err
	}
	mirrorTokens(tokens)
	return nil
}

// mirrorTokens copies the just-saved login tokens to Postgres. The file
// write above already succeeded, so DB trouble never fails the request.
func mirrorTokens(tokens []TokenData) {
	if tokenStore == nil {
		return
	}
	ids := make([]string, 0, len(tokens))
	payloads := make([][]byte, 0, len(tokens))
	for i, tok := range tokens {
		raw, err := json.Marshal(tok)
		if err != nil {
			continue
		}
		ids = append(ids, codexAccountID(tok, i))
		payloads = append(payloads, raw)
	}
	tokenStore.OAuthSave(context.Background(), "codex", ids, payloads)
}

// IsStale reports whether the token needs a refresh: expiry within 5 minutes,
// or LastRefresh older than RefreshHours when no expiry is stored.
func IsStale(t TokenData) bool {
	if !t.ExpiresAt.IsZero() {
		return time.Until(t.ExpiresAt) < 5*time.Minute
	}
	last, err := time.Parse(time.RFC3339Nano, t.LastRefresh)
	if err != nil {
		if last, err = time.Parse(time.RFC3339, t.LastRefresh); err != nil {
			return true
		}
	}
	return time.Since(last) > RefreshHours*time.Hour
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func codeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// BuildAuthorizeURL assembles the Codex CLI PKCE authorization link.
func BuildAuthorizeURL(redirectURI, state, verifier string, cfg Config) string {
	if strings.TrimSpace(redirectURI) == "" {
		redirectURI = RedirectURI
	}
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", cfg.clientID())
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", AuthorizeScopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge(verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	return AuthorizeURL + "?" + params.Encode()
}

type rawTokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type accountInfo struct {
	Email        string
	AccountID    string
	PlanType     string
	ExpiresAtSub time.Time
}

func decodeJWTPayload(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

func stringAt(m map[string]any, keys ...string) string {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if s, _ := cur[k].(string); s != "" {
				return strings.TrimSpace(s)
			}
			return ""
		}
		nxt, _ := cur[k].(map[string]any)
		if nxt == nil {
			return ""
		}
		cur = nxt
	}
	return ""
}

// AccountIDFromJWT extracts the ChatGPT account id from an identity or access token.
func AccountIDFromJWT(token string) (string, error) {
	claims, ok := decodeJWTPayload(strings.TrimSpace(token))
	if !ok {
		return "", errors.New("invalid identity token")
	}
	if id := stringAt(claims, "https://api.openai.com/auth", "chatgpt_account_id"); id != "" {
		return id, nil
	}
	if id := stringAt(claims, "chatgpt_account_id"); id != "" {
		return id, nil
	}
	// access tokens sometimes carry it under different shapes
	if auth, _ := claims["https://api.openai.com/auth"].(map[string]any); auth != nil {
		if id, _ := auth["account_id"].(string); strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), nil
		}
	}
	return "", errors.New("account ID was absent")
}

func infoFromTokens(idToken, accessToken string) accountInfo {
	var info accountInfo
	for _, tok := range []string{idToken, accessToken} {
		claims, ok := decodeJWTPayload(tok)
		if !ok {
			continue
		}
		if info.Email == "" {
			if e := stringAt(claims, "email"); e != "" {
				info.Email = e
			}
		}
		if info.AccountID == "" {
			if id, err := AccountIDFromJWT(tok); err == nil {
				info.AccountID = id
			}
		}
		if info.PlanType == "" {
			if p := stringAt(claims, "https://api.openai.com/auth", "chatgpt_plan_type"); p != "" {
				info.PlanType = p
			} else if p := stringAt(claims, "chatgpt_plan_type"); p != "" {
				info.PlanType = p
			} else if p := stringAt(claims, "plan"); p != "" {
				info.PlanType = p
			}
		}
	}
	return info
}

// IsPermanentRefreshFailure reports OAuth errors that will never succeed on retry.
func IsPermanentRefreshFailure(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"invalid_grant", "invalid_request", "unauthorized", "revoked", "expired", "deleted", "disabled"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func tokenRequest(ctx context.Context, client *http.Client, tokenURL string, form url.Values) (*rawTokenResp, []byte, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, body, resp.StatusCode, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr rawTokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, body, resp.StatusCode, fmt.Errorf("decode token response: %w", err)
	}
	return &tr, body, resp.StatusCode, nil
}

// ExchangeCode exchanges a PKCE authorization code for tokens.
func ExchangeCode(ctx context.Context, client *http.Client, cfg Config, code, verifier, redirectURI string) (*TokenData, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", cfg.clientID())
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	form.Set("code_verifier", strings.TrimSpace(verifier))
	if form.Get("redirect_uri") == "" {
		form.Set("redirect_uri", RedirectURI)
	}
	tr, _, _, err := tokenRequest(ctx, client, cfg.refreshURL(), form)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, errors.New("token exchange response missing access_token")
	}
	now := time.Now().UTC()
	td := &TokenData{
		AccessToken:  strings.TrimSpace(tr.AccessToken),
		RefreshToken: strings.TrimSpace(tr.RefreshToken),
		IDToken:      strings.TrimSpace(tr.IDToken),
		LastRefresh:  now.Format(time.RFC3339Nano),
	}
	exp := tr.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	td.ExpiresAt = now.Add(time.Duration(exp) * time.Second)
	if info := infoFromTokens(td.IDToken, td.AccessToken); info.AccountID != "" || info.Email != "" {
		td.AccountID = info.AccountID
		td.Email = info.Email
	}
	return td, nil
}

// Refresh exchanges the refresh token for a new access token (form-encoded, correct scopes).
func Refresh(ctx context.Context, client *http.Client, cfg Config, t TokenData) (*TokenData, error) {
	rt := strings.TrimSpace(t.RefreshToken)
	if rt == "" {
		return nil, errors.New("refresh_token is empty")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", cfg.clientID())
	form.Set("refresh_token", rt)
	form.Set("scope", RefreshScopes)
	tr, _, _, err := tokenRequest(ctx, client, cfg.refreshURL(), form)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, errors.New("refresh response missing access_token")
	}
	now := time.Now().UTC()
	out := t
	out.AccessToken = strings.TrimSpace(tr.AccessToken)
	if strings.TrimSpace(tr.RefreshToken) != "" {
		out.RefreshToken = strings.TrimSpace(tr.RefreshToken)
	}
	if strings.TrimSpace(tr.IDToken) != "" {
		out.IDToken = strings.TrimSpace(tr.IDToken)
	}
	exp := tr.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	out.ExpiresAt = now.Add(time.Duration(exp) * time.Second)
	out.LastRefresh = now.Format(time.RFC3339Nano)
	if info := infoFromTokens(out.IDToken, out.AccessToken); info.AccountID != "" {
		out.AccountID = info.AccountID
	} else if out.AccountID == "" {
		if id, err := AccountIDFromJWT(out.IDToken); err == nil {
			out.AccountID = id
		}
	}
	if info := infoFromTokens(out.IDToken, out.AccessToken); info.Email != "" && out.Email == "" {
		out.Email = info.Email
	}
	return &out, nil
}

// RefreshStale refreshes every stale token, persists the set, and reports change.
func RefreshStale(ctx context.Context, client *http.Client, cfg Config, path string) ([]TokenData, bool, error) {
	tokens, err := LoadTokens(path)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for i := range tokens {
		if strings.TrimSpace(tokens[i].RefreshToken) == "" || !IsStale(tokens[i]) {
			continue
		}
		updated, err := Refresh(ctx, client, cfg, tokens[i])
		if err != nil {
			return tokens, changed, fmt.Errorf("refresh codex token %d: %w", i, err)
		}
		tokens[i] = *updated
		changed = true
	}
	if changed {
		if err := SaveTokens(path, tokens); err != nil {
			return tokens, changed, err
		}
	}
	return tokens, changed, nil
}

// AuthenticateDevice runs the full PKCE login (Codex CLI flow) and saves credentials.
// It starts a localhost:1455 callback server, prints the browser URL, waits for
// the OAuth redirect, exchanges the code, and appends the new token to the store.
func AuthenticateDevice(ctx context.Context, client *http.Client, cfg Config, path string, output io.Writer) error {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	state, err := randomHex(32)
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}
	verifier, err := randomHex(64)
	if err != nil {
		return fmt.Errorf("generate code_verifier: %w", err)
	}
	authURL := BuildAuthorizeURL(RedirectURI, state, verifier, cfg)
	fmt.Fprintf(output, "Open this URL in your browser to sign in with ChatGPT:\n%s\n\n", authURL)
	fmt.Fprintln(output, "Waiting for approval on localhost:1455 (15 minutes)...")
	code, recvState, err := waitForOAuthCallback(ctx, RedirectURI, state)
	if err != nil {
		return err
	}
	_ = recvState
	td, err := ExchangeCode(ctx, client, cfg, code, verifier, RedirectURI)
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}
	if td.RefreshToken == "" {
		return errors.New("OAuth server did not return a refresh_token (offline_access scope required)")
	}
	existing, _ := LoadTokens(path)
	existing = append(existing, *td)
	if err := SaveTokens(path, existing); err != nil {
		return err
	}
	fmt.Fprintf(output, "Codex sign-in completed (%s). Credentials saved to %s\n", td.Email, path)
	return nil
}

func waitForOAuthCallback(ctx context.Context, redirectURI, wantState string) (code, state string, err error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", "", err
	}
	addr := u.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "80")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("listen %s for OAuth callback (is another login running?): %w", addr, err)
	}
	codeCh := make(chan string, 1)
	stateCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		c, s := q.Get("code"), q.Get("state")
		if c == "" || s == "" {
			http.Error(w, "missing code or state", http.StatusBadRequest)
			return
		}
		if s != wantState {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><body style="font-family:sans-serif;text-align:center;padding:40px"><h2>Signed in. You can close this tab.</h2></body>`))
		select {
		case codeCh <- c:
		default:
		}
		select {
		case stateCh <- s:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 15 * time.Second}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	timeout, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	select {
	case <-timeout.Done():
		return "", "", errors.New("OAuth callback timed out; run login again")
	case <-ctx.Done():
		return "", "", ctx.Err()
	case e := <-errCh:
		return "", "", e
	case c := <-codeCh:
		s := ""
		select {
		case s = <-stateCh:
		default:
		}
		return c, s, nil
	}
}

// modelsResponse mirrors the Codex backend /models payload.
// The backend switched from {"data":[{"id"}]} to
// {"models":[{"slug","visibility","priority"}]} and now requires
// ?client_version=. Both shapes are accepted; only visibility:list entries
// are kept, in catalog priority order.
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Models []struct {
		Slug       string `json:"slug"`
		ID         string `json:"id"`
		Visibility string `json:"visibility"`
		Priority   int    `json:"priority"`
	} `json:"models"`
}

// FetchModels lists models from the Codex backend ({base}/models).
func FetchModels(ctx context.Context, client *http.Client, baseURL string, cred Credential) ([]string, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	if strings.Contains(endpoint, "?") {
		endpoint += "&client_version=" + url.QueryEscape(ClientVersion)
	} else {
		endpoint += "?client_version=" + url.QueryEscape(ClientVersion)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.AccountID != "" {
		req.Header.Set("ChatGPT-Account-ID", cred.AccountID)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, resp.StatusCode, fmt.Errorf("codex models endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload modelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, resp.StatusCode, err
	}
	type ranked struct {
		id       string
		priority int
		order    int
	}
	var kept []ranked
	for i, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			kept = append(kept, ranked{id: id, order: i})
		}
	}
	for i, m := range payload.Models {
		id := strings.TrimSpace(m.Slug)
		if id == "" {
			id = strings.TrimSpace(m.ID)
		}
		if id == "" {
			continue
		}
		// Only visibility:list models are selectable; hide stays hidden.
		if strings.TrimSpace(m.Visibility) != "" && !strings.EqualFold(strings.TrimSpace(m.Visibility), "list") {
			continue
		}
		kept = append(kept, ranked{id: id, priority: m.Priority, order: i})
	}
	if len(kept) == 0 {
		return nil, resp.StatusCode, errors.New("codex models endpoint returned an empty list")
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].priority != kept[j].priority {
			return kept[i].priority < kept[j].priority
		}
		if kept[i].order != kept[j].order {
			return kept[i].order < kept[j].order
		}
		return kept[i].id < kept[j].id
	})
	models := make([]string, 0, len(kept))
	seen := map[string]bool{}
	for _, r := range kept {
		if !seen[r.id] {
			seen[r.id] = true
			models = append(models, r.id)
		}
	}
	return models, resp.StatusCode, nil
}

// NewUpstreamRequest builds the Codex backend /responses request.
func NewUpstreamRequest(ctx context.Context, baseURL string, body []byte, accountID string, accessToken string, stream bool) (*http.Request, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Originator", "codex_cli_rs")
	return req, nil
}
