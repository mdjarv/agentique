package peer

// SurfaceVersion is what a peer reports as `peerSurface`. The acting side reads
// it to tell "cannot act here" from "does not understand acting at all": an
// older release answers the route with a 404 and no version.
//
// Bump it when a route's contract changes, never for an additive field — wire
// fields stay optional (CLAUDE.md, wire compatibility).
const SurfaceVersion = 1

// SessionsResponse answers GET /api/peer/sessions.
type SessionsResponse struct {
	MachineID      string        `json:"machineId"`
	PeerSurface    int           `json:"peerSurface"`
	AcceptActions  bool          `json:"acceptActions"`
	AcceptPolicies bool          `json:"acceptPolicies"`
	Sessions       []SessionWire `json:"sessions"`
	Projects       []ProjectWire `json:"projects"`
}

// SessionWire is one of this machine's sessions as a paired server sees it:
// enough to name, place, rank and judge it, and nothing a browser would need to
// render it.
type SessionWire struct {
	ID                string `json:"id"`
	ProjectID         string `json:"projectId"`
	Name              string `json:"name,omitempty"`
	State             string `json:"state"`
	Model             string `json:"model,omitempty"`
	WorktreeBranch    string `json:"worktreeBranch,omitempty"`
	ArchivedAt        string `json:"archivedAt,omitempty"`
	UnseenCompletedAt string `json:"unseenCompletedAt,omitempty"`
	PendingApproval   bool   `json:"pendingApproval,omitempty"`
	PendingQuestion   bool   `json:"pendingQuestion,omitempty"`
	AutoApproveMode   string `json:"autoApproveMode,omitempty"`
	Origin            string `json:"origin,omitempty"`
	LastQueryAt       string `json:"lastQueryAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
}

// ProjectWire is one of this machine's projects. RemoteURL is the canonical git
// remote, the cross-machine name for a repository; the path is never sent, as
// it is a filesystem fact of this machine alone.
type ProjectWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	RemoteURL string `json:"remoteUrl,omitempty"`
}

// SendRequest is POST /api/peer/sessions/{id}/send.
//
// There is no origin field: a peer's send is assistant-origin because of the
// credential it came with. PolicyID is recorded on the turn and gated by
// [Settings.AcceptPolicies]; it is never trusted as permission.
type SendRequest struct {
	Prompt   string `json:"prompt"`
	PolicyID string `json:"policyId,omitempty"`
}

// SendResponse carries the delivery the owner decided on, which only the owner
// can know (CLAUDE.md, "a message's delivery is reported, never inferred").
type SendResponse struct {
	Delivery string `json:"delivery"`
}

// CreateRequest is POST /api/peer/sessions.
//
// The project is named by this machine's id or by canonical remote; a remote
// that matches two checkouts here is refused as ambiguous rather than guessed.
// Model is a FAMILY name resolved by this machine's catalog, so a model the
// acting server has never heard of cannot be spelled into a session here.
//
// Prompt is optional and, when present, is sent in the same request: creating
// and sending are one call for the reason they are one tool call on a voice
// call — as two, anything that ended the conversation between them left an
// empty session and someone told the work had started.
type CreateRequest struct {
	ProjectID string `json:"projectId,omitempty"`
	RemoteURL string `json:"remoteUrl,omitempty"`
	Model     string `json:"model,omitempty"`
	Name      string `json:"name,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	PolicyID  string `json:"policyId,omitempty"`
	// RequestID makes a retried create return the first result instead of a
	// second session. Scoped by credential on this side.
	RequestID string `json:"requestId,omitempty"`
}

// CreateResponse reports both halves. A send that failed after the create
// succeeded is not an error for the request: the session exists, and saying
// only "failed" is how somebody comes back to an empty session believing
// nothing was made.
type CreateResponse struct {
	Session    SessionWire `json:"session"`
	Delivery   string      `json:"delivery,omitempty"`
	SendReason string      `json:"sendReason,omitempty"`
	SendError  string      `json:"sendError,omitempty"`
}

// ErrorResponse is every refusal's body.
type ErrorResponse struct {
	Error    string   `json:"error"`
	Reason   string   `json:"reason"`
	Families []string `json:"families,omitempty"`
}
