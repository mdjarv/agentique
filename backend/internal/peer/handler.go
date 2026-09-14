package peer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// maxPromptBytes matches the socket's own bound on a prompt.
const maxPromptBytes = 1 << 20

// Sessions is the part of the session service the peer surface drives. It is
// the same service the composer's send and new-session flow reach, so a
// paired server's request and a click are one route into the pipeline.
type Sessions interface {
	ListAllSessions(ctx context.Context) (session.ListSessionsResult, error)
	GetSessionInfo(ctx context.Context, id string) (session.SessionInfo, error)
	CreateSession(ctx context.Context, p session.CreateSessionParams) (session.CreateSessionResult, error)
	EnqueueMessageWithOrigin(ctx context.Context, sessionID, prompt string,
		attachments []session.QueryAttachment, origin session.QueryOrigin) (session.MessageDelivery, error)
}

// Projects lists this machine's projects.
type Projects interface {
	ListProjects(ctx context.Context) ([]store.Project, error)
}

// Catalog resolves a model family name the way the picker does.
type Catalog interface {
	ResolveFamily(ctx context.Context, provider, spoken string) (providers.ModelInfo, bool)
	FamilyNames(ctx context.Context, provider string) []string
}

// Handler serves /api/peer/*.
type Handler struct {
	sessions  Sessions
	projects  Projects
	catalog   Catalog
	settings  Settings
	machineID string
	limits    *limiter
	outbox    *Outbox
}

// Option configures a [Handler].
type Option func(*Handler)

// WithSettings sets the owner's opt-ins. Without it both are off.
func WithSettings(s Settings) Option { return func(h *Handler) { h.settings = s } }

// WithMachineID names this machine in the list answer.
func WithMachineID(id string) Option { return func(h *Handler) { h.machineID = id } }

// WithCatalog resolves model families on create. Without one, a create that
// names a model is refused rather than guessed.
func WithCatalog(c Catalog) Option { return func(h *Handler) { h.catalog = c } }

// WithOutbox records follows on send and create, and serves the event poll.
// Without one there is no /api/peer/events and nothing a paired server sends
// here reports back.
func WithOutbox(o *Outbox) Option { return func(h *Handler) { h.outbox = o } }

// withClock replaces the limiter's clock, for tests.
func withClock(now func() time.Time) Option { return func(h *Handler) { h.limits = newLimiter(now) } }

// New builds the handler. It does no IO.
func New(sessions Sessions, projects Projects, opts ...Option) *Handler {
	h := &Handler{sessions: sessions, projects: projects, limits: newLimiter(time.Now)}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes mounts the surface. The auth middleware already refuses any
// credential but a peer's on these paths; each handler checks again, because a
// server running with auth disabled has no credential at all, and the surface
// must not open on the strength of a loopback listener.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/peer/sessions", h.handleList)
	mux.HandleFunc("POST /api/peer/sessions", h.handleCreate)
	mux.HandleFunc("POST /api/peer/sessions/{id}/send", h.handleSend)
	if h.outbox != nil {
		mux.HandleFunc("GET /api/peer/events", h.handleEvents)
	}
}

// handleEvents is the follower's poll: GET /api/peer/events?since=N&wait=S.
// A credential reads only its own rows, so two paired servers never see each
// other's news.
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	credential, ok := h.requirePeer(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	since, err := parseNonNegative(q.Get("since"))
	if err != nil {
		h.refuse(w, r, credential, refuse(http.StatusBadRequest, ReasonBadRequest, "since must be a non-negative integer"))
		return
	}
	waitSeconds, err := parseNonNegative(q.Get("wait"))
	if err != nil {
		h.refuse(w, r, credential, refuse(http.StatusBadRequest, ReasonBadRequest, "wait must be a non-negative integer of seconds"))
		return
	}
	out, err := h.outbox.Events(r.Context(), credential, since, time.Duration(waitSeconds)*time.Second)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("read peer events", err))
		return
	}
	httperror.JSON(w, http.StatusOK, out)
}

func parseNonNegative(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("not a non-negative integer")
	}
	return n, nil
}

