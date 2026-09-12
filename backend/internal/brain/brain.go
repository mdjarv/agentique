// Package brain is agentique's product service around the liftable internal/memory
// primitives. It composes a filestore (source of truth) with an optional
// Chroma+embeddings semantic index, maps agentique concepts (projects) to memory
// scopes, and persists per-scope consolidation fingerprints. All agentique-specific
// policy lives here so internal/memory and its sub-packages stay portable.
package brain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/cachestore"
	"github.com/mdjarv/agentique/backend/internal/memory/chroma"
	"github.com/mdjarv/agentique/backend/internal/memory/embedhttp"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

const defaultCollection = "agentique_memories"

// Config configures the brain service. Only Dir is required; when ChromaURL,
// EmbedURL and EmbedModel are all set and Chroma is reachable, semantic recall is
// enabled, otherwise the service uses keyword recall over the filestore.
type Config struct {
	Dir         string
	ChromaURL   string
	EmbedURL    string
	EmbedModel  string
	EmbedAPIKey string
	Collection  string
	// SemanticThreshold overrides the cosine link threshold for semantic similarity
	// clustering (RFC phase C). 0 uses memory.DefaultSemanticThreshold. Calibrate per
	// embedding model.
	SemanticThreshold float64
	// VectorVetoScore overrides the hybrid-recall veto floor: a candidate the embedder
	// scores at/below this (semantically unrelated) is dropped regardless of keyword
	// overlap (brain.md#semantic-recall priority #1). 0 uses memory.DefaultVectorVetoScore.
	// Inert without an embedder; MODEL-SPECIFIC — calibrate with SemanticThreshold.
	VectorVetoScore float64
	// Calibrate, when set and semantic mode is enabled, derives the cosine link/vouch
	// threshold and the vector veto floor from the live corpus's OWN pairwise cosine
	// distribution (model-specific auto-calibration, brain.md#semantic-recall #5) instead
	// of the hand-set defaults. Precedence: an explicit SemanticThreshold/VectorVetoScore
	// still wins per-knob; auto-calibration only fills the ones left 0. A too-thin corpus
	// or an embed failure falls back to the defaults. Inert without an embedder.
	Calibrate bool

	// SnapshotRetain bounds how many pre-churn brain snapshots are kept (brain/.snapshots/<ts>/).
	// 0 uses the built-in default (7). Env override: AGENTIQUE_BRAIN_SNAPSHOT_RETAIN.
	SnapshotRetain int

	// ArchiveFloor is the RECALL-side effective-confidence line below which a faded fact is
	// dropped from recall at read time (M5). 0 DISABLES the read-time fade (it is NOT promoted to
	// a default — the recall-cliff defense). The server only sets it non-zero when archiving is
	// enabled (archive-after set), so the fade and the churn's archival move together. The churn's
	// own floor (DecayPolicy.ArchiveFloor) is separate and DOES default to 0.35 when 0.
	ArchiveFloor float64

	// Graph tunes the knowledge-graph view (semantic kNN edge density + force-layout
	// curves). Zero-valued fields take the built-in defaults via GraphConfig.withDefaults.
	Graph GraphConfig
}

// Graph-view tuning defaults. The backend uses EdgeCap/EdgeThreshold to build the semantic
// kNN; the rest are passed to the frontend force layout on the graph payload. EdgeThreshold
// has no constant here — when unset it falls back to the recall cosThresh (resolved in New),
// so a single "related" line drives both recall and the graph by default.
const (
	DefaultGraphEdgeCap = 6
	// Similar-edge springs are deliberately weak and long: with thousands of cross-scope kNN
	// edges, a firm pull collapses every project cluster into one central hairball. These
	// values let a semantic edge *nudge* related facts together (and a strong one nudge harder
	// + sit a little closer) without overpowering the charge repulsion that keeps clusters
	// legible. Tuned against the live ~1.4k-fact graph; raise the strengths for a tighter web.
	DefaultGraphLinkStrengthBase = 0.012
	DefaultGraphLinkStrengthSpan = 0.13
	DefaultGraphLinkDistanceBase = 130.0
	DefaultGraphLinkDistanceSpan = 70.0
	DefaultGraphGravity          = 0.05
)

// GraphConfig is the resolved knowledge-graph tuning the Service holds. The two edge fields
// shape the backend semantic kNN; the force-layout fields are echoed to the frontend on the
// graph payload so the layout geometry is tunable per deployment. A 0 field means "default";
// withDefaults fills them. EdgeThreshold is special — 0 means "fall back to the recall
// cosThresh" and is resolved in New, not here, because cosThresh isn't known until then.
type GraphConfig struct {
	EdgeCap          int
	EdgeThreshold    float64
	LinkStrengthBase float64
	LinkStrengthSpan float64
	LinkDistanceBase float64
	LinkDistanceSpan float64
	Gravity          float64
}

// withDefaults returns g with every unset (0) force-layout field replaced by its built-in
// default. EdgeThreshold is left as-is (0 = "use cosThresh", handled by the caller).
func (g GraphConfig) withDefaults() GraphConfig {
	if g.EdgeCap <= 0 {
		g.EdgeCap = DefaultGraphEdgeCap
	}
	if g.LinkStrengthBase <= 0 {
		g.LinkStrengthBase = DefaultGraphLinkStrengthBase
	}
	if g.LinkStrengthSpan <= 0 {
		g.LinkStrengthSpan = DefaultGraphLinkStrengthSpan
	}
	if g.LinkDistanceBase <= 0 {
		g.LinkDistanceBase = DefaultGraphLinkDistanceBase
	}
	if g.LinkDistanceSpan <= 0 {
		g.LinkDistanceSpan = DefaultGraphLinkDistanceSpan
	}
	if g.Gravity <= 0 {
		g.Gravity = DefaultGraphGravity
	}
	return g
}

// Service is the agentique brain.
type Service struct {
	store memory.Store
	// cache is the read-through cache that fronts the filestore. It is the SAME object as
	// store in keyword mode; in semantic mode store is the chroma decorator that WRAPS this
	// cache. Held directly so RestoreSnapshot can Invalidate() it after an external file
	// rewrite (the chroma wrapper doesn't expose the cache). Set once in New, read-only after.
	cache    *cachestore.Store
	dir      string
	semantic bool

	// snapshotRetain caps the pre-churn snapshots kept under dir/.snapshots (read-only after New).
	snapshotRetain int
	// archiveFloor is the effective-confidence floor that fades cold facts out of a
	// model-facing pull at read time (M5), threaded into [Service.RecallForPull]'s Query;
	// 0 disables the fade. Read-only after New.
	//
	// It is deliberately NOT applied by [Service.Recall], which is the browsing recall
	// behind the memory page's search box and `agentique brain search`: a fact that has
	// faded but not yet been archived is still a live row on that page, and a row you can
	// see in the list and cannot find by searching for it is the surface telling the
	// operator two different things. Curating what is about to be forgotten is exactly
	// what those two surfaces are for.
	archiveFloor float64

	// embedder, when set (semantic mode), drives semantic similarity for clustering —
	// link/community/area edges blend Jaccard with embedding cosine (RFC phase C). nil =
	// lexical-only. cosThresh is the cosine link threshold (model-specific).
	embedder  memory.Embedder
	cosThresh float64
	// vetoScore is the hybrid-recall vector veto floor (model-specific) threaded into
	// every relevance Query; 0 lets memory.Recall apply its default. Inert without an embedder.
	vetoScore float64

	// graph is the resolved knowledge-graph tuning (deployment-configurable, defaults filled).
	// Its EdgeCap/EdgeThreshold drive SemanticEdges; the force-layout fields are echoed to the
	// frontend on the graph payload. Set once in New; read-only after, so no lock needed.
	graph GraphConfig

	// semEdgeCache memoizes the last few SemanticEdges results keyed by a corpus fingerprint
	// (the input records' ids+text-hashes plus the resolved threshold/cap), so a repeated graph
	// load over an unchanged corpus skips the O(n²·d) kNN entirely. A fingerprint changes the
	// instant any input fact's text or id changes, so a stale entry can never be served; the map
	// is bounded by clearing it when it exceeds semEdgeCacheMax (corpus churn invalidates every
	// entry anyway). Guarded by semEdgeMu, separate from the other locks.
	semEdgeMu    sync.Mutex
	semEdgeCache map[string][]memory.Edge

	// embedCache memoizes embeddings by text-hash so the per-pass corpus/scope re-embed
	// (embedRecords, now hit on every ApplyPlan/Consolidate/AssignAreas/graph load) only
	// calls the embedder for texts it hasn't seen. An embedding is a pure function of
	// (text, model) and the model is fixed for a Service's lifetime, so text-hash is a
	// sufficient key (id-independent: two facts with identical text share a vector). A
	// changed text yields a new key; stale entries are pruned to the live corpus on the
	// global checkpoint (pruneEmbedCache from AssignAreas), so the cache is bounded by the
	// live fact set, not by every text ever seen. Guarded by embedMu, separate from mu.
	//
	// The cache is also warmed from the vector store on first use (warmEmbedCache via
	// warmSrc), so a process restart does not re-embed an unchanged corpus — Chroma already
	// holds those vectors keyed by the same text.
	embedMu    sync.Mutex
	embedCache map[string][]float32

	// warmSrc loads existing vectors from the semantic index to seed embedCache after a
	// restart; nil in keyword mode. warmEmbedCache runs it at most once (guarded by warmMu /
	// warmed), retrying on a transient failure.
	warmSrc vectorWarmSource
	warmMu  sync.Mutex
	warmed  bool

	mu     sync.Mutex // guards the fingerprint + global-manifest files
	fpPath string
	gmPath string // per-scope content-hash manifest of the last global pass (RFC P5)
}

