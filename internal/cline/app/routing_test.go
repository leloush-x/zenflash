package app

import "testing"

func TestClinePoolUnavailableOnlyForClineRoute(t *testing.T) {
	tests := []struct {
		name         string
		route        string
		accountCount int
		want         bool
	}{
		{name: "anonymous OpenCode route needs no Cline account", route: "zen", want: false},
		{name: "paid Zen route is rejected separately", route: "reject", want: false},
		{name: "Cline route needs an account", route: "cline", want: true},
		{name: "Cline route uses its pool", route: "cline", accountCount: 1, want: false},
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
		t.Fatal("degraded Zen must stay available when the Cline pool is empty")
	}
	if !shouldFailoverToCline(true, true, true) {
		t.Fatal("degraded Zen should fail over when a Cline account is active")
	}
	if shouldFailoverToCline(false, true, true) || shouldFailoverToCline(true, false, true) {
		t.Fatal("failover requires both the setting and degraded Zen")
	}
}
