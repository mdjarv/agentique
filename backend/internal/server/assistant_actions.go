package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/storage"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// assistantActions performs an accepted proposal, and answers the facts one is
// judged on.
//
// It implements assistant.Actions, the seam that keeps internal/assistant off
// the session pipeline. Every executor here goes through the SAME service the
// WS ops call — `GitService.Merge`, `Service.ArchiveSession`,
// `Service.DeleteSession` — so a yes on a card and a click in the UI are one
// route, with one set of locks, one broadcast and one set of guards. Nothing in
// this file re-implements a rule; where it maps, it maps a vocabulary.
//
// It is reached only from [assistant.Service.Decide], on an explicit yes. The
// facts half is reached twice per proposal — once to write the card and once to
// check it is still true — so no method here has a side effect of its own.
// The core's interface is the contract, asserted here so a method renamed on
// either side is a compile error in this file rather than a nil executor in the
// verb table.
var _ assistant.Actions = (*assistantActions)(nil)

type assistantActions struct {
	svc     *session.Service
	gitSvc  *session.GitService
	mgr     *session.Manager
	queries *store.Queries
	catalog *providers.Catalog
	// probe answers storage's two git questions. Injected so a test can judge
	// the verdict mapping without a repository on disk.
	probe storage.SafetyProbe
}

func newAssistantActions(svc *session.Service, gitSvc *session.GitService, mgr *session.Manager,
	queries *store.Queries, catalog *providers.Catalog,
) *assistantActions {
	return &assistantActions{
		svc:     svc,
		gitSvc:  gitSvc,
		mgr:     mgr,
		queries: queries,
		catalog: catalog,
		probe:   storage.RealSafetyProbe(),
	}
}

// Merge implements assistant.Actions, in mode `merge`.
//
// Mode `merge` and not `complete` or `delete`: the proposal said "merge this
// branch", and archiving or deleting on the back of it would perform two
// uncontained verbs on one yes. Each has its own card.
func (a *assistantActions) Merge(ctx context.Context, sessionID string) (string, error) {
	result, err := a.gitSvc.Merge(ctx, sessionID, session.MergeModeMerge)
	if err != nil {
		return "", gitOpError("merge", err)
	}
	switch result.Status {
	case "merged":
		commit := result.CommitHash
		if len(commit) > 8 {
			commit = commit[:8]
		}
		if commit == "" {
			return "merged into the project's branch", nil
		}
		return "merged into the project's branch as " + commit, nil
	case "conflict":
		return "", &assistant.OutcomeError{
			Outcome: fmt.Sprintf("conflict -- it would conflict in %s, and the merge was undone",
				files(len(result.ConflictFiles))),
			Detail: strings.Join(result.ConflictFiles, ", "),
		}
	case "needs_rebase":
		return "", &assistant.OutcomeError{
			Outcome: "needs_rebase -- the project's branch has moved on, so this needs a rebase first",
		}
	case "dirty_worktree":
		return "", &assistant.OutcomeError{
			Outcome: "dirty_worktree -- the project's own checkout has uncommitted changes",
			Detail:  result.Error,
		}
	default:
		return "", &assistant.OutcomeError{
			Outcome: "git refused it, and nothing was merged",
			Detail:  result.Status + ": " + result.Error,
		}
	}
}

// Rebase implements assistant.Actions.
func (a *assistantActions) Rebase(ctx context.Context, sessionID string) (string, error) {
	result, err := a.gitSvc.Rebase(ctx, sessionID)
	if err != nil {
		return "", gitOpError("rebase", err)
	}
	switch result.Status {
	case "rebased":
		// Said here rather than nowhere: the rebase auto-commits the worktree
		// server-side before replaying, which is invisible everywhere else.
		return "rebased onto the project's branch, committing what was uncommitted first", nil
	case "conflict":
		return "", &assistant.OutcomeError{
			Outcome: fmt.Sprintf("conflict -- the replay conflicts in %s, and the rebase was undone",
				files(len(result.ConflictFiles))),
			Detail: strings.Join(result.ConflictFiles, ", "),
		}
	default:
		return "", &assistant.OutcomeError{
			Outcome: "git refused it, and nothing was rebased",
			Detail:  result.Status + ": " + result.Error,
		}
	}
}

// Archive implements assistant.Actions.
func (a *assistantActions) Archive(ctx context.Context, sessionID string) error {
	if err := a.svc.ArchiveSession(ctx, sessionID); err != nil {
		if errors.Is(err, session.ErrBusy) {
			return &assistant.OutcomeError{
				Outcome: "it started a turn in the meantime, and archiving is refused while one is " +
					"in flight",
				Detail: err.Error(),
			}
		}
		return err
	}
	return nil
}

// Delete implements assistant.Actions.
//
// Through Service.DeleteSession, which recurses into a lead's workers: the raw
// query is a single-row delete and leaves orphaned children and worktrees
// behind.
func (a *assistantActions) Delete(ctx context.Context, sessionID string) error {
	return a.svc.DeleteSession(ctx, sessionID)
}

