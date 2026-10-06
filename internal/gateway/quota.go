package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"zenflash-llm/internal/antigravity"
)

// AntigravityModelQuota is one model quota meter for the dashboard.
type AntigravityModelQuota struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"display_name,omitempty"`
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	IsInternal        bool     `json:"is_internal,omitempty"`
}

// AntigravityAccountQuota is the live quota for one stored account.
type AntigravityAccountQuota struct {
	Email       string                  `json:"email,omitempty"`
	ProjectID   string                  `json:"project_id,omitempty"`
	Models      []AntigravityModelQuota `json:"models"`
	FetchedAt   string                  `json:"fetched_at"`
	Error       string                  `json:"error,omitempty"`
	Total       int                     `json:"total"`
	FullQuota   int                     `json:"full_quota"`
	Partial     int                     `json:"partial"`
	Exhausted   int                     `json:"exhausted"`
	Unknown     int                     `json:"unknown"`
}

// AntigravityQuota queries fetchAvailableModels for every stored account plus
// static projectID:token keys. It never returns tokens, only meters.
func (g *Gateway) AntigravityQuota(ctx context.Context) []AntigravityAccountQuota {
	if g == nil {
		return []AntigravityAccountQuota{}
	}
	stored, _ := antigravity.LoadTokens(g.antigravityAuthPath)
	type cred struct {
		email   string
		token   string
		project string
	}
	var creds []cred
	for _, t := range stored {
		if strings.TrimSpace(t.AccessToken) == "" {
			continue
		}
		creds = append(creds, cred{email: strings.TrimSpace(t.Email), token: strings.TrimSpace(t.AccessToken), project: strings.TrimSpace(t.ProjectID)})
	}
	for _, k := range g.cfg.AntigravityKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if parts := strings.SplitN(k, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
			creds = append(creds, cred{project: strings.TrimSpace(parts[0]), token: strings.TrimSpace(parts[1])})
		}
	}
	out := make([]AntigravityAccountQuota, 0, len(creds))
	for _, c := range creds {
		qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		// Use a healthy proxy client when available so host blocks don't break quota.
		client := &http.Client{Timeout: 30 * time.Second}
		if g.transports != nil {
			if proxies := g.transports.items; len(proxies) > 0 {
				for _, p := range proxies {
					if p != nil && p.client != nil && p.healthy.Load() {
						client = p.client
						break
					}
				}
				if client.Timeout == 0 {
					client = proxies[0].client
				}
			}
		}
		quotas, err := antigravity.FetchQuota(qctx, client, c.token, c.project)
		cancel()
		entry := AntigravityAccountQuota{
			Email:     c.email,
			ProjectID: c.project,
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err != nil {
			entry.Error = err.Error()
			out = append(out, entry)
			continue
		}
		for _, q := range quotas {
			entry.Models = append(entry.Models, AntigravityModelQuota{
				ID:                q.ID,
				DisplayName:       q.DisplayName,
				RemainingFraction: q.RemainingFraction,
				IsInternal:        q.IsInternal,
			})
			if q.RemainingFraction == nil {
				entry.Unknown++
			} else if *q.RemainingFraction >= 0.999 {
				entry.FullQuota++
			} else if *q.RemainingFraction <= 0.001 {
				entry.Exhausted++
			} else {
				entry.Partial++
			}
		}
		entry.Total = len(entry.Models)
		out = append(out, entry)
	}
	if out == nil {
		return []AntigravityAccountQuota{}
	}
	return out
}
