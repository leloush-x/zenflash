package opencode

import (
	"net/http"
	"strings"

	"zenflash-llm/internal/config"
	"zenflash-llm/internal/identity"
)

// AnonymousKey is the shared OpenCode public credential for the free lane.
// It is sent as-is and never stored per-account like OAuth tiers.
const AnonymousKey = "public"

// ClientHeader identifies zenflash requests to OpenCode upstreams, matching
// what the OpenCode CLI sends.
const ClientHeader = "x-opencode-client"

// ClientValue is the client identity reported to OpenCode upstreams.
const ClientValue = "cli"

// Session header names carrying conversation correlation upstream.
const (
	SessionHeader         = "x-opencode-session"
	SessionAffinityHeader = "x-session-affinity"
	SessionIDHeader       = "X-Session-Id"
	RequestHeader         = "x-opencode-request"
	ProjectHeader         = "x-opencode-project"
	ParentSessionHeader   = "x-parent-session-id"
)

// Credentials normalizes configured static keys: trimmed, empties dropped,
// order preserved. Config input is already trimmed at load, so this is an
// idempotent pass that keeps key-pool construction identical.
func Credentials(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(key); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// SetSessionHeaders stamps OpenCode conversation correlation headers.
// OpenCode 1.18.x sends these to preserve provider-side prompt/session
// affinity. The legacy x-opencode-session header stays so older Zen
// deployments continue to recognize the request.
func SetSessionHeaders(h http.Header, ids identity.RequestIDs) {
	h.Set(ClientHeader, ClientValue)
	h.Set(SessionHeader, ids.Session)
	h.Set(SessionAffinityHeader, ids.Session)
	h.Set(SessionIDHeader, ids.Session)
	h.Set(RequestHeader, ids.Request)
	h.Set(ProjectHeader, ids.Project)
	if ids.ParentSession != "" {
		h.Set(ParentSessionHeader, ids.ParentSession)
	}
}

// Tiers lists the tiers served by OpenCode: Zen and Go. Cline is a separate
// provider with its own module and is deliberately not listed here.
func Tiers() []config.Tier {
	return []config.Tier{config.TierZen, config.TierGo}
}

// IsOpenCodeTier reports whether a tier is served by OpenCode.
func IsOpenCodeTier(tier config.Tier) bool {
	return tier == config.TierZen || tier == config.TierGo
}

// PrefixBinding maps one namespaced ID prefix to its tier.
type PrefixBinding struct {
	Prefix string
	Tier   config.Tier
}

// Prefixes is the single prefix-to-tier table for namespaced model IDs.
// opencode/ and zen/ pin the Zen tier, cline/ and go/ pin Go, codex/ and
// antigravity/ pin their own tiers. Bare IDs keep prefer-order routing.
func Prefixes() []PrefixBinding {
	return []PrefixBinding{
		{"opencode/", config.TierZen},
		{"zen/", config.TierZen},
		{"cline/", config.TierGo},
		{"go/", config.TierGo},
		{"codex/", config.TierCodex},
		{"antigravity/", config.TierAntigravity},
	}
}

// AliasID formats a namespaced alias: opencode/ for zen, cline/ for go.
// The go-tier alias keeps its established prefix so existing pinned routes
// keep working; the mapping itself is owned here.
func AliasID(tier config.Tier, raw string) string {
	switch tier {
	case config.TierZen:
		return "opencode/" + raw
	case config.TierGo:
		return "cline/" + raw
	case config.TierCodex:
		return "codex/" + raw
	case config.TierAntigravity:
		return "antigravity/" + raw
	default:
		return raw
	}
}

// ProviderLabel renders the provider string for /v1/models entries.
// Zen lists as zen; Go lists as cline only while pointed at the embedded
// Cline proxy, otherwise go. All other tiers list under their own name.
func ProviderLabel(tier config.Tier, goUpstream string) string {
	switch tier {
	case config.TierZen:
		return "zen"
	case config.TierGo:
		if strings.Contains(goUpstream, "3457") {
			return "cline"
		}
		return "go"
	default:
		return string(tier)
	}
}
