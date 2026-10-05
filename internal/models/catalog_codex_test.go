package models

import (
	"testing"

	"zenflash-llm/internal/config"
	wire "zenflash-llm/internal/protocol"
)

func TestCodexTierRouting(t *testing.T) {
	catalog := NewCatalog(config.TierZen, nil)
	catalog.Replace(nil, nil, []string{"gpt-5.2"})
	route, err := catalog.Route("gpt-5.2", false, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if route.Tier != config.TierCodex {
		t.Fatalf("tier = %s", route.Tier)
	}
	if route.Protocol != wire.Responses {
		t.Fatalf("protocol = %s", route.Protocol)
	}
	if _, err := catalog.RouteForTier("gpt-5.2", config.TierCodex, false, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.RouteForTier("gpt-5.2", config.TierCodex, false, false, false); err == nil {
		t.Fatal("expected error when no codex key is configured")
	}
}

func TestCodexTierPreferOrder(t *testing.T) {
	catalog := NewCatalog(config.TierCodex, nil)
	catalog.Replace([]string{"m"}, []string{"m"}, []string{"m"})
	route, err := catalog.Route("m", true, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if route.Tier != config.TierCodex {
		t.Fatalf("prefer=codex must route codex first, got %s", route.Tier)
	}
	if len(route.KeyTiers) == 0 || route.KeyTiers[0] != config.TierCodex {
		t.Fatalf("key tiers = %v", route.KeyTiers)
	}
}

func TestCodexCatalogPendingRoutes(t *testing.T) {
	catalog := NewCatalog(config.TierZen, nil)
	route, err := catalog.Route("anything", false, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if route.Tier != config.TierCodex || route.Protocol != wire.Responses {
		t.Fatalf("pending catalog should keep codex usable: %+v", route)
	}
}
