package claudeaccount

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/respond"
)

// Handler serves Claude CLI account status and manages login/logout flows.
type Handler struct {
	mu        sync.Mutex
	loginFn   context.CancelFunc      // non-nil while a login flow is active
	loginProc *claudecli.LoginProcess // non-nil while waiting for OAuth completion
}

type statusResponse struct {
	LoggedIn         bool   `json:"loggedIn"`
	Email            string `json:"email,omitempty"`
	OrgName          string `json:"orgName,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	AuthMethod       string `json:"authMethod,omitempty"`
}

// HandleStatus returns the current Claude CLI authentication status.
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := claudecli.AuthStatus(r.Context())
	if err != nil {
		respond.JSON(w, http.StatusOK, statusResponse{})
		return
	}
	if !status.LoggedIn {
		respond.JSON(w, http.StatusOK, statusResponse{})
		return
	}

	respond.JSON(w, http.StatusOK, statusResponse{
		LoggedIn:         true,
		Email:            status.Email,
		OrgName:          status.OrgName,
		SubscriptionType: status.SubscriptionType,
		AuthMethod:       status.AuthMethod,
	})
}

// HandleLogout logs out of the current Claude CLI account.
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if err := claudecli.AuthLogout(r.Context()); err != nil {
		slog.Error("claude auth logout failed", "error", err)
		respond.JSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}
	respond.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleLogin starts a Claude CLI login flow. Returns 202 with the OAuth URL;
// the frontend should poll HandleStatus until loggedIn becomes true.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.loginFn != nil {
		h.mu.Unlock()
		respond.JSON(w, http.StatusConflict, map[string]string{"error": "login already in progress"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	h.loginFn = cancel
	h.mu.Unlock()

	proc, err := claudecli.AuthLogin(ctx)
	if err != nil {
		h.clearLogin()
		slog.Error("claude auth login failed to start", "error", err)
		respond.JSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}

	if proc == nil {
		// Already logged in — no OAuth needed.
		h.clearLogin()
		respond.JSON(w, http.StatusOK, map[string]string{"status": "already_logged_in"})
		return
	}

	// Store process for cancellation, then wait in background.
	h.mu.Lock()
	h.loginProc = proc
	h.mu.Unlock()

	go func() {
		defer h.clearLogin()
		if err := proc.Wait(); err != nil {
			slog.Error("claude auth login failed", "error", err)
		}
	}()

	respond.JSON(w, http.StatusAccepted, map[string]string{
		"status": "login_started",
		"url":    proc.URL,
	})
}

// HandleLoginCancel aborts an in-progress login flow.
func (h *Handler) HandleLoginCancel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	proc := h.loginProc
	h.mu.Unlock()

	if proc == nil {
		respond.JSON(w, http.StatusOK, map[string]string{"status": "no_login_in_progress"})
		return
	}

	if err := proc.Cancel(); err != nil {
		slog.Error("failed to cancel login process", "error", err)
	}
	h.clearLogin()
	respond.JSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) clearLogin() {
	h.mu.Lock()
	if h.loginFn != nil {
		h.loginFn()
		h.loginFn = nil
	}
	h.loginProc = nil
	h.mu.Unlock()
}
