package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/allbin/agentique/backend/internal/store"
)

const (
	cookieName     = "agentique_session"
	sessionMaxAge  = 30 * 24 * time.Hour
	ceremonyTTL    = 5 * time.Minute
	inviteTokenTTL = 7 * 24 * time.Hour
)

type contextKey int

const userContextKey contextKey = 0

// Service handles WebAuthn authentication flows and session management.
type Service struct {
	queries    *store.Queries
	webauthn   *webauthn.WebAuthn
	ceremonies sync.Map // key: string -> *ceremonyEntry
}

type ceremonyEntry struct {
	session   *webauthn.SessionData
	userID    string
	expiresAt time.Time
}

// NewService creates a new auth Service. rpID is the domain (e.g. "localhost"),
// rpOrigins are the allowed origins (e.g. ["http://localhost:9201"]).
func NewService(queries *store.Queries, rpID string, rpOrigins []string) (*Service, error) {
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "Agentique",
		RPID:          rpID,
		RPOrigins:     rpOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn config: %w", err)
	}

	s := &Service{
		queries:  queries,
		webauthn: w,
	}

	go s.cleanupLoop()

	return s, nil
}

// UserFromContext returns the authenticated user from the request context, or nil.
func UserFromContext(ctx context.Context) *store.GetAuthSessionRow {
	u, _ := ctx.Value(userContextKey).(*store.GetAuthSessionRow)
	return u
}

func (s *Service) setUserContext(ctx context.Context, u *store.GetAuthSessionRow) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// loadUser loads a User with credentials from the database.
func (s *Service) loadUser(ctx context.Context, userID string) (*User, error) {
	u, err := s.queries.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	creds, err := s.queries.ListCredentialsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &User{
		User:        u,
		Credentials: credentialsFromStore(creds),
	}, nil
}

// loadUserByHandle implements the DiscoverableUserHandler for passkey login.
func (s *Service) loadUserByHandle(rawID, userHandle []byte) (webauthn.User, error) {
	return s.loadUser(context.Background(), string(userHandle))
}

// createSession creates a new auth session and returns the token.
func (s *Service) createSession(ctx context.Context, userID string) (string, error) {
	token, err := generateToken(32)
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(sessionMaxAge).UTC().Format(time.RFC3339)

	err = s.queries.CreateAuthSession(ctx, store.CreateAuthSessionParams{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

// setSessionCookie sets the auth session cookie on the response.
func (s *Service) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie removes the auth session cookie.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// saveCeremony stores WebAuthn session data for the duration of a begin/finish flow.
func (s *Service) saveCeremony(key string, session *webauthn.SessionData, userID string) {
	s.ceremonies.Store(key, &ceremonyEntry{
		session:   session,
		userID:    userID,
		expiresAt: time.Now().Add(ceremonyTTL),
	})
}

// loadCeremony retrieves and deletes ceremony session data.
func (s *Service) loadCeremony(key string) (*ceremonyEntry, error) {
	val, ok := s.ceremonies.LoadAndDelete(key)
	if !ok {
		return nil, errors.New("ceremony not found or expired")
	}

	entry := val.(*ceremonyEntry)
	if time.Now().After(entry.expiresAt) {
		return nil, errors.New("ceremony expired")
	}

	return entry, nil
}

// storeCredential persists a webauthn.Credential to the database.
func (s *Service) storeCredential(ctx context.Context, userID string, cred *webauthn.Credential) error {
	var transports []string
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}

	return s.queries.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		ID:              base64.RawURLEncoding.EncodeToString(cred.ID),
		UserID:          userID,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		Aaguid:          cred.Authenticator.AAGUID,
		SignCount:       int64(cred.Authenticator.SignCount),
		Transport:       strings.Join(transports, ","),
		BackupEligible:  boolToInt(cred.Flags.BackupEligible),
		BackupState:     boolToInt(cred.Flags.BackupState),
	})
}

// cleanupLoop periodically removes expired ceremonies and auth sessions.
func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.ceremonies.Range(func(key, val any) bool {
			if entry, ok := val.(*ceremonyEntry); ok && time.Now().After(entry.expiresAt) {
				s.ceremonies.Delete(key)
			}
			return true
		})

		if err := s.queries.DeleteExpiredAuthSessions(context.Background()); err != nil {
			slog.Error("failed to clean expired sessions", "error", err)
		}
	}
}

func generateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateUUID() string {
	return uuid.New().String()
}

// validateSession checks the session cookie and returns the session row if valid.
func (s *Service) validateSession(r *http.Request) (*store.GetAuthSessionRow, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetAuthSession(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("session not found")
		}
		return nil, err
	}

	return &row, nil
}
