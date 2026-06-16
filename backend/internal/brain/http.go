package brain

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// Handler serves the Brain tab HTTP API over a Service.
type Handler struct {
	Service *Service
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

// HandleConsolidate POST /api/brain/consolidate  {scope}
// Runs the consolidation pass for a scope. Without a configured Extractor this
// performs deterministic decay only; it always returns the changelog.
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
	rep, err := h.Service.Consolidate(r.Context(), scope, nil, memory.DecayPolicy{})
	if err != nil {
		return err
	}
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
	httperror.JSON(w, http.StatusOK, dto)
	return nil
}

// HandleStatus GET /api/brain/status
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) error {
	httperror.JSON(w, http.StatusOK, map[string]any{
		"semantic": h.Service.SemanticEnabled(),
	})
	return nil
}
