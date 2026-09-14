package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// maxOrientationNames bounds how many sessions the orientation paragraph names.
// Past a few it stops being orientation and becomes a list read aloud.
const maxOrientationNames = 4

// assistantDirectory answers the assistant's questions about this machine's
// sessions, for every surface on it: the thread, a live call, and the head.
//
// It is the server-side half of assistant.Directory. That package must not
// import the session pipeline — it is the layer a thread, a call and a gateway
// all attach to — so this is where a SessionInfo becomes something sayable.
// Every method degrades to nothing rather than failing: a directory that cannot
// read the database makes the assistant vaguer, never mute.
type assistantDirectory struct {
	svc        *session.Service
	queries    *store.Queries
	summarizer *sessionSummarizer
	// catalog resolves a spoken model family to a slug. It is the same catalog
	// the picker renders, so a family the operator can choose on screen is one
	// they can ask for out loud, with no release in between.
	catalog *providers.Catalog

	// machineID is this host's identity, stamped on every local row so the
	// assistant can tell its own sessions from a snapshot's remote ones.
	machineID string
	// machineName is read per call rather than captured, so a rename takes
	// effect without a restart — the same rule the health endpoint follows.
	machineName func(ctx context.Context) string

	// peers reads the sessions of every paired machine. Nil means this
	// directory describes this machine alone, which is what a test builds.
	peers peerSource
	// link acts on paired machines (docs/peers.md). Nil means nothing here
	// reaches another machine, whatever peers lists.
	link peerActions
}

// peerActions is what the directory and the dispatcher ask of a paired
// machine's peer surface. *peerlink.Client implements it.
type peerActions interface {
	Create(ctx context.Context, machineID string, req peer.CreateRequest) (peer.CreateResponse, error)
	Send(ctx context.Context, machineID, sessionID string, req peer.SendRequest) (peer.SendResponse, error)
	Follow(ctx context.Context, machineID, sessionID string) error
	Transcript(ctx context.Context, machineID, sessionID string) (string, error)
}

func newAssistantDirectory(svc *session.Service, queries *store.Queries, summarizer *sessionSummarizer,
	catalog *providers.Catalog, machineID string, machineName func(ctx context.Context) string,
) *assistantDirectory {
	return &assistantDirectory{
		svc:         svc,
		queries:     queries,
		summarizer:  summarizer,
		catalog:     catalog,
		machineID:   machineID,
		machineName: machineName,
	}
}

// Orientation implements assistant.Directory: one paragraph, spoken material.
//
// It covers every machine the operator has paired, because "what is going on"
// is a question about their work, not about which box answers it.
func (d *assistantDirectory) Orientation(ctx context.Context) string {
	rows, unreachable := d.everyRow(ctx)
	if len(rows) == 0 {
		return "There are no sessions on this machine yet." + unreachableSentence(unreachable)
	}

	var waiting, running []assistant.SessionRow
	remote := 0
	for _, row := range rows {
		if row.MachineID != d.machineID {
			remote++
		}
		if row.Attention != "" {
			waiting = append(waiting, row)
			continue
		}
		if row.State == string(session.StateRunning) {
			running = append(running, row)
		}
	}

	var b strings.Builder
	switch {
	case len(rows) == 1 && remote == 1:
		fmt.Fprintf(&b, "There is one session, on %s", rows[0].MachineName)
	case len(rows) == 1:
		b.WriteString("There is one session on this machine")
	case remote == 0:
		fmt.Fprintf(&b, "There are %d sessions on this machine", len(rows))
	default:
		fmt.Fprintf(&b, "There are %d sessions across your machines, %d of them on paired ones", len(rows), remote)
	}
	switch {
	case len(running) == 1:
		b.WriteString(", one running")
	case len(running) > 1:
		fmt.Fprintf(&b, ", %d running", len(running))
	}
	b.WriteString(".")

	switch len(waiting) {
	case 0:
		b.WriteString(" None of them are waiting on the operator.")
	case 1:
		fmt.Fprintf(&b, " One is waiting on the operator: %s.", namesWithReason(waiting))
	default:
		fmt.Fprintf(&b, " %d are waiting on the operator: %s.", len(waiting), namesWithReason(waiting))
	}
	b.WriteString(unreachableSentence(unreachable))
	return b.String()
}

