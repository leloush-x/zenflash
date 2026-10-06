package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"zenflash-llm/internal/antigravity"
	"zenflash-llm/internal/codex"
)

// browserLoginSession binds one browser sign-in to its PKCE verifier and the
// exact redirect URI the code must be exchanged with. Codes are pasted back
// from the loopback URL (remote-friendly) or captured by the public
// /oauth-callback endpoint when a custom client registered it.
type browserLoginSession struct {
	provider  string
	state     string
	verifier  string
	redirect  string
	createdAt time.Time
}

var (
	browserLoginMu       sync.Mutex
	browserLoginSessions = make(map[string]*browserLoginSession)
)

const browserLoginTTL = 15 * time.Minute

func randomLoginToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func browserLoginSet(id string, sess *browserLoginSession) {
	browserLoginMu.Lock()
	defer browserLoginMu.Unlock()
	now := time.Now()
	for key, existing := range browserLoginSessions {
		if now.Sub(existing.createdAt) > browserLoginTTL {
			delete(browserLoginSessions, key)
		}
	}
	browserLoginSessions[id] = sess
}

func browserLoginTake(id string) (*browserLoginSession, bool) {
	browserLoginMu.Lock()
	defer browserLoginMu.Unlock()
	sess, ok := browserLoginSessions[id]
	if !ok {
		return nil, false
	}
	delete(browserLoginSessions, id)
	if time.Since(sess.createdAt) > browserLoginTTL {
		return nil, false
	}
	return sess, true
}

func browserLoginTakeByState(state string) (string, *browserLoginSession, bool) {
	browserLoginMu.Lock()
	defer browserLoginMu.Unlock()
	for id, sess := range browserLoginSessions {
		if sess.state == state {
			delete(browserLoginSessions, id)
			if time.Since(sess.createdAt) > browserLoginTTL {
				return "", nil, false
			}
			return id, sess, true
		}
	}
	return "", nil, false
}

// BrowserLoginStart builds a sign-in link for the dashboard. redirectURI is
// optional: empty selects the official loopback callback for that provider.
// A custom https redirect is honored for Antigravity (custom Google client);
// Codex always uses the registered localhost:1455 callback.
func (g *Gateway) BrowserLoginStart(provider, redirectURI string) (sessionID, authURL string, err error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	redirectURI = strings.TrimSpace(redirectURI)
	switch provider {
	case "codex":
		redirectURI = codex.RedirectURI
	case "antigravity":
		if redirectURI == "" {
			redirectURI = "http://127.0.0.1:51121" + antigravity.CallbackPath
		} else if u, perr := url.Parse(redirectURI); perr != nil || (u.Hostname() != "127.0.0.1" && !strings.EqualFold(u.Hostname(), "localhost") && strings.ToLower(u.Scheme) != "https") {
			return "", "", fmt.Errorf("redirect must be a loopback URL or a custom https URL")
		}
	default:
		return "", "", fmt.Errorf("unknown provider %q", provider)
	}
	verifier, err := randomLoginToken(64)
	if err != nil {
		return "", "", err
	}
	state, err := randomLoginToken(32)
	if err != nil {
		return "", "", err
	}
	session, err := randomLoginToken(16)
	if err != nil {
		return "", "", err
	}
	switch provider {
	case "codex":
		authURL = codex.BuildAuthorizeURL(redirectURI, state, verifier, codex.DefaultConfig())
	case "antigravity":
		challenge := antigravity.CodeChallenge(verifier)
		authURL, err = antigravity.BuildAuthorizeURL(redirectURI, state, challenge)
		if err != nil {
			return "", "", err
		}
	}
	browserLoginSet(session, &browserLoginSession{provider: provider, state: state, verifier: verifier, redirect: redirectURI, createdAt: time.Now()})
	return session, authURL, nil
}

