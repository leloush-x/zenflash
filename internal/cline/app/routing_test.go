package app

import "testing"

func TestClinePoolUnavailableOnlyForClineRoute(t *testing.T) {
	tests := []struct {
		name         string
		route        string
		accountCount int
		want         bool
	}{
		{name: "anonymous OpenCode route needs no Cline account", route: "zen", accountCount: 0, want: false},
		{name: "paid Zen route is rejected separately", route: "reject", accountCount: 0, want: false},
		{name: "Cline route needs an account", route: "cline", accountCount: 0, want: true},
		{name: "Cline route can use its pool", route: "cline", accountCount: 1, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clinePoolUnavailable(test.route, test.accountCount); got != test.want {
				t.Fatalf("clinePoolUnavailable(%q, %d) = %t, want %t", test.route, test.accountCount, got, test.want)
			}
		})
	}
}

func TestZenFailoverRequiresActiveClineAccount(t *testing.T) {
	if shouldFailoverToCline(true, true, false) {
		t.Fatal("degraded anonymous Zen route must stay on Zen when the Cline pool is empty")
	}
	if !shouldFailoverToCline(true, true, true) {
		t.Fatal("degraded Zen route should fail over when an active Cline account is available")
	}
	if shouldFailoverToCline(false, true, true) || shouldFailoverToCline(true, false, true) {
		t.Fatal("failover should require both the setting and a degraded Zen route")
	}
}