// unreachableSentence says which paired machines are missing from an answer,
// or nothing when none are.
func unreachableSentence(machines []string) string {
	if len(machines) == 0 {
		return ""
	}
	return fmt.Sprintf(" %s did not answer, so nothing on it is included.", assistant.SpokenList(machines))
}

// ListSessions implements assistant.Directory.
//
// Uncut. Every caller bounds what it says, and every caller also MATCHES over
// what it is given — find_session, a voice target check — so a cut here made
// the thirteenth session unfindable rather than merely unlisted.
//
// Paired machines' sessions are in it. They are description only:
// [assistantDirectory.SessionBrief] never answers for one, and that is the test
// every verb that acts on a session applies.
func (d *assistantDirectory) ListSessions(ctx context.Context, filter string) []assistant.SessionRow {
	rows, _ := d.everyRow(ctx)
	kept := rows[:0]
	for _, row := range rows {
		if keepForFilter(row, filter) {
			kept = append(kept, row)
		}
	}
	return kept
}

// UnreachableMachines implements assistant.PeerReachability: the paired
// machines whose sessions the lists above could not include.
func (d *assistantDirectory) UnreachableMachines(ctx context.Context) []string {
	if d.peers == nil {
		return nil
	}
	return d.peers.View(ctx).Unreachable
}

// SessionBrief implements assistant.Directory. The false return is what "this
// session is not ours" looks like, which is the test for whether work can be
// started in it from this call.
func (d *assistantDirectory) SessionBrief(ctx context.Context, id string) (assistant.SessionRow, bool) {
	if id == "" {
		return assistant.SessionRow{}, false
	}
	info, err := d.svc.GetSessionInfo(ctx, id)
	if err != nil {
		return assistant.SessionRow{}, false
	}
	projects := d.projects(ctx)
	return d.toRow(ctx, info, projects), true
}

// Locate implements assistant.Locator: this machine's own session first, then a
// paired machine's, with the reach that machine allows.
func (d *assistantDirectory) Locate(ctx context.Context, id string) (assistant.SessionRow, bool) {
	if row, local := d.SessionBrief(ctx, id); local {
		return row, true
	}
	if d.peers == nil {
		return assistant.SessionRow{}, false
	}
	loc, ok := d.peers.Locate(ctx, id)
	if !ok {
		return assistant.SessionRow{}, false
	}
	return loc.Row, true
}

// Unfinished implements assistant.PeerSessionStates. A session its machine no
// longer lists is gone, which is finished; a machine that did not answer is
// unknown, which a budget counts as in flight.
func (d *assistantDirectory) Unfinished(ctx context.Context, machineID, sessionID string) (bool, bool) {
	if d.peers == nil {
		return false, false
	}
	s, listed, answered := d.peers.SessionState(ctx, machineID, sessionID)
	if !answered {
		return false, false
	}
	if !listed {
		return false, true
	}
	return s.ArchivedAt == "" && s.State != string(session.StateDone) && s.State != string(session.StateFailed), true
}

// FollowRemote implements assistant.RemoteFollower.
func (d *assistantDirectory) FollowRemote(ctx context.Context, machineID, sessionID string) error {
	if d.link == nil {
		return errors.New("no peer link")
	}
	return d.link.Follow(ctx, machineID, sessionID)
}

