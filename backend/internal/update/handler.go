package update

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Handler serves the update endpoints. Authenticated like every /api route:
// only a client may ask, and only a client may trigger an upgrade — never a
// peer machine, never as a side effect of anything else.
type Handler struct {
	Checker *Checker
	// Applier is nil when this build cannot apply upgrades in place; the
	// status endpoint still works and reports supported:false.
	Applier *Applier
}

// HandleStatus answers GET /api/update/status. `?refresh=1` forces a check
// (coalesced); without it the answer comes straight from cache and the
// request never touches the network.
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if h.Checker == nil {
		httperror.RespondError(w, httperror.NotFound("update checking is disabled"))
		return
	}
	st := h.Checker.Status()
	if r.URL.Query().Get("refresh") == "1" {
		st = h.Checker.Refresh(r.Context())
	}
	h.decorate(&st)
	httperror.JSON(w, http.StatusOK, st)
}

// decorate fills in the fields only the applier can answer: whether an upgrade
// could actually run here, what is stopping it, whether a turn is in flight,
// and any live progress.
func (h *Handler) decorate(st *Status) {
	if h.Applier == nil {
		st.Blocker = "this server was built without in-app upgrades"
		return
	}
	busy := h.Applier.Busy()
	st.Busy = len(busy) > 0
	st.BusyTurns = len(busy)
	st.Progress = h.Applier.Progress()

	if _, err := h.Applier.Preflight(); err != nil {
		// Not an error to report loudly: most of the time it just means this
		// platform is not verified, or no check has landed yet.
		st.Blocker = err.Error()
		return
	}
	st.Installable = true
}

// HandleApply answers POST /api/update/apply: 202 and the narration continues
// over the WS global topic. The reply is sent long before the restart — this
// returns as soon as the upgrade is under way.
func (h *Handler) HandleApply(w http.ResponseWriter, r *http.Request) {
	if h.Applier == nil {
		httperror.RespondError(w, httperror.NotFound("in-app upgrades are not available on this server"))
		return
	}
	var req struct {
		// Expect is the tag the client believes is latest. A mismatch means
		// its picture is older than ours, and it is refused rather than
		// silently installing something else.
		Expect string `json:"expect"`
		// Force overrides the busy refusal, at the cost of the running turns.
		Force bool `json:"force"`
	}
	// An empty body is fine — it means "whatever is latest, if idle".
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httperror.RespondError(w, httperror.BadRequest("expect/force body expected"))
		return
	}

	if err := h.Applier.Start(req.Expect, req.Force); err != nil {
		httperror.RespondError(w, applyError(err))
		return
	}
	slog.Info("update: apply accepted", "expect", req.Expect, "force", req.Force)
	httperror.JSON(w, http.StatusAccepted, h.Applier.Progress())
}

// HandleCancel answers DELETE /api/update/apply.
func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	if h.Applier == nil {
		httperror.RespondError(w, httperror.NotFound("in-app upgrades are not available on this server"))
		return
	}
	if err := h.Applier.Cancel(); err != nil {
		httperror.RespondError(w, applyError(err))
		return
	}
	httperror.JSON(w, http.StatusOK, h.Applier.Progress())
}

// applyError maps the applier's refusals onto status codes a client can act
// on: 409 for "not now" (busy, already running, too late), 400 for a stale
// expectation, 422 for a machine that simply cannot do this.
func applyError(err error) *httperror.Error {
	switch {
	case errors.Is(err, ErrBusy), errors.Is(err, ErrAlreadyRunning), errors.Is(err, ErrTooLate):
		return httperror.Conflict(err.Error())
	case errors.Is(err, ErrNotRunning):
		return httperror.NotFound(err.Error())
	case errors.Is(err, ErrStale):
		return httperror.BadRequest(err.Error())
	case errors.Is(err, ErrNotSupported), errors.Is(err, ErrNoRelease):
		return &httperror.Error{Status: http.StatusUnprocessableEntity, Message: err.Error()}
	default:
		return httperror.Internal(err.Error(), err)
	}
}
