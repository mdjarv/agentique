package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/voice"
)

// maxDirectoryRows bounds what one directory answer carries.
//
// Everything here is read aloud or fed to the speech model, and a list of forty
// sessions is neither. The rows are sorted before the cut, so what survives is
// what the operator most likely meant.
const maxDirectoryRows = 12

// maxOrientationNames bounds how many sessions the call-open paragraph names.
// Past a few it stops being orientation and becomes a list read aloud.
const maxOrientationNames = 4

// voiceDirectory answers the voice assistant's questions about this machine's
// sessions.
//
// It is the server-side half of voice.Directory: the voice package must not
// import the session pipeline, so this is where a SessionInfo becomes something
// speakable. Every method degrades to nothing rather than failing — a directory
// that cannot read the database makes the assistant vaguer, never mute.
type voiceDirectory struct {
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
}

func newVoiceDirectory(svc *session.Service, queries *store.Queries, summarizer *sessionSummarizer,
	catalog *providers.Catalog, machineID string, machineName func(ctx context.Context) string,
) *voiceDirectory {
	return &voiceDirectory{
		svc:         svc,
		queries:     queries,
		summarizer:  summarizer,
		catalog:     catalog,
		machineID:   machineID,
		machineName: machineName,
	}
}

// Orientation implements voice.Directory: one paragraph, spoken material.
func (d *voiceDirectory) Orientation(ctx context.Context) string {
	rows := d.rows(ctx)
	if len(rows) == 0 {
		return "There are no sessions on this machine yet."
	}

	var waiting, running []voice.SessionRow
	for _, row := range rows {
		if row.Attention != "" {
			waiting = append(waiting, row)
			continue
		}
		if row.State == string(session.StateRunning) {
			running = append(running, row)
		}
	}

	var b strings.Builder
	if len(rows) == 1 {
		b.WriteString("There is one session on this machine")
	} else {
		fmt.Fprintf(&b, "There are %d sessions on this machine", len(rows))
	}
	switch {
	case len(running) == 1:
		b.WriteString(", one of them running")
	case len(running) > 1:
		fmt.Fprintf(&b, ", %d of them running", len(running))
	}
	b.WriteString(".")

	if len(waiting) == 0 {
		b.WriteString(" None of them are waiting on the operator.")
		return b.String()
	}
	if len(waiting) == 1 {
		fmt.Fprintf(&b, " One is waiting on the operator: %s.", namesWithReason(waiting))
		return b.String()
	}
	fmt.Fprintf(&b, " %d are waiting on the operator: %s.", len(waiting), namesWithReason(waiting))
	return b.String()
}

// ListSessions implements voice.Directory.
func (d *voiceDirectory) ListSessions(ctx context.Context, filter string) []voice.SessionRow {
	rows := d.rows(ctx)
	kept := rows[:0]
	for _, row := range rows {
		if keepForFilter(row, filter) {
			kept = append(kept, row)
		}
	}
	if len(kept) > maxDirectoryRows {
		kept = kept[:maxDirectoryRows]
	}
	return kept
}

// SessionBrief implements voice.Directory. The false return is what "this
// session is not ours" looks like, which is the test for whether work can be
// started in it from this call.
func (d *voiceDirectory) SessionBrief(ctx context.Context, id string) (voice.SessionRow, bool) {
	if id == "" {
		return voice.SessionRow{}, false
	}
	info, err := d.svc.GetSessionInfo(ctx, id)
	if err != nil {
		return voice.SessionRow{}, false
	}
	projects := d.projects(ctx)
	return d.toRow(ctx, info, projects), true
}

// Summarize implements voice.Directory.
//
// It runs on its own goroutine with a detached context: the caller is a tool
// handler that has already answered, and the request that opened the call is
// long gone. deliver is called exactly once, whatever happens.
func (d *voiceDirectory) Summarize(ctx context.Context, id string, deliver func(summary string)) {
	if deliver == nil {
		return
	}
	if d.summarizer == nil || id == "" {
		deliver("")
		return
	}
	detached := context.WithoutCancel(ctx)
	go deliver(d.summarizer.Summary(detached, id))
}