// Summarize implements assistant.Directory.
//
// It runs on its own goroutine with a detached context: the caller is a tool
// handler that has already answered, and the request that opened the call is
// long gone. deliver is called exactly once, whatever happens.
//
// A paired machine's session is summarised HERE, from the transcript that
// machine serves: the owner need not run a summariser, and the paragraph is
// written by the same prompt and model as a local one.
func (d *assistantDirectory) Summarize(ctx context.Context, id string, deliver func(summary string)) {
	if deliver == nil {
		return
	}
	if d.summarizer == nil || id == "" {
		deliver("")
		return
	}
	detached := context.WithoutCancel(ctx)
	if _, local := d.SessionBrief(ctx, id); local || d.peers == nil || d.link == nil {
		go deliver(d.summarizer.Summary(detached, id))
		return
	}
	go func() {
		loc, ok := d.peers.Locate(detached, id)
		if !ok {
			deliver("")
			return
		}
		transcript, err := d.link.Transcript(detached, loc.Machine.MachineID, id)
		if err != nil {
			slog.Warn("assistant directory: remote transcript unavailable", "session", id,
				"machine", loc.Machine.MachineID, "error", err)
			deliver("")
			return
		}
		deliver(d.summarizer.SummaryOfTranscript(detached, loc.Machine.MachineID+":"+id, transcript))
	}()
}

// ListProjects implements assistant.Directory: this machine's projects and every
// paired machine's, most recently worked in first, uncut for the reason
// [assistantDirectory.ListSessions] is — list_projects narrows by name over
// this list.
//
// A repository checked out on two machines is two rows, each with its machine:
// launching is physical (docs/multi-machine.md). A paired machine's row says
// whether it accepts new sessions from here ([assistant.Reach]); the verbs
// refuse the ones that do not rather than this list hiding them, because
// "that is on zbook, which does not take work" is an answer and silence is not.
func (d *assistantDirectory) ListProjects(ctx context.Context) []assistant.ProjectRow {
	list, err := d.queries.ListProjects(ctx)
	if err != nil {
		slog.Warn("assistant directory: project list failed", "error", err)
		return nil
	}

	// "Recent" means work, not metadata: a project's own updated_at moves when
	// it is renamed, which is not what the operator means by the one they were
	// just in. The sessions already read for every other answer are what say so.
	lastWork := d.lastWorkByProject(ctx)

	machineName := ""
	if d.machineName != nil {
		machineName = d.machineName(ctx)
	}
	rows := make([]assistant.ProjectRow, 0, len(list))
	for _, project := range list {
		rows = append(rows, assistant.ProjectRow{
			ID:              project.ID,
			Name:            project.Name,
			Slug:            project.Slug,
			LastActivity:    lastWork[project.ID],
			MachineID:       d.machineID,
			MachineName:     machineName,
			RemoteURL:       project.RemoteUrl,
			Reach:           assistant.ReachLocal,
			AcceptsPolicies: true,
		})
	}
	if d.peers != nil {
		rows = append(rows, d.peers.Projects(ctx)...)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastActivity > rows[j].LastActivity
	})
	return rows
}

// CreateSession implements assistant.Directory.
//
// One route in: this is the same [session.Service.CreateSession] the composer's
// new-session flow reaches through the `session.create` WS handler, with the
// same worktree default. A second creation path would be a second set of rules
// about worktrees, quotas and idempotency to drift apart.
//
// The session is born **fullAuto** deliberately. Live voice has no spoken
// approval — you cannot approve what you cannot see, on a transcription — so a
// session created any other way would be refused at its own first dispatch,
// which is a worse outcome than the default: an empty session nobody can use
// from the call that made it. The consent gate is not weakened by this, because
// it was never the session's mode: it is the prompt, read back and agreed to
// out loud, and that read-back now names the new session too.
func (d *assistantDirectory) CreateSession(ctx context.Context, projectID, model string) (assistant.SessionRow, error) {
	if projectID == "" {
		return assistant.SessionRow{}, errors.New("no project")
	}
	if _, err := d.queries.GetProject(ctx, projectID); err != nil && d.peers != nil {
		if remote, ok := d.peerProject(ctx, projectID); ok {
			return d.createRemote(ctx, remote, model)
		}
	}

	slug, family, err := d.resolveSpokenModel(ctx, model)
	if err != nil {
		return assistant.SessionRow{}, err
	}

	proj, err := d.queries.GetProject(ctx, projectID)
	if err != nil {
		return assistant.SessionRow{}, fmt.Errorf("get project %q: %w", projectID, err)
	}

	result, err := d.svc.CreateSession(ctx, session.CreateSessionParams{
		ProjectID: projectID,
		Model:     slug,
		// The composer's default, so a session started by voice is the same
		// thing as one started on screen: a worktree where the project is a
		// repository, the folder itself where it is not.
		Worktree:        project.KindOf(proj.Path) == project.KindGit,
		AutoApproveMode: "fullAuto",
		// Where the work came from, carried INTO creation rather than stamped
		// after it: the row is pushed to every open client as part of being
		// created, so a mark applied a moment later would miss the only
		// announcement the session ever gets.
		Origin: session.OriginAssistant,
	})
	if err != nil {
		return assistant.SessionRow{}, fmt.Errorf("create session in %q: %w", projectID, err)
	}

	row := assistant.SessionRow{
		ID:              result.SessionID,
		Name:            result.Name,
		MachineID:       d.machineID,
		State:           result.State,
		Branch:          result.WorktreeBranch,
		Model:           family,
		LastActivity:    result.CreatedAt,
		Reach:           assistant.ReachLocal,
		AcceptsPolicies: true,
	}
	if d.machineName != nil {
		row.MachineName = d.machineName(ctx)
	}
	if project, err := d.queries.GetProject(ctx, projectID); err == nil {
		row.ProjectName = project.Name
		row.ProjectSlug = project.Slug
	}
	if row.Model == "" {
		row.Model = providers.ModelFamilyName(result.Model)
	}
	return row, nil
}

