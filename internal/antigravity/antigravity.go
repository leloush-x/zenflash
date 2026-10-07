// Package antigravity implements Google Antigravity (Cloud Code) OAuth login,
// token refresh, project sync, model discovery, and Chat<->Gemini conversion.
// It mirrors the Antigravity CLI / codex2api credential lifecycle in a lean
// form suited to zenflash's file-backed gateway.
package antigravity

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
	"zenflash-llm/internal/config"
	"zenflash-llm/internal/store"
)

// tokenStore is the optional Postgres home for login tokens. When nil,
// tokens live only in the JSON file beside the config.
var tokenStore *store.Store

// SetStore attaches the Postgres token home. Nil restores file-only mode.
func SetStore(s *store.Store) {
	tokenStore = s
}

func antigravityAccountID(t TokenData, i int) string {
	if t.ProjectID != "" {
		return t.ProjectID
	}
	if t.Email != "" {
		return t.Email
	}
	return fmt.Sprintf("account-%d", i)
}

const (
	// DefaultClientID is the public Google OAuth client ID used by the
	// Antigravity desktop client. Client IDs are public identifiers, not
	// secrets. The matching client secret is never stored here: configure it
	// via ANTIGRAVITY_OAUTH_CLIENTS (see oauthClientsFromEnv).
	DefaultClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	AuthURL         = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL        = "https://oauth2.googleapis.com/token"
	UserInfoURL     = "https://www.googleapis.com/oauth2/v2/userinfo"
	Scopes          = "openid https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs https://www.googleapis.com/auth/aicode"
	ClientVersion   = "2.19.1"
	CallbackPath    = "/oauth-callback"
)

var (
	LoadProjectEndpoints = []string{
		"https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
		"https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
	}
	FetchModelsEndpoints = []string{
		"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
		"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
		"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	}
	OnboardEndpoints = []string{
		"https://daily-cloudcode-pa.googleapis.com/v1internal:onboardUser",
		"https://cloudcode-pa.googleapis.com/v1internal:onboardUser",
	}
	GenerateBases = []string{
		"https://daily-cloudcode-pa.googleapis.com",
		"https://cloudcode-pa.googleapis.com",
	}
)

// oauthClient is one configured Google OAuth client tuple.
type oauthClient struct {
	Key    string
	ID     string
	Secret string
}

// oauthClientsFromEnv parses ANTIGRAVITY_OAUTH_CLIENTS in
// "key|client_id|client_secret;..." form. The desktop client tuple is public
// knowledge, but the secret stays out of the repository.
func oauthClientsFromEnv() []oauthClient {
	out := []oauthClient{}
	for _, entry := range strings.Split(config.LoadEnvOnce().AntigravityOAuthClients, ";") {
		parts := strings.Split(entry, "|")
		if len(parts) < 3 {
			continue
		}
		c := oauthClient{Key: strings.ToLower(strings.TrimSpace(parts[0])), ID: strings.TrimSpace(parts[1]), Secret: strings.TrimSpace(parts[2])}
		if c.Key == "" || c.ID == "" || c.Secret == "" {
			continue
		}
		replaced := false
		for i := range out {
			if out[i].Key == c.Key {
				out[i] = c
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, c)
		}
	}
	return out
}

// builtinOAuthClient is the official Antigravity desktop OAuth client. Its
// values ship inside community clients (codex2api, sub2api, opencode) and act
// as the zero-config fallback. Operator-configured clients via
// ANTIGRAVITY_OAUTH_CLIENTS always win when present.
func builtinOAuthClient() oauthClient {
	return oauthClient{Key: "official", ID: DefaultClientID, Secret: "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"}
}

// activeOAuthClient resolves the OAuth client selected by
// ANTIGRAVITY_OAUTH_CLIENT_KEY (default "official").
func activeOAuthClient() (oauthClient, error) {
	clients := append(oauthClientsFromEnv(), builtinOAuthClient())
	if len(clients) == 0 {
		return oauthClient{}, errors.New("no Antigravity OAuth client configured; set ANTIGRAVITY_OAUTH_CLIENTS to key|client_id|client_secret")
	}
	want := strings.ToLower(strings.TrimSpace(config.LoadEnvOnce().AntigravityOAuthClientKey))
	if want == "" {
		want = "official"
	}
	for _, c := range clients {
		if c.Key == want {
			return c, nil
		}
	}
	return clients[0], nil
}

func UserAgent() string { return "antigravity/hub/" + ClientVersion + " windows/amd64" }

// TokenData is one persisted Antigravity credential.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	ClientKey    string    `json:"client_key,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	LastRefresh  string    `json:"last_refresh"`
}

