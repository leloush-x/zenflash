package opencode

import (
	"net/http"
	"strings"

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
