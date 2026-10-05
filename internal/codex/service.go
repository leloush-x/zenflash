// Package codex provides an experimental Sign in with ChatGPT Responses
// provider. OAuth credentials and the per-account model cache are encrypted
// on disk; models are taken from each authorized account's own /v1/models.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wire "zenflash-llm/internal/protocol"
)

const (
	authorizeURL = "https://auth.openai.com/oauth/authorize"
	tokenURL     = "https://auth.openai.com/api/accounts/oauth/token"
	modelsURL    = "https://api.openai.com/v1/models"
	responsesURL = "https://api.openai.com/v1/responses"
	resource     = "https://api.openai.com/v1"
	redirectURI  = "http://127.0.0.1:1455/auth/callback"
	scopes       = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	OwnedBy     string `json:"owned_by"`
}
type account struct {
	ID           string       `json:"id"`
	Email        string       `json:"email,omitempty"`
	ClientID     string       `json:"client_id"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	IDToken      string       `json:"id_token"`
	ExpiresAt    time.Time    `json:"expires_at,omitempty"`
	Models       []Model      `json:"models,omitempty"`
	ModelsAt     time.Time    `json:"models_at,omitempty"`
	Requests30d  []time.Time  `json:"requests_30d,omitempty"`
	Usage30d     []usageEvent `json:"usage_30d,omitempty"`
}
type usageEvent struct {
	At     time.Time `json:"at"`
	Input  uint64    `json:"input_tokens"`
	Output uint64    `json:"output_tokens"`
}
type diskState struct {
	Accounts []account `json:"accounts"`
	HostID   string    `json:"host_id"`
}
type login struct {
	State, Nonce, Verifier string
	Created                time.Time
}
type AccountView struct {
	ID              string    `json:"id"`
	Email           string    `json:"email,omitempty"`
	Models          int       `json:"models"`
	ModelUpdated    time.Time `json:"models_updated,omitempty"`
	Requests30d     int       `json:"requests_30d"`
	InputTokens30d  uint64    `json:"input_tokens_30d"`
	OutputTokens30d uint64    `json:"output_tokens_30d"`
	Remaining       string    `json:"openai_remaining"`
}

func registrationID(a account) string {
	sum := sha256.Sum256([]byte(a.ID + "\x00" + a.ClientID))
	return hex.EncodeToString(sum[:8])
}

type Service struct {
	path    string
	key     []byte
	mu      sync.Mutex
	state   diskState
	pending map[string]login
	next    atomic.Uint64
	client  *http.Client
}

func New(path string) (*Service, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	keyPath := path + ".key"
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err == nil {
			err = os.WriteFile(keyPath, key, 0600)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid Codex store key")
	}
	_ = os.Chmod(keyPath, 0600)
	s := &Service{path: path, key: key, pending: map[string]login{}, client: &http.Client{Timeout: 10 * time.Minute}}
	if err := s.load(); err != nil {
		return nil, err
	}
	if s.state.HostID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.state.HostID = "urn:uuid:" + fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(b) < g.NonceSize() {
		return errors.New("invalid encrypted Codex store")
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
	if err != nil {
		return fmt.Errorf("decrypt Codex store: %w", err)
	}
	return json.Unmarshal(plain, &s.state)
}
func (s *Service) saveLocked() error {
	plain, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	b := g.Seal(nonce, nonce, plain, nil)
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".codex-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// StartLogin returns an OpenAI authorization link and a state-bound session ID.
func (s *Service) StartLogin() (map[string]string, error) {
	state, err := random(32)
	if err != nil {
		return nil, err
	}
	nonce, err := random(32)
	if err != nil {
		return nil, err
	}
	verifier, err := random(64)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	for key, pending := range s.pending {
		if time.Since(pending.Created) > 30*time.Minute {
			delete(s.pending, key)
		}
	}
	s.pending[state] = login{State: state, Nonce: nonce, Verifier: verifier, Created: time.Now()}
	s.mu.Unlock()
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "dynamic_agent_client")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", scopes)
	q.Set("resource", resource)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("ext_agent_host_id", s.state.HostID)
	q.Set("agent_name_hint", "ZenFlash")
	return map[string]string{"auth_url": authorizeURL + "?" + q.Encode(), "state": state, "redirect_uri": redirectURI}, nil
}

// CompleteLogin accepts the full localhost callback URL because this service
// can be managed remotely and cannot receive the browser's loopback request.
func (s *Service) CompleteLogin(ctx context.Context, raw string) (AccountView, error) {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	validCallbackHost := err == nil && u.Scheme == "http" && u.Port() == "1455" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")
	if err != nil || !validCallbackHost || u.Path != "/auth/callback" || u.User != nil || u.Fragment != "" {
		return AccountView{}, errors.New("paste the full http://127.0.0.1:1455/auth/callback URL")
	}
	q := u.Query()
	state, code, clientID := q.Get("state"), q.Get("code"), q.Get("client_id")
	if state == "" || code == "" || clientID == "" {
		return AccountView{}, errors.New("callback must include code, state, and issued client_id")
	}
	s.mu.Lock()
	attempt, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	if !ok || time.Since(attempt.Created) > 30*time.Minute {
		return AccountView{}, errors.New("login session is missing or expired")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", attempt.Verifier)
	form.Set("resource", resource)
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		ID      string `json:"id_token"`
		Scope   string `json:"scope"`
		Expires int64  `json:"expires_in"`
	}
	if err := s.postForm(ctx, tokenURL, form, &tok); err != nil {
		return AccountView{}, fmt.Errorf("OAuth exchange: %w", err)
	}
	if tok.Access == "" || tok.Refresh == "" || tok.ID == "" {
		return AccountView{}, errors.New("OAuth response did not include required tokens")
	}
	claims, err := s.validateIDToken(ctx, tok.ID, clientID, attempt.Nonce)
	if err != nil {
		return AccountView{}, err
	}
	if !hasScope(tok.Scope, "chatgpt.tokens.use.direct") || !hasScope(tok.Scope, "resource.invoke") {
		return AccountView{}, errors.New("ChatGPT plan usage was not granted; authorize both Responses API scopes")
	}
	acc := account{ID: claims.Subject, Email: claims.Email, ClientID: clientID, AccessToken: tok.Access, RefreshToken: tok.Refresh, IDToken: tok.ID, ExpiresAt: time.Now().Add(time.Duration(tok.Expires) * time.Second)}
	if acc.ID == "" {
		return AccountView{}, errors.New("validated identity token did not contain a subject")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := false
	for i := range s.state.Accounts {
		if s.state.Accounts[i].ID == acc.ID && s.state.Accounts[i].ClientID == clientID {
			acc.Models = s.state.Accounts[i].Models
			acc.ModelsAt = s.state.Accounts[i].ModelsAt
			acc.Requests30d = s.state.Accounts[i].Requests30d
			acc.Usage30d = s.state.Accounts[i].Usage30d
			s.state.Accounts[i] = acc
			updated = true
			break
		}
	}
	if !updated {
		s.state.Accounts = append(s.state.Accounts, acc)
	}
	if err := s.saveLocked(); err != nil {
		return AccountView{}, err
	}
	go func() {
		pollCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.RefreshModels(pollCtx)
	}()
	return view(acc), nil
}

func hasScope(raw, want string) bool {
	for _, v := range strings.Fields(raw) {
		if v == want {
			return true
		}
	}
	return false
}
func (s *Service) postForm(ctx context.Context, endpoint string, form url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.Unmarshal(b, dst)
}

type tokenClaims struct {
	Issuer   string `json:"iss"`
	Audience any    `json:"aud"`
	Subject  string `json:"sub"`
	Nonce    string `json:"nonce"`
	Email    string `json:"email"`
	Exp      int64  `json:"exp"`
}

func (s *Service) validateIDToken(ctx context.Context, token, audience, nonce string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("invalid OpenAI identity token")
	}
	h, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return tokenClaims{}, err
	}
	var header struct{ Alg, Kid string }
	if json.Unmarshal(h, &header) != nil || header.Alg != "RS256" {
		return tokenClaims{}, errors.New("unsupported identity-token signature")
	}
	key, err := s.jwk(ctx, header.Kid)
	if err != nil {
		return tokenClaims{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tokenClaims{}, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err = rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return tokenClaims{}, errors.New("OpenAI identity-token signature validation failed")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var c tokenClaims
	if err = json.Unmarshal(body, &c); err != nil {
		return c, err
	}
	if c.Issuer != "https://auth.openai.com" || !audienceMatch(c.Audience, audience) || c.Nonce != nonce || c.Subject == "" || time.Now().Unix() >= c.Exp {
		return c, errors.New("OpenAI identity token claims did not validate")
	}
	return c, nil
}

func audienceMatch(raw any, want string) bool {
	switch v := raw.(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if x == want {
				return true
			}
		}
	}
	return false
}
func (s *Service) jwk(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://auth.openai.com/.well-known/jwks.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("JWKS returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	for _, k := range raw.Keys {
		if k.Kid != kid || k.Kty != "RSA" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil || len(e) > 4 {
			return nil, errors.New("invalid OpenAI JWKS exponent")
		}
		exp := 0
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}, nil
	}
	return nil, errors.New("OpenAI signing key was not found")
}

func (s *Service) accountTokens(ctx context.Context, model string) (*account, error) {
	s.mu.Lock()
	now := time.Now()
	candidates := make([]account, 0, len(s.state.Accounts))
	for i := range s.state.Accounts {
		a := &s.state.Accounts[i]
		kept := a.Requests30d[:0]
		for _, at := range a.Requests30d {
			if at.After(now.Add(-30 * 24 * time.Hour)) {
				kept = append(kept, at)
			}
		}
		a.Requests30d = kept
		if modelAvailable(*a, model) {
			candidates = append(candidates, *a)
		}
	}
	if len(candidates) == 0 {
		s.mu.Unlock()
		return nil, errors.New("no linked account currently lists this model")
	}
	start := int(s.next.Add(1)-1) % len(candidates)
	s.mu.Unlock()
	for offset := 0; offset < len(candidates); offset++ {
		a := candidates[(start+offset)%len(candidates)]
		if !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(now.Add(time.Minute)) {
			refreshed, err := s.refreshAccount(ctx, a)
			if err != nil {
				continue
			}
			a = refreshed
		}
		return &a, nil
	}
	return nil, errors.New("no linked account has a valid access token for this model")
}

func (s *Service) refreshAccount(ctx context.Context, old account) (account, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", old.ClientID)
	form.Set("refresh_token", old.RefreshToken)
	form.Set("resource", resource)
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
	}
	if err := s.postForm(ctx, tokenURL, form, &tok); err != nil {
		return account{}, fmt.Errorf("refresh Codex account %s: %w", old.ID, err)
	}
	if tok.Access == "" {
		return account{}, errors.New("OpenAI refresh did not return an access token")
	}
	old.AccessToken = tok.Access
	if tok.Refresh != "" {
		old.RefreshToken = tok.Refresh
	}
	if tok.Expires > 0 {
		old.ExpiresAt = time.Now().Add(time.Duration(tok.Expires) * time.Second)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Accounts {
		if s.state.Accounts[i].ID == old.ID && s.state.Accounts[i].ClientID == old.ClientID {
			s.state.Accounts[i].AccessToken = old.AccessToken
			s.state.Accounts[i].RefreshToken = old.RefreshToken
			s.state.Accounts[i].ExpiresAt = old.ExpiresAt
			if err := s.saveLocked(); err != nil {
				return account{}, err
			}
			return old, nil
		}
	}
	return account{}, errors.New("Codex account was removed during refresh")
}
func modelAvailable(a account, id string) bool {
	for _, m := range a.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// RefreshModels polls every linked account; the published visibility field is
// authoritative for the account's model picker. Failed polls retain last-good data.
func (s *Service) RefreshModels(ctx context.Context) error {
	s.mu.Lock()
	accounts := append([]account(nil), s.state.Accounts...)
	s.mu.Unlock()
	if len(accounts) == 0 {
		return errors.New("no linked Codex accounts")
	}
	changed := false
	for _, a := range accounts {
		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(time.Now().Add(time.Minute)) {
			if refreshed, refreshErr := s.refreshAccount(pollCtx, a); refreshErr == nil {
				a = refreshed
			} else {
				cancel()
				continue
			}
		}
		req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, modelsURL, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Authorization", "Bearer "+a.AccessToken)
		resp, err := s.client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		var payload struct {
			Models []struct {
				Slug       string `json:"slug"`
				Name       string `json:"display_name"`
				Visibility string `json:"visibility"`
			} `json:"models"`
		}
		if resp.StatusCode/100 == 2 {
			err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload)
		} else {
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		resp.Body.Close()
		cancel()
		if err != nil {
			continue
		}
		models := make([]Model, 0, len(payload.Models))
		for _, m := range payload.Models {
			if m.Slug != "" && m.Visibility == "list" {
				models = append(models, Model{ID: m.Slug, DisplayName: m.Name, OwnedBy: "openai"})
			}
		}
		s.mu.Lock()
		for i := range s.state.Accounts {
			if s.state.Accounts[i].ID == a.ID && s.state.Accounts[i].ClientID == a.ClientID {
				s.state.Accounts[i].Models = models
				s.state.Accounts[i].ModelsAt = time.Now().UTC()
				changed = true
				break
			}
		}
		s.mu.Unlock()
	}
	if changed {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.saveLocked()
	}
	return errors.New("all account model polls failed; retaining last-good catalogs")
}

func (s *Service) Models() []Model {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := map[string]Model{}
	for _, a := range s.state.Accounts {
		for _, m := range a.Models {
			byID[m.ID] = m
		}
	}
	out := make([]Model, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func view(a account) AccountView {
	now := time.Now().Add(-30 * 24 * time.Hour)
	requests := 0
	for _, at := range a.Requests30d {
		if at.After(now) {
			requests++
		}
	}
	var input, output uint64
	for _, ev := range a.Usage30d {
		if ev.At.After(now) {
			input += ev.Input
			output += ev.Output
		}
	}
	return AccountView{ID: registrationID(a), Email: a.Email, Models: len(a.Models), ModelUpdated: a.ModelsAt, Requests30d: requests, InputTokens30d: input, OutputTokens30d: output, Remaining: "not exposed by OpenAI API"}
}
func (s *Service) Accounts() []AccountView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountView, 0, len(s.state.Accounts))
	for _, a := range s.state.Accounts {
		out = append(out, view(a))
	}
	return out
}
func (s *Service) DeleteAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.state.Accounts {
		if registrationID(a) == id {
			s.state.Accounts = append(s.state.Accounts[:i], s.state.Accounts[i+1:]...)
			return s.saveLocked()
		}
	}
	return os.ErrNotExist
}

func (s *Service) Start(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 10 * time.Minute
	}
	go func() {
		_ = s.RefreshModels(ctx)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.RefreshModels(ctx)
			}
		}
	}()
}

// ProxyResponses enforces the signed-in account's model catalog, forces the
// no-storage streaming contract, and relays the public Responses API stream.
func (s *Service) ProxyResponses(w http.ResponseWriter, r *http.Request, model string, payload map[string]any, external wire.Protocol, clientStream bool) {
	upstreamCtx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	payload["model"], payload["stream"], payload["store"] = model, true, false
	body, err := json.Marshal(payload)
	if err != nil {
		wire.WriteError(w, external, http.StatusBadRequest, "failed to encode request", "invalid_request_error", "")
		return
	}
	var a *account
	var resp *http.Response
	count := s.ModelAccountCount(model)
	if count < 1 {
		count = 1
	}
	for attempt := 0; attempt < count; attempt++ {
		a, err = s.accountTokens(upstreamCtx, model)
		if err != nil {
			wire.WriteError(w, external, http.StatusServiceUnavailable, err.Error(), "upstream_error", "")
			return
		}
		resp, err = s.sendResponse(upstreamCtx, body, *a)
		if err != nil {
			wire.WriteError(w, external, http.StatusBadGateway, "OpenAI Responses request failed", "upstream_error", "")
			return
		}
		if resp.StatusCode == http.StatusUnauthorized {
			_ = resp.Body.Close()
			if refreshed, refreshErr := s.refreshAccount(upstreamCtx, *a); refreshErr == nil {
				a = &refreshed
				resp, err = s.sendResponse(upstreamCtx, body, *a)
				if err != nil {
					wire.WriteError(w, external, http.StatusBadGateway, "OpenAI Responses request failed", "upstream_error", "")
					return
				}
			}
		}
		if (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusUnauthorized) || attempt+1 >= count {
			break
		}
		_ = resp.Body.Close()
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if external != wire.Responses {
			if converted, convErr := wire.ConvertResponse(wire.Responses, external, responseBody); convErr == nil {
				responseBody = converted
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(responseBody)
		return
	}
	if !clientStream {
		streamBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			wire.WriteError(w, external, http.StatusBadGateway, "failed to read streamed response", "upstream_error", "")
			return
		}
		completed, input, output := completedResponse(streamBody)
		if len(completed) == 0 {
			wire.WriteError(w, external, http.StatusBadGateway, "OpenAI stream ended without response.completed", "upstream_error", "")
			return
		}
		if external != wire.Responses {
			completed, err = wire.ConvertResponse(wire.Responses, external, completed)
			if err != nil {
				wire.WriteError(w, external, http.StatusBadGateway, "failed to translate Codex response", "upstream_error", "")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(completed)
		s.recordUsage(registrationID(*a), input, output)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	if external != wire.Responses {
		usage, _, streamErr := wire.TranscodeStream(r.Context(), w, resp.Body, wire.Responses, external, model)
		if streamErr == nil {
			s.recordUsage(registrationID(*a), uint64(max(0, usage.Input)), uint64(max(0, usage.Output)))
		} else {
			s.recordUsage(registrationID(*a), 0, 0)
		}
		return
	}
	var input, output uint64
	completed := false
	reader := bufio.NewReaderSize(resp.Body, 32<<10)
	for {
		line, readErr := reader.ReadString('\n')
		if _, err := io.WriteString(w, line); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if strings.HasPrefix(line, "data: ") {
			var event struct {
				Type     string `json:"type"`
				Response struct {
					Usage struct {
						Input  uint64 `json:"input_tokens"`
						Output uint64 `json:"output_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event) == nil && event.Type == "response.completed" {
				input, output, completed = event.Response.Usage.Input, event.Response.Usage.Output, true
			}
		}
		if readErr != nil {
			break
		}
	}
	if completed {
		s.recordUsage(registrationID(*a), input, output)
	} else {
		s.recordUsage(registrationID(*a), 0, 0)
	}
}