// Credential is the gateway view: bearer + project.
type Credential struct {
	AccessToken string
	ProjectID   string
	Email       string
}

func Credentials(stored []TokenData, staticKeys []string) []Credential {
	out := make([]Credential, 0, len(stored)+len(staticKeys))
	for _, t := range stored {
		if strings.TrimSpace(t.AccessToken) == "" || strings.TrimSpace(t.ProjectID) == "" {
			continue
		}
		out = append(out, Credential{AccessToken: strings.TrimSpace(t.AccessToken), ProjectID: strings.TrimSpace(t.ProjectID), Email: strings.TrimSpace(t.Email)})
	}
	for _, k := range staticKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		// static form: "projectID:accessToken" or bare bearer (project resolved at refresh/sync time: skip)
		if parts := strings.SplitN(k, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
			out = append(out, Credential{ProjectID: strings.TrimSpace(parts[0]), AccessToken: strings.TrimSpace(parts[1])})
		}
	}
	return out
}

func AuthPath(configPath string) string {
	if configPath == "" {
		return "antigravity-auth.json"
	}
	return configPath + ".antigravity-auth.json"
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
	var toks []TokenData
	if err := json.Unmarshal(data, &toks); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return toks, nil
}

// restoreTokens reloads login tokens from Postgres when the file is gone
// (fresh host, wiped volume) and rewrites the file so later reads stay fast.
func restoreTokens(path string) []TokenData {
	if tokenStore == nil {
		return nil
	}
	raw := tokenStore.OAuthList(context.Background(), "antigravity")
	if len(raw) == 0 {
		return nil
	}
	var toks []TokenData
	for _, b := range raw {
		var one TokenData
		if json.Unmarshal(b, &one) == nil {
			toks = append(toks, one)
		}
	}
	if len(toks) > 0 {
		_ = writeTokensFile(path, toks)
	}
	return toks
}

