package codex

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestStartLoginUsesPlanSharingAuthorizationEndpoint(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "codex.enc"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(result["auth_url"])
	if err != nil {
		t.Fatal(err)
	}
	if authURL.Host != "auth.openai.com" || authURL.Path != "/api/accounts/authorize" {
		t.Fatalf("unexpected authorization endpoint: %s", authURL.Redacted())
	}
	query := authURL.Query()
	if query.Get("client_id") != "dynamic_agent_client" || query.Get("redirect_uri") != redirectURI {
		t.Fatalf("unexpected client or redirect URI: client_id=%q redirect_uri=%q", query.Get("client_id"), query.Get("redirect_uri"))
	}
	if query.Get("state") == "" || query.Get("nonce") == "" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatal("authorization URL is missing required state, nonce, or PKCE parameters")
	}
}
