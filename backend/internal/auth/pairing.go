package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Multi-machine pairing (docs/multi-machine.md): a one-time,
// short-lived, human-typeable token is minted on the server (CLI `agentique
// pair`, or an admin session) and exchanged by a remote client for a
// long-lived bearer auth session. WebSocket upgrades never carry the bearer
// token in the URL — they redeem a short-lived one-time ticket instead.

const (
	pairingTokenTTL    = 5 * time.Minute
	pairingTokenMaxTTL = 24 * time.Hour
	wsTicketTTL        = 5 * time.Minute
	adminSecretFile    = "admin-secret"

	// Crockford-ish alphabet without 0/1/I/O — 12 chars ≈ 60 bits, QR- and
	// transcription-friendly.
	pairingAlphabet    = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	pairingTokenLength = 12
)

// adminSecretHeader authenticates the local CLI: the caller proves data-dir
// access by presenting the secret the server persisted there at startup.
const adminSecretHeader = "X-Agentique-Admin-Secret"

type wsTicket struct {
	sessionToken string
	expiresAt    time.Time
}

// LoadOrCreateAdminSecret returns the admin secret persisted in dataDir,
// generating one (0600) on first run. Possession proves data-dir access and
// authorizes minting pairing tokens and managing auth sessions via the CLI.
func LoadOrCreateAdminSecret(dataDir string) (string, error) {
	path := filepath.Join(dataDir, adminSecretFile)

	raw, err := os.ReadFile(path)
	if err == nil {
		if secret := strings.TrimSpace(string(raw)); secret != "" {
			return secret, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read admin secret: %w", err)
	}

	secret, err := generateToken(32)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist admin secret: %w", err)
	}
	return secret, nil
}

// SetAdminSecret arms the data-dir-secret auth path for pairing/session
// management endpoints. Empty (the default) keeps that path disabled.
func (s *Service) SetAdminSecret(secret string) {
	s.adminSecret = secret
}

// generatePairingToken returns a 12-char token from the pairing alphabet,
// rejection-sampled so the distribution stays uniform.
func generatePairingToken() (string, error) {
	// Largest multiple of len(alphabet) below 256 — bytes at or above it are
	// discarded instead of introducing modulo bias.
	const rejectionLimit = (256 / len(pairingAlphabet)) * len(pairingAlphabet)

	out := make([]byte, 0, pairingTokenLength)
	buf := make([]byte, 32)
	for len(out) < pairingTokenLength {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate pairing token: %w", err)
		}
		for _, b := range buf {
			if int(b) >= rejectionLimit {
				continue
			}
			out = append(out, pairingAlphabet[int(b)%len(pairingAlphabet)])
			if len(out) == pairingTokenLength {
				break
			}
		}
	}
	return string(out), nil
}

// normalizePairingToken uppercases and strips separators so a hand-typed
// token survives spacing/case variations.
func normalizePairingToken(token string) string {
	token = strings.ToUpper(strings.TrimSpace(token))
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, token)
}

// requireAdmin authorizes pairing/session-management calls: a valid admin
// auth session, or the data-dir admin secret header (CLI path). Returns the
// acting user's id and, when authenticated via session, that session row.
func (s *Service) requireAdmin(r *http.Request) (string, *store.GetAuthSessionRow, error) {
	if session, err := s.authenticateRequest(r); err == nil && session != nil {
		if session.IsAdmin == 0 {
			return "", nil, errors.New("admin access required")
		}
		return session.UserID, session, nil
	}

	presented := r.Header.Get(adminSecretHeader)
	// Fail closed: an unset secret must never match, including against an
	// empty header.
	if s.adminSecret == "" || presented == "" ||
		subtle.ConstantTimeCompare([]byte(presented), []byte(s.adminSecret)) != 1 {
		return "", nil, errors.New("admin session or admin secret required")
	}

	admin, err := s.queries.GetAdminUser(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, errors.New("no admin user registered yet — open the web UI and register first")
		}
		return "", nil, fmt.Errorf("resolve admin user: %w", err)
	}
	return admin.ID, nil, nil
}

// handleMintPairingToken creates a one-time pairing token bound to the acting
// admin user. POST /api/auth/pairing-tokens {ttlSeconds?}.
func (s *Service) handleMintPairingToken(w http.ResponseWriter, r *http.Request) {
	userID, _, err := s.requireAdmin(r)
	if err != nil {
		httperror.RespondError(w, httperror.Unauthorized(err.Error()))
		return
	}

	var req struct {
		TTLSeconds int `json:"ttlSeconds"`
	}
	// An empty body means defaults; a malformed one is an error.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httperror.RespondError(w, httperror.BadRequest("invalid request body"))
		return
	}

	ttl := pairingTokenTTL
	if req.TTLSeconds > 0 {
		ttl = min(time.Duration(req.TTLSeconds)*time.Second, pairingTokenMaxTTL)
	}

	token, err := generatePairingToken()
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate pairing token", err))
		return
	}

	expiresAt := time.Now().Add(ttl).UTC().Format(time.RFC3339)
	if err := s.queries.CreatePairingToken(r.Context(), store.CreatePairingTokenParams{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}); err != nil {
		httperror.RespondError(w, httperror.Internal("create pairing token", err))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt,
	})
}

