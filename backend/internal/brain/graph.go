package brain

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// graphNodeDTO is a memory enriched with its structural-graph centrality (RFC P2).
// Centrality is computed on demand, never persisted, so it only appears on the graph
// endpoint — the plain memory list leaves these zero.
type graphNodeDTO struct {
	memoryDTO
	Degree      int     `json:"degree"`
	Betweenness float64 `json:"betweenness"`
	// X, Y are the fact's position in the 2D semantic projection of its embedding
	// (PCA, normalized to [-1,1]) — present only in semantic mode, so the frontend can
	// offer a vector layout and otherwise fall back to the structural force layout.
	X *float64 `json:"x,omitempty"`
	Y *float64 `json:"y,omitempty"`
}

// graphReportDTO is the derived "what the brain knows" panel (graphify analyze.py
// analogs). It references node ids; the client resolves them against Nodes.
type graphReportDTO struct {
	// GodNodes are the most-connected, load-bearing facts (top degree).
	GodNodes []string `json:"godNodes"`
	// Bridges are bottleneck facts connecting otherwise-separate clusters (top
	// betweenness) — the riskiest to lose.
	Bridges []string `json:"bridges"`
	// NeedsConfirmation are the brain's least-trusted facts (low confidence,
	// non-protected): the "confirm what I'm unsure about" queue.
	NeedsConfirmation []string `json:"needsConfirmation"`
	// Isolated are facts with no structural links (a knowledge-gap / low-cohesion
	// signal — graphify's floating dots).
	Isolated []string `json:"isolated"`
	// DueForReview are well-established facts that have gone cold (high storage, low
	// retrieval) — the spaced-review queue (RFC-LD D6): resurface before forgetting.
	DueForReview []string `json:"dueForReview"`
	// Interference are similar-but-not-duplicate fact pairs an agent could conflate —
	// the "same, or distinct?" disambiguation queue (RFC-LD D5).
	Interference []memory.InterferencePair `json:"interference"`
}

type graphDTO struct {
	Nodes  []graphNodeDTO `json:"nodes"`
	Report graphReportDTO `json:"report"`
}

const (
	maxGodNodes          = 8
	maxBridges           = 8
	maxNeedsConfirmation = 25
	maxDueForReview      = 15
	maxInterference      = 25
)

// confirmable reports whether a fact is eligible for the confirm UX: not pinned,
// not locked, not already ground truth (human). Mirrors memory.isProtected's intent
// from the glue side (that predicate is unexported in the core).
func confirmable(r memory.Record) bool {
	return !r.Pinned && !r.Locked && r.Source != memory.SourceHuman
}

// HandleGraph GET /api/brain/graph?scope= — the force-graph + insights payload.
// Returns every memory (optionally one scope) annotated with degree/betweenness over
// the persisted structural link graph, plus a derived report (god nodes, bridges,
// confirm queue, knowledge gaps). Computed request-time (RFC open-decision #5).
func (h *Handler) HandleGraph(w http.ResponseWriter, r *http.Request) error {
	var scopes []memory.Scope
	if sc := r.URL.Query().Get("scope"); sc != "" {
		scopes = []memory.Scope{memory.Scope(sc)}
	}
	recs, err := h.Service.List(r.Context(), scopes...)
	if err != nil {
		return err
	}
	// Captures are never part of the knowledge graph (they aren't recalled and have
	// no structural edges); exclude them so centrality and the report reflect durable
	// facts only.
	durable := make([]memory.Record, 0, len(recs))
	for _, rec := range recs {
		if rec.Source != memory.SourceCapture {
			durable = append(durable, rec)
		}
	}

	cent := memory.ComputeCentrality(durable)

	// Semantic layout: project every durable fact's embedding to 2D so the frontend can
	// lay the graph out by meaning (clusters → spatial clusters). Best-effort and
	// semantic-only — nil coords in lexical mode (or on an embed failure) leave the
	// frontend on its structural force layout.
	coords, perr := h.Service.ProjectRecords(r.Context(), durable)
	if perr != nil {
		slog.Warn("brain: graph semantic projection failed; using structural layout only", "error", perr)
	}

	nodes := make([]graphNodeDTO, 0, len(durable))
	for _, rec := range durable {
		c := cent[rec.ID]
		node := graphNodeDTO{memoryDTO: toDTO(rec), Degree: c.Degree, Betweenness: c.Betweenness}
		if p, ok := coords[rec.ID]; ok {
			x, y := p.X, p.Y
			node.X, node.Y = &x, &y
		}
		nodes = append(nodes, node)
	}

	// In semantic mode, make interference detection embedding-aware (else it stays
	// lexical) — the graph view is a request-time endpoint, not the per-turn hot path,
	// so a one-shot embed of the durable set is acceptable. Nil in lexical mode.
	simOpts := h.Service.semanticSimOptions(r.Context(), durable)
	httperror.JSON(w, http.StatusOK, graphDTO{Nodes: nodes, Report: buildReport(durable, cent, time.Now().UTC(), simOpts...)})
	return nil
}

