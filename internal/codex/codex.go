// Package codex implements the Codex (ChatGPT backend) upstream auth flow:
// device-code sign-in, encrypted-at-rest token storage, refresh, and the
// model listing used for catalog discovery. It mirrors the credential
// lifecycle used by the Codex CLI and codex2api.
package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultCodexBase  = "https://chatgpt.com/backend-api/codex"
	DefaultRefreshURL = "https://auth.openai.com/oauth/token"
	DefaultClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	UserAgent         = "OpenAI/codex"
	RefreshHours      = 8
)

const authIssuer = "https://auth.openai.com"

// Config resolves how to reach the Codex backend and its OAuth issuer.
type Config struct {
	CodexBase  string
	RefreshURL string
	ClientID   string
}

// DefaultConfig pins the public Codex endpoints.
func DefaultConfig() Config {
	return Config{CodexBase: DefaultCodexBase, RefreshURL: DefaultRefreshURL, ClientID: DefaultClientID}
}

// TokenData is one account's credential set. It is persisted beside the
// service configuration so restarts keep the sign-in.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
	LastRefresh  string `json:"last_refresh"`
}

// Credential is the gateway's view of one upstream key: the current access
// token plus the optional ChatGPT account id header value.
type Credential struct {
	AccessToken string
	AccountID   string
}

// Credentials returns the union of the stored tokens (with refresh) and the
// statically configured raw keys. Static keys carry no account id.
func Credentials(stored []TokenData, staticKeys []string) []Credential {
	out := make([]Credential, 0, len(stored)+len(staticKeys))
	for _, t := range stored {
		if t.AccessToken != "" {
			out = append(out, Credential{AccessToken: t.AccessToken, AccountID: t.AccountID})
		}
	}
	for _, key := range staticKeys {
		if key != "" {
			out = append(out, Credential{AccessToken: key})
		}
	}
	return out
}

// AuthPath derives the token store location from the config path.
func AuthPath(configPath string) string {
	if configPath == "" {
		return "codex-auth.json"
	}
	return configPath + ".codex-auth.json"
}

// LoadTokens reads the persisted token set. A missing file is an empty set,
// not an error.
func LoadTokens(path string) ([]TokenData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var tokens []TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return tokens, nil
}

// SaveTokens writes the token set atomically with owner-only permissions.
func SaveTokens(path string, tokens []TokenData) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// IsStale reports whether the token is older than RefreshHours.
func IsStale(t TokenData) bool {
	last, err := time.Parse(time.RFC3339Nano, t.LastRefresh)
	if err != nil {
		return true
	}
	return time.Since(last) > RefreshHours*time.Hour
}

// Refresh exchanges the refresh token for a new access token.
func Refresh(ctx context.Context, client *http.Client, cfg Config, t TokenData) (*TokenData, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     cfg.ClientID,
		"grant_type":    "refresh_token",
		"refresh_token": t.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.RefreshURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("refresh failed %d: %s", resp.StatusCode, b)
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	newT := t
	if r.AccessToken != "" {
		newT.AccessToken = r.AccessToken
	}
	if r.RefreshToken != "" {
		newT.RefreshToken = r.RefreshToken
	}
	newT.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)
	return &newT, nil
}

