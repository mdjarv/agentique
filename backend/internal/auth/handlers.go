package auth

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// RegisterRoutes registers all auth endpoints on the given mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/status", s.handleStatus)
	mux.HandleFunc("POST /api/auth/register/begin", s.handleRegisterBegin)
	mux.HandleFunc("POST /api/auth/register/finish", s.handleRegisterFinish)
	mux.HandleFunc("POST /api/auth/login/begin", s.handleLoginBegin)
	mux.HandleFunc("POST /api/auth/login/finish", s.handleLoginFinish)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/auth/invite", s.handleCreateInvite)
	mux.HandleFunc("GET /api/auth/invite/{token}", s.handleValidateInvite)

	// Multi-machine pairing + bearer sessions (pairing.go). All under
	// /api/auth/ (exempt from the middleware) — each handler enforces its own
	// auth: mint/list/revoke require an admin session or the data-dir admin
	// secret; exchange is authenticated by the pairing token itself.
	mux.HandleFunc("POST /api/auth/pairing-tokens", s.handleMintPairingToken)
	mux.HandleFunc("POST /api/auth/pair", s.handlePairExchange)
	mux.HandleFunc("POST /api/auth/identity-proof", s.handleIdentityProof)
	mux.HandleFunc("POST /api/auth/ws-ticket", s.handleCreateWSTicket)
	mux.HandleFunc("GET /api/auth/sessions", s.handleListSessions)
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", s.handleRevokeSession)
	mux.HandleFunc("DELETE /api/auth/session", s.handleRevokeCurrentBearer)
}

// handleStatus returns the current auth state.
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.queries.CountUsers(r.Context())
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count users", err))
		return
	}

	credCount, err := s.queries.CountWebAuthnCredentials(r.Context())
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count credentials", err))
		return
	}

	resp := map[string]any{
		"authEnabled":     true,
		"userCount":       count,
		"credentialCount": credCount,
	}

	session, err := s.authenticateRequest(r)
	if err == nil && session != nil {
		resp["authenticated"] = true
		resp["user"] = map[string]any{
			"id":               session.UserID,
			"displayName":      session.DisplayName,
			"isAdmin":          session.IsAdmin != 0,
			"sidebarFocusMode": session.SidebarFocusMode != 0,
		}
	} else {
		resp["authenticated"] = false
	}

	httperror.JSON(w, http.StatusOK, resp)
}

type registerBeginRequest struct {
	DisplayName string `json:"displayName"`
	InviteToken string `json:"inviteToken,omitempty"`
}