// vectorWarmSource returns the (document, embedding) pairs already held by the vector index,
// so the brain can warm its text-hash embedding cache after a restart instead of re-embedding
// an unchanged corpus. *chroma.Store satisfies it; tests use a fake.
type vectorWarmSource interface {
	LoadVectors(ctx context.Context) ([]chroma.VectorRecord, error)
}

// New builds the service, creating the brain directory and (optionally) the
// semantic index. It never fails because the vector backend is unavailable — it
// degrades to keyword recall and logs a warning.
func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("brain: Dir is required")
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("brain: create dir: %w", err)
	}
	// Wrap the filestore in a read-through cache: per-turn auto-recall (fluid recall)
	// calls List every turn, and the cache avoids re-reading every markdown file each
	// time. All writes funnel through this Service's store, so the cache stays consistent.
	base := cachestore.New(filestore.New(cfg.Dir))
	svc := &Service{
		store:          base,
		cache:          base,
		dir:            cfg.Dir,
		snapshotRetain: cfg.SnapshotRetain,
		archiveFloor:   cfg.ArchiveFloor,
		embedCache:     make(map[string][]float32),
		semEdgeCache:   make(map[string][]memory.Edge),
		graph:          cfg.Graph.withDefaults(),
		fpPath:         filepath.Join(cfg.Dir, ".fingerprints.json"),
		gmPath:         filepath.Join(cfg.Dir, ".global-manifest.json"),
	}

	if cfg.ChromaURL != "" && cfg.EmbedURL != "" && cfg.EmbedModel != "" {
		client := chroma.NewClient(cfg.ChromaURL)
		if err := client.Heartbeat(ctx); err != nil {
			slog.Warn("brain: chroma unreachable; using keyword recall", "url", cfg.ChromaURL, "error", err)
		} else {
			coll := cfg.Collection
			if coll == "" {
				coll = defaultCollection
			}
			emb := embedhttp.New(cfg.EmbedURL, cfg.EmbedModel, embedhttp.WithAPIKey(cfg.EmbedAPIKey))
			cs, err := chroma.NewStore(ctx, base, client, emb, coll, chroma.WithErrorHandler(func(e error) {
				slog.Warn("brain: vector index degraded", "error", e)
			}))
			if err != nil {
				slog.Warn("brain: chroma store init failed; using keyword recall", "error", err)
			} else {
				svc.store = cs
				svc.semantic = true
				svc.embedder = emb
				svc.warmSrc = cs // warm embedCache from Chroma on first use (no cold-start re-embed)
				svc.cosThresh = cfg.SemanticThreshold
				if svc.cosThresh <= 0 {
					svc.cosThresh = memory.DefaultSemanticThreshold
				}
				svc.vetoScore = cfg.VectorVetoScore
				if svc.vetoScore <= 0 {
					svc.vetoScore = memory.DefaultVectorVetoScore
				}
				slog.Info("brain: semantic recall enabled", "collection", coll, "cosineThreshold", svc.cosThresh, "vectorVeto", svc.vetoScore)
				if cfg.Calibrate {
					svc.applyCalibration(ctx, cfg.SemanticThreshold > 0, cfg.VectorVetoScore > 0)
				}
				// An unset graph edge threshold falls back to the (possibly calibrated)
				// recall "related" line, so the graph and recall share one cosine floor.
				if svc.graph.EdgeThreshold <= 0 {
					svc.graph.EdgeThreshold = svc.cosThresh
				}
			}
		}
	}
	return svc, nil
}

// calibrationTimeout bounds the boot-time corpus embed so an auto-calibration pass
// can't hang server startup if the embedder is slow or wedged.
const calibrationTimeout = 2 * time.Minute

// applyCalibration derives the model-specific cosine/veto thresholds from the live
// corpus and overrides the ones the operator did NOT pin explicitly (explicitCos /
// explicitVeto). Best-effort: any failure (no corpus, thin corpus, embed error) keeps
// the thresholds already set and logs why — calibration must never break boot.
func (s *Service) applyCalibration(ctx context.Context, explicitCos, explicitVeto bool) {
	cctx, cancel := context.WithTimeout(ctx, calibrationTimeout)
	defer cancel()
	res, err := s.Calibrate(cctx)
	if err != nil {
		slog.Warn("brain: auto-calibration failed; keeping thresholds",
			"error", err, "cosineThreshold", s.cosThresh, "vectorVeto", s.vetoScore)
		return
	}
	if !res.OK {
		slog.Warn("brain: auto-calibration skipped (corpus too thin); keeping thresholds",
			"pairs", res.Sample.Len(), "cosineThreshold", s.cosThresh, "vectorVeto", s.vetoScore)
		return
	}
	if !explicitCos {
		s.cosThresh = res.Thresholds.CosineThreshold
	}
	if !explicitVeto {
		s.vetoScore = res.Thresholds.VectorVeto
	}
	slog.Info("brain: semantic thresholds auto-calibrated",
		"pairs", res.Sample.Len(),
		"p25", res.Sample.Percentile(0.25), "p50", res.Sample.Percentile(0.50),
		"p99", res.Sample.Percentile(0.99),
		"cosineThreshold", s.cosThresh, "vectorVeto", s.vetoScore)
}