// peerProject finds a paired machine's project by the owner's id.
func (d *assistantDirectory) peerProject(ctx context.Context, projectID string) (assistant.ProjectRow, bool) {
	for _, row := range d.peers.Projects(ctx) {
		if row.ID == projectID {
			return row, true
		}
	}
	return assistant.ProjectRow{}, false
}

// createRemote creates a session in a paired machine's project through its peer
// surface. The model stays a spoken family name: the owner resolves it against
// its own catalog, which is the one that decides what it can run.
//
// The prompt, if any, is sent afterwards by the caller through the dispatcher,
// the same two steps a local creation takes, so the refusal a send can meet is
// the same one whichever machine the session is on.
func (d *assistantDirectory) createRemote(ctx context.Context, project assistant.ProjectRow, model string) (assistant.SessionRow, error) {
	if d.link == nil {
		return assistant.SessionRow{}, &assistant.RefusedError{Reason: "no-peer-link",
			Say: "this server cannot act on other machines"}
	}
	if !project.Reach.CanAct() {
		return assistant.SessionRow{}, &assistant.RefusedError{Reason: string(project.Reach),
			Say: project.MachineName + " does not accept new sessions from this server"}
	}
	created, err := d.link.Create(ctx, project.MachineID, peer.CreateRequest{
		ProjectID: project.ID,
		Model:     strings.TrimSpace(model),
		RequestID: uuid.NewString(),
	})
	if err != nil {
		err = peerError(err, project.MachineName)
		var unknown *assistant.UnknownModelError
		if errors.As(err, &unknown) {
			unknown.Spoken = strings.TrimSpace(model)
		}
		return assistant.SessionRow{}, err
	}

	row := assistant.SessionRow{
		ID:              created.Session.ID,
		Name:            created.Session.Name,
		ProjectName:     project.Name,
		ProjectSlug:     project.Slug,
		MachineID:       project.MachineID,
		MachineName:     project.MachineName,
		State:           created.Session.State,
		Branch:          created.Session.WorktreeBranch,
		Model:           providers.ModelFamilyName(created.Session.Model),
		LastActivity:    created.Session.CreatedAt,
		Reach:           project.Reach,
		AcceptsPolicies: project.AcceptsPolicies,
	}
	if m, err := d.queries.GetMachine(ctx, project.MachineID); err == nil {
		d.peers.Remember(peerLocation{Machine: m, Session: created.Session, Row: row})
	}
	return row, nil
}