// handleRegisterBegin starts passkey registration.
func (s *Service) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body"))
		return
	}

	if req.DisplayName == "" {
		httperror.RespondError(w, httperror.BadRequest("displayName is required"))
		return
	}

	ctx := r.Context()
	userCount, err := s.queries.CountUsers(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count users", err))
		return
	}

	credCount, err := s.queries.CountWebAuthnCredentials(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count credentials", err))
		return
	}

	// authUser drives the ceremony. pending is non-nil when the user row does
	// not exist yet — it is created in handleRegisterFinish, only after the
	// authenticator's response verifies. Nothing here writes to the users
	// table: an unauthenticated caller must not be able to leave state behind.
	var authUser *User
	var pending *pendingUser
	inviteToken := ""

	switch {
	case userCount == 0:
		// First user — no invite needed, becomes the full-access operator.
		pending = &pendingUser{displayName: req.DisplayName, isAdmin: 1}
		authUser = &User{User: store.User{ID: generateUUID(), DisplayName: req.DisplayName, IsAdmin: 1}}

	case credCount == 0:
		// Rekey — users exist but every credential was cleared (a deliberate
		// reset), so an existing user may re-register by display name.
		//
		// The failure message is deliberately identical to the success path's
		// absence of one: a distinct "no such display name" reply turns this
		// into a name-enumeration oracle for an unauthenticated caller.
		existing, err := s.queries.GetUserByDisplayName(ctx, req.DisplayName)
		if err != nil {
			httperror.RespondError(w, httperror.BadRequest("registration is not available for that name"))
			return
		}
		authUser = &User{User: existing}

	default:
		// Normal flow — an authenticated operator adding another passkey to
		// THEIR OWN account, or a new user redeeming an invite.
		session, authErr := s.validateSession(r)
		if authErr == nil && session != nil {
			// Adding a credential to the caller's existing account. This used
			// to fall through and CreateUser, so "add another passkey" quietly
			// produced a second, non-admin account instead of a second
			// credential on the first one.
			existing, err := s.loadUser(ctx, session.UserID)
			if err != nil {
				httperror.RespondError(w, httperror.Internal("load user", err))
				return
			}
			authUser = existing
			break
		}

		if req.InviteToken == "" {
			httperror.RespondError(w, httperror.BadRequest("invite token required"))
			return
		}
		if _, err := s.queries.GetInviteToken(ctx, req.InviteToken); err != nil {
			httperror.RespondError(w, httperror.BadRequest("invalid or expired invite token"))
			return
		}
		inviteToken = req.InviteToken
		pending = &pendingUser{displayName: req.DisplayName, isAdmin: 0}
		authUser = &User{User: store.User{ID: generateUUID(), DisplayName: req.DisplayName}}
	}

	opts := []webauthn.RegistrationOption{
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExtensions(map[string]any{"credProps": true}),
	}

	creation, session, err := s.webauthn.BeginMediatedRegistration(authUser, protocol.MediationDefault, opts...)
	if err != nil {
		slog.Error("webauthn begin registration failed", "error", err)
		httperror.RespondError(w, httperror.Internal("start registration", err))
		return
	}

	ceremonyKey := "reg:" + authUser.ID
	if err := s.saveCeremony(ceremonyKey, session, authUser.ID, pending); err != nil {
		httperror.RespondError(w, httperror.TooManyRequests(err.Error()))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"options":     creation,
		"ceremonyKey": ceremonyKey,
		"inviteToken": inviteToken,
	})
}

// handleRegisterFinish completes passkey registration.
func (s *Service) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	// The ceremony key comes as a query param so the body is the credential response.
	ceremonyKey := r.URL.Query().Get("ceremonyKey")
	inviteToken := r.URL.Query().Get("inviteToken")

	if ceremonyKey == "" {
		httperror.RespondError(w, httperror.BadRequest("ceremonyKey is required"))
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		httperror.RespondError(w, httperror.BadRequest(err.Error()))
		return
	}

	ctx := r.Context()

	var user *User
	if entry.pending != nil {
		// Not persisted yet — verify against the in-memory identity the
		// ceremony was started with, then create the row.
		user = &User{User: store.User{
			ID:          entry.userID,
			DisplayName: entry.pending.displayName,
			IsAdmin:     entry.pending.isAdmin,
		}}
	} else {
		user, err = s.loadUser(ctx, entry.userID)
		if err != nil {
			httperror.RespondError(w, httperror.Internal("load user", err))
			return
		}
	}

	cred, err := s.webauthn.FinishRegistration(user, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish registration failed", "error", err)
		httperror.RespondError(w, httperror.BadRequest("registration verification failed"))
		return
	}

	if entry.pending != nil {
		// Re-check the precondition the ceremony was authorized under. Two
		// concurrent first-run registrations would otherwise both believe they
		// are the first and both create a full-access operator.
		count, cerr := s.queries.CountUsers(ctx)
		if cerr != nil {
			httperror.RespondError(w, httperror.Internal("count users", cerr))
			return
		}
		if entry.pending.isAdmin == 1 && count > 0 {
			httperror.RespondError(w, httperror.Conflict("an operator is already registered"))
			return
		}
		created, cerr := s.queries.CreateUser(ctx, store.CreateUserParams{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			IsAdmin:     user.IsAdmin,
		})
		if cerr != nil {
			httperror.RespondError(w, httperror.Internal("create user", cerr))
			return
		}
		user.User = created
	}

	if err := s.storeCredential(ctx, user.ID, cred); err != nil {
		httperror.RespondError(w, httperror.Internal("store credential", err))
		return
	}

	// Mark invite token as used.
	if inviteToken != "" {
		if err := s.queries.UseInviteToken(ctx, store.UseInviteTokenParams{
			UsedBy: sql.NullString{String: user.ID, Valid: true},
			Token:  inviteToken,
		}); err != nil {
			slog.Warn("failed to mark invite token as used", "token", inviteToken, "error", err)
		}
	}

	// Create auth session.
	token, err := s.createSession(ctx, user.ID, "", "cookie")
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	httperror.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":               user.ID,
			"displayName":      user.DisplayName,
			"isAdmin":          user.IsAdmin != 0,
			"sidebarFocusMode": user.SidebarFocusMode != 0,
		},
	})
}