// Calibrate embeds the whole durable corpus and derives model-specific cosine/veto
// thresholds from its pairwise cosine distribution (memory.Calibrate). It is the
// reusable measure-first helper behind both New's opt-in auto-calibration and the
// `brain calibrate` CLI. The embed funnels through the text-hash cache, so it also
// warms that cache for the next consolidation pass. Returns an error only on an
// operational failure (no embedder, list, or embed); a corpus too thin to trust is a
// successful call with Result.OK=false.
func (s *Service) Calibrate(ctx context.Context) (memory.CalibrationResult, error) {
	if s.embedder == nil {
		return memory.CalibrationResult{}, fmt.Errorf("brain: calibrate requires semantic mode (no embedder configured)")
	}
	all, err := s.store.List(ctx)
	if err != nil {
		return memory.CalibrationResult{}, fmt.Errorf("brain: calibrate list corpus: %w", err)
	}
	recs := durableRecords(all)
	if len(recs) < 2 {
		return memory.CalibrationResult{}, nil // not an error — just nothing to calibrate over
	}
	vecs, err := s.embedRecords(ctx, recs)
	if err != nil {
		return memory.CalibrationResult{}, fmt.Errorf("brain: calibrate embed corpus: %w", err)
	}
	ordered := make([][]float32, 0, len(recs))
	for _, r := range recs {
		if v := vecs[r.ID]; len(v) > 0 {
			ordered = append(ordered, v)
		}
	}
	return memory.Calibrate(ordered, memory.CalibrationOptions{}), nil
}

// SemanticEnabled reports whether vector recall is active.
func (s *Service) SemanticEnabled() bool { return s.semantic }

// reindexer is the rebuild-the-vector-index capability the semantic store exposes.
// *chroma.Store satisfies it; the bare filestore (keyword mode) does not.
type reindexer interface {
	Reindex(ctx context.Context) error
}

// Reindex rebuilds the entire semantic vector index from the markdown source of truth.
// The index is maintained lazily on each write, so a bulk hand-edit of the markdown
// files or an embedding-model change leaves vectors stale or missing until the next
// pass touches them; this re-embeds and re-upserts the whole durable corpus in one shot.
// It errors in keyword mode, where there is no vector index to rebuild.
func (s *Service) Reindex(ctx context.Context) error {
	rx, ok := s.store.(reindexer)
	if !ok {
		return fmt.Errorf("brain: reindex requires semantic mode (no vector index configured)")
	}
	return rx.Reindex(ctx)
}

// semEdgeCacheMax bounds the per-corpus SemanticEdges cache. The graph view is queried for at
// most a handful of scope filters; past that the corpus has almost certainly changed, so we drop
// the whole map rather than maintain an LRU — a stale corpus invalidates every entry anyway.
const semEdgeCacheMax = 8

// SemanticEdges returns the embedding-derived *relationship* set for the brain graph: for each
// record, edges to its nearest neighbours in embedding space (cosine ≥ the configured edge
// threshold, capped per node). The frontend force simulation self-balances these into clusters,
// so similar memories pull together organically — the backend supplies nodes and relationships,
// never positions. Returns nil when semantic mode is off (no embedder), so the graph degrades to
// the structural (provenance/related/lexical) edges.
//
// The result is memoized by a corpus fingerprint: a repeated load over an unchanged corpus skips
// both the re-embed and the O(n²·d) kNN and returns the cached edges. Threshold and per-node cap
// come from the resolved graph config, so a deployment can tune the graph's density.
func (s *Service) SemanticEdges(ctx context.Context, records []memory.Record) ([]memory.Edge, error) {
	if !s.semantic || len(records) < 2 {
		return nil, nil
	}

	// Fingerprint the request (corpus content + the resolved knobs). A hit skips everything
	// below; a miss recomputes and caches under the new key. The fingerprint changes the
	// instant any input fact's text or id changes, so a cached result can never go stale.
	fp := semanticEdgeFingerprint(records, s.graph.EdgeThreshold, s.graph.EdgeCap)
	s.semEdgeMu.Lock()
	if edges, ok := s.semEdgeCache[fp]; ok {
		s.semEdgeMu.Unlock()
		return edges, nil
	}
	s.semEdgeMu.Unlock()

	vecs, err := s.embedRecords(ctx, records)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	mat := make([][]float32, 0, len(records))
	for _, r := range records {
		if v, ok := vecs[r.ID]; ok && len(v) > 0 {
			ids = append(ids, r.ID)
			mat = append(mat, v)
		}
	}
	edges := memory.SemanticEdges(ids, mat, s.graph.EdgeThreshold, s.graph.EdgeCap)

	s.semEdgeMu.Lock()
	if len(s.semEdgeCache) >= semEdgeCacheMax {
		s.semEdgeCache = make(map[string][]memory.Edge)
	}
	s.semEdgeCache[fp] = edges
	s.semEdgeMu.Unlock()
	return edges, nil
}

