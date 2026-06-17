// Package brain is agentique's product service around the liftable internal/memory
// primitives. It composes a filestore (source of truth) with an optional
// Chroma+embeddings semantic index, maps agentique concepts (projects) to memory
// scopes, and persists per-scope consolidation fingerprints. All agentique-specific
// policy lives here so internal/memory and its sub-packages stay portable.
package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
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
}

// Service is the agentique brain.
type Service struct {
	store    memory.Store
	dir      string
	semantic bool

	mu     sync.Mutex // guards the fingerprint + global-manifest files
	fpPath string
	gmPath string // per-scope content-hash manifest of the last global pass (RFC P5)
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
	base := filestore.New(cfg.Dir)
	svc := &Service{
		store:  base,
		dir:    cfg.Dir,
		fpPath: filepath.Join(cfg.Dir, ".fingerprints.json"),
		gmPath: filepath.Join(cfg.Dir, ".global-manifest.json"),
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
				slog.Info("brain: semantic recall enabled", "collection", coll)
			}
		}
	}
	return svc, nil
}

// SemanticEnabled reports whether vector recall is active.
func (s *Service) SemanticEnabled() bool { return s.semantic }

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
	all, err := s.store.List(ctx, recallScopes(scope)...)
	if err != nil {
		return memory.Record{}, err
	}
	// Dedup against durable facts only — never against raw episodic captures,
	// which would drop the durable write and echo a capture back to the caller.
	existing := make([]memory.Record, 0, len(all))
	for _, r := range all {
		if r.Source != memory.SourceCapture {
			existing = append(existing, r)
		}
	}
	if dup, ok := memory.FindDuplicate(text, existing, memory.DefaultDuplicateThreshold); ok {
		return dup, nil
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

// Capture stages a raw episodic memory (Source "capture") for later distillation
// by the consolidation pass. Captures are never injected directly.
func (s *Service) Capture(ctx context.Context, scope memory.Scope, text string) (memory.Record, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return memory.Record{}, fmt.Errorf("brain: empty capture text")
	}
	r := memory.New(scope, text, memory.CategoryFact, memory.SourceCapture)
	if err := s.store.Put(ctx, r); err != nil {
		return memory.Record{}, err
	}
	return r, nil
}

// Recall returns pinned plus query-relevant memories across the given scopes.
func (s *Service) Recall(ctx context.Context, scopes []memory.Scope, query string, k int) (memory.Result, error) {
	return memory.Recall(ctx, s.store, memory.Query{Text: query, Scopes: scopes, K: k})
}

// List returns memories in the given scopes (all scopes when none given).
func (s *Service) List(ctx context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	return s.store.List(ctx, scopes...)
}

// PinnedPreamble formats the always-injected (pinned) facts for a project plus
// global as a system-preamble block, or "" when there are none. Read-only; this
// is the automatic, push side of recall (the agent still pulls more via
// MemorySearch). Pinned facts are exempt from decay, so injection doesn't bump
// their use count.
func (s *Service) PinnedPreamble(ctx context.Context, projectID string) string {
	scope := ScopeForProject(projectID)
	// Empty query => pinned only (the relevance path needs a query).
	res, err := memory.Recall(ctx, s.store, memory.Query{Scopes: recallScopes(scope)})
	if err != nil || len(res.Pinned) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Memory (your persistent brain)\n\n")
	b.WriteString("Durable facts learned about this user and project across past sessions — treat them as established context. Use the MemorySearch tool to recall more for the task at hand.\n")
	for _, r := range res.Pinned {
		b.WriteString("- ")
		b.WriteString(r.Text)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
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
		if r.Source != memory.SourceCapture {
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
// the periodic "sleep" pass to know what to consolidate.
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

// LearnFromTranscript distills durable facts from a finished session's transcript
// and adds them to the scope (deduped against existing facts). Best-effort: a
// chunk that fails extraction is skipped. Returns the count of new facts written.
func (s *Service) LearnFromTranscript(ctx context.Context, scope memory.Scope, events []TranscriptEvent, ex memory.Extractor) (int, error) {
	chunks := BuildTranscript(events, extractMaxChars)
	added := 0
	for _, chunk := range chunks {
		cands, err := ex.Extract(ctx, []string{chunk})
		if err != nil {
			continue
		}
		for _, c := range cands {
			if _, err := s.Add(ctx, scope, c.Text, c.Category, memory.SourceConsolidated); err == nil {
				added++
			}
		}
	}
	return added, nil
}

// Get returns a single memory by ID.
func (s *Service) Get(ctx context.Context, id string) (memory.Record, error) {
	return s.store.Get(ctx, id)
}

// Delete removes a memory by ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

// Update edits a memory's text/category. Because edits come from a human via the
// Brain UI, the record is marked human-authored (and thus protected from
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
		r.UpdatedAt = time.Now().UTC()
	})
}

func (s *Service) mutate(ctx context.Context, id string, fn func(*memory.Record)) (memory.Record, error) {
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
// Force (reorganize even when the scope is unchanged — re-tidy after a
// prompt/algorithm change) and MinSurvivorRatio (relax the over-deletion guard for
// an aggressive pass); its zero value reproduces the conservative behaviour.
func (s *Service) Consolidate(ctx context.Context, scope memory.Scope, ex memory.Extractor, decay memory.DecayPolicy, dryRun bool, opts TidyOptions) (memory.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fps := s.loadFingerprints()
	rep, err := memory.Consolidate(ctx, s.store, ex, scope, memory.ConsolidateOptions{
		PrevFingerprint:  fps[string(scope)],
		Force:            opts.Force,
		Decay:            decay,
		DryRun:           dryRun,
		MinSurvivorRatio: opts.MinSurvivorRatio,
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

// TidyOptions are the per-scope consolidation knobs the brain layer adds on top of
// the model (which the Extractor carries). Force re-runs the reorganization even
// when the scope is unchanged since the last pass (re-tidy after a prompt/algorithm
// change). MinSurvivorRatio relaxes the over-deletion guard for an aggressive Tidy
// (0 = conservative default). The zero value reproduces the original behaviour.
type TidyOptions struct {
	Force            bool
	MinSurvivorRatio float64
}

// Plan runs the LLM phase of consolidation for a scope and returns the proposal
// without writing anything. The model runs only here; the caller previews the plan
// (ApplyPlan with dryRun) and then applies it (ApplyPlan), so Opus is never invoked
// twice for one preview→apply cycle.
// Plan is read-only (lists facts, calls the model), so it deliberately does NOT
// hold s.mu: that lock guards writes/fingerprints and must not be held across a
// multi-minute LLM run, which would block every other brain op (incl. live
// MemorySearch). Staleness is caught by ApplyPlan's fingerprint check.
func (s *Service) Plan(ctx context.Context, scope memory.Scope, ex memory.Extractor, decay memory.DecayPolicy, opts TidyOptions) (memory.Plan, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, err := memory.ApplyPlan(ctx, s.store, scope, plan, memory.ConsolidateOptions{
		Decay:  decay,
		DryRun: dryRun,
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
// per-scope fingerprints of the scopes it touched so a later Tidy re-evaluates them.
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
	return rep, nil
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
// Tidy fingerprints: this tracks "the state all projects were in the last time we
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