// peerError turns a peer client failure into the assistant's vocabulary: an
// unknown model is the question it already is locally, an owner's refusal is a
// sentence to relay, and anything else is an error.
func peerError(err error, machineName string) error {
	var refusal *peerlink.RefusalError
	if !errors.As(err, &refusal) {
		if errors.Is(err, peerlink.ErrNoPeerSurface) {
			return &assistant.RefusedError{Reason: "peer-old", Say: machineName + " runs an older release that cannot take this"}
		}
		return err
	}
	if refusal.Reason == peer.ReasonUnknownModel {
		return &assistant.UnknownModelError{Families: refusal.Families}
	}
	return &assistant.RefusedError{Reason: refusal.Reason, Say: machineName + " said " + refusal.Message}
}

// resolveSpokenModel turns a spoken family name into a slug to create with, and
// the label to say back.
//
// Empty means the default, which is whatever the service gives the composer —
// resolving one here would be a second copy of that decision. Anything else
// must be a family the catalog actually lists: guessing at a model id is the
// one mistake in this flow the operator cannot see happening.
func (d *assistantDirectory) resolveSpokenModel(ctx context.Context, spoken string) (slug, family string, err error) {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return "", "", nil
	}
	if d.catalog == nil {
		return "", "", &assistant.UnknownModelError{Spoken: spoken}
	}
	if model, ok := d.catalog.ResolveFamily(ctx, "claude", spoken); ok {
		return model.Slug, model.DisplayName, nil
	}
	return "", "", &assistant.UnknownModelError{
		Spoken:   spoken,
		Families: d.catalog.FamilyNames(ctx, "claude"),
	}
}

// lastWorkByProject is when each project was last actually worked in.
func (d *assistantDirectory) lastWorkByProject(ctx context.Context) map[string]string {
	result, err := d.svc.ListAllSessions(ctx)
	if err != nil {
		slog.Warn("assistant directory: session list failed", "error", err)
		return nil
	}
	out := make(map[string]string, len(result.Sessions))
	for _, info := range result.Sessions {
		at := firstNonEmptyOf(info.LastQueryAt, info.UpdatedAt, info.CreatedAt)
		if at > out[info.ProjectID] {
			out[info.ProjectID] = at
		}
	}
	return out
}

// everyRow is this machine's sessions and every paired machine's, most urgent
// then newest first, with the machines that did not answer.
//
// A row is kept once, and this machine's copy wins: its database is the truth
// about its own sessions, where a peer's answer is a description of its own.
func (d *assistantDirectory) everyRow(ctx context.Context) ([]assistant.SessionRow, []string) {
	rows := d.rows(ctx)
	if d.peers == nil {
		return rows, nil
	}
	view := d.peers.View(ctx)
	return mergeRows(rows, view.Rows), view.Unreachable
}

// mergeRows appends the peer rows local does not already hold, and sorts.
func mergeRows(local, peer []assistant.SessionRow) []assistant.SessionRow {
	seen := make(map[string]bool, len(local))
	for _, row := range local {
		seen[row.ID] = true
	}
	for _, row := range peer {
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		local = append(local, row)
	}
	sortRows(local)
	return local
}

// sortRows orders rows the deck's way: attention first, then recency.
func sortRows(rows []assistant.SessionRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := assistant.AttentionRank(rows[i].Attention), assistant.AttentionRank(rows[j].Attention)
		if a != b {
			return a < b
		}
		return rows[i].LastActivity > rows[j].LastActivity
	})
}

// rows reads every live session on this machine, newest activity first.
func (d *assistantDirectory) rows(ctx context.Context) []assistant.SessionRow {
	result, err := d.svc.ListAllSessions(ctx)
	if err != nil {
		slog.Warn("assistant directory: session list failed", "error", err)
		return nil
	}

	projects := d.projects(ctx)
	rows := make([]assistant.SessionRow, 0, len(result.Sessions))
	for _, info := range result.Sessions {
		// Archived is the operator filing a session away. It is not part of the
		// picture they are asking about, and it is the one section the UI itself
		// collapses by construction.
		if info.ArchivedAt != "" {
			continue
		}
		rows = append(rows, d.toRow(ctx, info, projects))
	}

	sortRows(rows)
	return rows
}

