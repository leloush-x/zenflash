package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zenflash-llm/internal/antigravity"
	"zenflash-llm/internal/codex"
)

// OAuthAccount is the operator-facing view of one file-backed CLI credential.
// It never exposes bearer or refresh values.
type OAuthAccount struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Email     string `json:"email,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Stale     bool   `json:"stale"`
}

// OAuthAccounts lists stored Codex + Antigravity credentials.
func (g *Gateway) OAuthAccounts() ([]OAuthAccount, error) {
	out := []OAuthAccount{}
	if g == nil {
		return out, nil
	}
	if toks, err := codex.LoadTokens(g.codexAuthPath); err == nil {
		for i, t := range toks {
			id := strings.TrimSpace(t.Email)
			if id == "" {
				id = strings.TrimSpace(t.AccountID)
			}
			if id == "" {
				id = fmt.Sprintf("codex-%d", i)
			}
			exp := ""
			if !t.ExpiresAt.IsZero() {
				exp = t.ExpiresAt.UTC().Format(time.RFC3339)
			} else if strings.TrimSpace(t.LastRefresh) != "" {
				exp = strings.TrimSpace(t.LastRefresh)
			}
			out = append(out, OAuthAccount{ID: id, Provider: "codex", Email: strings.TrimSpace(t.Email), AccountID: strings.TrimSpace(t.AccountID), ExpiresAt: exp, Stale: codex.IsStale(t)})
		}
	}
	if toks, err := antigravity.LoadTokens(g.antigravityAuthPath); err == nil {
		for i, t := range toks {
			id := strings.TrimSpace(t.Email)
			if id == "" {
				id = strings.TrimSpace(t.ProjectID)
			}
			if id == "" {
				id = fmt.Sprintf("antigravity-%d", i)
			}
			exp := ""
			if !t.ExpiresAt.IsZero() {
				exp = t.ExpiresAt.UTC().Format(time.RFC3339)
			}
			out = append(out, OAuthAccount{ID: id, Provider: "antigravity", Email: strings.TrimSpace(t.Email), ProjectID: strings.TrimSpace(t.ProjectID), ExpiresAt: exp, Stale: antigravity.IsStale(t)})
		}
	}
	return out, nil
}

func (g *Gateway) rebuildCodexPool() {
	if g == nil || g.transports == nil {
		return
	}
	nodes, err := newCodexNodePool(g.codexCredentials(), g.transports, g.cooldown())
	if err == nil && nodes != nil {
		g.codexNodes.Store(nodes)
	}
}

func (g *Gateway) rebuildAntigravityPool() {
	if g == nil || g.transports == nil {
		return
	}
	nodes, err := newCodexNodePool(g.antigravityCredentials(), g.transports, g.cooldown())
	if err == nil && nodes != nil {
		g.antigravityNodes.Store(nodes)
	}
}

func (g *Gateway) cooldown() time.Duration {
	d := time.Duration(g.cfg.Performance.FailureCooldownSeconds) * time.Second
	if d <= 0 {
		d = 15 * time.Second
	}
	return d
}

// ImportCodexRefreshToken validates a pasted ChatGPT refresh token by
// refreshing it once, then appends or replaces the stored entry.
func (g *Gateway) ImportCodexRefreshToken(ctx context.Context, refreshToken string) (OAuthAccount, error) {
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return OAuthAccount{}, fmt.Errorf("refresh_token is required")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	refreshed, err := codex.Refresh(ctx, client, codex.DefaultConfig(), codex.TokenData{RefreshToken: rt})
	if err != nil {
		return OAuthAccount{}, err
	}
	existing, _ := codex.LoadTokens(g.codexAuthPath)
	replaced := false
	for i, t := range existing {
		if strings.TrimSpace(t.AccountID) != "" && strings.TrimSpace(t.AccountID) == strings.TrimSpace(refreshed.AccountID) {
			existing[i] = *refreshed
			replaced = true
			break
		}
		if strings.TrimSpace(t.RefreshToken) == rt {
			existing[i] = *refreshed
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, *refreshed)
	}
	if err := codex.SaveTokens(g.codexAuthPath, existing); err != nil {
		return OAuthAccount{}, err
	}
	g.rebuildCodexPool()
	g.triggerCatalogRefresh()
	id := strings.TrimSpace(refreshed.Email)
	if id == "" {
		id = strings.TrimSpace(refreshed.AccountID)
	}
	return OAuthAccount{ID: id, Provider: "codex", Email: strings.TrimSpace(refreshed.Email), AccountID: strings.TrimSpace(refreshed.AccountID), ExpiresAt: refreshed.ExpiresAt.UTC().Format(time.RFC3339)}, nil
}

// ImportAntigravityRefreshToken validates a pasted Google refresh token by
// refreshing and syncing profile/project, then stores it.
func (g *Gateway) ImportAntigravityRefreshToken(ctx context.Context, refreshToken string) (OAuthAccount, error) {
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return OAuthAccount{}, fmt.Errorf("refresh_token is required")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	base := antigravity.TokenData{RefreshToken: rt}
	refreshed, err := antigravity.Refresh(ctx, client, base)
	if err != nil {
		return OAuthAccount{}, err
	}
	synced, _, syncErr := antigravity.Sync(ctx, client, *refreshed)
	if syncErr != nil {
		// Keep the bearer so a later refresh can retry project sync.
		synced = *refreshed
	}
	existing, _ := antigravity.LoadTokens(g.antigravityAuthPath)
	replaced := false
	for i, t := range existing {
		if strings.TrimSpace(t.ProjectID) != "" && strings.TrimSpace(t.ProjectID) == strings.TrimSpace(synced.ProjectID) && strings.TrimSpace(synced.ProjectID) != "" {
			existing[i] = synced
			replaced = true
			break
		}
		if strings.TrimSpace(t.RefreshToken) == rt {
			existing[i] = synced
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, synced)
	}
	if err := antigravity.SaveTokens(g.antigravityAuthPath, existing); err != nil {
		return OAuthAccount{}, err
	}
	g.rebuildAntigravityPool()
	g.triggerCatalogRefresh()
	id := strings.TrimSpace(synced.Email)
	if id == "" {
		id = strings.TrimSpace(synced.ProjectID)
	}
	warn := ""
	if syncErr != nil {
		warn = syncErr.Error()
		_ = warn
	}
	return OAuthAccount{ID: id, Provider: "antigravity", Email: strings.TrimSpace(synced.Email), ProjectID: strings.TrimSpace(synced.ProjectID), ExpiresAt: synced.ExpiresAt.UTC().Format(time.RFC3339)}, syncErr
}

// DeleteOAuthAccount removes one stored credential by email, account/project id, or list id.
func (g *Gateway) DeleteOAuthAccount(provider, id string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	match := func(candidates ...string) bool {
		for _, c := range candidates {
			if strings.TrimSpace(c) != "" && strings.EqualFold(strings.TrimSpace(c), id) {
				return true
			}
		}
		return false
	}
	switch provider {
	case "codex":
		existing, _ := codex.LoadTokens(g.codexAuthPath)
		kept := make([]codex.TokenData, 0, len(existing))
		removed := false
		for i, t := range existing {
			if !removed && (match(t.Email, t.AccountID, fmt.Sprintf("codex-%d", i))) {
				removed = true
				continue
			}
			kept = append(kept, t)
		}
		if !removed {
			return fmt.Errorf("codex account %q not found", id)
		}
		if err := codex.SaveTokens(g.codexAuthPath, kept); err != nil {
			return err
		}
		g.rebuildCodexPool()
		g.triggerCatalogRefresh()
		return nil
	case "antigravity":
		existing, _ := antigravity.LoadTokens(g.antigravityAuthPath)
		kept := make([]antigravity.TokenData, 0, len(existing))
		removed := false
		for i, t := range existing {
			if !removed && (match(t.Email, t.ProjectID, fmt.Sprintf("antigravity-%d", i))) {
				removed = true
				continue
			}
			kept = append(kept, t)
		}
		if !removed {
			return fmt.Errorf("antigravity account %q not found", id)
		}
		if err := antigravity.SaveTokens(g.antigravityAuthPath, kept); err != nil {
			return err
		}
		g.rebuildAntigravityPool()
		g.triggerCatalogRefresh()
		return nil
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
}

// RefreshOAuthAccounts forces an immediate stale-token refresh for both providers.
func (g *Gateway) RefreshOAuthAccounts(ctx context.Context) {
	g.refreshCodexTokens(ctx)
	g.refreshAntigravityTokens(ctx)
}

// triggerCatalogRefresh refreshes /v1/models in background so newly linked
// or removed accounts adapt immediately instead of waiting for the tick.
func (g *Gateway) triggerCatalogRefresh() {
	if g == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		g.RefreshModelsNow(ctx)
	}()
}