// follow records that this credential's server is following the session. A
// failure is logged, never fatal to the action that already happened: the send
// went, and the worst case is news that does not come back.
func (h *Handler) follow(ctx context.Context, sessionID, credential, policyID string) {
	if h.outbox == nil {
		return
	}
	if err := h.outbox.Follow(ctx, sessionID, credential, policyID); err != nil {
		slog.Warn("peer: follow not recorded", "session", sessionID, "credential", credential, "error", err)
	}
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePeer(w, r); !ok {
		return
	}
	ctx := r.Context()
	list, err := h.sessions.ListAllSessions(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("list sessions", err))
		return
	}
	projects, err := h.projects.ListProjects(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("list projects", err))
		return
	}

	out := SessionsResponse{
		MachineID:      h.machineID,
		PeerSurface:    SurfaceVersion,
		AcceptActions:  h.settings.AcceptActions,
		AcceptPolicies: h.settings.AcceptActions && h.settings.AcceptPolicies,
		Sessions:       make([]SessionWire, 0, len(list.Sessions)),
		Projects:       make([]ProjectWire, 0, len(projects)),
	}
	for _, info := range list.Sessions {
		out.Sessions = append(out.Sessions, toSessionWire(info))
	}
	for _, p := range projects {
		out.Projects = append(out.Projects, ProjectWire{ID: p.ID, Name: p.Name, Slug: p.Slug, RemoteURL: p.RemoteUrl})
	}
	httperror.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleSend(w http.ResponseWriter, r *http.Request) {
	credential, ok := h.requirePeer(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		h.refuse(w, r, credential, refuse(http.StatusBadRequest, ReasonBadRequest, "session id must be a UUID"))
		return
	}
	var req SendRequest
	if !h.decode(w, r, credential, &req) {
		return
	}
	if verdict := validatePrompt(req.Prompt, true); verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}
	if len(req.PolicyID) > maxPolicyIDBytes {
		h.refuse(w, r, credential, refuse(http.StatusBadRequest, ReasonBadRequest, "policyId is too long"))
		return
	}
	// The opt-in is judged before the session is looked up, so a machine that
	// has not opted in reveals nothing about which ids exist.
	if verdict := judgeOptIn(h.settings, req.PolicyID); verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}

	ctx := r.Context()
	info, err := h.sessions.GetSessionInfo(ctx, id)
	if err != nil {
		h.refuse(w, r, credential, refuse(http.StatusNotFound, ReasonNotFound, "no such session here"))
		return
	}
	verdict := JudgeSend(h.settings, req.PolicyID, SendFacts{
		Archived:        info.ArchivedAt != "",
		WorktreeBranch:  info.WorktreeBranch,
		AutoApproveMode: info.AutoApproveMode,
	})
	if verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}
	if !h.limits.take(credential+":send", SendsPerMinute, time.Minute) {
		h.refuse(w, r, credential, refuse(http.StatusTooManyRequests, ReasonRate, "too many sends from this server in the last minute"))
		return
	}

	delivery, err := h.sessions.EnqueueMessageWithOrigin(ctx, id, req.Prompt, nil, session.QueryOrigin{
		Kind:     session.OriginAssistant,
		PolicyID: req.PolicyID,
	})
	if err != nil {
		httperror.RespondError(w, httperror.Internal("send to session", err))
		return
	}
	h.follow(ctx, id, credential, req.PolicyID)
	slog.Info("peer: sent", "session", id, "credential", credential, "delivery", delivery, "policy", req.PolicyID)
	httperror.JSON(w, http.StatusOK, SendResponse{Delivery: string(delivery)})
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	credential, ok := h.requirePeer(w, r)
	if !ok {
		return
	}
	var req CreateRequest
	if !h.decode(w, r, credential, &req) {
		return
	}
	if verdict := validateCreate(req); verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}
	if verdict := judgeOptIn(h.settings, req.PolicyID); verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}

	ctx := r.Context()
	list, err := h.sessions.ListAllSessions(ctx)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("list sessions", err))
		return
	}
	if verdict := JudgeCreate(h.settings, req.PolicyID, countInFlightAssistant(list.Sessions)); verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}

	projectID, verdict := h.resolveProject(ctx, req)
	if verdict.Refused() {
		h.refuse(w, r, credential, verdict)
		return
	}
	model, families, verdict := h.resolveModel(ctx, req.Model)
	if verdict.Refused() {
		h.refuseWithFamilies(w, r, credential, verdict, families)
		return
	}
	if !h.limits.take(credential+":create", CreatesPerHour, time.Hour) {
		h.refuse(w, r, credential, refuse(http.StatusTooManyRequests, ReasonRate, "too many sessions created by this server in the last hour"))
		return
	}

	params := session.CreateSessionParams{
		ProjectID:       projectID,
		Name:            strings.TrimSpace(req.Name),
		Model:           model,
		Worktree:        true,
		AutoApproveMode: fullAutoMode,
		Origin:          session.OriginAssistant,
	}
	if req.RequestID != "" {
		params.IdempotencyKey = "peer:" + credential + ":" + req.RequestID
	}
	created, err := h.sessions.CreateSession(ctx, params)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create session", err))
		return
	}

	out := CreateResponse{Session: SessionWire{
		ID: created.SessionID, ProjectID: projectID, Name: created.Name, State: created.State,
		Model: created.Model, WorktreeBranch: created.WorktreeBranch, AutoApproveMode: created.AutoApproveMode,
		Origin: session.OriginAssistant, CreatedAt: created.CreatedAt,
	}}
	h.follow(ctx, created.SessionID, credential, req.PolicyID)
	if req.Prompt != "" {
		h.sendAfterCreate(ctx, credential, created.SessionID, req, &out)
	}
	slog.Info("peer: created", "session", created.SessionID, "credential", credential, "policy", req.PolicyID)
	httperror.JSON(w, http.StatusOK, out)
}

