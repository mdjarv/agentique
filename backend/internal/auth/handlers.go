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

	"github.com/mdjarv/agentique/backend/internal/httperr"
	"github.com/mdjarv/agentique/backend/internal/respond"
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
}

// handleStatus returns the current auth state.
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.queries.CountUsers(r.Context())
	if err != nil {
		respond.Error(w, httperr.Internal("count users", err))
		return
	}

	resp := map[string]any{
		"authEnabled": true,
		"userCount":   count,
	}

	session, err := s.validateSession(r)
	if err == nil && session != nil {
		resp["authenticated"] = true
		resp["user"] = map[string]any{
			"id":          session.UserID,
			"displayName": session.DisplayName,
			"isAdmin":     session.IsAdmin != 0,
		}
	} else {
		resp["authenticated"] = false
	}

	respond.JSON(w, http.StatusOK, resp)
}

type registerBeginRequest struct {
	DisplayName string `json:"displayName"`
	InviteToken string `json:"inviteToken,omitempty"`
}

// handleRegisterBegin starts passkey registration.
func (s *Service) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, httperr.BadRequest("invalid request body"))
		return
	}

	if req.DisplayName == "" {
		respond.Error(w, httperr.BadRequest("displayName is required"))
		return
	}

	ctx := r.Context()
	count, err := s.queries.CountUsers(ctx)
	if err != nil {
		respond.Error(w, httperr.Internal("count users", err))
		return
	}

	isAdmin := int64(0)
	inviteToken := ""

	if count == 0 {
		// First user — no invite needed, becomes admin.
		isAdmin = 1
	} else {
		// Subsequent users need a valid invite token OR existing auth.
		session, authErr := s.validateSession(r)
		if authErr == nil && session != nil {
			// Authenticated user adding another passkey — handled below.
		} else {
			// Need invite token.
			if req.InviteToken == "" {
				respond.Error(w, httperr.BadRequest("invite token required"))
				return
			}
			_, err := s.queries.GetInviteToken(ctx, req.InviteToken)
			if err != nil {
				respond.Error(w, httperr.BadRequest("invalid or expired invite token"))
				return
			}
			inviteToken = req.InviteToken
		}
	}

	// Create the user.
	userID := generateUUID()
	user, err := s.queries.CreateUser(ctx, store.CreateUserParams{
		ID:          userID,
		DisplayName: req.DisplayName,
		IsAdmin:     isAdmin,
	})
	if err != nil {
		respond.Error(w, httperr.Internal("create user", err))
		return
	}

	authUser := &User{User: user}

	opts := []webauthn.RegistrationOption{
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExtensions(map[string]any{"credProps": true}),
	}

	creation, session, err := s.webauthn.BeginMediatedRegistration(authUser, protocol.MediationDefault, opts...)
	if err != nil {
		slog.Error("webauthn begin registration failed", "error", err)
		respond.Error(w, httperr.Internal("start registration", err))
		return
	}

	ceremonyKey := "reg:" + userID
	s.saveCeremony(ceremonyKey, session, userID)

	respond.JSON(w, http.StatusOK, map[string]any{
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
		respond.Error(w, httperr.BadRequest("ceremonyKey is required"))
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		respond.Error(w, httperr.BadRequest(err.Error()))
		return
	}

	user, err := s.loadUser(r.Context(), entry.userID)
	if err != nil {
		respond.Error(w, httperr.Internal("load user", err))
		return
	}

	cred, err := s.webauthn.FinishRegistration(user, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish registration failed", "error", err)
		respond.Error(w, httperr.BadRequest("registration verification failed"))
		return
	}

	ctx := r.Context()
	if err := s.storeCredential(ctx, user.ID, cred); err != nil {
		respond.Error(w, httperr.Internal("store credential", err))
		return
	}

	// Mark invite token as used.
	if inviteToken != "" {
		_ = s.queries.UseInviteToken(ctx, store.UseInviteTokenParams{
			UsedBy: sql.NullString{String: user.ID, Valid: true},
			Token:  inviteToken,
		})
	}

	// Create auth session.
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		respond.Error(w, httperr.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	respond.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":          user.ID,
			"displayName": user.DisplayName,
			"isAdmin":     user.IsAdmin != 0,
		},
	})
}

// handleLoginBegin starts discoverable passkey login.
func (s *Service) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, session, err := s.webauthn.BeginDiscoverableMediatedLogin(protocol.MediationDefault)
	if err != nil {
		slog.Error("webauthn begin login failed", "error", err)
		respond.Error(w, httperr.Internal("start login", err))
		return
	}

	ceremonyKey := "login:" + generateToken(16)
	s.saveCeremony(ceremonyKey, session, "")

	respond.JSON(w, http.StatusOK, map[string]any{
		"options":     assertion,
		"ceremonyKey": ceremonyKey,
	})
}

// handleLoginFinish completes passkey login.
func (s *Service) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	ceremonyKey := r.URL.Query().Get("ceremonyKey")
	if ceremonyKey == "" {
		respond.Error(w, httperr.BadRequest("ceremonyKey is required"))
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		respond.Error(w, httperr.BadRequest(err.Error()))
		return
	}

	validatedUser, validatedCred, err := s.webauthn.FinishPasskeyLogin(s.loadUserByHandle, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish login failed", "error", err)
		respond.Error(w, httperr.BadRequest("login verification failed"))
		return
	}

	// Update sign count and flags.
	credID := validatedCred.ID
	_ = s.queries.UpdateCredentialAfterLogin(r.Context(), store.UpdateCredentialAfterLoginParams{
		SignCount:      int64(validatedCred.Authenticator.SignCount),
		BackupEligible: boolToInt(validatedCred.Flags.BackupEligible),
		BackupState:    boolToInt(validatedCred.Flags.BackupState),
		ID:             encodeCredentialID(credID),
	})

	user := validatedUser.(*User)
	token, err := s.createSession(r.Context(), user.ID)
	if err != nil {
		respond.Error(w, httperr.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	respond.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":          user.ID,
			"displayName": user.DisplayName,
			"isAdmin":     user.IsAdmin != 0,
		},
	})
}

// handleLogout clears the auth session.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		_ = s.queries.DeleteAuthSession(r.Context(), cookie.Value)
	}

	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateInvite creates a new invite token. Requires admin auth.
func (s *Service) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	session := UserFromContext(r.Context())
	if session == nil || session.IsAdmin == 0 {
		respond.Error(w, httperr.BadRequest("admin access required"))
		return
	}

	token := generateToken(32)
	expiresAt := time.Now().Add(inviteTokenTTL).UTC().Format(time.RFC3339)

	err := s.queries.CreateInviteToken(r.Context(), store.CreateInviteTokenParams{
		Token:     token,
		CreatedBy: session.UserID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		respond.Error(w, httperr.Internal("create invite", err))
		return
	}

	respond.JSON(w, http.StatusOK, map[string]any{
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
			respond.JSON(w, http.StatusOK, map[string]any{"valid": false})
			return
		}
		respond.Error(w, httperr.Internal("validate token", err))
		return
	}

	respond.JSON(w, http.StatusOK, map[string]any{"valid": true})
}

func encodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}
