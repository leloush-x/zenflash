package store

import (
	"path/filepath"
	"testing"
)

func TestSyncedFilesIncludeClinePoolInStateDir(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("STATE_DIR", stateDir)
	files := syncedFiles(filepath.Join(stateDir, "config.json"))
	for _, file := range files {
		if file.slot == "file:cline-accounts" {
			want := filepath.Join(stateDir, ".cline-accounts.json")
			if file.path != want {
				t.Fatalf("Cline pool sync path = %q, want %q", file.path, want)
			}
			return
		}
	}
	t.Fatal("Cline account pool is missing from Postgres file sync")
}