// sendAfterCreate is the second half of a create that carried a prompt. Its
// failure is reported beside the session, never instead of it.
func (h *Handler) sendAfterCreate(ctx context.Context, credential, sessionID string, req CreateRequest, out *CreateResponse) {
	if !h.limits.take(credential+":send", SendsPerMinute, time.Minute) {
		out.SendReason = ReasonRate
		out.SendError = "the session was created, but too many sends came from this server in the last minute"
		return
	}
	delivery, err := h.sessions.EnqueueMessageWithOrigin(ctx, sessionID, req.Prompt, nil, session.QueryOrigin{
		Kind:     session.OriginAssistant,
		PolicyID: req.PolicyID,
	})
	if err != nil {
		slog.Warn("peer: send after create failed", "session", sessionID, "error", err)
		out.SendError = "the session was created, but the prompt could not be sent"
		return
	}
	out.Delivery = string(delivery)
}

func (h *Handler) resolveProject(ctx context.Context, req CreateRequest) (string, Refusal) {
	projects, err := h.projects.ListProjects(ctx)
	if err != nil {
		slog.Warn("peer: project list failed", "error", err)
		return "", refuse(http.StatusServiceUnavailable, ReasonNoProject, "this machine could not read its projects")
	}
	if req.ProjectID != "" {
		for _, p := range projects {
			if p.ID == req.ProjectID {
				return p.ID, Refusal{}
			}
		}
		return "", refuse(http.StatusNotFound, ReasonNoProject, "no such project here")
	}
	var match []store.Project
	for _, p := range projects {
		if p.RemoteUrl != "" && p.RemoteUrl == req.RemoteURL {
			match = append(match, p)
		}
	}
	switch len(match) {
	case 0:
		return "", refuse(http.StatusNotFound, ReasonNoProject, "that repository is not checked out here")
	case 1:
		return match[0].ID, Refusal{}
	default:
		return "", refuse(http.StatusConflict, ReasonAmbiguous, "that repository is checked out %d times here", len(match))
	}
}

func (h *Handler) resolveModel(ctx context.Context, spoken string) (string, []string, Refusal) {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return "", nil, Refusal{}
	}
	if h.catalog == nil {
		return "", nil, refuse(http.StatusUnprocessableEntity, ReasonUnknownModel, "this machine cannot resolve model names")
	}
	if model, ok := h.catalog.ResolveFamily(ctx, "claude", spoken); ok {
		return model.Slug, nil, Refusal{}
	}
	return "", h.catalog.FamilyNames(ctx, "claude"),
		refuse(http.StatusUnprocessableEntity, ReasonUnknownModel, "there is no model called %q here", spoken)
}