// ListProjects implements voice.Directory: this machine's projects, most
// recently worked in first.
//
// Local only, and that is the point rather than a limitation. A project row
// here is somewhere [voiceDirectory.CreateSession] can actually put a session,
// and creation goes through this server's session service — a repository
// checked out on a paired machine is not one of those places.
func (d *voiceDirectory) ListProjects(ctx context.Context) []voice.ProjectRow {
	list, err := d.queries.ListProjects(ctx)
	if err != nil {
		slog.Warn("voice directory: project list failed", "error", err)
		return nil
	}

	// "Recent" means work, not metadata: a project's own updated_at moves when
	// it is renamed, which is not what the operator means by the one they were
	// just in. The sessions already read for every other answer are what say so.
	lastWork := d.lastWorkByProject(ctx)

	rows := make([]voice.ProjectRow, 0, len(list))
	for _, project := range list {
		rows = append(rows, voice.ProjectRow{
			ID:           project.ID,
			Name:         project.Name,
			Slug:         project.Slug,
			LastActivity: lastWork[project.ID],
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastActivity > rows[j].LastActivity
	})
	if len(rows) > maxDirectoryRows {
		rows = rows[:maxDirectoryRows]
	}
	return rows
}

// CreateSession implements voice.Directory.
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
func (d *voiceDirectory) CreateSession(ctx context.Context, projectID, model string) (voice.SessionRow, error) {
	if projectID == "" {
		return voice.SessionRow{}, errors.New("no project")
	}

	slug, family, err := d.resolveSpokenModel(ctx, model)
	if err != nil {
		return voice.SessionRow{}, err
	}

	result, err := d.svc.CreateSession(ctx, session.CreateSessionParams{
		ProjectID: projectID,
		Model:     slug,
		// The composer's default, so a session started by voice is the same
		// thing as one started on screen.
		Worktree:        true,
		AutoApproveMode: "fullAuto",
	})
	if err != nil {
		return voice.SessionRow{}, fmt.Errorf("create session in %q: %w", projectID, err)
	}

	row := voice.SessionRow{
		ID:           result.SessionID,
		Name:         result.Name,
		MachineID:    d.machineID,
		State:        result.State,
		Branch:       result.WorktreeBranch,
		Model:        family,
		LastActivity: result.CreatedAt,
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

// resolveSpokenModel turns a spoken family name into a slug to create with, and
// the label to say back.
//
// Empty means the default, which is whatever the service gives the composer —
// resolving one here would be a second copy of that decision. Anything else
// must be a family the catalog actually lists: guessing at a model id is the
// one mistake in this flow the operator cannot see happening.
func (d *voiceDirectory) resolveSpokenModel(ctx context.Context, spoken string) (slug, family string, err error) {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return "", "", nil
	}
	if d.catalog == nil {
		return "", "", &voice.UnknownModelError{Spoken: spoken}
	}
	if model, ok := d.catalog.ResolveFamily(ctx, "claude", spoken); ok {
		return model.Slug, model.DisplayName, nil
	}
	return "", "", &voice.UnknownModelError{
		Spoken:   spoken,
		Families: d.catalog.FamilyNames(ctx, "claude"),
	}
}

// lastWorkByProject is when each project was last actually worked in.
func (d *voiceDirectory) lastWorkByProject(ctx context.Context) map[string]string {
	result, err := d.svc.ListAllSessions(ctx)
	if err != nil {
		slog.Warn("voice directory: session list failed", "error", err)
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

// rows reads every live session on this machine, newest activity first.
func (d *voiceDirectory) rows(ctx context.Context) []voice.SessionRow {
	result, err := d.svc.ListAllSessions(ctx)
	if err != nil {
		slog.Warn("voice directory: session list failed", "error", err)
		return nil
	}

	projects := d.projects(ctx)
	rows := make([]voice.SessionRow, 0, len(result.Sessions))
	for _, info := range result.Sessions {
		// Archived is the operator filing a session away. It is not part of the
		// picture they are asking about, and it is the one section the UI itself
		// collapses by construction.
		if info.ArchivedAt != "" {
			continue
		}
		rows = append(rows, d.toRow(ctx, info, projects))
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := voice.AttentionRank(rows[i].Attention), voice.AttentionRank(rows[j].Attention)
		if a != b {
			return a < b
		}
		return rows[i].LastActivity > rows[j].LastActivity
	})
	return rows
}

// toRow turns one SessionInfo into something speakable.
func (d *voiceDirectory) toRow(ctx context.Context, info session.SessionInfo, projects map[string]store.Project) voice.SessionRow {
	row := voice.SessionRow{
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
	}
	return row
}

// projects loads the project rows once per answer, so a list of twenty sessions
// is one query rather than twenty.
func (d *voiceDirectory) projects(ctx context.Context) map[string]store.Project {
	list, err := d.queries.ListProjects(ctx)
	if err != nil {
		slog.Warn("voice directory: project list failed", "error", err)
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
		return voice.AttentionApproval
	}
	if info.PendingQuestion != nil {
		return voice.AttentionQuestion
	}
	// Unread mirrors the deck's rule (use-deck-rows / needs-you): a completion
	// nobody has looked at counts only once the run has actually stopped.
	if info.UnseenCompletedAt != nil && info.State != string(session.StateRunning) {
		return voice.AttentionUnread
	}
	return ""
}

// keepForFilter applies one of the four filters. An unknown filter keeps
// everything recent rather than nothing: a mis-transcribed word must not turn
// into an empty answer.
func keepForFilter(row voice.SessionRow, filter string) bool {
	switch filter {
	case voice.FilterNeedsAttention:
		return row.Attention != ""
	case voice.FilterRunning:
		return row.State == string(session.StateRunning)
	default:
		return true
	}
}

// namesWithReason renders the waiting sessions as speech: a few names, each
// with what it is waiting for, and a count for the rest.
func namesWithReason(rows []voice.SessionRow) string {
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
	case voice.AttentionApproval:
		return "needs approval"
	case voice.AttentionQuestion:
		return "asked a question"
	case voice.AttentionUnread:
		return "finished, unread"
	default:
		return "waiting"
	}
}

// displayName is what to call a session out loud. An unnamed session still gets
// something sayable, since its id is not.
func displayName(row voice.SessionRow) string {
	if row.Name != "" {
		return row.Name
	}
	if row.ProjectName != "" {
		return "an unnamed session in " + row.ProjectName
	}
	return "an unnamed session"
}
