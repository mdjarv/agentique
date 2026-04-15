package claudeaccount

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/httperror"
)

// Handler serves Claude CLI account status and manages login/logout flows.
type Handler struct {
	client    *claudecli.Client
	mu        sync.Mutex
	loginFn   context.CancelFunc      // non-nil while a login flow is active
	loginProc *claudecli.LoginProcess // non-nil while waiting for OAuth completion
	loginGen  uint64                  // incremented on each new login; stale goroutines become no-ops
}

func NewHandler() *Handler {
	return &Handler{
		client: claudecli.NewClient([]claudecli.ClientOption{
			claudecli.WithLogger(slog.Default()),
		}),
	}
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
	status, err := h.client.AuthStatus(r.Context())
	if err != nil {
		httperror.JSON(w, http.StatusOK, statusResponse{})
		return
	}
	if !status.LoggedIn {
		httperror.JSON(w, http.StatusOK, statusResponse{})
		return
	}

	httperror.JSON(w, http.StatusOK, statusResponse{
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
		httperror.JSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}
	httperror.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleLogin starts a Claude CLI login flow. Returns 202 with the OAuth URL;
// the frontend should poll HandleStatus until loggedIn becomes true.
// If a login is already in progress it is cancelled and replaced.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	// Cancel any stale login before starting a new one.
	if h.loginFn != nil {
		if h.loginProc != nil {
			_ = h.loginProc.Cancel()
		}
		h.loginFn()
		h.loginFn = nil
		h.loginProc = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.loginFn = cancel
	h.loginGen++
	gen := h.loginGen
	h.mu.Unlock()

	proc, err := h.client.AuthLogin(ctx, claudecli.WithNoBrowser())
	if err != nil {
		h.clearLoginIfGen(gen)
		slog.Error("claude auth login failed to start", "error", err)
		httperror.JSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}

	if proc == nil {
		// Already logged in — no OAuth needed.
		h.clearLoginIfGen(gen)
		httperror.JSON(w, http.StatusOK, map[string]string{"status": "already_logged_in"})
		return
	}

	// Store process for cancellation, then wait in background.
	h.mu.Lock()
	h.loginProc = proc
	h.mu.Unlock()

	go func() {
		defer h.clearLoginIfGen(gen)
		if err := proc.Wait(); err != nil {
			slog.Error("claude auth login failed", "error", err)
		}
	}()

	httperror.JSON(w, http.StatusAccepted, map[string]string{
		"status": "login_started",
		"url":    proc.AutoOpenURL,
	})
}

// HandleLoginCancel aborts an in-progress login flow.
func (h *Handler) HandleLoginCancel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	proc := h.loginProc
	gen := h.loginGen
	h.mu.Unlock()

	if proc == nil {
		httperror.JSON(w, http.StatusOK, map[string]string{"status": "no_login_in_progress"})
		return
	}

	if err := proc.Cancel(); err != nil {
		slog.Error("failed to cancel login process", "error", err)
	}
	h.clearLoginIfGen(gen)
	httperror.JSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// HandleLoginCode submits an authorization code to an in-progress login.
// This is used when the OAuth redirect fails and the user receives a code to paste.
func (h *Handler) HandleLoginCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		httperror.JSON(w, http.StatusBadRequest, map[string]string{"error": "missing code"})
		return
	}

	h.mu.Lock()
	proc := h.loginProc
	h.mu.Unlock()

	if proc == nil {
		httperror.JSON(w, http.StatusConflict, map[string]string{"error": "no login in progress"})
		return
	}

	if err := proc.SubmitCode(body.Code); err != nil {
		slog.Error("failed to submit login code", "error", err)
		httperror.JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to submit code"})
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]string{"status": "code_submitted"})
}

// clearLoginIfGen clears login state only if the generation matches,
// preventing a stale goroutine from clearing a newer login.
func (h *Handler) clearLoginIfGen(gen uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loginGen != gen {
		return
	}
	if h.loginFn != nil {
		h.loginFn()
		h.loginFn = nil
	}
	h.loginProc = nil
}
