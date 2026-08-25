package auth

import "testing"

func TestRequiresAuth(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/auth/login/begin", false},
		{"/api/auth/status", false},
		{"/api/health", false},
		{"/api/projects", true},
		{"/api/sessions", true},
		{"/ws", true},
		{"/api/voice/live", true},
		{"/", false},
		{"/some-route", false},
		{"/api/", true},
		{"/api/auth", true}, // no trailing slash

		// requiresAuth protects the /api/ prefix and the exact string "/ws";
		// everything else falls through as an SPA asset. A socket mounted
		// outside those two shapes would be unauthenticated, which is why the
		// voice endpoint lives under /api/ rather than at /ws/voice.
		{"/ws/voice", false},
	}

	for _, tt := range tests {
		if got := requiresAuth(tt.path); got != tt.want {
			t.Errorf("requiresAuth(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// A wsTicket is a bearer credential in a URL, so every path that may redeem one
// must also be a path the middleware authenticates. Mounting a socket outside
// requiresAuth's two shapes and adding it here would otherwise open an
// unauthenticated endpoint that accepts a credential.
func TestWSUpgradePathsRequireAuth(t *testing.T) {
	for path := range wsUpgradePaths {
		if !requiresAuth(path) {
			t.Errorf("wsUpgradePaths has %q but requiresAuth(%q) = false: a ticket-redeeming endpoint must be authenticated", path, path)
		}
	}
}
