package codex

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestStartLoginUsesStaticCLIClient(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "codex.enc"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.StartLogin("")
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(result["auth_url"])
	if err != nil {
		t.Fatal(err)
	}
	if authURL.Host != "auth.openai.com" || authURL.Path != "/oauth/authorize" {
		t.Fatalf("unexpected authorization endpoint: %s", authURL.Redacted())
	}
	query := authURL.Query()
	if query.Get("client_id") != DefaultClientID {
		t.Fatalf("must use the static CLI client: %q", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != RedirectURI {
		t.Fatalf("unexpected redirect URI: %q", query.Get("redirect_uri"))
	}
	if query.Get("scope") != AuthorizeScopes {
		t.Fatalf("unexpected scope: %q", query.Get("scope"))
	}
	if query.Get("state") == "" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatal("authorization URL is missing required state or PKCE parameters")
	}
	if query.Get("id_token_add_organizations") != "true" || query.Get("codex_cli_simplified_flow") != "true" {
		t.Fatal("authorization URL is missing the CLI flow parameters codex2api sends")
	}
	if query.Has("resource") || query.Has("nonce") || query.Has("agent_name_hint") || query.Has("ext_agent_host_id") {
		t.Fatal("authorization URL must not carry dynamic-client parameters")
	}
	if result["redirect_uri"] != RedirectURI {
		t.Fatalf("unexpected returned redirect URI: %q", result["redirect_uri"])
	}
}

func TestStartLoginIgnoresLegacyIssuedClientID(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "codex.enc"))
	if err != nil {
		t.Fatal(err)
	}
	// Older dashboard sessions may still submit an oaiapp_ ID from the retired
	// dynamic flow; it must not fail or leak into the authorization URL.
	result, err := service.StartLogin("oaiapp_example123")
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(result["auth_url"])
	if err != nil {
		t.Fatal(err)
	}
	if got := authURL.Query().Get("client_id"); got != DefaultClientID {
		t.Fatalf("legacy client ID leaked into authorization URL: %q", got)
	}
}

func TestCompleteLoginPersistsAccountToDisk(t *testing.T) {
	dir := t.TempDir()
	service, err := New(filepath.Join(dir, "codex.enc"))
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartLogin("")
	if err != nil {
		t.Fatal(err)
	}
	state := started["state"]
	if state == "" {
		t.Fatal("login start must return a state")
	}
	service.mu.Lock()
	_, ok := service.pending[state]
	service.mu.Unlock()
	if !ok {
		t.Fatal("login session was not tracked")
	}
	// Exercise the same disk write CompleteLogin performs after a successful
	// OAuth exchange, without network access.
	service.mu.Lock()
	service.state.Accounts = append(service.state.Accounts, account{
		ID: "acc-1", Email: "user@example.com", ClientID: DefaultClientID,
		AccessToken: "access-123", RefreshToken: "refresh-123",
	})
	saveErr := service.saveLocked()
	service.mu.Unlock()
	if saveErr != nil {
		t.Fatal(saveErr)
	}
	// A fresh Service on the same path must see the linked account: this is
	// the regression test for logins that vanished after restart/redeploy.
	reloaded, err := New(filepath.Join(dir, "codex.enc"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range reloaded.Accounts() {
		if a.ID == "acc-1" || a.Email == "user@example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reloaded accounts missing login: %+v", reloaded.Accounts())
	}
}