// semanticEdgeFingerprint is a content hash of a SemanticEdges request: every input record's id
// and text (text drives the embedding, id labels the edge) plus the threshold and per-node cap
// that shape the output. Records are hashed in id order, so the same corpus fingerprints
// identically regardless of List order; ids and texts are length-prefixed so no id/text content
// can forge a collision across the field boundary. Any fact added, removed, or edited — or a knob
// change — yields a new fingerprint.
func semanticEdgeFingerprint(records []memory.Record, threshold float64, perNodeCap int) string {
	byID := make(map[string]string, len(records))
	ids := make([]string, 0, len(records))
	for _, r := range records {
		byID[r.ID] = r.Text
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	h := sha256.New()
	fmt.Fprintf(h, "t=%v;cap=%d;", threshold, perNodeCap)
	for _, id := range ids {
		text := byID[id]
		fmt.Fprintf(h, "%d:%s%d:%s", len(id), id, len(text), text)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ScopeForProject maps an agentique project ID to a memory scope. An empty
// project ID maps to the global scope.
func ScopeForProject(projectID string) memory.Scope {
	if strings.TrimSpace(projectID) == "" {
		return memory.ScopeGlobal
	}
	return memory.Scope("project:" + projectID)
}

// recallScopes returns the scopes to search for a primary scope: the scope itself
// plus global (deduplicated).
func recallScopes(scope memory.Scope) []memory.Scope {
	if scope == memory.ScopeGlobal || scope == "" {
		return []memory.Scope{memory.ScopeGlobal}
	}
	return []memory.Scope{scope, memory.ScopeGlobal}
}

// Add stores a new memory, deduplicating against existing memories in the same
// scope and global. If a duplicate exists it is returned unchanged (idempotent).
func (s *Service) Add(ctx context.Context, scope memory.Scope, text string, category memory.Category, source memory.Source) (memory.Record, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return memory.Record{}, fmt.Errorf("brain: empty memory text")
	}
	if category == "" {
		category = memory.CategoryFact
	}
	if source == "" {
		source = memory.SourceAgent
	}
	// Hold s.mu across the whole List → dedup/Reinforce → Put critical section so a
	// concurrent ingest (M3 fires lock-free Capture on every clean completion) and the
	// churn (Consolidate already holds s.mu) are mutually exclusive — no lost Reinforce
	// increment (RMW), no cachestore stale-cache install. No caller holds s.mu here.
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.store.List(ctx, recallScopes(scope)...)
	if err != nil {
		return memory.Record{}, err
	}
	// Dedup against durable facts only — never against raw episodic captures,
	// which would drop the durable write and echo a capture back to the caller.
	existing := make([]memory.Record, 0, len(all))
	for _, r := range all {
		if !r.Source.Staged() {
			existing = append(existing, r)
		}
	}
	if dup, ok := memory.FindDuplicate(text, existing, memory.DefaultDuplicateThreshold); ok {
		// Re-observation of a known durable fact: strengthen it (count the corroboration,
		// refresh recency, nudge confidence toward the ceiling) instead of discarding the
		// signal, and return the strengthened record (same ID).
		reinforced := memory.Reinforce(dup, time.Now().UTC())
		if err := s.store.Put(ctx, reinforced); err != nil {
			return memory.Record{}, fmt.Errorf("brain: reinforce duplicate %s: %w", dup.ID, err)
		}
		return reinforced, nil
	}
	r := memory.New(scope, text, category, source)
	if category == memory.CategoryIdentity {
		r.Pinned = true
	}
	if err := s.store.Put(ctx, r); err != nil {
		return memory.Record{}, err
	}
	return r, nil
}

// Capture stages a RAW episodic memory (Source "capture") from a finished session,
// carrying the candidate's own category, for later promotion by the churn. Captures are
// the ingest tier (tier 1): NEVER injected (recall excludes every capture-tier source)
// and NEVER pinned — not even CategoryIdentity, because pinning would inject a raw
// capture. The only path to injectability is consolidation promoting capture →
// consolidated (stamping DerivedFrom provenance). In M2 there is no dedup: genuinely-new
// captures accumulate and capture-vs-capture never dedups; M4 adds capture-vs-*durable*
// reinforcement on this same signature (the dedup set stays durable-only, so this
// invariant holds).
func (s *Service) Capture(ctx context.Context, scope memory.Scope, text string, category memory.Category) (memory.Record, error) {
	return s.CaptureFrom(ctx, scope, text, category, memory.SourceCapture)
}

// CaptureFrom is [Service.Capture] with the capture tier's provenance named.
//
// Two sources reach this door and they are not interchangeable: a sentence the
// operator said (memory.SourceCapture) and a sentence an agent wrote about
// repository content nobody here authored (memory.SourceReported). Both are staged
// and neither is injectable, so the distinction costs nothing today.
//
// It also buys nothing yet, and that is worth knowing before relying on it:
// consolidation folds both tiers into one untyped slice of sentences
// (memory.PlanConsolidation) and mints every survivor as memory.SourceConsolidated,
// so a promoted fact carries no trace of which door it came through. The tier is
// preserved HERE because losing it at the door is irreversible, and weighing the two
// differently needs the extractor to take records rather than strings — see
// docs/assistant.md, "Build notes".
//
// A source that is not capture tier is REFUSED rather than coerced: this is the one
// entry point whose whole contract is "staged, never injected", and a durable source
// arriving here would write an injectable fact through a door that promises it cannot.
func (s *Service) CaptureFrom(ctx context.Context, scope memory.Scope, text string, category memory.Category, source memory.Source) (memory.Record, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return memory.Record{}, fmt.Errorf("brain: empty capture text")
	}
	if category == "" {
		category = memory.CategoryFact
	}
	if !source.Staged() {
		return memory.Record{}, fmt.Errorf("brain: capture source %q is not capture tier", source)
	}
	// Same atomic critical section + race fix as Add (see there). A re-observation arriving
	// on the ingest tier reinforces the DURABLE fact instead of stacking a redundant capture;
	// the dedup set stays durable-only, so capture-vs-capture still never dedups (M2's invariant).
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.store.List(ctx, recallScopes(scope)...)
	if err != nil {
		return memory.Record{}, err
	}
	existing := make([]memory.Record, 0, len(all))
	for _, r := range all {
		if !r.Source.Staged() {
			existing = append(existing, r)
		}
	}
	if dup, ok := memory.FindDuplicate(text, existing, memory.DefaultDuplicateThreshold); ok {
		reinforced := memory.Reinforce(dup, time.Now().UTC())
		if err := s.store.Put(ctx, reinforced); err != nil {
			return memory.Record{}, fmt.Errorf("brain: reinforce duplicate %s: %w", dup.ID, err)
		}
		return reinforced, nil
	}
	r := memory.New(scope, text, category, source) // genuinely-new: stage it (Pinned stays false)
	if err := s.store.Put(ctx, r); err != nil {
		return memory.Record{}, fmt.Errorf("brain: capture: %w", err)
	}
	return r, nil
}

// Recall returns pinned plus query-relevant memories across the given scopes, for a
// person to read: the memory page's search box and `agentique brain search`.
//
// It applies no read-time disuse fade. A faded fact is one the churn has not archived
// yet, so it is still a live row in the list beside this search — see
// [Service.RecallForPull] for the half that does fade, and the archiveFloor field for
// why the split is here rather than in memory.Recall.
func (s *Service) Recall(ctx context.Context, scopes []memory.Scope, query string, k int) (memory.Result, error) {
	return s.recall(ctx, scopes, query, k, 0)
}

// RecallForPull is [Service.Recall] for a model: the pull behind the assistant's
// `recall` verb, reached through internal/server's assistant.Memory.
//
// The one difference is the read-time disuse fade (M5): with archiving enabled a fact
// whose effective confidence has eroded to the floor is dropped WITHOUT being written,
// so a cold fact stops being asserted to a head a while before the churn archives it,
// reversibly. That is what an operator opting into `archive-after` asked for, and this
// is the only surface that feeds what it returns to a model — the fade would be a lie
// on a page whose job is showing what is there.
func (s *Service) RecallForPull(ctx context.Context, scopes []memory.Scope, query string, k int) (memory.Result, error) {
	return s.recall(ctx, scopes, query, k, s.archiveFloor)
}

// recall is the one query both entry points build, so the thresholds cannot drift
// between the surface a person reads and the one a head pulls from.
func (s *Service) recall(ctx context.Context, scopes []memory.Scope, query string, k int, archiveFloor float64) (memory.Result, error) {
	return memory.Recall(ctx, s.store, memory.Query{
		Text:             query,
		Scopes:           scopes,
		K:                k,
		VectorVetoScore:  s.vetoScore,
		VectorVouchScore: s.cosThresh,
		ArchiveFloor:     archiveFloor,
	})
}

// List returns memories in the given scopes (all scopes when none given).
func (s *Service) List(ctx context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	return s.store.List(ctx, scopes...)
}

// PinnedPreamble formats the always-injected (pinned) facts for a project plus
// global as a system-preamble block, or "" when there are none. Read-only.
// Pinned facts are exempt from decay, so injection doesn't bump their use count.
//
// NOTHING RENDERS THIS TODAY. It composed a coding session's preamble, and as of
// M2 memory reaches no coding session at all (docs/assistant.md, the M2
// contract): the assistant is the brain's only reader and it builds its own
// "What you remember" section from [Service.List]. It is kept because the shape —
// pinned facts for a scope, formatted for a model — is what a second consumer of
// the liftable core would want, and its text no longer names a tool: the two
// sentences that told a model to use MemorySearch and MemoryFlag outlived those
// tools by one release and are gone.
func (s *Service) PinnedPreamble(ctx context.Context, projectID string) string {
	scope := ScopeForProject(projectID)
	// Empty query => pinned only (the relevance path needs a query).
	res, err := memory.Recall(ctx, s.store, memory.Query{Scopes: recallScopes(scope)})
	if err != nil || len(res.Pinned) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Memory (your persistent brain)\n\n")
	b.WriteString("Durable facts learned about this user and project across past sessions — treat them as established context.\n")
	for _, r := range res.Pinned {
		b.WriteString("- ")
		b.WriteString(r.Text)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// OperatingContract formats the project's high-confidence preferences as a directive
// system-preamble block — standing instructions the agent should act on by default, NOT
// the soft "background context, verify first" framing of PinnedPreamble/RecallBlock
// (brain.md#the-outcome-signal, part 2). Only CategoryPreference facts at/above
// memory.ActOnConfidence and not flagged for review qualify: a preference earns the
// authority to drive behavior by being human-confirmed or outcome-corroborated. Returns
// "" when the brain is disabled, the project is empty, or nothing qualifies. Read-only.
//
// Nothing renders this today either, and for the same reason as [Service.PinnedPreamble];
// see there.
func (s *Service) OperatingContract(ctx context.Context, projectID string) string {
	scope := ScopeForProject(projectID)
	all, err := s.store.List(ctx, recallScopes(scope)...)
	if err != nil {
		slog.Warn("brain: operating-contract list failed", "project", projectID, "error", err)
		return ""
	}
	contract := make([]memory.Record, 0, len(all))
	for _, r := range all {
		if r.Category != memory.CategoryPreference || r.Source.Staged() {
			continue
		}
		if memory.IsArchived(r) { // archived = cold tier, never an acted-on directive (M5)
			continue
		}
		if r.ReviewNote != "" { // flagged/contradicted prefs don't get to drive behavior
			continue
		}
		if memory.NormalizeConfidence(r).ConfidenceScore < memory.ActOnConfidence {
			continue
		}
		contract = append(contract, r)
	}
	if len(contract) == 0 {
		return ""
	}
	// Deterministic: strongest first, then id. Stable across restarts and testable.
	sort.Slice(contract, func(i, j int) bool {
		ci, cj := contract[i].ConfidenceScore, contract[j].ConfidenceScore
		if ci != cj {
			return ci > cj
		}
		return contract[i].ID < contract[j].ID
	})
	var b strings.Builder
	b.WriteString("## Operating contract (act on these by default)\n\n")
	b.WriteString("High-confidence preferences the user confirmed or that have proven correct across sessions. Treat them as standing instructions to follow without re-asking — not background context. An explicit instruction this session overrides a contract item; say so plainly if one turns out stale or wrong.\n")
	for _, r := range contract {
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(r.Text, "\n", " "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// minRecallQueryTokens gates recall: a query with fewer distinct content tokens than
// this ("ok", "go for it", "sounds good") carries too little retrieval intent to query
// against, so nothing is recalled for it.
const minRecallQueryTokens = 2

// RecallBlock runs a relevance query and returns a `<brain>` envelope of
// query-relevant, non-pinned facts, plus the ids it surfaced.
//
// NOTHING CALLS THIS, AND WIRING IT BACK WOULD BREAK THE M2 INVARIANT. It was the
// session-injection composer: `Session.injectRecall` asked it for a block each turn and
// passed the seen-set as exclude, which is the per-turn delta recall the M2 contract
// removed. It is NOT the assistant's `recall` verb — that reads [Service.Recall] through
// internal/server's assistant.Memory, over every scope, with no envelope and no
// seen-set, because knowledge is pulled and only news is pushed (docs/assistant.md).
// CLAUDE.md's brain section spells the rule: do not reintroduce injection into sessions,
// not per-turn, not first-turn, not pinned facts in a preamble.
//
// It survives only as a test lens, like [Service.PinnedPreamble]: six cases in this
// package drive real recall semantics through it — the veto and vouch thresholds, the
// lone-token guard, the capture gate, cross-scope leakage and associative expansion.
// Repointing those at [Service.Recall] and deleting this is the structurally correct
// follow-up; it is a test refactor rather than a removal, which is why the M2 passes
// left it.
//
// exclude is the set of fact ids the caller already holds; they are filtered out so a
// second call returns only what is new. Returns ("", nil) when the store is empty, the
// query is too thin, or nothing new matches. Pinned facts and captures are never
// included (pinned facts were the preamble's own always-on set; captures are unpromoted).
//
// It stamps BumpUses/LastUsedAt on every fact it returns. Best-effort: a stamp failure is
// logged, never fatal to the call.
func (s *Service) RecallBlock(ctx context.Context, projectID, prompt string, exclude map[string]struct{}) (string, []string) {
	prompt = strings.TrimSpace(prompt)
	if memory.TokenCount(prompt) < minRecallQueryTokens {
		return "", nil
	}
	scope := ScopeForProject(projectID)
	res, err := memory.Recall(ctx, s.store, memory.Query{Text: prompt, Scopes: recallScopes(scope), VectorVetoScore: s.vetoScore, VectorVouchScore: s.cosThresh, ArchiveFloor: s.archiveFloor})
	if err != nil {
		slog.Warn("brain: task-relevant recall failed", "project", projectID, "error", err)
		return "", nil
	}

	fresh := make([]memory.Record, 0, len(res.Recalled))
	for _, r := range res.Recalled {
		if _, seen := exclude[r.ID]; seen {
			continue // already surfaced this session — don't re-inject
		}
		fresh = append(fresh, r)
	}
	if len(fresh) == 0 {
		return "", nil
	}

	ids := make([]string, 0, len(fresh))
	var b strings.Builder
	// A <brain> envelope keeps recalled memory unambiguously separate from the words
	// of whoever asked, and the frontend parses the tag to render a "Recalled from
	// memory" card for the transcripts that still carry one.
	b.WriteString("<brain>\n")
	for _, r := range fresh {
		// id as an attribute keeps the UUID out of the prose, and it is what the
		// caller passes back to confirm or flag the fact — see
		// brain.md#the-outcome-signal.
		fmt.Fprintf(&b, "  <fact id=%q>%s</fact>\n", r.ID, escapeFactText(strings.ReplaceAll(r.Text, "\n", " ")))
		ids = append(ids, r.ID)
	}
	b.WriteString("</brain>")
	if err := memory.BumpUses(ctx, s.store, ids...); err != nil {
		slog.Warn("brain: bump uses on recall injection", "project", projectID, "error", err)
	}
	return b.String(), ids
}

// escapeFactText escapes the three characters that would break the <fact> element
// when the frontend parses the <brain> envelope. Quotes are deliberately left raw:
// they only need escaping inside attributes (the id, which is a clean UUID), and
// escaping them in the body would make the text read worse to the model.
func escapeFactText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;") // must run first
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// ImportRecords merges records into targetScope, skipping any that duplicate an
// existing fact in that scope (or global). It preserves text/category/source and
// the pinned/locked flags but assigns fresh IDs and timestamps, so importing the
// same bundle twice is idempotent. Returns the number of new facts written.
func (s *Service) ImportRecords(ctx context.Context, targetScope memory.Scope, recs []memory.Record) (int, error) {
	existing, err := s.store.List(ctx, recallScopes(targetScope)...)
	if err != nil {
		return 0, err
	}
	pool := make([]memory.Record, 0, len(existing))
	for _, r := range existing {
		if !r.Source.Staged() {
			pool = append(pool, r)
		}
	}
	added := 0
	for _, src := range recs {
		text := strings.TrimSpace(src.Text)
		if text == "" {
			continue
		}
		if _, dup := memory.FindDuplicate(text, pool, memory.DefaultDuplicateThreshold); dup {
			continue
		}
		category := src.Category
		if category == "" {
			category = memory.CategoryFact
		}
		source := src.Source
		if source == "" {
			source = memory.SourceHuman
		}
		nr := memory.New(targetScope, text, category, source)
		nr.Pinned = src.Pinned
		nr.Locked = src.Locked
		if err := s.store.Put(ctx, nr); err != nil {
			return added, err
		}
		pool = append(pool, nr)
		added++
	}
	return added, nil
}

// ListScopes returns the distinct scopes that currently hold memories — used by
// scheduled consolidation to know what to consolidate.
func (s *Service) ListScopes(ctx context.Context) ([]memory.Scope, error) {
	all, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[memory.Scope]struct{}, len(all))
	var scopes []memory.Scope
	for _, r := range all {
		if _, ok := seen[r.Scope]; ok {
			continue
		}
		seen[r.Scope] = struct{}{}
		scopes = append(scopes, r.Scope)
	}
	return scopes, nil
}

// Get returns a single memory by ID.
func (s *Service) Get(ctx context.Context, id string) (memory.Record, error) {
	return s.store.Get(ctx, id)
}

// Delete removes a memory by ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

// Update edits a memory's text/category. Because edits come from a human on the
// memory page, the record is marked human-authored (and thus protected from
// consolidation rewrite/decay).
func (s *Service) Update(ctx context.Context, id, text string, category memory.Category) (memory.Record, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return memory.Record{}, err
	}
	if t := strings.TrimSpace(text); t != "" {
		r.Text = t
	}
	if category != "" {
		r.Category = category
	}
	r.Source = memory.SourceHuman
	r.ReviewNote = "" // a hand-edit resolves any pending review
	if r.Lifecycle == memory.LifecycleArchived {
		// A hand-edit revives an archived fact: flip it back to active and restart the disuse
		// clock so it is not immediately re-archived (M5 restore path).
		r.Lifecycle = memory.LifecycleActive
		r.LastUsedAt = time.Now().UTC()
	}
	r.UpdatedAt = time.Now().UTC()
	if err := s.store.Put(ctx, r); err != nil {
		return memory.Record{}, err
	}
	return r, nil
}

// SetPinned toggles whether a memory is always injected.
func (s *Service) SetPinned(ctx context.Context, id string, pinned bool) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) { r.Pinned = pinned })
}

// SetLocked toggles whether a memory is exempt from consolidation/decay.
func (s *Service) SetLocked(ctx context.Context, id string, locked bool) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) { r.Locked = locked })
}