// buildReport derives the insight lists from the records + their centrality. Pure
// data shaping, factored out so it is unit-testable without an HTTP round-trip. simOpts,
// when present (semantic mode), make interference detection embedding-aware.
func buildReport(recs []memory.Record, cent map[string]memory.Centrality, now time.Time, simOpts ...memory.SimOption) graphReportDTO {
	rep := graphReportDTO{
		GodNodes:          []string{},
		Bridges:           []string{},
		NeedsConfirmation: []string{},
		Isolated:          []string{},
		DueForReview:      []string{},
		Interference:      []memory.InterferencePair{},
	}
	byID := make(map[string]memory.Record, len(recs))
	for _, r := range recs {
		byID[r.ID] = r
	}

	ids := make([]string, len(recs))
	for i, r := range recs {
		ids[i] = r.ID
	}

	// God nodes: most-connected, load-bearing facts. A god node must actually be
	// load-bearing (degree ≥ 2), so a lone pair doesn't qualify. Ties broken by uses
	// then id for determinism.
	god := append([]string(nil), ids...)
	sort.Slice(god, func(a, b int) bool {
		da, db := cent[god[a]].Degree, cent[god[b]].Degree
		if da != db {
			return da > db
		}
		if ua, ub := byID[god[a]].Uses, byID[god[b]].Uses; ua != ub {
			return ua > ub
		}
		return god[a] < god[b]
	})
	for _, id := range god {
		if len(rep.GodNodes) >= maxGodNodes || cent[id].Degree < 2 {
			break
		}
		rep.GodNodes = append(rep.GodNodes, id)
	}

	// Bridges: highest betweenness (connectors between clusters).
	bridges := append([]string(nil), ids...)
	sort.Slice(bridges, func(a, b int) bool {
		ba, bb := cent[bridges[a]].Betweenness, cent[bridges[b]].Betweenness
		if ba != bb {
			return ba > bb
		}
		return bridges[a] < bridges[b]
	})
	for _, id := range bridges {
		if len(rep.Bridges) >= maxBridges || cent[id].Betweenness <= 0 {
			break
		}
		rep.Bridges = append(rep.Bridges, id)
	}

	// Needs-confirmation: the brain's least-trusted facts. Non-protected facts at or
	// below the confirmation score (cross-project generalizations, AMBIGUOUS facts).
	// Lowest score first, then least-used, then oldest — surface the weakest first.
	// A fact qualifies if it's a low-confidence non-protected fact OR it was explicitly
	// flagged as contradicted on recall (RFC-LD D2) — a flagged protected fact must
	// still surface for the human even though its score is untouched.
	var confirm []string
	for _, r := range recs {
		lowConfidence := confirmable(r) && r.ConfidenceScore <= memory.NeedsConfirmationScore
		if lowConfidence || r.ReviewNote != "" {
			confirm = append(confirm, r.ID)
		}
	}
	sort.Slice(confirm, func(a, b int) bool {
		ra, rb := byID[confirm[a]], byID[confirm[b]]
		if ra.ConfidenceScore != rb.ConfidenceScore {
			return ra.ConfidenceScore < rb.ConfidenceScore
		}
		if ra.Uses != rb.Uses {
			return ra.Uses < rb.Uses
		}
		if !ra.UpdatedAt.Equal(rb.UpdatedAt) {
			return ra.UpdatedAt.Before(rb.UpdatedAt)
		}
		return ra.ID < rb.ID
	})
	if len(confirm) > maxNeedsConfirmation {
		confirm = confirm[:maxNeedsConfirmation]
	}
	rep.NeedsConfirmation = append(rep.NeedsConfirmation, confirm...)

	// Isolated: structural gaps (no edges). Reported in id order for stability.
	for _, id := range ids {
		if cent[id].Degree == 0 {
			rep.Isolated = append(rep.Isolated, id)
		}
	}

	// Due-for-review (RFC-LD D6): well-established facts gone cold — resurface before
	// disuse decays them. Empty right after a consolidation (everything is freshly touched);
	// fills in as facts age without recall.
	for _, r := range memory.DueForReview(recs, now, maxDueForReview) {
		rep.DueForReview = append(rep.DueForReview, r.ID)
	}

	// Interference (RFC-LD D5): similar-but-not-duplicate pairs to disambiguate. simOpts
	// (semantic mode) also surface semantic near-duplicates, not just lexical ones.
	rep.Interference = memory.DetectInterference(recs, memory.DefaultRelatedThreshold, memory.DefaultDuplicateThreshold, maxInterference, simOpts...)

	return rep
}
