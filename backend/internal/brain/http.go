package brain

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/msggen"
)

// Handler serves the Brain tab HTTP API over a Service. Runner backs the LLM
// reorganization during consolidation preview; it may be nil, in which case
// preview falls back to deterministic dedup/decay only.
type Handler struct {
	Service *Service
	Runner  msggen.Runner
}

// extractorFor builds the consolidation Extractor for a requested model. An empty
// model (or a missing Runner) yields nil — deterministic dedup/decay only. The
// model is the caller's choice; we never default it.
func (h *Handler) extractorFor(model string) (memory.Extractor, error) {
	if model == "" || h.Runner == nil {
		return nil, nil
	}
	m, err := ParseModel(model)
	if err != nil {
		return nil, httperror.BadRequest(err.Error())
	}
	return NewClaudeExtractor(h.Runner, m), nil
}

type memoryDTO struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"`
	Text        string    `json:"text"`
	Category    string    `json:"category"`
	Source      string    `json:"source"`
	Pinned      bool      `json:"pinned"`
	Locked      bool      `json:"locked"`
	Uses        int       `json:"uses"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	DerivedFrom []string  `json:"derivedFrom,omitempty"`
	Related     []string  `json:"related,omitempty"`
}

func toDTO(r memory.Record) memoryDTO {
	return memoryDTO{
		ID: r.ID, Scope: string(r.Scope), Text: r.Text, Category: string(r.Category),
		Source: string(r.Source), Pinned: r.Pinned, Locked: r.Locked, Uses: r.Uses,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, DerivedFrom: r.DerivedFrom, Related: r.Related,
	}
}

func toDTOs(rs []memory.Record) []memoryDTO {
	out := make([]memoryDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, toDTO(r))
	}
	return out
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return httperror.BadRequest("invalid JSON body")
	}
	return nil
}

func mapErr(err error) error {
	if errors.Is(err, memory.ErrNotFound) {
		return httperror.NotFound("memory not found")
	}
	return err
}

// HandleList GET /api/brain/memories?scope=  — list all, or one scope.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) error {
	var scopes []memory.Scope
	if sc := r.URL.Query().Get("scope"); sc != "" {
		scopes = []memory.Scope{memory.Scope(sc)}
	}
	recs, err := h.Service.List(r.Context(), scopes...)
	if err != nil {
		return err
	}
	httperror.JSON(w, http.StatusOK, toDTOs(recs))
	return nil
}

// HandleCreate POST /api/brain/memories  {scope,text,category}
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope    string `json:"scope"`
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	scope := memory.Scope(body.Scope)
	if scope == "" {
		scope = memory.ScopeGlobal
	}
	rec, err := h.Service.Add(r.Context(), scope, body.Text, memory.Category(body.Category), memory.SourceHuman)
	if err != nil {
		return httperror.BadRequest(err.Error())
	}
	httperror.JSON(w, http.StatusCreated, toDTO(rec))
	return nil
}

// HandleGet GET /api/brain/memories/{id}
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.Service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}
	httperror.JSON(w, http.StatusOK, toDTO(rec))
	return nil
}

// HandleUpdate PUT /api/brain/memories/{id}  {text,category}
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	rec, err := h.Service.Update(r.Context(), r.PathValue("id"), body.Text, memory.Category(body.Category))
	if err != nil {
		return mapErr(err)
	}
	httperror.JSON(w, http.StatusOK, toDTO(rec))
	return nil
}

// HandleDelete DELETE /api/brain/memories/{id}
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Service.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// HandlePin POST /api/brain/memories/{id}/pin  {pinned}
func (h *Handler) HandlePin(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	rec, err := h.Service.SetPinned(r.Context(), r.PathValue("id"), body.Pinned)
	if err != nil {
		return mapErr(err)
	}
	httperror.JSON(w, http.StatusOK, toDTO(rec))
	return nil
}

// HandleLock POST /api/brain/memories/{id}/lock  {locked}
func (h *Handler) HandleLock(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Locked bool `json:"locked"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	rec, err := h.Service.SetLocked(r.Context(), r.PathValue("id"), body.Locked)
	if err != nil {
		return mapErr(err)
	}
	httperror.JSON(w, http.StatusOK, toDTO(rec))
	return nil
}

// HandleSearch GET /api/brain/search?q=&scope=
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query().Get("q")
	scope := memory.Scope(r.URL.Query().Get("scope"))
	res, err := h.Service.Recall(r.Context(), recallScopes(scope), q, memory.DefaultRecallK)
	if err != nil {
		return err
	}
	httperror.JSON(w, http.StatusOK, map[string]any{
		"pinned":   toDTOs(res.Pinned),
		"recalled": toDTOs(res.Recalled),
	})
	return nil
}