// Confirm marks a low-confidence fact as user-confirmed ground truth: it becomes
// human-authored (EXTRACTED, top score) and is thereby exempt from consolidation
// rewrite and decay. This is the accept side of the "confirm what I'm unsure about"
// UX (RFC P2); the reject side is a plain Delete.
func (s *Service) Confirm(ctx context.Context, id string) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) {
		r.Source = memory.SourceHuman
		r.Confidence = memory.ConfidenceExtracted
		r.ConfidenceScore = memory.ScoreGroundTruth
		r.ReviewNote = "" // confirming resolves any pending review
		r.UpdatedAt = time.Now().UTC()
	})
}

// Flag records that a memory was found contradicted (RFC-LD D2 reconsolidation):
// it weakens a non-protected fact into the review band and stores the reason, never
// deleting it — the human confirms (accepts), edits, or deletes from the queue. The
// entry points are the assistant's flag_memory verb and the memory page's own
// control; the reject UI is Delete. No session tool reaches it (the M2 contract).
func (s *Service) Flag(ctx context.Context, id, reason string) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) {
		*r = memory.MarkContradicted(*r, reason, time.Now().UTC())
	})
}

// MarkHelped records the POSITIVE outcome (RFC-LD D2, brain.md#the-outcome-signal): a
// reader confirmed a recalled fact was used/correct. It increments Helped, refreshes
// recency, and raises a non-protected fact's confidence toward CorroborationCeiling — so
// earned trust can graduate a preference into the operating contract. The negative twin
// is Flag.
//
// NOTHING PRODUCES THIS TODAY. The session-era MemoryUsed tool that fed it is gone with
// the rest of the session surface (the M2 contract), and the assistant's own accept half
// is [Service.Confirm]: a pull is not a confirmation, so recall alone must not move
// trust. It is kept because the positive half of the outcome signal is a shape a second
// consumer of the liftable core would want, not because something calls it.
func (s *Service) MarkHelped(ctx context.Context, id string) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) {
		*r = memory.MarkHelped(*r, time.Now().UTC())
	})
}

