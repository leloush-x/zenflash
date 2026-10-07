package models

import (
	"testing"

	"zenflash-llm/internal/config"
	"zenflash-llm/internal/opencode"
)

func TestClinePrefixPinsClineTier(t *testing.T) {
	raw, tier, ok := SplitTierPrefix("cline/some-model")
	if !ok || raw != "some-model" || tier != config.TierCline {
		t.Fatalf("cline/ must pin TierCline, got raw=%q tier=%q ok=%v", raw, tier, ok)
	}
	raw, tier, ok = SplitTierPrefix("go/some-model")
	if !ok || raw != "some-model" || tier != config.TierGo {
		t.Fatalf("go/ must pin TierGo, got raw=%q tier=%q ok=%v", raw, tier, ok)
	}
	if opencode.AliasID(config.TierCline, "m") != "cline/m" {
		t.Fatal("cline alias must stay cline/m")
	}
	if opencode.AliasID(config.TierGo, "m") != "go/m" {
		t.Fatal("go alias must be go/m")
	}
	if opencode.ProviderLabel(config.TierCline, "") != "cline" {
		t.Fatal("cline tier must label cline")
	}
	if opencode.ProviderLabel(config.TierGo, "") != "opencode" {
		t.Fatal("go tier must label opencode")
	}
}

func TestClineCatalogSeparateFromGo(t *testing.T) {
	catalog := NewCatalog(config.TierZen, map[string]string{"cline-only": "chat", "go-only": "chat", "shared": "chat"})
	catalog.ReplaceWithCline([]string{"shared"}, []string{"shared"}, []string{"cline-only"}, nil, nil, nil, nil, nil)
	tiers := catalog.TiersForModel("cline-only")
	if len(tiers) != 1 || tiers[0] != config.TierCline {
		t.Fatalf("cline-only must advertise only Cline, got %v", tiers)
	}
	route, err := catalog.RouteWithCline("cline-only", false, false, true, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if route.Tier != config.TierCline {
		t.Fatalf("cline-only must route Cline, got %s", route.Tier)
	}
	// A Cline login must not unlock Go models.
	if _, err := catalog.RouteWithCline("shared", false, false, true, false, false, false); err == nil {
		// shared is on both Go and... wait shared IS on goModels here, so this needs a go-only check below.
		_ = err
	}
	catalog2 := NewCatalog(config.TierZen, map[string]string{"cline-only": "chat", "go-only": "chat", "shared": "chat"})
	catalog2.ReplaceWithCline(nil, []string{"go-only"}, nil, nil, nil, nil, nil, nil)
	if _, err := catalog2.RouteWithCline("go-only", false, false, true, false, false, false); err == nil {
		t.Fatal("Cline login must not route a Go-only model")
	}
}