// Reclaim implements assistant.Actions.
//
// Service.ReclaimSessions re-plans server-side and intersects with the
// request, so an accepted card can only ever narrow what happens — which is
// also why a skip is a normal answer here rather than an error.
func (a *assistantActions) Reclaim(ctx context.Context, sessionID string) (string, error) {
	result, skipped, err := a.svc.ReclaimSessions(ctx, []string{sessionID})
	if err != nil {
		return "", err
	}
	if len(result.Removed) == 0 {
		reason := "there was nothing left on disk for it"
		if len(skipped) > 0 && skipped[0].Reason != "" {
			reason = skipped[0].Reason
		}
		return "", &assistant.OutcomeError{Outcome: "nothing was freed: " + reason}
	}
	return fmt.Sprintf("reclaimed %s of disk, keeping its row and its branch",
		megabytes(result.FreedBytes)), nil
}

// Dissolve implements assistant.Actions.
func (a *assistantActions) Dissolve(ctx context.Context, channelID string, keepHistory bool) error {
	if keepHistory {
		return a.svc.DissolveChannelKeepHistory(ctx, channelID)
	}
	return a.svc.DissolveChannel(ctx, channelID)
}

// SetModel implements assistant.Actions.
func (a *assistantActions) SetModel(ctx context.Context, sessionID, model string) error {
	if err := a.svc.SetSessionModel(ctx, sessionID, model); err != nil {
		return notLiveOutcome(err, "the model")
	}
	return nil
}

// SetMode implements assistant.Actions.
func (a *assistantActions) SetMode(_ context.Context, sessionID, mode string) error {
	if err := a.svc.SetPermissionMode(sessionID, mode); err != nil {
		return notLiveOutcome(err, "the permission mode")
	}
	return nil
}

// BranchFacts implements assistant.Actions, FRESH.
//
// Through RefreshGitStatus rather than the session list, which reads
// `branchStatusCache` with a 60-second TTL: a proposal's whole claim is that it
// was judged on the facts as they are, and the cache deliberately covers what
// nothing reports. The refresh also broadcasts, which is right — everything
// else on screen learns the same thing at the same moment.
func (a *assistantActions) BranchFacts(ctx context.Context, sessionID string) (assistant.BranchFacts, error) {
	snap, err := a.gitSvc.RefreshGitStatus(ctx, sessionID)
	if err != nil {
		return assistant.BranchFacts{}, fmt.Errorf("git status for %s: %w", sessionID, err)
	}
	return assistant.BranchFacts{
		Ahead:       snap.CommitsAhead,
		Behind:      snap.CommitsBehind,
		Dirty:       snap.HasUncommitted,
		MergeStatus: snap.MergeStatus,
		Busy:        a.Busy(ctx, sessionID),
	}, nil
}

