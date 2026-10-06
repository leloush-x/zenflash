package store

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// syncedFiles lists every file-backed state mirrored into Postgres slots.
// Binary blobs (.enc/.key) are base64-wrapped; JSON files are stored raw.
func syncedFiles(configPath string) []struct{ path, slot string } {
	base := func(suffix string) string { return configPath + suffix }
	dir := filepath.Dir(configPath)
	return []struct{ path, slot string }{
		{base(".sessions.json"), "file:sessions"},
		{base(".codex-auth.json"), "file:codex-auth"},
		{base(".antigravity-auth.json"), "file:antigravity-auth"},
		{base(".models.catalog.json"), "file:models-catalog"},
		{base(".models.dev.json"), "file:models-dev"},
		{base(".codex.enc"), "file:codex-enc"},
		{base(".codex.enc.key"), "file:codex-enc-key"},
		{filepath.Join(dir, "data", ".zen-config.json"), "file:zen-config"},
	}
}

// SyncFiles mirrors file state into Postgres and restores missing files from
// it. Files stay authoritative at runtime (zero call-site changes, zero
// behavior change); the DB is the durable copy. Best-effort and never fails.
func (s *Store) SyncFiles(ctx context.Context, configPath string) {
	if s == nil || s.pool == nil || strings.TrimSpace(configPath) == "" {
		return
	}
	n := 0
	for _, f := range syncedFiles(configPath) {
		data, ferr := os.ReadFile(f.path)
		remote := s.CatalogLoad(ctx, f.slot)
		switch {
		case ferr == nil && strings.TrimSpace(remote) == "":
			s.CatalogSave(ctx, f.slot, encodeSlot(f.path, data))
		case ferr != nil && strings.TrimSpace(remote) != "":
			if raw, ok := decodeSlot(f.path, remote); ok {
				_ = os.MkdirAll(filepath.Dir(f.path), 0700)
				_ = os.WriteFile(f.path, raw, 0600)
			}
		case ferr == nil && strings.TrimSpace(remote) != "":
			if encodeSlot(f.path, data) != remote {
				s.CatalogSave(ctx, f.slot, encodeSlot(f.path, data))
			}
		}
		n++
	}
	s.mu.Lock()
	s.lastSync = time.Now().UTC()
	s.slotCount = n
	s.mu.Unlock()
}

// StartFileSync runs SyncFiles at startup and every minute. Nil-safe.
func (s *Store) StartFileSync(ctx context.Context, configPath string) {
	if s == nil || s.pool == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.SyncFiles(ctx, configPath)
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-s.quit:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				s.SyncFiles(ctx, configPath)
			}
		}
	}()
}

func encodeSlot(path string, data []byte) string {
	if strings.HasSuffix(path, ".enc") || strings.HasSuffix(path, ".key") {
		return "base64:" + base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}

func decodeSlot(path, slot string) ([]byte, bool) {
	if strings.HasPrefix(slot, "base64:") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(slot, "base64:"))
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	return []byte(slot), true
}
