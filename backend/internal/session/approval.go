package session

import (
	"encoding/json"

	claudecli "github.com/allbin/claudecli-go"
)

// approvalState groups the approval- and permission-related fields of a Session.
// All access must be protected by the owning Session's mu.
type approvalState struct {
	pendingApprovals map[string]*pendingApproval
	pendingQuestions map[string]*pendingQuestion
	autoApproveMode  string // "manual", "auto", "fullAuto"
	permissionMode   string // "default", "plan", "acceptEdits"
}

// newApprovalState returns an initialized approvalState.
func newApprovalState() approvalState {
	return approvalState{
		pendingApprovals: make(map[string]*pendingApproval),
		pendingQuestions: make(map[string]*pendingQuestion),
		permissionMode:   "default",
	}
}

// resolveBypassable auto-resolves any pending approvals that the current mode
// configuration would bypass. Returns the approval IDs that were resolved.
// Caller must hold the Session's mu.
func (a *approvalState) resolveBypassable() []string {
	var resolved []string
	for _, pa := range a.pendingApprovals {
		if shouldBypassPermission(a.autoApproveMode, a.permissionMode, pa.toolName) {
			select {
			case pa.ch <- &claudecli.PermissionResponse{Allow: true}:
				resolved = append(resolved, pa.id)
			default:
			}
		}
	}
	return resolved
}

// toolInterceptor handles a tool-use request before it reaches the normal
// approval flow. Returns a response directly, bypassing the pending approval
// mechanism entirely.
type toolInterceptor func(input json.RawMessage) (*claudecli.PermissionResponse, error)
