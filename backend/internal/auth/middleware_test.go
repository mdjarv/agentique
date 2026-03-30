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
		{"/", false},
		{"/some-route", false},
		{"/api/", true},
		{"/api/auth", true}, // no trailing slash
	}

	for _, tt := range tests {
		if got := requiresAuth(tt.path); got != tt.want {
			t.Errorf("requiresAuth(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
