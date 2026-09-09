package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// Where a resolved model id came from. Three surfaces read this verdict — the
// SessionModel MCP tool, `agentique sessions model`, and the session details
// panel — so it is a closed set and never blank: "we do not know" is a stated
// answer, not an empty field.
const (
	// ModelSourceInitEvent: this session's own run reported the id, either from
	// the live pipeline or from the row a previous run of it wrote.
	ModelSourceInitEvent = "init_event"
	// ModelSourceCatalog: this session never reported one, so the id is what the
	// same provider and slug resolved to for somebody else. A hint, not history.
	ModelSourceCatalog = "catalog"
	// ModelSourceUnresolved: nothing has reported a model for this slug yet.
	ModelSourceUnresolved = "unresolved"
)

// ModelReport answers "which upstream model is this session actually running
// on" — the question the requested slug cannot answer, because "opus" is a
// moving alias and a session started last week is still pinned to whatever it
// meant then.
type ModelReport struct {
	SessionID string `json:"sessionId"`
	Provider  string `json:"provider"`
	// RequestedSlug is what was asked for (sessions.model): an alias like
	// "opus", or a concrete id when the session was pinned to one.
	RequestedSlug string `json:"requestedSlug"`
	// ResolvedModelID is the concrete upstream id, empty when Source is
	// unresolved.
	ResolvedModelID string `json:"resolvedModelId,omitempty"`
	// ResolvedAt dates the reading: UTC RFC3339 seconds. Empty when there is
	// nothing to date, and never guessed — a live pipeline reporting an id the
	// row does not yet carry is reported without a stamp rather than with now.
	ResolvedAt string `json:"resolvedAt,omitempty"`
	Source     string `json:"source"`
}

// modelInspectQueries is the read surface ModelInspector needs: the session row
// and, when that row has nothing, the catalog's learned mapping.
type modelInspectQueries interface {
	GetSession(ctx context.Context, id string) (store.Session, error)
	GetModelResolution(ctx context.Context, arg store.GetModelResolutionParams) (store.ModelResolution, error)
}

// ModelInspector resolves one session's upstream model from the strongest
// evidence available. One resolver, because three surfaces ask the same
// question and an answer that differs between them is worse than no answer.
type ModelInspector struct {
	queries modelInspectQueries
	// live returns the running pipeline's captured model id for a session, or
	// "" when that session has no live pipeline. A func rather than an
	// interface so the manager stays out of this file's dependencies.
	live func(sessionID string) string
}

// NewModelInspector builds an inspector. live may be nil, which is the right
// shape for a caller that only has the database (no running sessions).
func NewModelInspector(q modelInspectQueries, live func(sessionID string) string) *ModelInspector {
	return &ModelInspector{queries: q, live: live}
}

// InspectSessionModel reports the upstream model behind a session.
//
// Strongest evidence first: the live pipeline (current by construction), then
// the row this or an earlier run of the same session wrote, then the catalog's
// learned mapping for the same provider and slug, then unresolved. Only the
// first two are this session's own history; the catalog is a hint about what
// the slug means today, which is why it says so in Source.
func (i *ModelInspector) InspectSessionModel(ctx context.Context, sessionID string) (ModelReport, error) {
	row, err := i.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ModelReport{}, fmt.Errorf("get session %s: %w", sessionID, err)
	}

	rep := ModelReport{
		SessionID:     sessionID,
		Provider:      normalizeProvider(row.Provider),
		RequestedSlug: row.Model,
		Source:        ModelSourceUnresolved,
	}
	rowAt := nullStr(row.ResolvedAt)

	if live := i.liveResolved(sessionID); live != "" {
		rep.ResolvedModelID = live
		rep.Source = ModelSourceInitEvent
		// The row is stamped by the same callback that fed the pipeline, so it
		// dates this id whenever it names it. A row still carrying an older
		// id dates that one, not this one.
		if live == row.ResolvedModel {
			rep.ResolvedAt = rowAt
		}
		return rep, nil
	}

	if row.ResolvedModel != "" {
		rep.ResolvedModelID = row.ResolvedModel
		rep.ResolvedAt = rowAt
		rep.Source = ModelSourceInitEvent
		return rep, nil
	}

	if rep.RequestedSlug == "" {
		return rep, nil
	}

	res, err := i.queries.GetModelResolution(ctx, store.GetModelResolutionParams{
		Provider: rep.Provider,
		Slug:     rep.RequestedSlug,
	})
	if err != nil {
		// Nothing learned for this slug is the common case and not a fault.
		// Anything else is, but the honest answer is still "unresolved" —
		// inspection reports what is known, it does not fail the caller.
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("model inspect: catalog lookup failed",
				"session_id", sessionID, "provider", rep.Provider, "slug", rep.RequestedSlug, "error", err)
		}
		return rep, nil
	}
	if res.ResolvedID == "" {
		return rep, nil
	}

	rep.ResolvedModelID = res.ResolvedID
	rep.ResolvedAt = res.LastSeenAt
	rep.Source = ModelSourceCatalog
	return rep, nil
}

// InspectSessionModel answers the model question for one session, reading the
// live pipeline when the session is running. It is what the SessionModel MCP
// tool and GET /api/sessions/{id}/model both call, so the tool and the CLI
// cannot report different models for the same session.
func (s *Service) InspectSessionModel(ctx context.Context, sessionID string) (ModelReport, error) {
	return NewModelInspector(s.queries, s.liveResolvedModel).InspectSessionModel(ctx, sessionID)
}

func (s *Service) liveResolvedModel(sessionID string) string {
	sess := s.mgr.Get(sessionID)
	if sess == nil {
		return ""
	}
	return sess.ResolvedModel()
}

func (i *ModelInspector) liveResolved(sessionID string) string {
	if i.live == nil {
		return ""
	}
	return i.live(sessionID)
}
