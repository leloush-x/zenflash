// Package codex provides a ChatGPT-backend Responses provider. OAuth
// credentials and the per-account model cache are encrypted on disk; models
// are taken from each authorized account's own backend /models listing. Login
// uses the Codex CLI PKCE flow (static public client), shared with codex.go
// and matching codex2api.
package codex

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	State, Verifier string
	Created         time.Time
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

// StartLogin returns a Codex CLI PKCE authorization link and a state-bound
// session ID. The issuedClientID argument is accepted for API compatibility
// but ignored: this flow uses the static public CLI client, exactly like
// codex2api and the Codex CLI, so no per-registration client ID exists.
func (s *Service) StartLogin(_ string) (map[string]string, error) {
	state, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomHex(64)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	for key, pending := range s.pending {
		if time.Since(pending.Created) > 30*time.Minute {
			delete(s.pending, key)
		}
	}
	s.pending[state] = login{State: state, Verifier: verifier, Created: time.Now()}
	s.mu.Unlock()
	return map[string]string{"auth_url": BuildAuthorizeURL(RedirectURI, state, verifier, DefaultConfig()), "state": state, "redirect_uri": RedirectURI}, nil
}

// CompleteLogin accepts the full localhost callback URL because this service
// can be managed remotely and cannot receive the browser's loopback request.
func (s *Service) CompleteLogin(ctx context.Context, raw string) (AccountView, error) {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	validCallbackHost := err == nil && u.Scheme == "http" && u.Port() == "1455" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")
	if err != nil || !validCallbackHost || u.Path != "/auth/callback" || u.User != nil || u.Fragment != "" {
		return AccountView{}, errors.New("paste the full http://localhost:1455/auth/callback URL")
	}
	q := u.Query()
	state, code := q.Get("state"), q.Get("code")
	if state == "" || code == "" {
		return AccountView{}, errors.New("callback must include code and state")
	}
	s.mu.Lock()
	attempt, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	if !ok || time.Since(attempt.Created) > 30*time.Minute {
		return AccountView{}, errors.New("login session is missing or expired")
	}
	exchanged, err := ExchangeCode(ctx, s.client, DefaultConfig(), code, attempt.Verifier, RedirectURI)
	if err != nil {
		return AccountView{}, fmt.Errorf("OAuth exchange: %w", err)
	}
	if strings.TrimSpace(exchanged.RefreshToken) == "" {
		return AccountView{}, errors.New("OAuth response did not include a refresh token; confirm offline_access was granted")
	}
	id := strings.TrimSpace(exchanged.AccountID)
	if id == "" {
		id = strings.TrimSpace(exchanged.Email)
	}
	if id == "" {
		return AccountView{}, errors.New("OAuth response did not identify the ChatGPT account")
	}
	acc := account{ID: id, Email: strings.TrimSpace(exchanged.Email), ClientID: DefaultClientID, AccessToken: exchanged.AccessToken, RefreshToken: exchanged.RefreshToken, IDToken: exchanged.IDToken, ExpiresAt: exchanged.ExpiresAt}
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := false
	for i := range s.state.Accounts {
		if s.state.Accounts[i].ID == acc.ID {
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
	refreshed, err := Refresh(ctx, s.client, DefaultConfig(), TokenData{
		AccessToken:  old.AccessToken,
		RefreshToken: old.RefreshToken,
		IDToken:      old.IDToken,
		AccountID:    old.ID,
		Email:        old.Email,
		ExpiresAt:    old.ExpiresAt,
	})
	if err != nil {
		return account{}, fmt.Errorf("refresh Codex account %s: %w", old.ID, err)
	}
	old.AccessToken = refreshed.AccessToken
	old.RefreshToken = refreshed.RefreshToken
	old.IDToken = refreshed.IDToken
	old.ExpiresAt = refreshed.ExpiresAt
	old.ClientID = DefaultClientID
	if email := strings.TrimSpace(refreshed.Email); email != "" {
		old.Email = email
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Accounts {
		if s.state.Accounts[i].ID == old.ID {
			s.state.Accounts[i].AccessToken = old.AccessToken
			s.state.Accounts[i].RefreshToken = old.RefreshToken
			s.state.Accounts[i].IDToken = old.IDToken
			s.state.Accounts[i].ExpiresAt = old.ExpiresAt
			s.state.Accounts[i].ClientID = old.ClientID
			s.state.Accounts[i].Email = old.Email
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
		names, _, err := FetchModels(pollCtx, s.client, DefaultCodexBase, Credential{AccessToken: a.AccessToken, AccountID: a.ID})
		cancel()
		if err != nil {
			continue
		}
		models := make([]Model, 0, len(names))
		for _, name := range names {
			if name != "" {
				models = append(models, Model{ID: name, OwnedBy: "openai"})
			}
		}
		s.mu.Lock()
		for i := range s.state.Accounts {
			if s.state.Accounts[i].ID == a.ID {
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
	req, err := NewUpstreamRequest(ctx, DefaultCodexBase, body, strings.TrimSpace(a.ID), a.AccessToken, true)
	if err != nil {
		return nil, err
	}
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
