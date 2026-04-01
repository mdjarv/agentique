package claudeaccount

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"

	"github.com/allbin/agentique/backend/internal/respond"
)

// Handler serves Claude CLI account status and manages login/logout flows.
type Handler struct {
	mu              sync.Mutex
	loginInProgress bool
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
	path, err := exec.LookPath("claude")
	if err != nil {
		respond.JSON(w, http.StatusOK, statusResponse{})
		return
	}

	out, err := exec.Command(path, "auth", "status").Output()
	if err != nil {
		respond.JSON(w, http.StatusOK, statusResponse{})
		return
	}

	var raw struct {
		LoggedIn         bool   `json:"loggedIn"`
		Email            string `json:"email"`
		OrgName          string `json:"orgName"`
		SubscriptionType string `json:"subscriptionType"`
		AuthMethod       string `json:"authMethod"`
	}
	if err := json.Unmarshal(out, &raw); err != nil || !raw.LoggedIn {
		respond.JSON(w, http.StatusOK, statusResponse{})
		return
	}

	respond.JSON(w, http.StatusOK, statusResponse{
		LoggedIn:         true,
		Email:            raw.Email,
		OrgName:          raw.OrgName,
		SubscriptionType: raw.SubscriptionType,
		AuthMethod:       raw.AuthMethod,
	})
}

// HandleLogout logs out of the current Claude CLI account.
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	path, err := exec.LookPath("claude")
	if err != nil {
		respond.JSON(w, http.StatusInternalServerError, map[string]string{"error": "claude CLI not found"})
		return
	}

	if err := exec.Command(path, "auth", "logout").Run(); err != nil {
		slog.Error("claude auth logout failed", "error", err)
		respond.JSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}

	respond.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleLogin starts a Claude CLI login flow (opens browser). Returns 202 immediately;
// the frontend should poll HandleStatus until loggedIn becomes true.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	path, err := exec.LookPath("claude")
	if err != nil {
		respond.JSON(w, http.StatusInternalServerError, map[string]string{"error": "claude CLI not found"})
		return
	}

	h.mu.Lock()
	if h.loginInProgress {
		h.mu.Unlock()
		respond.JSON(w, http.StatusConflict, map[string]string{"error": "login already in progress"})
		return
	}
	h.loginInProgress = true
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			h.loginInProgress = false
			h.mu.Unlock()
		}()
		if err := exec.Command(path, "auth", "login").Run(); err != nil {
			slog.Error("claude auth login failed", "error", err)
		}
	}()

	respond.JSON(w, http.StatusAccepted, map[string]string{"status": "login_started"})
}
