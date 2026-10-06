package models

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"zenflash-llm/internal/config"
	wire "zenflash-llm/internal/protocol"
)

func TestReasoningOptionsStayExactAndValidate(t *testing.T) {
	var model capabilityModel
	if err := json.Unmarshal([]byte(`{"reasoning":true,"reasoning_options":[{"type":"effort","values":["low",null,"max"]},{"type":"budget","values":["ignored"]}]}`), &model); err != nil {
		t.Fatal(err)
	}
	metadata := model.metadata()
	if got, want := string(metadata.ReasoningEfforts[0]), `"low"`; got != want {
		t.Fatalf("first effort = %s, want %s", got, want)
	}
	if got := string(metadata.ReasoningEfforts[1]); got != "null" {
		t.Fatalf("null effort was not preserved: %s", got)
	}
	if got, want := string(metadata.ReasoningEfforts[2]), `"max"`; got != want {
		t.Fatalf("last effort = %s, want %s", got, want)
	}

	catalog := NewCatalog(config.TierZen, nil)
	catalog.ReplaceWithCapabilities([]string{"model"}, nil, nil, nil, nil, nil, map[config.Tier]map[string]Metadata{
		config.TierZen: {"model": metadata},
	})
	for _, effort := range []string{"low", "none", "max"} {
		if err := catalog.ValidateReasoningEffort("model", config.TierZen, effort); err != nil {
			t.Errorf("declared effort %q rejected: %v", effort, err)
		}
	}
	if err := catalog.ValidateReasoningEffort("model", config.TierZen, "medium"); err == nil {
		t.Fatal("undeclared effort was accepted")
	}
	if err := catalog.ValidateReasoningEffort("unknown-model", config.TierZen, "medium"); err != nil {
		t.Fatalf("unknown effort metadata should pass through: %v", err)
	}
}

func TestReasoningEffortsSurviveCatalogCache(t *testing.T) {
	path := t.TempDir() + "/catalog.json"
	catalog := NewCatalog(config.TierZen, nil)
	catalog.SetCachePath(path)
	catalog.ReplaceWithCapabilities([]string{"model"}, nil, nil, nil, nil, nil, map[config.Tier]map[string]Metadata{
		config.TierZen: {"model": {ReasoningEfforts: []json.RawMessage{json.RawMessage(`"high"`), json.RawMessage(`null`)}}},
	})
	if err := catalog.SaveCache(); err != nil {
		t.Fatal(err)
	}

	restored := NewCatalog(config.TierZen, nil)
	if err := restored.LoadCache(path); err != nil {
		t.Fatal(err)
	}
	if err := restored.ValidateReasoningEffort("model", config.TierZen, "high"); err != nil {
		t.Fatalf("cached declared effort rejected: %v", err)
	}
	if err := restored.ValidateReasoningEffort("model", config.TierZen, "low"); err == nil {
		t.Fatal("cached undeclared effort was accepted")
	}
}

func TestCatalogCacheMissStaleFallbackAndBadCache(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/catalog.json"
	catalog := NewCatalog(config.TierZen, nil)
	catalog.SetCachePath(path)
	catalog.ReplaceWithCapabilities([]string{"fixture"}, nil, nil, nil, map[config.Tier]map[string]wire.Protocol{
		config.TierZen: {"fixture": wire.Chat},
	}, nil, nil)
	if err := catalog.SaveCache(); err != nil {
		t.Fatal(err)
	}

	miss := NewCatalog(config.TierZen, nil)
	if err := miss.LoadCache(dir + "/missing.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache miss error = %v, want os.ErrNotExist", err)
	}

	stale := NewCatalog(config.TierZen, nil)
	if err := stale.LoadCache(path); err != nil {
		t.Fatal(err)
	}
	if !stale.Snapshot().Stale || !stale.Supported("fixture") {
		t.Fatal("last good disk cache was not retained as a usable stale snapshot")
	}

	badPath := dir + "/bad.json"
	if err := os.WriteFile(badPath, []byte(`{"schema_version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := stale.LoadCache(badPath); err == nil {
		t.Fatal("unsupported cache schema was accepted")
	}
	if !stale.Supported("fixture") || !stale.Snapshot().Stale {
		t.Fatal("bad cache replaced or cleared the last good snapshot")
	}
}
