package auth

import (
	"net/http"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Middleware returns an HTTP middleware that enforces authentication.
// Requests to exempt paths pass through without auth checks.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requiresAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		session, err := s.validateSession(r)
		if err != nil {
			httperror.RespondError(w, httperror.Unauthorized("unauthorized").WithCause(err))
			return
		}

		ctx := s.setUserContext(r.Context(), session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requiresAuth returns true for paths that need authentication.
func requiresAuth(path string) bool {
	// Auth endpoints are always accessible.
	if strings.HasPrefix(path, "/api/auth/") {
		return false
	}

	// Health endpoint is always accessible.
	if path == "/api/health" {
		return false
	}

	// Protect API and WebSocket.
	if strings.HasPrefix(path, "/api/") || path == "/ws" {
		return true
	}

	// SPA static files are always accessible.
	return false
}