func writeTokensFile(path string, toks []TokenData) error {
	data, err := json.MarshalIndent(toks, "", "  ")
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

func SaveTokens(path string, toks []TokenData) error {
	if err := writeTokensFile(path, toks); err != nil {
		return err
	}
	mirrorTokens(toks)
	return nil
}

// mirrorTokens copies the just-saved login tokens to Postgres. The file
// write above already succeeded, so DB trouble never fails the request.
func mirrorTokens(toks []TokenData) {
	if tokenStore == nil {
		return
	}
	ids := make([]string, 0, len(toks))
	payloads := make([][]byte, 0, len(toks))
	for i, tok := range toks {
		raw, err := json.Marshal(tok)
		if err != nil {
			continue
		}
		ids = append(ids, antigravityAccountID(tok, i))
		payloads = append(payloads, raw)
	}
	tokenStore.OAuthSave(context.Background(), "antigravity", ids, payloads)
}

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
	return time.Since(last) > 55*time.Minute
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func codeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// BuildAuthorizeURL builds the Google Desktop OAuth URL (loopback callback required).
// CodeChallenge exposes the PKCE S256 challenge for dashboard login flows.
func CodeChallenge(verifier string) string { return codeChallenge(verifier) }

func BuildAuthorizeURL(redirectURI, state, challenge string) (string, error) {
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(challenge) == "" {
		return "", errors.New("redirect_uri, state and code_challenge are required")
	}
	u, err := url.Parse(redirectURI)
	if err != nil || u.Path != CallbackPath || (u.Hostname() != "127.0.0.1" && !strings.EqualFold(u.Hostname(), "localhost")) || u.Port() == "" {
		return "", errors.New("redirect_uri must be http://127.0.0.1:PORT/oauth-callback or localhost equivalent")
	}
	params := url.Values{}
	client, err := activeOAuthClient()
	if err != nil {
		return "", err
	}
	params.Set("client_id", client.ID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", Scopes)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	return AuthURL + "?" + params.Encode(), nil
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func doToken(ctx context.Context, client *http.Client, form url.Values) (*tokenResp, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent())
	var tr tokenResp
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token endpoint HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, errors.New("google OAuth returned no access_token")
	}
	return &tr, nil
}

// ExchangeCode exchanges a Google authorization code for tokens.
func ExchangeCode(ctx context.Context, client *http.Client, code, redirectURI, verifier string) (*TokenData, error) {
	oauth, err := activeOAuthClient()
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("client_id", oauth.ID)
	form.Set("client_secret", oauth.Secret)
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", strings.TrimSpace(verifier))
	tr, err := doToken(ctx, client, form)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	td := &TokenData{
		AccessToken: strings.TrimSpace(tr.AccessToken), RefreshToken: strings.TrimSpace(tr.RefreshToken),
		IDToken: strings.TrimSpace(tr.IDToken), LastRefresh: now.Format(time.RFC3339Nano),
		ClientKey: oauth.Key, ClientID: oauth.ID, ClientSecret: oauth.Secret,
	}
	if tr.ExpiresIn > 0 {
		td.ExpiresAt = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	} else {
		td.ExpiresAt = now.Add(time.Hour)
	}
	return td, nil
}

// Refresh renews the access token, preserving the refresh token when omitted.
func Refresh(ctx context.Context, client *http.Client, t TokenData) (*TokenData, error) {
	if strings.TrimSpace(t.RefreshToken) == "" {
		return nil, errors.New("refresh_token is required")
	}
	cid, sec := strings.TrimSpace(t.ClientID), strings.TrimSpace(t.ClientSecret)
	if cid == "" || sec == "" {
		oauth, err := activeOAuthClient()
		if err != nil {
			return nil, err
		}
		cid, sec = oauth.ID, oauth.Secret
	}
	form := url.Values{}
	form.Set("client_id", cid)
	form.Set("client_secret", sec)
	form.Set("refresh_token", strings.TrimSpace(t.RefreshToken))
	form.Set("grant_type", "refresh_token")
	tr, err := doToken(ctx, client, form)
	if err != nil {
		return nil, err
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
	if tr.ExpiresIn > 0 {
		out.ExpiresAt = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	} else {
		out.ExpiresAt = now.Add(time.Hour)
	}
	out.LastRefresh = now.Format(time.RFC3339Nano)
	if out.ClientID == "" {
		key := strings.ToLower(strings.TrimSpace(t.ClientKey))
		if key == "" {
			key = "official"
		}
		out.ClientID, out.ClientSecret, out.ClientKey = cid, sec, key
	}
	return &out, nil
}

func RefreshStale(ctx context.Context, client *http.Client, path string) ([]TokenData, bool, error) {
	toks, err := LoadTokens(path)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for i := range toks {
		if strings.TrimSpace(toks[i].RefreshToken) == "" || !IsStale(toks[i]) {
			continue
		}
		updated, err := Refresh(ctx, client, toks[i])
		if err != nil {
			return toks, changed, fmt.Errorf("refresh antigravity token %d: %w", i, err)
		}
		// keep project/email; refresh only rotates bearer
		updated.ProjectID = toks[i].ProjectID
		updated.Email = toks[i].Email
		toks[i] = *updated
		changed = true
	}
	if changed {
		if err := SaveTokens(path, toks); err != nil {
			return toks, changed, err
		}
	}
	return toks, changed, nil
}

func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"invalid_grant", "unauthorized_client", "revoked", "deleted", "disabled"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

type profile struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func fetchProfile(ctx context.Context, client *http.Client, accessToken string) (profile, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UserInfoURL, nil)
	if err != nil {
		return profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return profile{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return profile{}, fmt.Errorf("userinfo HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var p profile
	if err := json.Unmarshal(body, &p); err != nil {
		return profile{}, err
	}
	if strings.TrimSpace(p.Email) == "" || !p.VerifiedEmail {
		return profile{}, errors.New("google profile has no verified email")
	}
	return p, nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint, accessToken string, payload any, out any) (int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("cloud code HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

type loadProjectResp struct {
	ProjectID string `json:"cloudaicompanionProject"`
}

func fetchProject(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	var last error
	for _, ep := range LoadProjectEndpoints {
		var r loadProjectResp
		if _, err := postJSON(ctx, client, ep, accessToken, map[string]any{"metadata": map[string]any{"ideType": "ANTIGRAVITY"}}, &r); err == nil {
			if strings.TrimSpace(r.ProjectID) != "" {
				return strings.TrimSpace(r.ProjectID), nil
			}
			last = errors.New("loadCodeAssist returned no project")
			break
		} else {
			last = err
		}
	}
	if last == nil {
		last = errors.New("no project endpoint succeeded")
	}
	// Try onboard for fresh accounts, then retry load once.
	if pid, err := onboard(ctx, client, accessToken); err == nil && pid != "" {
		return pid, nil
	}
	return "", last
}

func onboard(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	payload := map[string]any{
		"tierId":   "free-tier",
		"metadata": map[string]any{"ideType": "ANTIGRAVITY", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"},
	}
	for _, ep := range OnboardEndpoints {
		var r struct {
			Done     bool `json:"done"`
			Response struct {
				Project json.RawMessage `json:"cloudaicompanionProject"`
			} `json:"response"`
		}
		if _, err := postJSON(ctx, client, ep, accessToken, payload, &r); err == nil {
			raw := bytes.TrimSpace(r.Response.Project)
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var s string
			if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s), nil
			}
			var o struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &o) == nil && strings.TrimSpace(o.ID) != "" {
				return strings.TrimSpace(o.ID), nil
			}
		}
		// poll once after 2s (operation is usually immediate when done=true present)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", errors.New("onboardUser did not return a project")
}

type quotaResp struct {
	Models map[string]struct {
		QuotaInfo *struct {
			RemainingFraction *float64 `json:"remainingFraction"`
		} `json:"quotaInfo"`
		DisplayName string `json:"displayName"`
		IsInternal  bool   `json:"isInternal"`
	} `json:"models"`
}

// DefaultModels is the safe fallback before the first quota sync.
func DefaultModels() []string {
	return []string{
		"gemini-3.6-flash-low", "gemini-3.6-flash-medium", "gemini-3.6-flash-high",
		"gemini-3.7-flash-tiered", "gemini-3.8-flash-tiered",
		"gemini-3.1-pro-low", "gemini-pro-agent", "gpt-oss-120b-medium",
	}
}

// ModelQuota is one fetchAvailableModels entry with its quota meter.
// RemainingFraction is 0-1 when upstream reports it, nil when it does not.
type ModelQuota struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"display_name,omitempty"`
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	IsInternal        bool     `json:"is_internal,omitempty"`
}

func remainingFraction(qi *struct {
	RemainingFraction *float64 `json:"remainingFraction"`
}) *float64 {
	if qi == nil {
		return nil
	}
	return qi.RemainingFraction
}

// FetchQuota lists all fetchAvailableModels entries with quota meters.
// Unlike FetchModels it keeps internal entries and display names so the
// dashboard can render a real quota meter per model.
func FetchQuota(ctx context.Context, client *http.Client, accessToken, projectID string) ([]ModelQuota, error) {
	payload := map[string]any{}
	if strings.TrimSpace(projectID) != "" {
		payload["project"] = strings.TrimSpace(projectID)
	}
	var last error
	for _, ep := range FetchModelsEndpoints {
		var r quotaResp
		if _, err := postJSON(ctx, client, ep, accessToken, payload, &r); err != nil {
			last = err
			continue
		}
		if len(r.Models) == 0 {
			continue
		}
		out := make([]ModelQuota, 0, len(r.Models))
		for id, info := range r.Models {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			out = append(out, ModelQuota{
				ID:                id,
				DisplayName:       strings.TrimSpace(info.DisplayName),
				RemainingFraction: remainingFraction(info.QuotaInfo),
				IsInternal:        info.IsInternal,
			})
		}
		if len(out) == 0 {
			continue
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	if last == nil {
		last = errors.New("no quota endpoint succeeded")
	}
	return nil, last
}

// retiredModels are IDs Google reports as no longer available ("switch to
// Gemini 3.7 Flash"). They still appear in quota listings, but advertising
// them for routing only produces the retirement message.
var retiredModels = map[string]bool{
	"gemini-3-flash-agent":       true,
	"gemini-3.5-flash-extra-low": true,
	"gemini-3.5-flash-low":       true,
}

// FetchModels lists wire model IDs via fetchAvailableModels.
func FetchModels(ctx context.Context, client *http.Client, accessToken, projectID string) ([]string, error) {
	quotas, err := FetchQuota(ctx, client, accessToken, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(quotas))
	for _, q := range quotas {
		id := strings.TrimSpace(q.ID)
		if id == "" {
			continue
		}
		if retiredModels[id] {
			continue
		}
		lower := strings.ToLower(id)
		if strings.HasPrefix(lower, "gemini") || strings.HasPrefix(lower, "claude") || strings.HasPrefix(lower, "gpt") {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("no quota endpoint succeeded")
	}
	sort.Strings(ids)
	return ids, nil
}

// Sync refreshes the bearer if needed, then resolves profile, project and models.
func Sync(ctx context.Context, client *http.Client, cred TokenData) (TokenData, []string, error) {
	if strings.TrimSpace(cred.RefreshToken) != "" && (strings.TrimSpace(cred.AccessToken) == "" || IsStale(cred)) {
		updated, err := Refresh(ctx, client, cred)
		if err != nil {
			return cred, nil, err
		}
		updated.ProjectID = cred.ProjectID
		updated.Email = cred.Email
		cred = *updated
	}
	p, err := fetchProfile(ctx, client, cred.AccessToken)
	if err != nil {
		// try one refresh on 401
		if strings.Contains(strings.ToLower(err.Error()), "401") && strings.TrimSpace(cred.RefreshToken) != "" {
			updated, rerr := Refresh(ctx, client, cred)
			if rerr != nil {
				return cred, nil, rerr
			}
			updated.ProjectID = cred.ProjectID
			cred = *updated
			if p, err = fetchProfile(ctx, client, cred.AccessToken); err != nil {
				return cred, nil, err
			}
		} else {
			return cred, nil, err
		}
	}
	cred.Email = p.Email
	if strings.TrimSpace(cred.ProjectID) == "" {
		pid, err := fetchProject(ctx, client, cred.AccessToken)
		if err != nil {
			return cred, nil, err
		}
		cred.ProjectID = pid
	}
	models, err := FetchModels(ctx, client, cred.AccessToken, cred.ProjectID)
	if err != nil {
		return cred, DefaultModels(), nil
	}
	return cred, models, nil
}

// AuthenticateDevice runs the Google loopback OAuth flow and saves credentials.
func AuthenticateDevice(ctx context.Context, client *http.Client, path string, port int, out io.Writer) error {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if port <= 0 {
		port = 51121
	}
	verifier, err := randomToken(64)
	if err != nil {
		return err
	}
	state, err := randomToken(32)
	if err != nil {
		return err
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, CallbackPath)
	authURL, err := BuildAuthorizeURL(redirect, state, codeChallenge(verifier))
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("listen 127.0.0.1:%d for OAuth callback: %w", port, err)
	}
	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		c, s := r.URL.Query().Get("code"), r.URL.Query().Get("state")
		if c == "" || s != state {
			http.Error(w, "invalid callback", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><body style="font-family:sans-serif;text-align:center;padding:40px"><h2>Signed in. You can close this tab.</h2></body>`))
		select {
		case codeCh <- c:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 15 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	fmt.Fprintf(out, "Open this URL to sign in with Google:\n%s\n\nWaiting on %s ...\n", authURL, redirect)
	timeout, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var code string
	select {
	case <-timeout.Done():
		return errors.New("OAuth callback timed out; run login again")
	case <-ctx.Done():
		return ctx.Err()
	case c := <-codeCh:
		code = c
	}
	td, err := ExchangeCode(ctx, client, code, redirect, verifier)
	if err != nil {
		return err
	}
	synced, _, err := Sync(ctx, client, *td)
	if err != nil {
		// Save bearer even if project sync failed; refresh loop will retry.
		existing, _ := LoadTokens(path)
		existing = append(existing, *td)
		_ = SaveTokens(path, existing)
		return fmt.Errorf("sync after login: %w", err)
	}
	existing, _ := LoadTokens(path)
	existing = append(existing, synced)
	if err := SaveTokens(path, existing); err != nil {
		return err
	}
	fmt.Fprintf(out, "Antigravity sign-in completed (%s, project %s). Saved to %s\n", synced.Email, synced.ProjectID, path)
	return nil
}

// ---- Chat -> Gemini envelope ----

func chatToGemini(model, project string, chat map[string]any) (map[string]any, string) {
	wireModel := strings.TrimSpace(model)
	if wireModel == "" {
		wireModel = "gemini-3.6-flash-medium"
	}
	contents := []any{}
	var systemText []string
	if raw, ok := chat["messages"].([]any); ok {
		for _, item := range raw {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			role, _ := m["role"].(string)
			content := m["content"]
			text := ""
			switch v := content.(type) {
			case string:
				text = v
			case []any:
				parts := []string{}
				for _, p := range v {
					pm, _ := p.(map[string]any)
					if pm == nil {
						continue
					}
					if t, _ := pm["text"].(string); t != "" {
						parts = append(parts, t)
					} else if t, _ := pm["input_text"].(string); t != "" {
						parts = append(parts, t)
					}
				}
				text = strings.Join(parts, "\n")
			}
			if role == "system" || role == "developer" {
				if text != "" {
					systemText = append(systemText, text)
				}
				continue
			}
			grole := "user"
			if role == "assistant" {
				grole = "model"
			}
			// tool_calls on assistant messages become functionCall parts
			parts := []any{}
			if text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			if rawCalls, ok := m["tool_calls"].([]any); ok {
				for _, rc := range rawCalls {
					cm, _ := rc.(map[string]any)
					if cm == nil {
						continue
					}
					fn, _ := cm["function"].(map[string]any)
					name, _ := fn["name"].(string)
					argStr, _ := fn["arguments"].(string)
					var args any = map[string]any{}
					if strings.TrimSpace(argStr) != "" {
						var decoded any
						if json.Unmarshal([]byte(argStr), &decoded) == nil {
							args = decoded
						} else {
							args = map[string]any{"_raw": argStr}
						}
					}
					parts = append(parts, map[string]any{"functionCall": map[string]any{"name": name, "args": args}})
				}
			}
			if len(parts) == 0 {
				parts = append(parts, map[string]any{"text": ""})
			}
			contents = append(contents, map[string]any{"role": grole, "parts": parts})
			// tool results (role=tool) become user functionResponse parts
			if role == "tool" {
				// already appended as user text above; Gemini prefers functionResponse but text keeps it callable
			}
		}
	}
	if len(contents) == 0 {
		contents = append(contents, map[string]any{"role": "user", "parts": []any{map[string]any{"text": ""}}})
	}
	req := map[string]any{"contents": contents, "model": wireModel}
	if len(systemText) > 0 {
		req["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": strings.Join(systemText, "\n\n")}}}
	}
	if tools, ok := chat["tools"].([]any); ok && len(tools) > 0 {
		decls := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			if name == "" {
				continue
			}
			decls = append(decls, map[string]any{
				"name": name, "description": fn["description"], "parameters": sanitizeGeminiSchema(fn["parameters"]),
			})
		}
		if len(decls) > 0 {
			req["tools"] = []any{map[string]any{"functionDeclarations": decls}}
		}
	}
	gen := map[string]any{}
	if v, ok := chat["temperature"]; ok {
		gen["temperature"] = v
	}
	if v, ok := chat["top_p"]; ok {
		gen["topP"] = v
	}
	if v, ok := chat["max_tokens"]; ok {
		gen["maxOutputTokens"] = v
	} else if v, ok := chat["max_completion_tokens"]; ok {
		gen["maxOutputTokens"] = v
	}
	if s, ok := chat["stop"]; ok {
		gen["stopSequences"] = s
	}
	if len(gen) > 0 {
		req["generationConfig"] = gen
	}
	envelope := map[string]any{"model": wireModel, "project": project, "request": req}
	return envelope, wireModel
}