// DeleteVerdict implements assistant.Actions, over storage.Evaluate.
//
// The same judgement the Storage page and `agentique prune` read, which is the
// point: "is this safe to delete" has one answer per session, and it fails
// closed — anything git cannot answer is unknown, and unknown is not safe.
func (a *assistantActions) DeleteVerdict(ctx context.Context, sessionID string) (assistant.DeleteVerdict, error) {
	dbSess, err := a.queries.GetSession(ctx, sessionID)
	if err != nil {
		return assistant.DeleteVerdict{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	projectPath := ""
	if project, err := a.queries.GetProject(ctx, dbSess.ProjectID); err == nil {
		projectPath = project.Path
	}

	verdicts := storage.Evaluate(a.probe, storage.SafetyInput{
		Terminal:     terminalState(dbSess.State),
		Live:         a.svc.LiveSessionIDs()[sessionID],
		Merged:       dbSess.WorktreeMerged != 0,
		ProjectPath:  projectPath,
		Branch:       nullStringOf(dbSess.WorktreeBranch),
		WorktreePath: nullStringOf(dbSess.WorktreePath),
	})
	return assistant.DeleteVerdict{
		Safe:        verdicts.Safety.Safe(),
		Reclaimable: verdicts.Reclaimable,
		Safety:      string(verdicts.Safety),
		Reason:      verdicts.Safety.Reason(),
	}, nil
}

// Busy implements assistant.Actions.
//
// From the runtime's own turn lifecycle, never from session state: State()
// reports Idle for one dispatch before the completion that caused it is
// broadcast. A session with no live process is not busy.
func (a *assistantActions) Busy(_ context.Context, sessionID string) bool {
	live := a.mgr.Get(sessionID)
	return live != nil && live.TurnInFlight()
}

// ChannelBusy implements assistant.Actions.
//
// The error is load-bearing: it is how "that is not a channel on this machine"
// reaches the head, so nothing is proposed about a channel this server does not
// hold.
func (a *assistantActions) ChannelBusy(ctx context.Context, channelID string) (assistant.ChannelFacts, error) {
	channel, err := a.queries.GetChannel(ctx, channelID)
	if err != nil {
		return assistant.ChannelFacts{}, fmt.Errorf("channel %s: %w", channelID, err)
	}
	members, err := a.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return assistant.ChannelFacts{}, fmt.Errorf("channel %s members: %w", channelID, err)
	}
	facts := assistant.ChannelFacts{Name: channel.Name, Members: len(members)}
	for _, member := range members {
		if a.Busy(ctx, member.ID) {
			facts.Busy++
		}
	}
	return facts, nil
}

// SessionSettings implements assistant.Actions.
//
// Live is read from the runtime registry and the two values from the row, which
// is where both setters persist them: the row is what a card should quote, and
// a live session's in-memory value cannot disagree with it for longer than the
// write that set it.
//
// The capabilities come from session.CapabilitiesForProvider, the same table
// the session's own `capabilities` wire field carries and the composer's
// controls are gated on — so the proposal card and the UI agree about what a
// codex session will accept, and neither has a provider list of its own. The
// provider name rides along to be named in the refusal.
func (a *assistantActions) SessionSettings(ctx context.Context, sessionID string) (assistant.SessionSettings, error) {
	dbSess, err := a.queries.GetSession(ctx, sessionID)
	if err != nil {
		return assistant.SessionSettings{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	caps := session.CapabilitiesForProvider(dbSess.Provider)
	return assistant.SessionSettings{
		Model:           dbSess.Model,
		Mode:            dbSess.PermissionMode,
		Live:            a.mgr.Get(sessionID) != nil,
		Provider:        caps.Provider,
		ModelSwitch:     caps.ModelSwitch,
		PlanMode:        caps.PlanMode,
		AcceptEditsMode: caps.AcceptEditsMode,
	}, nil
}

// ResolveModel implements assistant.Actions.
//
// The catalog's own resolution, so a spoken family name becomes a model this
// deployment actually offers and never a guessed id — and against the
// PROVIDER's own half of the catalog, because the target of set_session_model
// is an existing session whose provider is knowable. Resolving "opus" against
// claude for a codex session answered a slug that session cannot run, which
// SetSessionModel would then have persisted into its model column.
//
// An empty provider is claude, which is what normalizeProvider does for an
// unset column; the caller passes what SessionSettings read, so this is only
// the degenerate case of a row with no provider at all.
func (a *assistantActions) ResolveModel(ctx context.Context, provider, spoken string) (assistant.ModelChoice, error) {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return assistant.ModelChoice{}, errors.New("no model")
	}
	if provider == "" {
		provider = "claude"
	}
	if a.catalog == nil {
		return assistant.ModelChoice{}, &assistant.UnknownModelError{Spoken: spoken}
	}
	if model, ok := a.catalog.ResolveFamily(ctx, provider, spoken); ok {
		return assistant.ModelChoice{ID: model.Slug, Label: model.DisplayName}, nil
	}
	return assistant.ModelChoice{}, &assistant.UnknownModelError{
		Spoken:   spoken,
		Families: a.catalog.FamilyNames(ctx, provider),
	}
}

// gitOpError turns a refused git operation into an outcome a card can show.
//
// The busy case is the one worth naming: a turn can open between the check and
// the yes, and "it started working" is a different thing to read than "git
// refused it".
func gitOpError(what string, err error) error {
	if errors.Is(err, session.ErrBusy) {
		return &assistant.OutcomeError{
			Outcome: "it started working in the meantime, so nothing touched its branch",
			Detail:  err.Error(),
		}
	}
	slog.Warn("assistant: git operation failed", "operation", what, "error", err)
	return &assistant.OutcomeError{Outcome: "git could not " + what + " it", Detail: err.Error()}
}

// notLiveOutcome names the one failure both setters share: the CLI is gone, so
// there is nothing running to change.
func notLiveOutcome(err error, what string) error {
	if errors.Is(err, session.ErrNotLive) {
		return &assistant.OutcomeError{
			Outcome: "its CLI stopped in the meantime, so there was nothing to change " + what + " on",
			Detail:  err.Error(),
		}
	}
	return err
}

// terminalState is storage's own reading of a finished session, spelled here
// because the predicate behind it is unexported. Keep the two in step: a state
// missing from this list reads as live, which is the fail-closed direction.
func terminalState(state string) bool {
	switch session.State(state) {
	case session.StateDone, session.StateStopped, session.StateFailed:
		return true
	default:
		return false
	}
}

func nullStringOf(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// megabytes is the one size this file prints. Whole MiB, because a card saying
// "reclaimed 1.37 GiB" and a disk gauge that reads slightly differently are two
// numbers about one thing.
func megabytes(n int64) string {
	mb := n / (1024 * 1024)
	if mb < 1 {
		return "under a megabyte"
	}
	return fmt.Sprintf("%d MB", mb)
}

// files renders a conflict count the way a sentence reads it.
func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}