// MarkAutoHelped records the same positive outcome as MarkHelped but with the gentler
// AutoCorroborationGapClose weight: it is what an INFERRED outcome is worth, where
// something judged that a fact helped rather than a reader saying so. The Helped count
// and recency stamp are identical; only the confidence step is softer, so a machine
// inference can never move trust as fast as an explicit MarkHelped or a human Confirm.
//
// Producerless for the same reason as MarkHelped: the session-end transcript judge that
// emitted it went with the session surface (the M2 contract). The weight is the part
// worth keeping — whoever comes to produce an inferred outcome inherits the rule that it
// weighs half an explicit one, which is what its remaining test pins.
func (s *Service) MarkAutoHelped(ctx context.Context, id string) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) {
		*r = memory.MarkHelpedWith(*r, time.Now().UTC(), memory.AutoCorroborationGapClose)
	})
}

// Restore pulls an archived (cold-tier) fact back into the live set: it flips Lifecycle
// to active and restarts the disuse clock (LastUsedAt=now) so the fact is not immediately
// re-archived, exactly mirroring the Update un-archive branch but without a content edit.
// A non-archived fact is left unchanged (idempotent). This is a NORMAL write — it funnels
// through mutate/Put, so the read-through cache stays consistent (unlike a snapshot
// restore, which rewrites files underneath the cache). It is the dedicated, no-edit
// counterpart to the M5 "archived = restorable" cold tier (brain.md#brain-ui F3).
func (s *Service) Restore(ctx context.Context, id string) (memory.Record, error) {
	return s.mutate(ctx, id, func(r *memory.Record) {
		if r.Lifecycle != memory.LifecycleArchived {
			return // already live — no-op
		}
		r.Lifecycle = memory.LifecycleActive
		r.LastUsedAt = time.Now().UTC()
	})
}

func (s *Service) mutate(ctx context.Context, id string, fn func(*memory.Record)) (memory.Record, error) {
	// Hold s.mu across Get→fn→Put so a human action (Confirm/Flag/SetPinned/…) is not lost to a
	// concurrent reinforce (Add/Capture) or churn (Consolidate) RMW — all the durable single-fact
	// writers funnel through s.mu. No s.mu holder calls mutate, so there is no self-deadlock. (The
	// per-turn recall BumpUses stays unlocked by design — counters are approximate and recall must
	// not block on a long churn; see docs/tech-debt.md.)
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return memory.Record{}, err
	}
	fn(&r)
	if err := s.store.Put(ctx, r); err != nil {
		return memory.Record{}, err
	}
	return r, nil
}

// MarkUsed increments the use counter for memories that were injected/returned.
func (s *Service) MarkUsed(ctx context.Context, ids ...string) error {
	return memory.BumpUses(ctx, s.store, ids...)
}

