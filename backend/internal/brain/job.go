package brain

import (
	"context"
	"log/slog"
	"net/http"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// Push event types broadcast over the WebSocket bus to every connected tab.
const (
	consolidationPush = "brain.consolidation" // job lifecycle (running/progress/done/error)
	// EventBrainUpdated signals a memory was added/changed/removed (HTTP, agent, or
	// background automation). Drives the nav "flare" and a list refresh.
	EventBrainUpdated = "brain.updated"
)

const (
	phaseRunning = "running"
	phaseDone    = "done"
	phaseError   = "error"
)

// JobState is the broadcast-and-fetchable state of the single active (or most
// recent) consolidation job. Consolidation runs in the background under a stable
// context — NOT the HTTP request's — so a request hiccup can't SIGTERM the model
// subprocess, and progress reaches every tab via the bus instead of one blocking
// response. Plan carries the model's proposal so a tab that picks the job up can
// Apply it.
type JobState struct {
	ID      string     `json:"id"`
	Kind    string     `json:"kind"`  // "scope" | "global" | "all"
	Scope   string     `json:"scope,omitempty"`
	Model   string     `json:"model,omitempty"`
	Phase   string     `json:"phase"` // running | done | error
	Current int        `json:"current"`
	Total   int        `json:"total"`
	Report  *reportDTO `json:"report,omitempty"`  // changelog, set on done (scope/global)
	Plan    any        `json:"plan,omitempty"`    // memory.Plan (scope) or memory.GlobalPlan (global)
	Changes int        `json:"changes,omitempty"` // aggregate change count for "all"
	Error   string     `json:"error,omitempty"`
}

// publishJob stores a snapshot as the current job and broadcasts it. Callers pass
// a value (not a pointer) so concurrent reads never race the runner's mutations.
func (h *Handler) publishJob(j JobState) {
	h.jobMu.Lock()
	snapshot := j
	h.job = &snapshot
	h.jobMu.Unlock()
	if h.Bus != nil {
		h.Bus.Broadcast(consolidationPush, j)
	}
}

func (h *Handler) currentJob() *JobState {
	h.jobMu.Lock()
	defer h.jobMu.Unlock()
	return h.job
}

// brainChanged notifies all tabs that the memory set changed (drives the nav
// "flare" and a memory-list refresh). Best-effort and fire-and-forget.
func (h *Handler) brainChanged() {
	if h.Bus != nil {
		h.Bus.Broadcast(EventBrainUpdated, map[string]string{})
	}
}

// beginJob reserves the single job slot, refusing if one is already running.
func (h *Handler) beginJob(j JobState) (JobState, error) {
	h.jobMu.Lock()
	if h.job != nil && h.job.Phase == phaseRunning {
		h.jobMu.Unlock()
		return JobState{}, httperror.Conflict("a consolidation is already running")
	}
	snapshot := j
	h.job = &snapshot
	h.jobMu.Unlock()
	if h.Bus != nil {
		h.Bus.Broadcast(consolidationPush, j)
	}
	return j, nil
}

func (h *Handler) failJob(job JobState, err error) {
	job.Phase = phaseError
	job.Error = err.Error()
	h.publishJob(job)
	slog.Warn("brain: consolidation job failed", "kind", job.Kind, "scope", job.Scope, "error", err)
}

func (h *Handler) finishJob(job JobState, rep memory.Report, plan any) {
	dto := toReportDTO(rep)
	job.Phase = phaseDone
	job.Report = &dto
	job.Plan = plan
	h.publishJob(job)
}

// startScopeJob kicks off a per-scope preview (Plan + dry-run apply) in the
// background and returns the initial running state immediately. mode selects the
// reorganize strategy ("" = conservative, "aggressive" = collapse hard); force
// re-runs even when the scope is unchanged since the last pass.
func (h *Handler) startScopeJob(scope memory.Scope, model, mode string, force bool) (JobState, error) {
	job, err := h.beginJob(JobState{
		ID: uuid.NewString(), Kind: "scope", Scope: string(scope), Model: model, Phase: phaseRunning,
	})
	if err != nil {
		return JobState{}, err
	}
	go h.runScopeJob(job, scope, model, mode, force)
	return job, nil
}

func (h *Handler) runScopeJob(job JobState, scope memory.Scope, model, mode string, force bool) {
	ctx := context.Background()
	exOpts, minSurvivorRatio := reorganizeModePolicy(mode)
	var ex memory.Extractor
	if model != "" && h.Runner != nil {
		m, err := ParseModel(model)
		if err != nil {
			h.failJob(job, err)
			return
		}
		ex = NewClaudeExtractor(h.Runner, m, exOpts...)
	}
	plan, err := h.Service.Plan(ctx, scope, ex, memory.DecayPolicy{}, TidyOptions{Force: force, MinSurvivorRatio: minSurvivorRatio})
	if err != nil {
		h.failJob(job, err)
		return
	}
	rep, err := h.Service.ApplyPlan(ctx, scope, plan, memory.DecayPolicy{}, true)
	if err != nil {
		h.failJob(job, err)
		return
	}
	h.finishJob(job, rep, plan)
}

// startGlobalJob kicks off a cross-scope promotion preview in the background.
func (h *Handler) startGlobalJob(model string) (JobState, error) {
	if h.Runner == nil {
		return JobState{}, httperror.BadRequest("global consolidation requires a model")
	}
	m, err := ParseModel(model)
	if err != nil {
		return JobState{}, httperror.BadRequest(err.Error())
	}
	job, err := h.beginJob(JobState{
		ID: uuid.NewString(), Kind: "global", Model: model, Phase: phaseRunning,
	})
	if err != nil {
		return JobState{}, err
	}
	go h.runGlobalJob(job, m)
	return job, nil
}

func (h *Handler) runGlobalJob(job JobState, m claudecli.Model) {
	ctx := context.Background()
	ex := NewClaudeExtractor(h.Runner, m)
	plan, err := h.Service.PlanGlobal(ctx, ex, memory.ConsolidateOptions{
		Progress: func(done, total int) {
			job.Current, job.Total = done, total
			h.publishJob(job)
		},
		OnError: func(e error) {
			slog.Warn("brain: global promote batch failed (skipped)", "error", e)
		},
	})
	if err != nil {
		h.failJob(job, err)
		return
	}
	rep, err := h.Service.ApplyGlobal(ctx, plan, true) // dry-run for the changelog
	if err != nil {
		h.failJob(job, err)
		return
	}
	h.finishJob(job, rep, plan)
}

// startTidyAllJob kicks off a bulk consolidation of every scope in the background.
// Unlike preview jobs it AUTO-APPLIES each scope (an on-demand sleep pass), relying
// on the consolidation guards; progress is per-scope.
func (h *Handler) startTidyAllJob(model string) (JobState, error) {
	if h.Runner == nil {
		return JobState{}, httperror.BadRequest("tidy all requires a model")
	}
	m, err := ParseModel(model)
	if err != nil {
		return JobState{}, httperror.BadRequest(err.Error())
	}
	job, err := h.beginJob(JobState{ID: uuid.NewString(), Kind: "all", Model: model, Phase: phaseRunning})
	if err != nil {
		return JobState{}, err
	}
	go h.runTidyAllJob(job, m)
	return job, nil
}

func (h *Handler) runTidyAllJob(job JobState, m claudecli.Model) {
	ctx := context.Background()
	ex := NewClaudeExtractor(h.Runner, m)
	scopes, err := h.Service.ListScopes(ctx)
	if err != nil {
		h.failJob(job, err)
		return
	}
	job.Total = len(scopes)
	h.publishJob(job)
	for i, scope := range scopes {
		rep, cerr := h.Service.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, false, false)
		if cerr != nil {
			// One bad scope shouldn't sink the bulk pass — log and continue.
			slog.Warn("brain: tidy all: scope failed", "scope", scope, "error", cerr)
		} else {
			changes := len(rep.Promoted) + len(rep.Rewritten) + len(rep.Abstracted) + len(rep.Deleted) + len(rep.Decayed)
			job.Changes += changes
			if changes > 0 {
				h.brainChanged() // flare + refresh as each scope lands
			}
		}
		job.Current = i + 1
		h.publishJob(job)
	}
	job.Phase = phaseDone
	h.publishJob(job)
}

// HandleConsolidateJob GET /api/brain/consolidate/job
// Returns the current/most-recent job so a freshly opened tab can resync to an
// in-flight (or just-finished) consolidation it didn't see start.
func (h *Handler) HandleConsolidateJob(w http.ResponseWriter, r *http.Request) error {
	httperror.JSON(w, http.StatusOK, map[string]any{"job": h.currentJob()})
	return nil
}