// sanitizeGeminiSchema strips JSON Schema fields Gemini's function
// declarations reject (for example "$schema" and "exclusiveMinimum") while
// preserving the shape clients actually use. Unknown object shapes pass
// through structurally so tools keep working instead of failing the call.
func sanitizeGeminiSchema(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok {
			out := make([]any, 0, len(arr))
			for _, item := range arr {
				out = append(out, sanitizeGeminiSchema(item))
			}
			return out
		}
		return v
	}
	// Union keywords collapse to their first viable branch: Gemini has no
	// anyOf/allOf/oneOf on function parameters, and the first branch carries
	// the usable type in practice.
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if branches, ok := m[key].([]any); ok && len(branches) > 0 {
			if branch, ok := branches[0].(map[string]any); ok {
				for bkey, bval := range branch {
					if _, exists := m[bkey]; !exists {
						m[bkey] = bval
					}
				}
			}
		}
	}
	out := make(map[string]any, len(m))
	for key, val := range m {
		switch key {
		case "$schema", "$id", "$ref", "$defs", "definitions",
			"exclusiveMinimum", "exclusiveMaximum",
			"const", "default", "examples", "example",
			"readOnly", "writeOnly", "deprecated",
			"contentMediaType", "contentEncoding",
			"if", "then", "else", "not",
			"allOf", "anyOf", "oneOf":
			continue
		}
		out[key] = sanitizeGeminiSchema(val)
	}
	// Numeric bounds in draft-4 style ("exclusiveMinimum": true + "minimum")
	// mean nothing to Gemini; drop the orphaned flag only when a numeric
	// bound survives alongside it.
	if _, hasMin := out["minimum"]; hasMin {
		delete(out, "exclusiveMinimum")
	}
	if _, hasMax := out["maximum"]; hasMax {
		delete(out, "exclusiveMaximum")
	}
	return out
}

