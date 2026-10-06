package store

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// Importer copies today's file formats into Postgres once (idempotent, no loss).
// It never deletes file data and skips slots already present in the DB.
type ImportResult struct {
	SeededKeys bool
	Settings   int
	Catalog    int
}

// ImportOnce migrates config snapshot + catalog caches + sessions marker.
func (s *Store) ImportOnce(ctx context.Context, configPath string, configJSON []byte) (ImportResult, error) {
	var out ImportResult
	if s == nil || s.pool == nil {
		return out, nil
	}
	done, _ := s.GetSetting(ctx, "v2.imported")
	if strings.TrimSpace(done) != "" {
		return out, nil
	}
	if len(configJSON) > 0 {
		var v map[string]any
		if err := json.Unmarshal(configJSON, &v); err == nil {
			keys := serverKeysFromConfig(v)
			if err := s.SeedKeys(ctx, keys); err == nil && len(keys) > 0 {
				out.SeededKeys = true
			}
			s.SetSetting(ctx, "v2.config_snapshot", string(configJSON))
			out.Settings++
		}
	}
	tryFile := func(path, slot string) {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return
		}
		if existing := s.CatalogLoad(ctx, slot); strings.TrimSpace(existing) != "" {
			return
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return
		}
		s.CatalogSave(ctx, slot, string(data))
		out.Catalog++
	}
	tryFile(configPath+".models.catalog.json", "models.catalog")
	tryFile(configPath+".models.dev.json", "models.dev")
	s.SetSetting(ctx, "v2.imported", `"1"`)
	return out, nil
}

func serverKeysFromConfig(v map[string]any) []string {
	raw, _ := v["server_keys"].([]any)
	var out []string
	for _, x := range raw {
		if str, ok := x.(string); ok && strings.TrimSpace(str) != "" {
			out = append(out, strings.TrimSpace(str))
		}
	}
	return out
}