// handlePairExchange consumes a pairing token and returns a bearer session.
// POST /api/auth/pair {token, label} — unauthenticated by design; the token is
// the credential.
func (s *Service) handlePairExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body"))
		return
	}

	token := normalizePairingToken(req.Token)
	if token == "" {
		httperror.RespondError(w, httperror.BadRequest("token is required"))
		return
	}

	// Consumed atomically in SQL: expired, already-used, and unknown tokens
	// are indistinguishable to the caller.
	row, err := s.queries.ConsumePairingToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httperror.RespondError(w, httperror.Unauthorized("invalid or expired pairing token"))
			return
		}
		httperror.RespondError(w, httperror.Internal("consume pairing token", err))
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "paired client"
	}

	bearer, err := s.createSession(r.Context(), row.UserID, label, "bearer")
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	user, err := s.queries.GetUser(r.Context(), row.UserID)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("load user", err))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"token":     bearer,
		"expiresAt": time.Now().Add(sessionMaxAge).UTC().Format(time.RFC3339),
		"user": map[string]any{
			"id":          user.ID,
			"displayName": user.DisplayName,
			"isAdmin":     user.IsAdmin != 0,
		},
	})
}

// handleCreateWSTicket mints a one-time short-lived ticket the client appends
// as ?wsTicket= on the upgrade URL, so the long-lived bearer token never
// appears in a URL. POST /api/auth/ws-ticket.
func (s *Service) handleCreateWSTicket(w http.ResponseWriter, r *http.Request) {
	session, err := s.authenticateRequest(r)
	if err != nil || session == nil {
		httperror.RespondError(w, httperror.Unauthorized("unauthorized"))
		return
	}

	ticket, err := generateToken(32)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate ticket", err))
		return
	}

	expires := time.Now().Add(wsTicketTTL)
	s.wsTickets.Store(ticket, &wsTicket{sessionToken: session.Token, expiresAt: expires})

	httperror.JSON(w, http.StatusOK, map[string]any{
		"ticket":    ticket,
		"expiresAt": expires.UTC().Format(time.RFC3339),
	})
}

// redeemWSTicket resolves a one-time WebSocket ticket back to its auth
// session. The session is re-validated against the database so a revoked
// bearer session cannot ride a pre-minted ticket.
func (s *Service) redeemWSTicket(ctx context.Context, ticket string) (*store.GetAuthSessionRow, error) {
	val, ok := s.wsTickets.LoadAndDelete(ticket)
	if !ok {
		return nil, errors.New("ws ticket not found or already used")
	}
	entry := val.(*wsTicket)
	if time.Now().After(entry.expiresAt) {
		return nil, errors.New("ws ticket expired")
	}

	row, err := s.queries.GetAuthSession(ctx, entry.sessionToken)
	if err != nil {
		return nil, errors.New("session for ws ticket no longer valid")
	}
	return &row, nil
}

// handleListSessions lists auth sessions (no secrets). GET /api/auth/sessions.
func (s *Service) handleListSessions(w http.ResponseWriter, r *http.Request) {
	_, current, err := s.requireAdmin(r)
	if err != nil {
		httperror.RespondError(w, httperror.Unauthorized(err.Error()))
		return
	}

	rows, err := s.queries.ListAuthSessions(r.Context())
	if err != nil {
		httperror.RespondError(w, httperror.Internal("list sessions", err))
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":          row.ID.String,
			"userId":      row.UserID,
			"displayName": row.DisplayName,
			"label":       row.Label,
			"kind":        row.Kind,
			"expiresAt":   row.ExpiresAt,
			"createdAt":   row.CreatedAt,
			"current":     current != nil && current.ID.String == row.ID.String,
		})
	}
	httperror.JSON(w, http.StatusOK, out)
}

// handleRevokeSession deletes one auth session by public id.
// DELETE /api/auth/sessions/{id}. Revoking the caller's own current session is
// refused — use logout for that.
func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	_, current, err := s.requireAdmin(r)
	if err != nil {
		httperror.RespondError(w, httperror.Unauthorized(err.Error()))
		return
	}

	id := r.PathValue("id")
	if current != nil && current.ID.String == id {
		httperror.RespondError(w, httperror.BadRequest("refusing to revoke the current session — use logout"))
		return
	}

	n, err := s.queries.DeleteAuthSessionByID(r.Context(), sql.NullString{String: id, Valid: true})
	if err != nil {
		httperror.RespondError(w, httperror.Internal("revoke session", err))
		return
	}
	if n == 0 {
		httperror.RespondError(w, httperror.NotFound("session not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