func (s *Service) sendResponse(ctx context.Context, body []byte, a account) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responsesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "zenflash-codex-experimental/1.0")
	return s.client.Do(req)
}
func (s *Service) ModelAccountCount(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, a := range s.state.Accounts {
		if modelAvailable(a, model) {
			n++
		}
	}
	return n
}

func completedResponse(body []byte) ([]byte, uint64, uint64) {
	var result json.RawMessage
	var in, out uint64
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event) != nil || event.Type != "response.completed" {
			continue
		}
		result = event.Response
		var usage struct {
			Usage struct {
				Input  uint64 `json:"input_tokens"`
				Output uint64 `json:"output_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(result, &usage)
		in, out = usage.Usage.Input, usage.Usage.Output
	}
	return result, in, out
}

func (s *Service) recordUsage(accountID string, input, output uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for i := range s.state.Accounts {
		a := &s.state.Accounts[i]
		if registrationID(*a) != accountID {
			continue
		}
		keptRequests := a.Requests30d[:0]
		for _, t := range a.Requests30d {
			if t.After(cutoff) {
				keptRequests = append(keptRequests, t)
			}
		}
		a.Requests30d = append(keptRequests, time.Now().UTC())
		keptUsage := a.Usage30d[:0]
		for _, event := range a.Usage30d {
			if event.At.After(cutoff) {
				keptUsage = append(keptUsage, event)
			}
		}
		a.Usage30d = append(keptUsage, usageEvent{At: time.Now().UTC(), Input: input, Output: output})
		_ = s.saveLocked()
		return
	}
}

func (s *Service) AuthorizeLocalKey(keys []string, r *http.Request) bool {
	if len(keys) == 0 {
		return true
	}
	candidates := []string{strings.TrimSpace(r.Header.Get("x-api-key"))}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		candidates = append(candidates, strings.TrimSpace(h[7:]))
	}
	for _, k := range keys {
		for _, c := range candidates {
			if len(c) == len(k) && subtle.ConstantTimeCompare([]byte(c), []byte(k)) == 1 {
				return true
			}
		}
	}
	return false
}
func ParseModelList(raw []byte) ([]string, error) {
	var v struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return strings.Fields(v.Model), nil
}