type changeDTO struct {
	Before memoryDTO `json:"before"`
	After  memoryDTO `json:"after"`
}

type reportDTO struct {
	Scope            string      `json:"scope"`
	Promoted         []memoryDTO `json:"promoted"`
	Rewritten        []changeDTO `json:"rewritten"`
	Abstracted       []memoryDTO `json:"abstracted"`
	Deleted          []memoryDTO `json:"deleted"`
	Decayed          []memoryDTO `json:"decayed"`
	CapturesConsumed []string    `json:"capturesConsumed"`
	Skipped          bool        `json:"skipped"`
	ReorgRefused     bool        `json:"reorgRefused"`
}

func toReportDTO(rep memory.Report) reportDTO {
	dto := reportDTO{
		Scope:            string(rep.Scope),
		Promoted:         toDTOs(rep.Promoted),
		Abstracted:       toDTOs(rep.Abstracted),
		Deleted:          toDTOs(rep.Deleted),
		Decayed:          toDTOs(rep.Decayed),
		CapturesConsumed: rep.CapturesConsumed,
		Skipped:          rep.Skipped,
		ReorgRefused:     rep.ReorgRefused,
	}
	for _, c := range rep.Rewritten {
		dto.Rewritten = append(dto.Rewritten, changeDTO{Before: toDTO(c.Before), After: toDTO(c.After)})
	}
	return dto
}

// HandleConsolidate POST /api/brain/consolidate  {scope}
// Deterministic dedup/decay only (no model), one-shot. Retained for callers that
// don't want the preview→apply flow; the Brain tab uses preview/apply below.
func (h *Handler) HandleConsolidate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope string `json:"scope"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	scope := memory.Scope(body.Scope)
	if scope == "" {
		scope = memory.ScopeGlobal
	}
	rep, err := h.Service.Consolidate(r.Context(), scope, nil, memory.DecayPolicy{}, false)
	if err != nil {
		return err
	}
	httperror.JSON(w, http.StatusOK, toReportDTO(rep))
	return nil
}

// previewDTO is the preview response: the changelog to render plus the plan the
// client holds and posts back to apply (stateless — no server-side plan cache).
type previewDTO struct {
	Report reportDTO   `json:"report"`
	Plan   memory.Plan `json:"plan"`
}

// HandlePreviewConsolidate POST /api/brain/consolidate/preview  {scope, model}
// Runs the LLM phase once and returns the proposed changelog PLUS the plan that
// produced it. The client holds the plan and posts it back to apply — so applying
// commits exactly what was previewed and never re-runs the model.
func (h *Handler) HandlePreviewConsolidate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope string `json:"scope"`
		Model string `json:"model"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	scope := memory.Scope(body.Scope)
	if scope == "" {
		scope = memory.ScopeGlobal
	}
	ex, err := h.extractorFor(body.Model)
	if err != nil {
		return err
	}
	plan, err := h.Service.Plan(r.Context(), scope, ex, memory.DecayPolicy{})
	if err != nil {
		return err
	}
	rep, err := h.Service.ApplyPlan(r.Context(), scope, plan, memory.DecayPolicy{}, true)
	if err != nil {
		return err
	}
	httperror.JSON(w, http.StatusOK, previewDTO{Report: toReportDTO(rep), Plan: plan})
	return nil
}

// HandleApplyConsolidate POST /api/brain/consolidate/apply  {plan}
// Applies a plan from a prior preview deterministically — no model call. Returns
// 409 if the scope changed since the preview (stale plan); the client re-previews.
func (h *Handler) HandleApplyConsolidate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Plan memory.Plan `json:"plan"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	scope := body.Plan.Scope
	if scope == "" {
		return httperror.BadRequest("plan is missing a scope")
	}
	rep, err := h.Service.ApplyPlan(r.Context(), scope, body.Plan, memory.DecayPolicy{}, false)
	if errors.Is(err, memory.ErrStalePlan) {
		return httperror.Conflict("the brain changed since this preview — re-run preview")
	}
	if err != nil {
		return err
	}
	httperror.JSON(w, http.StatusOK, toReportDTO(rep))
	return nil
}

// HandleStatus GET /api/brain/status
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) error {
	httperror.JSON(w, http.StatusOK, map[string]any{
		"semantic": h.Service.SemanticEnabled(),
	})
	return nil
}