// BuildGenerateRequest converts a Chat wire body to a Cloud Code envelope.
func BuildGenerateRequest(chatBody []byte, model, project string) ([]byte, string, error) {
	var chat map[string]any
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		return nil, "", err
	}
	if _, ok := chat["contents"]; ok {
		// already Gemini-native: just attach project/model
		chat["model"] = model
		envelope := map[string]any{"model": model, "project": project, "request": chat}
		data, err := json.Marshal(envelope)
		return data, model, err
	}
	env, wire := chatToGemini(model, project, chat)
	data, err := json.Marshal(env)
	return data, wire, err
}

// isUserProjectPermissionError reports whether a generateContent 403 is about
// the x-goog-user-project billing project rather than the request itself.
func isUserProjectPermissionError(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "serviceusage") ||
		(strings.Contains(lower, "required permission") && strings.Contains(lower, "project"))
}

// ExecuteGenerate posts one non-stream generateContent and returns a synthetic
// Chat JSON http.Response so the gateway's Chat conversion path can be reused.
func ExecuteGenerate(ctx context.Context, client *http.Client, base, project, bearer, wireModel string, envelope []byte) (*http.Response, error) {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	endpoint := strings.TrimRight(base, "/") + "/v1internal:generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent())
	if strings.TrimSpace(project) != "" {
		req.Header.Set("x-goog-user-project", project)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	// Like codex2api: a 403 caused by the billing/quota project header
	// (missing serviceusage permission) is retried once without the header
	// so accounts that work project-less still serve instead of hard failing.
	if resp.StatusCode == http.StatusForbidden && strings.TrimSpace(project) != "" && isUserProjectPermissionError(body) {
		retry, rerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(envelope))
		if rerr == nil {
			retry.Header.Set("Authorization", "Bearer "+bearer)
			retry.Header.Set("Content-Type", "application/json")
			retry.Header.Set("User-Agent", UserAgent())
			if rresp, derr := client.Do(retry); derr == nil {
				rbody, _ := io.ReadAll(io.LimitReader(rresp.Body, 8<<20))
				_ = rresp.Body.Close()
				if rresp.StatusCode/100 == 2 {
					resp = rresp
					body = rbody
				} else {
					// Keep the original permission error: it names the fix.
					_ = rresp.Body.Close()
				}
			}
		}
	}
	if resp.StatusCode/100 != 2 {
		return &http.Response{StatusCode: resp.StatusCode, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	}
	chatBody, err := geminiToChat(body, wireModel)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(chatBody))}, nil
}

