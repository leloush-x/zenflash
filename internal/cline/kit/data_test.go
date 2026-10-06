package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataPathMigratesLegacyPoolToStateDir(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	legacyDir := filepath.Join(exeDir, "data")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, ".cline-accounts.json")
	legacyData := []byte(`{"accounts":[{"email":"account@example.test"}]}`)
	if err := os.WriteFile(legacyPath, legacyData, 0600); err != nil {
		t.Fatal(err)
	}

	got := resolveDataPath(".cline-accounts.json", stateDir, exeDir, "")
	want := filepath.Join(stateDir, ".cline-accounts.json")
	if got != want {
		t.Fatalf("ResolveDataPath() = %q, want %q", got, want)
	}
	stored, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(legacyData) {
		t.Fatalf("migrated file = %q, want %q", stored, legacyData)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy file was removed: %v", err)
	}
}

func TestResolveDataPathPrefersExistingStateDirFile(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(exeDir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(exeDir, "data", "accounts.json")
	statePath := filepath.Join(stateDir, "accounts.json")
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := resolveDataPath("accounts.json", stateDir, exeDir, ""); got != statePath {
		t.Fatalf("ResolveDataPath() = %q, want %q", got, statePath)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "current" {
		t.Fatalf("existing state file changed to %q", data)
	}
}