// RefreshStale refreshes every stale token, persists the set, and reports
// whether anything changed.
func RefreshStale(ctx context.Context, client *http.Client, cfg Config, path string) ([]TokenData, bool, error) {
	tokens, err := LoadTokens(path)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for i := range tokens {
		if tokens[i].RefreshToken == "" || !IsStale(tokens[i]) {
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

// DeviceAuthorization is one in-progress device-code login.
type DeviceAuthorization struct {
	VerificationURL string
	UserCode        string
	DeviceAuthID    string
	Interval        time.Duration
}

// StartDeviceAuthorization begins the headless ChatGPT device-code flow.
func StartDeviceAuthorization(ctx context.Context, client *http.Client, cfg Config) (*DeviceAuthorization, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	var device struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	resp, err := deviceJSONRequest(ctx, client, "/deviceauth/usercode", map[string]string{"client_id": cfg.ClientID}, &device)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("device code login unavailable or rejected (HTTP %d); check that device-code sign-in is enabled for your account", resp.StatusCode)
	}
	defer resp.Body.Close()
	if device.UserCode == "" {
		device.UserCode = device.UserCodeAlt
	}
	if device.DeviceAuthID == "" || device.UserCode == "" {
		return nil, errors.New("device authorization response was missing required fields")
	}
	interval := 5 * time.Second
	if len(device.Interval) > 0 {
		var seconds int
		if json.Unmarshal(device.Interval, &seconds) == nil && seconds > 0 {
			interval = time.Duration(seconds) * time.Second
		} else {
			var text string
			if json.Unmarshal(device.Interval, &text) == nil {
				if _, err := fmt.Sscan(text, &seconds); err == nil && seconds > 0 {
					interval = time.Duration(seconds) * time.Second
				}
			}
		}
	}
	return &DeviceAuthorization{VerificationURL: authIssuer + "/codex/device", UserCode: device.UserCode, DeviceAuthID: device.DeviceAuthID, Interval: interval}, nil
}

// CompleteDeviceAuthorization polls the device-code grant, exchanges the code
// for tokens, and persists them.
func CompleteDeviceAuthorization(ctx context.Context, client *http.Client, cfg Config, path string, device *DeviceAuthorization) error {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()
	interval := device.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var grant struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeChallenge     string `json:"code_challenge"`
		CodeVerifier      string `json:"code_verifier"`
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("device authorization expired; run login again")
		case <-ticker.C:
		}
		pollResp, err := deviceJSONRequest(ctx, client, "/deviceauth/token", map[string]string{
			"device_auth_id": device.DeviceAuthID,
			"user_code":      device.UserCode,
		}, &grant)
		if err != nil {
			return fmt.Errorf("poll device authorization: %w", err)
		}
		switch pollResp.StatusCode {
		case http.StatusOK:
			if grant.AuthorizationCode == "" || grant.CodeVerifier == "" {
				pollResp.Body.Close()
				return errors.New("device authorization response was incomplete")
			}
		case http.StatusForbidden, http.StatusNotFound:
			pollResp.Body.Close()
			continue
		default:
			pollResp.Body.Close()
			return fmt.Errorf("device authorization failed (HTTP %d)", pollResp.StatusCode)
		}
		pollResp.Body.Close()
		break
	}
	if grant.CodeChallenge == "" {
		sum := sha256.Sum256([]byte(grant.CodeVerifier))
		grant.CodeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(grant.CodeVerifier))
	if expected := base64.RawURLEncoding.EncodeToString(sum[:]); expected != grant.CodeChallenge {
		return errors.New("device authorization returned an invalid PKCE challenge")
	}
	tokens, err := exchangeDeviceCode(ctx, client, cfg, grant.AuthorizationCode, grant.CodeVerifier)
	if err != nil {
		return err
	}
	accountID, err := AccountIDFromJWT(tokens.IDToken)
	if err != nil {
		return fmt.Errorf("read ChatGPT account from sign-in response: %w", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return errors.New("OAuth response did not include required tokens")
	}
	tokensSlice := []TokenData{{
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		AccountID: accountID, LastRefresh: time.Now().UTC().Format(time.RFC3339Nano),
	}}
	existing, _ := LoadTokens(path)
	existing = append(existing, tokensSlice...)
	return SaveTokens(path, existing)
}

func deviceJSONRequest(ctx context.Context, client *http.Client, path string, request, response any) (*http.Response, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authIssuer+"/api/accounts"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

type deviceOAuthTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func exchangeDeviceCode(ctx context.Context, client *http.Client, cfg Config, code, verifier string) (*deviceOAuthTokens, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     cfg.ClientID,
		"code":          code,
		"redirect_uri":  authIssuer + "/deviceauth/callback",
		"code_verifier": verifier,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.RefreshURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange device authorization: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("exchange device authorization failed (HTTP %d)", resp.StatusCode)
	}
	var tokens deviceOAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("decode OAuth response: %w", err)
	}
	return &tokens, nil
}

// AccountIDFromJWT extracts the ChatGPT account id from an identity token.
func AccountIDFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid identity token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid identity token payload")
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	if claims.Auth.AccountID == "" {
		return "", errors.New("account ID was absent")
	}
	return claims.Auth.AccountID, nil
}

// AuthenticateDevice runs the full device-code login and saves the result.
func AuthenticateDevice(ctx context.Context, client *http.Client, cfg Config, path string, output io.Writer) error {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	device, err := StartDeviceAuthorization(ctx, client, cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Open %s and enter this one-time code: %s\n", device.VerificationURL, device.UserCode)
	fmt.Fprintln(output, "Waiting for approval (the code expires in 15 minutes)...")
	if err := CompleteDeviceAuthorization(ctx, client, cfg, path, device); err != nil {
		return err
	}
	fmt.Fprintf(output, "Codex sign-in completed. Credentials were saved to %s\n", path)
	return nil
}

// modelsResponse mirrors the Codex backend /models payload.
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// FetchModels lists models from the Codex backend ({base}/models) using the
// Codex request headers.
func FetchModels(ctx context.Context, client *http.Client, baseURL string, cred Credential) ([]string, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, resp.StatusCode, fmt.Errorf("codex models endpoint returned HTTP %d", resp.StatusCode)
	}
	var payload modelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, resp.StatusCode, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	if len(models) == 0 {
		return nil, resp.StatusCode, errors.New("codex models endpoint returned an empty list")
	}
	return models, resp.StatusCode, nil
}

// NewUpstreamRequest builds the Codex backend /responses request with the
// Codex-specific headers. zenflash's Responses wire bodies are compatible
// with this endpoint.
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
	return req, nil
}
