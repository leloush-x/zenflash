package models

import (
	"strings"

	"zenflash-llm/internal/config"
	"zenflash-llm/internal/opencode"
)

// Tier prefixes for explicit routing. Bare IDs keep today's prefer-order
// behavior; prefixed IDs pin one tier. opencode/ == zen, cline/ == go.
func SplitTierPrefix(id string) (raw string, tier config.Tier, ok bool) {
	id = strings.TrimSpace(id)
	for _, p := range opencode.Prefixes() {
		if strings.HasPrefix(id, p.Prefix) && len(id) > len(p.Prefix) {
			return strings.TrimSpace(id[len(p.Prefix):]), p.Tier, true
		}
	}
	return id, "", false
}

// TiersForModel reports which tiers advertise a raw model ID.
func (c *Catalog) TiersForModel(raw string) []config.Tier {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []config.Tier
	if c.zen[raw] {
		out = append(out, config.TierZen)
	}
	if c.goModels[raw] {
		out = append(out, config.TierGo)
	}
	if c.codexModels[raw] {
		out = append(out, config.TierCodex)
	}
	if c.antigravityModels[raw] {
		out = append(out, config.TierAntigravity)
	}
	return out
}

// SupportedNamespaced accepts bare IDs (today's behavior) and tier-prefixed
// IDs when the pinned tier advertises the raw model.
func (c *Catalog) SupportedNamespaced(id string) bool {
	raw, tier, ok := SplitTierPrefix(id)
	if !ok {
		return c.Supported(id)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch tier {
	case config.TierZen:
		return c.tierSupportedLocked(raw, tier) && (c.zen[raw] || c.pendingLocked())
	case config.TierGo:
		return c.tierSupportedLocked(raw, tier) && (c.goModels[raw] || c.pendingLocked())
	case config.TierCodex:
		return c.tierSupportedLocked(raw, tier) && (c.codexModels[raw] || c.pendingLocked())
	case config.TierAntigravity:
		return c.tierSupportedLocked(raw, tier) && (c.antigravityModels[raw] || c.pendingLocked())
	default:
		return false
	}
}

func (c *Catalog) pendingLocked() bool {
	return len(c.zen) == 0 && len(c.goModels) == 0 && len(c.codexModels) == 0 && len(c.antigravityModels) == 0
}

// RoutePinned resolves a tier-prefixed ID to one tier. Authenticated pins use
// RouteForTier semantics; a zen pin may use the anonymous lane when the model
// is anonymous-eligible and no zen key exists.
func (c *Catalog) RoutePinned(raw string, tier config.Tier, hasZenKeys, hasGoKeys, hasCodexKeys, hasAntigravityKeys, hasAnonymous bool) (Route, error) {
	hasKeys := hasZenKeys
	switch tier {
	case config.TierGo:
		hasKeys = hasGoKeys
	case config.TierCodex:
		hasKeys = hasCodexKeys
	case config.TierAntigravity:
		hasKeys = hasAntigravityKeys
	}
	if hasKeys {
		return c.RouteForTierWithAntigravity(raw, tier, hasZenKeys, hasGoKeys, hasCodexKeys, hasAntigravityKeys)
	}
	if hasAnonymous && tier == config.TierZen {
		route, err := c.RouteWithAntigravity(raw, hasZenKeys, hasGoKeys, hasCodexKeys, hasAntigravityKeys, true)
		if err == nil && route.Anonymous {
			return route, nil
		}
	}
	return c.RouteForTierWithAntigravity(raw, tier, hasZenKeys, hasGoKeys, hasCodexKeys, hasAntigravityKeys)
}
