package update

import (
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Handler serves the update endpoints. Authenticated like every /api route —
// only a client may ask, and (from V3) only a client may trigger an upgrade.
type Handler struct {
	Checker *Checker
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
	httperror.JSON(w, http.StatusOK, st)
}