// BrowserLoginComplete exchanges a pasted loopback callback URL (or a raw
// code) for stored credentials.
func (g *Gateway) BrowserLoginComplete(ctx context.Context, sessionID, callbackURL string) (OAuthAccount, error) {
	sess, ok := browserLoginTake(strings.TrimSpace(sessionID))
	if !ok {
		return OAuthAccount{}, fmt.Errorf("sign-in session expired; start again")
	}
	code, state := splitCallback(strings.TrimSpace(callbackURL))
	if code == "" {
		return OAuthAccount{}, fmt.Errorf("no authorization code found in that URL")
	}
	if state == "" || state != sess.state {
		return OAuthAccount{}, fmt.Errorf("sign-in state mismatch; start again")
	}
	return g.finishBrowserLogin(ctx, sess, code)
}

// BrowserLoginCompleteByState serves the public /oauth-callback endpoint:
// the code arrives directly when the provider redirected to this gateway.
func (g *Gateway) BrowserLoginCompleteByState(ctx context.Context, code, state string) (OAuthAccount, error) {
	_, sess, ok := browserLoginTakeByState(strings.TrimSpace(state))
	if !ok {
		return OAuthAccount{}, fmt.Errorf("sign-in session expired; start again")
	}
	if strings.TrimSpace(code) == "" {
		return OAuthAccount{}, fmt.Errorf("sign-in callback is missing its code")
	}
	return g.finishBrowserLogin(ctx, sess, strings.TrimSpace(code))
}

func (g *Gateway) finishBrowserLogin(ctx context.Context, sess *browserLoginSession, code string) (OAuthAccount, error) {
	switch sess.provider {
	case "codex":
		td, err := codex.ExchangeCode(ctx, nil, codex.DefaultConfig(), code, sess.verifier, sess.redirect)
		if err != nil {
			return OAuthAccount{}, err
		}
		if td.RefreshToken == "" {
			return OAuthAccount{}, fmt.Errorf("provider did not return a refresh token")
		}
		existing, _ := codex.LoadTokens(g.codexAuthPath)
		replaced := false
		for i, t := range existing {
			if strings.TrimSpace(t.AccountID) != "" && strings.TrimSpace(t.AccountID) == strings.TrimSpace(td.AccountID) {
				existing[i] = *td
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, *td)
		}
		if err := codex.SaveTokens(g.codexAuthPath, existing); err != nil {
			return OAuthAccount{}, err
		}
		g.rebuildCodexPool()
		id := strings.TrimSpace(td.Email)
		if id == "" {
			id = strings.TrimSpace(td.AccountID)
		}
		exp := ""
		if !td.ExpiresAt.IsZero() {
			exp = td.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return OAuthAccount{ID: id, Provider: "codex", Email: strings.TrimSpace(td.Email), AccountID: strings.TrimSpace(td.AccountID), ExpiresAt: exp}, nil
	case "antigravity":
		td, err := antigravity.ExchangeCode(ctx, nil, code, sess.redirect, sess.verifier)
		if err != nil {
			return OAuthAccount{}, err
		}
		synced, _, syncErr := antigravity.Sync(ctx, nil, *td)
		if syncErr == nil {
			td = &synced
		}
		existing, _ := antigravity.LoadTokens(g.antigravityAuthPath)
		replaced := false
		for i, t := range existing {
			if strings.TrimSpace(t.ProjectID) != "" && strings.TrimSpace(t.ProjectID) == strings.TrimSpace(td.ProjectID) && strings.TrimSpace(td.ProjectID) != "" {
				existing[i] = *td
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, *td)
		}
		if err := antigravity.SaveTokens(g.antigravityAuthPath, existing); err != nil {
			return OAuthAccount{}, err
		}
		g.rebuildAntigravityPool()
		id := strings.TrimSpace(td.Email)
		if id == "" {
			id = strings.TrimSpace(td.ProjectID)
		}
		exp := ""
		if !td.ExpiresAt.IsZero() {
			exp = td.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return OAuthAccount{ID: id, Provider: "antigravity", Email: strings.TrimSpace(td.Email), ProjectID: strings.TrimSpace(td.ProjectID), ExpiresAt: exp}, syncErr
	default:
		return OAuthAccount{}, fmt.Errorf("unknown provider %q", sess.provider)
	}
}

// splitCallback pulls code/state out of a pasted callback URL or a bare code.
func splitCallback(input string) (code, state string) {
	if input == "" || !strings.Contains(input, "://") {
		return strings.TrimSpace(input), ""
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", ""
	}
	return u.Query().Get("code"), u.Query().Get("state")
}