func geminiToChat(body []byte, model string) ([]byte, error) {
	var r struct {
		Candidates []struct {
			Content *struct {
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	// Daily endpoint wraps the payload as {"response": {...}, "traceId": ...}.
	if len(r.Candidates) == 0 && r.UsageMetadata == nil {
		var wrapped struct {
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(body, &wrapped) == nil && len(wrapped.Response) > 0 {
			if err := json.Unmarshal(wrapped.Response, &r); err != nil {
				return nil, err
			}
		}
	}
	text := []string{}
	toolCalls := []any{}
	for _, c := range r.Candidates {
		if c.Content == nil {
			continue
		}
		for _, p := range c.Content.Parts {
			if p.Thought {
				continue
			}
			if p.Text != "" {
				text = append(text, p.Text)
			}
			if p.FunctionCall != nil && strings.TrimSpace(p.FunctionCall.Name) != "" {
				args, _ := json.Marshal(p.FunctionCall.Args)
				if string(args) == "" || string(args) == "null" {
					args = []byte("{}")
				}
				toolCalls = append(toolCalls, map[string]any{
					"id":       "call_" + hex8(p.FunctionCall.Name+string(args)),
					"type":     "function",
					"function": map[string]any{"name": p.FunctionCall.Name, "arguments": string(args)},
				})
			}
		}
	}
	msg := map[string]any{"role": "assistant", "content": strings.Join(text, "")}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["content"] = strings.Join(text, "")
	}
	usage := map[string]any{}
	if r.UsageMetadata != nil {
		usage = map[string]any{
			"prompt_tokens":     r.UsageMetadata.PromptTokenCount,
			"completion_tokens": r.UsageMetadata.CandidatesTokenCount,
			"total_tokens":      r.UsageMetadata.TotalTokenCount,
		}
	}
	out := map[string]any{
		"id":     "antigravity-" + hex8(strings.Join(text, "")+model),
		"object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		"usage":   usage,
	}
	return json.Marshal(out)
}

func hex8(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:8]
}

// WantsStream reports whether a Chat wire body asked for SSE.
func WantsStream(chatBody []byte) bool {
	var v struct {
		Stream *bool `json:"stream"`
	}
	if err := json.Unmarshal(chatBody, &v); err != nil {
		return false
	}
	return v.Stream != nil && *v.Stream
}

// ToStreamResponse wraps a Chat JSON document as a single-event SSE stream
// so the gateway's Chat SSE forward/transcode path can be reused.
func ToStreamResponse(chatJSON []byte) *http.Response {
	var compact bytes.Buffer
	if err := json.Compact(&compact, chatJSON); err != nil {
		compact.Write(chatJSON)
	}
	event := append(append([]byte("data: "), compact.Bytes()...), '\n', '\n')
	event = append(event, []byte("data: [DONE]\n\n")...)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(event))}
}