// Consolidate runs the consolidation pass for one scope, threading the persisted
// fingerprint so the LLM reorganization is skipped when nothing changed. A nil
// Extractor restricts the pass to deterministic decay. When dryRun is set the
// pass writes nothing — it returns the changelog it WOULD apply and leaves the
// persisted fingerprint untouched so a later real run still proceeds. opts carries
// Force (reorganize even when the scope is unchanged — re-consolidate after a
// prompt/algorithm change) and MinSurvivorRatio (relax the over-deletion guard for
// an aggressive pass); its zero value reproduces the conservative behaviour.
func (s *Service) Consolidate(ctx context.Context, scope memory.Scope, ex memory.Extractor, decay memory.DecayPolicy, dryRun bool, opts ConsolidateOpts) (memory.Report, error) {
	// Semantic SimOptions for the post-apply graph rebuild (embeds the scope) — computed
	// before the lock and skipped on dry run, as in ApplyPlan.
	var simOpts []memory.SimOption
	if !dryRun {
		simOpts = s.scopeSimOptions(ctx, scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fps := s.loadFingerprints()
	rep, err := memory.Consolidate(ctx, s.store, ex, scope, memory.ConsolidateOptions{
		PrevFingerprint:  fps[string(scope)],
		Force:            opts.Force,
		Decay:            decay,
		DryRun:           dryRun,
		MinSurvivorRatio: opts.MinSurvivorRatio,
		SimOptions:       simOpts,
	})
	if err != nil {
		return rep, err
	}
	if !dryRun {
		fps[string(scope)] = rep.Fingerprint
		s.saveFingerprints(fps)
	}
	return rep, nil
}

// ConsolidateOpts are the per-scope consolidation knobs the brain layer adds on top of
// the model (which the Extractor carries). Force re-runs the reorganization even
// when the scope is unchanged since the last pass (re-consolidate after a prompt/algorithm
// change). MinSurvivorRatio relaxes the over-deletion guard for an aggressive consolidation
// (0 = conservative default). The zero value reproduces the original behaviour.
type ConsolidateOpts struct {
	Force            bool
	MinSurvivorRatio float64
}

// Plan runs the LLM phase of consolidation for a scope and returns the proposal
// without writing anything. The model runs only here; the caller previews the plan
// (ApplyPlan with dryRun) and then applies it (ApplyPlan), so Opus is never invoked
// twice for one preview→apply cycle.
// Plan is read-only (lists facts, calls the model), so it deliberately does NOT
// hold s.mu: that lock guards writes/fingerprints and must not be held across a
// multi-minute LLM run, which would block every other brain op (a live recall
// included). Staleness is caught by ApplyPlan's fingerprint check.
func (s *Service) Plan(ctx context.Context, scope memory.Scope, ex memory.Extractor, decay memory.DecayPolicy, opts ConsolidateOpts) (memory.Plan, error) {
	fps := s.loadFingerprints()
	return memory.PlanConsolidation(ctx, s.store, ex, scope, memory.ConsolidateOptions{
		PrevFingerprint:  fps[string(scope)],
		Force:            opts.Force,
		Decay:            decay,
		MinSurvivorRatio: opts.MinSurvivorRatio,
	})
}

// ApplyPlan applies (dryRun=false) or previews (dryRun=true) a plan deterministically
// — no model calls. It returns memory.ErrStalePlan if the scope changed since the
// plan was made. A real apply persists the new fingerprint so the next pass can skip
// an unchanged set.
func (s *Service) ApplyPlan(ctx context.Context, scope memory.Scope, plan memory.Plan, decay memory.DecayPolicy, dryRun bool) (memory.Report, error) {
	// Compute semantic SimOptions (embeds the scope) BEFORE taking the lock: s.mu guards
	// writes/fingerprints and must not be held across a network embed. Skipped on dry run
	// (the post-apply graph rebuild that consumes them doesn't run for a preview).
	var simOpts []memory.SimOption
	if !dryRun {
		simOpts = s.scopeSimOptions(ctx, scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, err := memory.ApplyPlan(ctx, s.store, scope, plan, memory.ConsolidateOptions{
		Decay:      decay,
		DryRun:     dryRun,
		SimOptions: simOpts,
	})
	if err != nil {
		return rep, err
	}
	if !dryRun {
		fps := s.loadFingerprints()
		fps[string(scope)] = rep.Fingerprint
		s.saveFingerprints(fps)
	}
	return rep, nil
}

// PlanGlobal runs the LLM phase of cross-scope consolidation: it scans every
// project scope and proposes which facts to lift into global (recurring across
// projects, or inherently user-level), subsuming the per-project copies. Writes
// nothing; the model runs only here.
// PlanGlobal is read-only (see Plan); it takes opts so the host can thread live
// progress and per-batch error callbacks through the chunked promotion pass. No
// lock is held during the LLM run.
//
// It loads the persisted per-scope manifest as opts.PrevManifest so the pass can
// skip the model when no project changed since the last global pass (RFC P5 — the
// incremental rebuild). When the (non-skipped) pass yields nothing to promote it
// records the current manifest, so repeated previews over an unchanged, already-
// clean brain stay cheap. A pass that DOES propose promotions records nothing — the
// manifest only advances once those promotions are actually applied (see ApplyGlobal),
// so an unapplied preview can never be wrongly skipped.
func (s *Service) PlanGlobal(ctx context.Context, pr memory.Promoter, opts memory.ConsolidateOptions) (memory.GlobalPlan, error) {
	if opts.PrevManifest == nil {
		s.mu.Lock()
		opts.PrevManifest = s.loadGlobalManifest()
		s.mu.Unlock()
	}
	plan, err := memory.PlanGlobalPromotion(ctx, s.store, pr, opts)
	if err != nil {
		return plan, err
	}
	if !plan.Skipped && len(plan.Promotions) == 0 {
		s.mu.Lock()
		s.saveGlobalManifest(plan.Fingerprints)
		s.mu.Unlock()
	}
	return plan, nil
}

// ApplyGlobal applies (dryRun=false) or previews (dryRun=true) a global plan
// deterministically — no model calls. Returns memory.ErrStalePlan if any affected
// scope changed since the plan was made. A real apply invalidates the persisted
// per-scope fingerprints of the scopes it touched so a later consolidation re-evaluates them.
func (s *Service) ApplyGlobal(ctx context.Context, plan memory.GlobalPlan, dryRun bool) (memory.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, err := memory.ApplyGlobalPromotion(ctx, s.store, plan, memory.ConsolidateOptions{DryRun: dryRun})
	if err != nil {
		return rep, err
	}
	if dryRun {
		return rep, nil
	}
	if len(rep.Deleted) > 0 || len(rep.Promoted) > 0 {
		fps := s.loadFingerprints()
		for _, r := range rep.Deleted {
			delete(fps, string(r.Scope))
		}
		delete(fps, string(memory.ScopeGlobal))
		s.saveFingerprints(fps)
	}
	// Advance the global manifest to the post-apply state (recomputed from the live
	// store, since apply may have deleted subsumed copies) so the next pass can skip
	// while no project changes. RFC P5 incremental rebuild.
	if m, merr := memory.ScopeManifest(ctx, s.store); merr == nil {
		s.saveGlobalManifest(m)
	}
	// A promotion changed cross-scope structure — refresh topic areas (B). Use the
	// Service method (s.AssignAreas), not memory.AssignAreas directly, so the rebuild is
	// embedding-aware in semantic mode (C); the bare memory call was lexical-only even
	// with an embedder configured. s.AssignAreas does not take s.mu, so no re-entrancy.
	if _, aerr := s.AssignAreas(ctx); aerr != nil {
		slog.Warn("brain: assign areas after global apply failed", "error", aerr)
	}
	return rep, nil
}

// AssignAreas recomputes the cross-scope topic areas across the whole brain and persists
// Record.Area (B). Run after a pass that can change cross-scope structure — scheduled
// consolidation, consolidate-all, or a global promotion. In semantic mode it embeds the corpus and blends
// cosine into the area clustering (C); otherwise it is lexical. Deterministic and
// idempotent; the area index is rebuildable, never the source of truth. Returns the
// number of records whose area changed.
func (s *Service) AssignAreas(ctx context.Context) (int, error) {
	all, err := s.store.List(ctx)
	if err != nil {
		return 0, err
	}
	durable := durableRecords(all)
	opts := s.semanticSimOptions(ctx, durable)
	// Whole-brain checkpoint: trim cache entries for texts no longer present (edits/deletes
	// since the last pass) so the cache stays bounded by the live corpus.
	s.pruneEmbedCache(durable)
	return memory.AssignAreas(ctx, s.store, memory.DefaultAreaThreshold, memory.DefaultMinPromotionScopes, opts...)
}

// PreviewAreas computes the same cross-scope areas as [Service.AssignAreas] and persists
// NOTHING — the read behind the assistant's memory index, which carries one line per area
// with its size and its scopes and never a fact's text (docs/assistant.md, the M2
// contract).
//
// It is [Service.AssignAreas] minus the two things a write pass does: it does not store
// Record.Area, and it does not prune the embed cache, which is a whole-brain checkpoint
// that belongs to a pass that actually rewrote something. It takes no lock for the same
// reason AssignAreas does not.
//
// The corpus listing is skipped entirely without an embedder: semanticSimOptions is the
// only thing that wanted it, and lexical clustering reads the store itself.
func (s *Service) PreviewAreas(ctx context.Context) ([]memory.AreaInfo, error) {
	var opts []memory.SimOption
	if s.embedder != nil {
		all, err := s.store.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("brain: preview areas: %w", err)
		}
		opts = s.semanticSimOptions(ctx, durableRecords(all))
	}
	infos, err := memory.PreviewAreas(ctx, s.store, memory.DefaultAreaThreshold,
		memory.DefaultMinPromotionScopes, opts...)
	if err != nil {
		return nil, fmt.Errorf("brain: preview areas: %w", err)
	}
	return infos, nil
}

// durableRecords returns the non-capture records (the set areas/links cluster over).
func durableRecords(all []memory.Record) []memory.Record {
	out := make([]memory.Record, 0, len(all))
	for _, r := range all {
		if !r.Source.Staged() {
			out = append(out, r)
		}
	}
	return out
}

// scopeSimOptions builds the semantic SimOptions for a single-scope clustering pass
// (ApplyPlan/Consolidate's post-apply RelinkScope + AssignCommunities). It lists the
// scope's durable records and embeds them; nil (lexical-only) when no embedder is set,
// the scope is empty, or listing/embedding fails — clustering then degrades to Jaccard.
func (s *Service) scopeSimOptions(ctx context.Context, scope memory.Scope) []memory.SimOption {
	if s.embedder == nil {
		return nil
	}
	all, err := s.store.List(ctx, scope)
	if err != nil {
		slog.Warn("brain: list scope for semantic clustering failed; clustering lexically", "scope", scope, "error", err)
		return nil
	}
	return s.semanticSimOptions(ctx, durableRecords(all))
}

// semanticSimOptions returns the SimOptions that turn on embedding-blended similarity for
// a clustering pass: a lookup over freshly-computed vectors for `records` plus the
// configured cosine threshold. Returns nil (lexical-only) when no embedder is configured
// or embedding fails, so clustering always degrades cleanly to Jaccard.
func (s *Service) semanticSimOptions(ctx context.Context, records []memory.Record) []memory.SimOption {
	if s.embedder == nil || len(records) == 0 {
		return nil
	}
	vecs, err := s.embedRecords(ctx, records)
	if err != nil {
		slog.Warn("brain: embed for similarity failed; clustering lexically", "error", err)
		return nil
	}
	return []memory.SimOption{
		memory.WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
		memory.WithCosineThreshold(s.cosThresh),
	}
}

// embedRecords returns id → vector for the records, embedding only texts not already in
// embedCache (keyed by text-hash) and memoizing the misses. Distinct miss TEXTS are
// embedded once each (deduped) and chunked to bound request size. The embedder is the only
// thing that touches the network, so this is what makes the now-frequent per-pass re-embed
// cheap after the first pass.
func (s *Service) embedRecords(ctx context.Context, records []memory.Record) (map[string][]float32, error) {
	out := make(map[string][]float32, len(records))

	// Seed the cache from the vector store once per process so a restart over an unchanged
	// corpus re-embeds nothing (the misses below then resolve from the warmed cache).
	s.warmEmbedCache(ctx)

	// Resolve cache hits and collect the distinct miss texts.
	s.embedMu.Lock()
	missByKey := make(map[string]string) // key -> text, deduped
	keyByID := make(map[string]string, len(records))
	for _, r := range records {
		key := embedKey(r.Text)
		keyByID[r.ID] = key
		if v, ok := s.embedCache[key]; ok {
			out[r.ID] = v
			continue
		}
		missByKey[key] = r.Text
	}
	s.embedMu.Unlock()

	if len(missByKey) > 0 {
		keys := make([]string, 0, len(missByKey))
		texts := make([]string, 0, len(missByKey))
		for k, t := range missByKey {
			keys = append(keys, k)
			texts = append(texts, t)
		}
		const batch = 64
		fresh := make(map[string][]float32, len(keys))
		for i := 0; i < len(texts); i += batch {
			end := i + batch
			if end > len(texts) {
				end = len(texts)
			}
			vecs, err := s.embedder.Embed(ctx, texts[i:end])
			if err != nil {
				return nil, err
			}
			if len(vecs) != end-i {
				return nil, fmt.Errorf("brain: embedder returned %d vectors for %d texts", len(vecs), end-i)
			}
			for j, v := range vecs {
				fresh[keys[i+j]] = v
			}
		}
		s.embedMu.Lock()
		for k, v := range fresh {
			s.embedCache[k] = v
		}
		s.embedMu.Unlock()
		// Fill the misses into the output by id.
		for _, r := range records {
			if _, ok := out[r.ID]; ok {
				continue
			}
			if v, ok := fresh[keyByID[r.ID]]; ok {
				out[r.ID] = v
			}
		}
	}
	return out, nil
}

// embedKey is the cache key for a text: a content hash. The embedding depends only on the
// text (the model is fixed per Service), so identical texts share a vector and an edited
// text gets a fresh key.
func embedKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

// warmEmbedCache seeds embedCache with the vectors already held by the semantic index, so the
// first clustering pass after a process restart does not re-embed an unchanged corpus. It runs
// at most once per process; a Chroma/network failure leaves the cache cold and is retried on
// the next pass (warmed stays false), never failing the caller. Keyed by text-hash, matching
// the live embed path — a fact whose text is unchanged since it was indexed resolves from the
// warmed entry. No-op in keyword mode (warmSrc nil). warmMu serializes concurrent first passes
// so only one bulk fetch runs.
func (s *Service) warmEmbedCache(ctx context.Context) {
	if s.warmSrc == nil {
		return
	}
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	if s.warmed {
		return
	}
	vecs, err := s.warmSrc.LoadVectors(ctx)
	if err != nil {
		slog.Warn("brain: warm embed cache from vector store failed; will retry next pass", "error", err)
		return // leave warmed=false so a transient failure doesn't permanently disable warming
	}
	s.embedMu.Lock()
	for _, v := range vecs {
		if len(v.Embedding) == 0 || v.Document == "" {
			continue
		}
		key := embedKey(v.Document)
		if _, ok := s.embedCache[key]; !ok {
			s.embedCache[key] = v.Embedding
		}
	}
	cached := len(s.embedCache)
	s.embedMu.Unlock()
	s.warmed = true
	slog.Info("brain: warmed embed cache from vector store", "vectors", len(vecs), "cached", cached)
}

// pruneEmbedCache drops cache entries whose text-hash is absent from live (the current durable
// corpus), bounding the cache by the live fact set rather than by every text ever embedded —
// edited/deleted facts' stale vectors don't accumulate. Called from the whole-brain checkpoint
// (AssignAreas, run after every scheduled-consolidation/consolidate-all/global pass) where the full live set is known;
// pruning on a per-scope embed would wrongly evict other scopes' entries. No-op in keyword mode.
func (s *Service) pruneEmbedCache(live []memory.Record) {
	if s.embedder == nil {
		return
	}
	keep := make(map[string]struct{}, len(live))
	for _, r := range live {
		keep[embedKey(r.Text)] = struct{}{}
	}
	s.embedMu.Lock()
	for k := range s.embedCache {
		if _, ok := keep[k]; !ok {
			delete(s.embedCache, k)
		}
	}
	s.embedMu.Unlock()
}

func (s *Service) loadFingerprints() map[string]string {
	m := map[string]string{}
	if data, err := os.ReadFile(s.fpPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func (s *Service) saveFingerprints(m map[string]string) {
	writeJSONAtomic(s.fpPath, m, "fingerprints")
}

// loadGlobalManifest / saveGlobalManifest persist the per-scope content-hash
// manifest of the last global promotion pass (RFC P5). Separate from the per-scope
// Consolidation fingerprints: this tracks "the state all projects were in the last time we
// looked for cross-scope patterns" so an incremental pass can skip the model when
// nothing changed. Callers hold s.mu.
func (s *Service) loadGlobalManifest() map[string]string {
	m := map[string]string{}
	if data, err := os.ReadFile(s.gmPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func (s *Service) saveGlobalManifest(m map[string]string) {
	writeJSONAtomic(s.gmPath, m, "global manifest")
}

// writeJSONAtomic marshals v and writes it to path via a temp file + rename so a
// crash mid-write can't leave a truncated index. label names the artifact in logs.
func writeJSONAtomic(path string, v any, label string) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("brain: persist "+label, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("brain: persist "+label, "error", err)
	}
}