// toRow turns one SessionInfo into something speakable.
func (d *assistantDirectory) toRow(ctx context.Context, info session.SessionInfo, projects map[string]store.Project) assistant.SessionRow {
	row := assistant.SessionRow{
		Reach:           assistant.ReachLocal,
		AcceptsPolicies: true,
		ID:        info.ID,
		Name:      info.Name,
		MachineID: d.machineID,
		State:     info.State,
		Attention: attentionOf(info),
		Branch:    info.WorktreeBranch,
		// The family name, never the version: that is the vocabulary the
		// operator chose in and the one they will hear back.
		Model:        providers.ModelFamilyName(info.Model),
		LastActivity: firstNonEmptyOf(info.LastQueryAt, info.UpdatedAt, info.CreatedAt),
	}
	if d.machineName != nil {
		row.MachineName = d.machineName(ctx)
	}
	if project, ok := projects[info.ProjectID]; ok {
		row.ProjectName = project.Name
		row.ProjectSlug = project.Slug
		// The id comes from the project row rather than the session's own field, so
		// it is set only where this machine actually holds that project — which is
		// what makes it safe to file a memory scope under.
		row.ProjectID = project.ID
	}
	return row
}

// projects loads the project rows once per answer, so a list of twenty sessions
// is one query rather than twenty.
func (d *assistantDirectory) projects(ctx context.Context) map[string]store.Project {
	list, err := d.queries.ListProjects(ctx)
	if err != nil {
		slog.Warn("assistant directory: project list failed", "error", err)
		return nil
	}
	byID := make(map[string]store.Project, len(list))
	for _, project := range list {
		byID[project.ID] = project
	}
	return byID
}

// attentionOf says why a session is waiting on the operator, in the deck's
// vocabulary and in its order: the two that hold a process outrank the one that
// does not.
func attentionOf(info session.SessionInfo) string {
	if info.PendingApproval != nil {
		return assistant.AttentionApproval
	}
	if info.PendingQuestion != nil {
		return assistant.AttentionQuestion
	}
	// Unread mirrors the deck's rule (use-deck-rows / needs-you): a completion
	// nobody has looked at counts only once the run has actually stopped.
	if info.UnseenCompletedAt != nil && info.State != string(session.StateRunning) {
		return assistant.AttentionUnread
	}
	return ""
}

// keepForFilter applies one of the four filters. An unknown filter keeps
// everything recent rather than nothing: a mis-transcribed word must not turn
// into an empty answer.
func keepForFilter(row assistant.SessionRow, filter string) bool {
	switch filter {
	case assistant.FilterNeedsAttention:
		return row.Attention != ""
	case assistant.FilterRunning:
		return row.State == string(session.StateRunning)
	default:
		return true
	}
}

// namesWithReason renders the waiting sessions as speech: a few names, each
// with what it is waiting for, and a count for the rest.
func namesWithReason(rows []assistant.SessionRow) string {
	named := rows
	var extra int
	if len(named) > maxOrientationNames {
		extra = len(named) - maxOrientationNames
		named = named[:maxOrientationNames]
	}

	parts := make([]string, 0, len(named)+1)
	for _, row := range named {
		parts = append(parts, fmt.Sprintf("%q (%s)", displayName(row), attentionWords(row.Attention)))
	}
	if extra > 0 {
		parts = append(parts, fmt.Sprintf("and %d more", extra))
	}
	return strings.Join(parts, ", ")
}

// attentionWords says a reason the way a person would.
func attentionWords(attention string) string {
	switch attention {
	case assistant.AttentionApproval:
		return "needs approval"
	case assistant.AttentionQuestion:
		return "asked a question"
	case assistant.AttentionUnread:
		return "finished, unread"
	default:
		return "waiting"
	}
}

// displayName is what to call a session out loud. An unnamed session still gets
// something sayable, since its id is not.
func displayName(row assistant.SessionRow) string {
	if row.Name != "" {
		return row.Name
	}
	if row.ProjectName != "" {
		return "an unnamed session in " + row.ProjectName
	}
	return "an unnamed session"
}