// handleLoginBegin starts discoverable passkey login.
func (s *Service) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, session, err := s.webauthn.BeginDiscoverableMediatedLogin(protocol.MediationDefault)
	if err != nil {
		slog.Error("webauthn begin login failed", "error", err)
		httperror.RespondError(w, httperror.Internal("start login", err))
		return
	}

	rawKey, err := generateToken(16)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate ceremony key", err))
		return
	}
	ceremonyKey := "login:" + rawKey
	if err := s.saveCeremony(ceremonyKey, session, "", nil); err != nil {
		httperror.RespondError(w, httperror.TooManyRequests(err.Error()))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"options":     assertion,
		"ceremonyKey": ceremonyKey,
	})
}

// handleLoginFinish completes passkey login.
func (s *Service) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	ceremonyKey := r.URL.Query().Get("ceremonyKey")
	if ceremonyKey == "" {
		httperror.RespondError(w, httperror.BadRequest("ceremonyKey is required"))
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		httperror.RespondError(w, httperror.BadRequest(err.Error()))
		return
	}

	validatedUser, validatedCred, err := s.webauthn.FinishPasskeyLogin(s.loadUserByHandle, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish login failed", "error", err)
		httperror.RespondError(w, httperror.BadRequest("login verification failed"))
		return
	}

	// Update sign count and flags.
	credID := validatedCred.ID
	if err := s.queries.UpdateCredentialAfterLogin(r.Context(), store.UpdateCredentialAfterLoginParams{
		SignCount:      int64(validatedCred.Authenticator.SignCount),
		BackupEligible: boolToInt(validatedCred.Flags.BackupEligible),
		BackupState:    boolToInt(validatedCred.Flags.BackupState),
		ID:             encodeCredentialID(credID),
	}); err != nil {
		slog.Warn("failed to update credential after login", "error", err)
	}

	user := validatedUser.(*User)
	token, err := s.createSession(r.Context(), user.ID, "", "cookie")
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	httperror.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":               user.ID,
			"displayName":      user.DisplayName,
			"isAdmin":          user.IsAdmin != 0,
			"sidebarFocusMode": user.SidebarFocusMode != 0,
		},
	})
}

// handleLogout clears the auth session.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.authenticateRequest(r)
	if session != nil && authErr == nil {
		if err := s.queries.DeleteAuthSession(r.Context(), session.Token); err != nil {
			slog.Warn("failed to delete logout session", "error", err)
		}
		if session.ID.Valid {
			s.closeSessionConnections(session.ID.String)
		}
	}

	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateInvite creates a new invite token. Requires admin auth.
//
// Authorization comes from requireAdmin, NOT from the request context: every
// /api/auth/ path is exempt from the auth middleware, so the context user is
// always nil here. Reading it meant this endpoint returned 403 unconditionally
// and no invite could ever be created.
func (s *Service) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	userID, _, err := s.requireAdmin(r)
	if err != nil {
		httperror.RespondError(w, httperror.Forbidden(err.Error()))
		return
	}

	token, err := generateToken(32)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate token", err))
		return
	}
	expiresAt := time.Now().Add(inviteTokenTTL).UTC().Format(time.RFC3339)

	err = s.queries.CreateInviteToken(r.Context(), store.CreateInviteTokenParams{
		Token:     token,
		CreatedBy: userID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create invite", err))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt,
	})
}

// handleValidateInvite checks if an invite token is valid.
func (s *Service) handleValidateInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	_, err := s.queries.GetInviteToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httperror.JSON(w, http.StatusOK, map[string]any{"valid": false})
			return
		}
		httperror.RespondError(w, httperror.Internal("validate token", err))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{"valid": true})
}

func encodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}
