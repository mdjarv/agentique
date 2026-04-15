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
}

// handleStatus returns the current auth state.
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.queries.CountUsers(r.Context())
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count users", err))
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
	count, err := s.queries.CountUsers(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("count users", err))
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
				httperror.RespondError(w, httperror.BadRequest("invite token required"))
				return
			}
			_, err := s.queries.GetInviteToken(ctx, req.InviteToken)
			if err != nil {
				httperror.RespondError(w, httperror.BadRequest("invalid or expired invite token"))
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
		httperror.RespondError(w, httperror.Internal("create user", err))
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
		httperror.RespondError(w, httperror.Internal("start registration", err))
		return
	}

	ceremonyKey := "reg:" + userID
	s.saveCeremony(ceremonyKey, session, userID)

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

	user, err := s.loadUser(r.Context(), entry.userID)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("load user", err))
		return
	}

	cred, err := s.webauthn.FinishRegistration(user, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish registration failed", "error", err)
		httperror.RespondError(w, httperror.BadRequest("registration verification failed"))
		return
	}

	ctx := r.Context()
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
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	httperror.JSON(w, http.StatusOK, map[string]any{
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
		httperror.RespondError(w, httperror.Internal("start login", err))
		return
	}

	rawKey, err := generateToken(16)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate ceremony key", err))
		return
	}
	ceremonyKey := "login:" + rawKey
	s.saveCeremony(ceremonyKey, session, "")

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
	token, err := s.createSession(r.Context(), user.ID)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	s.setSessionCookie(w, r, token)
	httperror.JSON(w, http.StatusOK, map[string]any{
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
		httperror.RespondError(w, httperror.Forbidden("admin access required"))
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
		CreatedBy: session.UserID,
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
