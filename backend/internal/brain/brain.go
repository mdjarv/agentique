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

	mu     sync.Mutex // guards the fingerprint file
	fpPath string
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
	svc := &Service{store: base, dir: cfg.Dir, fpPath: filepath.Join(cfg.Dir, ".fingerprints.json")}

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
// Extractor restricts the pass to deterministic decay.
func (s *Service) Consolidate(ctx context.Context, scope memory.Scope, ex memory.Extractor, decay memory.DecayPolicy) (memory.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fps := s.loadFingerprints()
	rep, err := memory.Consolidate(ctx, s.store, ex, scope, memory.ConsolidateOptions{
		PrevFingerprint: fps[string(scope)],
		Decay:           decay,
	})
	if err != nil {
		return rep, err
	}
	fps[string(scope)] = rep.Fingerprint
	s.saveFingerprints(fps)
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
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	tmp := s.fpPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("brain: persist fingerprints", "error", err)
		return
	}
	if err := os.Rename(tmp, s.fpPath); err != nil {
		slog.Warn("brain: persist fingerprints", "error", err)
	}
}
