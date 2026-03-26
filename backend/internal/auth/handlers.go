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

	"github.com/allbin/agentique/backend/internal/store"
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
		respondError(w, http.StatusInternalServerError, "failed to count users")
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

	respondJSON(w, http.StatusOK, resp)
}

type registerBeginRequest struct {
	DisplayName string `json:"displayName"`
	InviteToken string `json:"inviteToken,omitempty"`
}

// handleRegisterBegin starts passkey registration.
func (s *Service) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DisplayName == "" {
		respondError(w, http.StatusBadRequest, "displayName is required")
		return
	}

	ctx := r.Context()
	count, err := s.queries.CountUsers(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to count users")
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
				respondError(w, http.StatusForbidden, "invite token required")
				return
			}
			_, err := s.queries.GetInviteToken(ctx, req.InviteToken)
			if err != nil {
				respondError(w, http.StatusForbidden, "invalid or expired invite token")
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
		respondError(w, http.StatusInternalServerError, "failed to create user")
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
		respondError(w, http.StatusInternalServerError, "failed to start registration")
		return
	}

	ceremonyKey := "reg:" + userID
	s.saveCeremony(ceremonyKey, session, userID)

	respondJSON(w, http.StatusOK, map[string]any{
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
		respondError(w, http.StatusBadRequest, "ceremonyKey is required")
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.loadUser(r.Context(), entry.userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	cred, err := s.webauthn.FinishRegistration(user, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish registration failed", "error", err)
		respondError(w, http.StatusBadRequest, "registration verification failed")
		return
	}

	ctx := r.Context()
	if err := s.storeCredential(ctx, user.ID, cred); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to store credential")
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
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	s.setSessionCookie(w, r, token)
	respondJSON(w, http.StatusOK, map[string]any{
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
		respondError(w, http.StatusInternalServerError, "failed to start login")
		return
	}

	ceremonyKey := "login:" + generateToken(16)
	s.saveCeremony(ceremonyKey, session, "")

	respondJSON(w, http.StatusOK, map[string]any{
		"options":     assertion,
		"ceremonyKey": ceremonyKey,
	})
}

// handleLoginFinish completes passkey login.
func (s *Service) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	ceremonyKey := r.URL.Query().Get("ceremonyKey")
	if ceremonyKey == "" {
		respondError(w, http.StatusBadRequest, "ceremonyKey is required")
		return
	}

	entry, err := s.loadCeremony(ceremonyKey)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	validatedUser, validatedCred, err := s.webauthn.FinishPasskeyLogin(s.loadUserByHandle, *entry.session, r)
	if err != nil {
		slog.Error("webauthn finish login failed", "error", err)
		respondError(w, http.StatusUnauthorized, "login verification failed")
		return
	}

	// Update sign count.
	credID := validatedCred.ID
	_ = s.queries.UpdateCredentialSignCount(r.Context(), store.UpdateCredentialSignCountParams{
		SignCount: int64(validatedCred.Authenticator.SignCount),
		ID:        encodeCredentialID(credID),
	})

	user := validatedUser.(*User)
	token, err := s.createSession(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	s.setSessionCookie(w, r, token)
	respondJSON(w, http.StatusOK, map[string]any{
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
		respondError(w, http.StatusForbidden, "admin access required")
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
		respondError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
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
			respondJSON(w, http.StatusOK, map[string]any{"valid": false})
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to validate token")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"valid": true})
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func encodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}
