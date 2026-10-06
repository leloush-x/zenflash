package gateway

import (
	"testing"

	"zenflash-llm/internal/config"
)

func TestGoFallbackRequiresAnActiveEmbeddedClineAccount(t *testing.T) {
	g := &Gateway{cfg: config.Config{GoKeys: []string{"free"}}}
	if !g.hasGoKeys() {
		t.Fatal("standalone Go upstream should use configured credentials")
	}
	g.SetGoAccountAvailabilityProvider(func() bool { return false })
	if g.hasGoKeys() {
		t.Fatal("embedded Cline placeholder key must not count without an account")
	}
	g.SetGoAccountAvailabilityProvider(func() bool { return true })
	if !g.hasGoKeys() {
		t.Fatal("embedded Cline fallback should be available with an active account")
	}
}
