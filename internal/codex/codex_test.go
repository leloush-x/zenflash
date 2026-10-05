package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialsMergesStoredAndStatic(t *testing.T) {
	stored := []TokenData{{AccessToken: "a", AccountID: "acct"}, {AccessToken: ""}, {AccessToken: "b"}}
	creds := Credentials(stored, []string{"raw1", "", "raw2"})
	if len(creds) != 4 {
		t.Fatalf("expected 4 credentials, got %d", len(creds))
	}
	if creds[0].AccessToken != "a" || creds[0].AccountID != "acct" {
		t.Fatalf("first credential mismatch: %+v", creds[0])
	}
	if creds[3].AccessToken != "raw2" || creds[3].AccountID != "" {
		t.Fatalf("static credential mismatch: %+v", creds[3])
	}
}

func TestAuthPath(t *testing.T) {
	if got := AuthPath("/etc/zen/config.json"); got != "/etc/zen/config.json.codex-auth.json" {
		t.Fatalf("auth path = %s", got)
	}
	if got := AuthPath(""); got != "codex-auth.json" {
		t.Fatalf("auth path = %s", got)
	}
}

func TestLoadSaveTokensRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	tokens := []TokenData{{AccessToken: "a", RefreshToken: "r", AccountID: "acct", LastRefresh: time.Now().UTC().Format(time.RFC3339Nano)}}
	if err := SaveTokens(path, tokens); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	loaded, err := LoadTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].AccessToken != "a" || loaded[0].AccountID != "acct" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if missing, err := LoadTokens(filepath.Join(dir, "missing.json")); err != nil || missing != nil {
		t.Fatalf("missing file should yield nil tokens, got %v %v", missing, err)
	}
}

func TestIsStale(t *testing.T) {
	fresh := TokenData{LastRefresh: time.Now().UTC().Format(time.RFC3339Nano)}
	if IsStale(fresh) {
		t.Fatal("fresh token marked stale")
	}
	old := TokenData{LastRefresh: time.Now().Add(-9 * time.Hour).UTC().Format(time.RFC3339Nano)}
	if !IsStale(old) {
		t.Fatal("old token not stale")
	}
	if !IsStale(TokenData{}) {
		t.Fatal("unparseable timestamp should be stale")
	}
}

func TestAccountIDFromJWT(t *testing.T) {
	payload := map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-123"},
	}
	raw, _ := json.Marshal(payload)
	token := "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".sig"
	id, err := AccountIDFromJWT(token)
	if err != nil || id != "acct-123" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	if _, err := AccountIDFromJWT("bad"); err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestFetchModelsSendsCodexHeaders(t *testing.T) {
	var gotAuth, gotAccount, gotUA, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("ChatGPT-Account-ID")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.2"},{"id":"gpt-5.2-codex"}]}`))
	}))
	defer srv.Close()
	models, status, err := FetchModels(context.Background(), srv.Client(), srv.URL, Credential{AccessToken: "tok", AccountID: "acct"})
	if err != nil || status != 200 {
		t.Fatalf("err = %v status = %d", err, status)
	}
	if len(models) != 2 || models[0] != "gpt-5.2" {
		t.Fatalf("models = %v", models)
	}
	if gotAuth != "Bearer tok" || gotAccount != "acct" || gotUA != UserAgent || gotPath != "/models" {
		t.Fatalf("headers: auth=%q account=%q ua=%q path=%q", gotAuth, gotAccount, gotUA, gotPath)
	}
}

func TestNewUpstreamRequest(t *testing.T) {
	req, err := NewUpstreamRequest(context.Background(), "https://chatgpt.com/backend-api/codex/", []byte(`{}`), "acct", "tok", true)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("url = %s", req.URL)
	}
	if req.Header.Get("Authorization") != "Bearer tok" || req.Header.Get("ChatGPT-Account-ID") != "acct" || req.Header.Get("User-Agent") != UserAgent || req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("headers = %v", req.Header)
	}
}