// requirePeer answers the credential's public id, or refuses.
func (h *Handler) requirePeer(w http.ResponseWriter, r *http.Request) (string, bool) {
	row := auth.UserFromContext(r.Context())
	if row == nil || row.Kind != auth.KindPeer || !row.ID.Valid || row.ID.String == "" {
		h.refuse(w, r, "", refuse(http.StatusForbidden, ReasonNotPeer, "this route needs a peer credential"))
		return "", false
	}
	return row.ID.String, true
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, credential string, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.refuse(w, r, credential, refuse(http.StatusBadRequest, ReasonBadRequest, "invalid request body"))
		return false
	}
	return true
}

// refuse writes a refusal and logs it, so a request that did nothing can be
// accounted for afterwards — the rule every refusal in the voice path follows.
func (h *Handler) refuse(w http.ResponseWriter, r *http.Request, credential string, verdict Refusal) {
	h.refuseWithFamilies(w, r, credential, verdict, nil)
}

func (h *Handler) refuseWithFamilies(w http.ResponseWriter, r *http.Request, credential string, verdict Refusal, families []string) {
	route := ""
	if r != nil {
		route = r.Method + " " + r.URL.Path
	}
	slog.Info("peer: refused", "reason", verdict.Reason, "route", route, "credential", credential)
	httperror.JSON(w, verdict.Status, ErrorResponse{Error: verdict.Message, Reason: verdict.Reason, Families: families})
}

func validatePrompt(prompt string, required bool) Refusal {
	if required && strings.TrimSpace(prompt) == "" {
		return refuse(http.StatusBadRequest, ReasonBadRequest, "prompt is empty")
	}
	if len(prompt) > maxPromptBytes {
		return refuse(http.StatusBadRequest, ReasonBadRequest, "prompt is longer than %d bytes", maxPromptBytes)
	}
	return Refusal{}
}

func validateCreate(req CreateRequest) Refusal {
	switch {
	case (req.ProjectID == "") == (req.RemoteURL == ""):
		return refuse(http.StatusBadRequest, ReasonBadRequest, "name the project by exactly one of projectId or remoteUrl")
	case req.ProjectID != "" && uuid.Validate(req.ProjectID) != nil:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "projectId must be a UUID")
	case len(req.RemoteURL) > 512:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "remoteUrl is too long")
	case len(req.Model) > maxModelBytes:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "model is too long")
	case utf8.RuneCountInString(req.Name) > maxSessionNameRunes:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "name is too long")
	case len(req.PolicyID) > maxPolicyIDBytes:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "policyId is too long")
	case len(req.RequestID) > maxRequestIDBytes:
		return refuse(http.StatusBadRequest, ReasonBadRequest, "requestId is too long")
	}
	return validatePrompt(req.Prompt, false)
}

// countInFlightAssistant counts assistant-origin sessions that are unfinished:
// not archived, not done, not failed. A PARKED session still counts, on the
// rule the assistant's own policy budget follows — a restart's reap must not
// hand the slots back while the work is still open.
func countInFlightAssistant(infos []session.SessionInfo) int {
	n := 0
	for _, info := range infos {
		if info.Origin != session.OriginAssistant || info.ArchivedAt != "" {
			continue
		}
		if info.State == "done" || info.State == "failed" {
			continue
		}
		n++
	}
	return n
}

func toSessionWire(info session.SessionInfo) SessionWire {
	out := SessionWire{
		ID:              info.ID,
		ProjectID:       info.ProjectID,
		Name:            info.Name,
		State:           info.State,
		Model:           info.Model,
		WorktreeBranch:  info.WorktreeBranch,
		ArchivedAt:      info.ArchivedAt,
		PendingApproval: info.PendingApproval != nil,
		PendingQuestion: info.PendingQuestion != nil,
		AutoApproveMode: info.AutoApproveMode,
		Origin:          info.Origin,
		LastQueryAt:     info.LastQueryAt,
		UpdatedAt:       info.UpdatedAt,
		CreatedAt:       info.CreatedAt,
	}
	if info.UnseenCompletedAt != nil {
		out.UnseenCompletedAt = *info.UnseenCompletedAt
	}
	return out
}
