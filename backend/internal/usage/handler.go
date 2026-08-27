package usage

import (
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Handler serves GET /api/usage. Authenticated like every /api route: reading
// it needs only a session, because every client shows the indicator.
//
// `?refresh=1` bypasses the reuse window — the interval absorbs incidental
// opens, it does not overrule somebody who pressed the button.
type Handler struct {
	Collector *Collector
}

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if h.Collector == nil {
		httperror.RespondError(w, httperror.NotFound("usage reporting is disabled"))
		return
	}
	doc := h.Collector.Document(r.Context())
	if r.URL.Query().Get("refresh") == "1" {
		doc = h.Collector.Refresh(r.Context(), true)
	}
	httperror.JSON(w, http.StatusOK, doc)
}
