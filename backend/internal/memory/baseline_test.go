package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// updateGolden rewrites the committed baseline golden files instead of asserting
// against them. Run `go test ./internal/memory/... -run TestBaseline -update`.
// One flag per package.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

// fixedNow anchors any baseline timestamp arithmetic to a deterministic instant.
// NOTE: Recall.rank and ApplyPlan both read a NON-injectable time.Now() (recall.go,
// consolidate.go), so this clock is used only for constructing the corpus, never to
// drive recall/decay. The baseline therefore asserts wall-clock-stable projections
// (recall: top-K set membership + top-1; decay: far-past timestamps that are stale
// under any real wall clock) rather than anything that depends on "now".
var fixedNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// staleLongAgo is far enough in the past that ApplyPlan's wall-clock decay sees a
// corpus fact as stale regardless of when the test runs (locks the current
// delete-on-decay behaviour; M5 flips it to archive).
var staleLongAgo = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// baselineCorpus is the fixed fixture the recall and interference goldens are taken
// over: stable hand-assigned ids f01..f10, fixed text and timestamps, spanning every
// Category, every injectable Source, one SourceCapture (must be excluded from recall),
// and one pinned identity fact (must land in Pinned, never Recalled). Keyword scores
// are deliberately well-separated so the small recency tiebreaker cannot flip the
// ranked answer across runs.
func baselineCorpus() []Record {
	mkrec := func(id, text string, cat Category, src Source) Record {
		r := New(ScopeGlobal, text, cat, src)
		r.ID = id
		// Deterministic, distinct creation times so filestore-style ordering is stable;
		// recall asserts set membership so map order is irrelevant either way.
		r.CreatedAt = fixedNow.Add(-time.Duration(len(id)) * time.Hour)
		r.UpdatedAt = r.CreatedAt
		return r
	}

	f01 := mkrec("f01", "The user's name is Mathias Djarv.", CategoryIdentity, SourceHuman)
	f01.Pinned = true
	f02 := mkrec("f02", "The user prefers the just build tool and dislikes raw npx commands.", CategoryPreference, SourceHuman)
	f03 := mkrec("f03", "The authentication flow lives in the internal auth package.", CategoryFact, SourceAgent)
	f04 := mkrec("f04", "The agentique project uses sqlc to generate database query code.", CategoryProject, SourceConsolidated)
	f05 := mkrec("f05", "Reach the user by email at their registered mailbox.", CategoryContact, SourceHuman)
	f06 := mkrec("f06", "The current goal is shipping the brain evolution migration band.", CategoryGoal, SourceAgent)
	f07 := mkrec("f07", "A pending task is to regenerate the golden baseline fixtures.", CategoryTask, SourceConsolidated)
	f08 := mkrec("f08", "The sqlite database file is stored in the local share directory.", CategoryFact, SourceConsolidated)
	f09 := mkrec("f09", "Episodic capture mentioning quantum widgets that must stay excluded.", CategoryFact, SourceCapture)
	f10 := mkrec("f10", "The user wants dark mode enabled across the editor interface.", CategoryPreference, SourceAgent)
	// f11/f12 share ~half their token set (service, layer, handles, cache, tokens) so they
	// land in the interference band (≥0.3, <0.6 token-Jaccard); their tokens deliberately
	// avoid the recall-query vocabulary so the recall goldens stay stable.
	f11 := mkrec("f11", "The service layer handles request routing and cache tokens.", CategoryFact, SourceAgent)
	f12 := mkrec("f12", "The service layer handles billing payments and cache tokens.", CategoryFact, SourceConsolidated)

	return []Record{f01, f02, f03, f04, f05, f06, f07, f08, f09, f10, f11, f12}
}

// reportSnapshot is a JSON-stable, sorted projection of a consolidation Report that
// kills memStore.List map-order nondeterminism. Promoted facts carry fresh UUIDs, so
// they are projected by Text (deterministic); everything else by id.
type reportSnapshot struct {
	PromotedTexts    []string `json:"promotedTexts"`
	CapturesConsumed []string `json:"capturesConsumed"`
	RewrittenIDs     []string `json:"rewrittenIds"`
	AbstractedTexts  []string `json:"abstractedTexts"`
	DeletedIDs       []string `json:"deletedIds"`
	DecayedIDs       []string `json:"decayedIds"`
	Skipped          bool     `json:"skipped"`
	ReorgRefused     bool     `json:"reorgRefused"`
}

func normalizeReport(rep Report) reportSnapshot {
	texts := func(rs []Record) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.Text)
		}
		sort.Strings(out)
		return out
	}
	ids := func(rs []Record) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.ID)
		}
		sort.Strings(out)
		return out
	}
	rewritten := make([]string, 0, len(rep.Rewritten))
	for _, c := range rep.Rewritten {
		rewritten = append(rewritten, c.After.ID)
	}
	sort.Strings(rewritten)
	consumed := append([]string(nil), rep.CapturesConsumed...)
	sort.Strings(consumed)
	return reportSnapshot{
		PromotedTexts:    texts(rep.Promoted),
		CapturesConsumed: consumed,
		RewrittenIDs:     rewritten,
		AbstractedTexts:  texts(rep.Abstracted),
		DeletedIDs:       ids(rep.Deleted),
		DecayedIDs:       ids(rep.Decayed),
		Skipped:          rep.Skipped,
		ReorgRefused:     rep.ReorgRefused,
	}
}

// finalStoreIDs returns the sorted ids remaining in a store after a pass.
func finalStoreIDs(t *testing.T, store Store) []string {
	t.Helper()
	all, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	out := make([]string, 0, len(all))
	for _, r := range all {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	return append(b, '\n')
}

// assertGolden compares got against testdata/baseline/<name>, or rewrites it under -update.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "baseline", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden %s mismatch:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

func sortedIDs(rs []Record) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

// recallSnapshot is the wall-clock-stable binding projection of a recall result:
// the pinned id set, the single top-1 recalled id, and the recalled id set. Strict
// byte order is intentionally NOT asserted because rank()'s recency term reads a
// non-injectable clock and scales per-record by StorageStrength (see fixedNow).
type recallSnapshot struct {
	Query        string   `json:"query"`
	Pinned       []string `json:"pinned"`
	RecalledTop1 string   `json:"recalledTop1"`
	RecalledSet  []string `json:"recalledSet"`
}

func TestBaselineRecallRanking(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		query string
	}{
		{"build_tooling", "how do I run the build with the just tool or npx"},
		{"empty_query", "   "},
		{"identity", "what is the user name and identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore(baselineCorpus()...)
			res, err := Recall(ctx, store, Query{Text: tc.query, Scopes: []Scope{ScopeGlobal}, K: 3})
			if err != nil {
				t.Fatal(err)
			}
			top1 := ""
			if len(res.Recalled) > 0 {
				top1 = res.Recalled[0].ID
			}
			snap := recallSnapshot{
				Query:        tc.query,
				Pinned:       sortedIDs(res.Pinned),
				RecalledTop1: top1,
				RecalledSet:  sortedIDs(res.Recalled),
			}
			// Capture exclusion + pinned-identity exemption are load-bearing invariants.
			if contains(res.Recalled, "f09") {
				t.Fatalf("capture f09 must never be recalled: %+v", res.Recalled)
			}
			assertGolden(t, "recall_"+tc.name+".golden", mustJSON(t, snap))
		})
	}
}

func TestBaselineConsolidateReport(t *testing.T) {
	ctx := context.Background()

	t.Run("promote_reorg_decay_off", func(t *testing.T) {
		store := newMemStore(consolidateBaseRecords()...)
		ex := fakeExtractor{
			extract: func(_ []string) []Candidate {
				return []Candidate{{Text: "Distilled durable fact about the delta subsystem.", Category: CategoryFact}}
			},
			reorganize: func(facts []Fact) []Fact {
				out := append([]Fact(nil), facts...)
				for i := range out {
					if out[i].ID == "d1" {
						out[i].Text = "Durable fact one about the alpha subsystem, clarified."
					}
				}
				return out
			},
		}
		rep, err := Consolidate(ctx, store, ex, ScopeGlobal, ConsolidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		snap := normalizeReport(rep)
		if len(snap.DecayedIDs) != 0 {
			t.Fatalf("decay off: expected no decay, got %v", snap.DecayedIDs)
		}
		final := finalStoreIDs(t, store)
		for _, id := range []string{"h1", "l1", "p1"} {
			if !containsString(final, id) {
				t.Fatalf("protected fact %s must survive, final=%v", id, final)
			}
		}
		assertGolden(t, "consolidate_promote_reorg_decay_off.golden", mustJSON(t, snap))
	})

	t.Run("decay_archives", func(t *testing.T) {
		store := newMemStore(decayBaseRecords()...)
		rep, err := Consolidate(ctx, store, fakeExtractor{}, ScopeGlobal,
			ConsolidateOptions{Decay: DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: DefaultArchiveConfidenceFloor}})
		if err != nil {
			t.Fatal(err)
		}
		snap := normalizeReport(rep)
		final := finalStoreIDs(t, store)
		// M5 (the committed delete→archive audit): a decayed fact is ARCHIVED
		// (Lifecycle=archived), not deleted — it stays in the store, out of recall, restorable.
		// rep.Decayed still reports them, so the golden (DecayedIDs) is unchanged; the flip is
		// the store-state assertion below (was "absent", now "present + archived").
		for _, id := range []string{"d1", "d2", "d3"} {
			if !containsString(final, id) {
				t.Fatalf("archived fact %s must remain in the store, final=%v", id, final)
			}
			got, err := store.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get(%s): %v", id, err)
			}
			if got.Lifecycle != LifecycleArchived {
				t.Fatalf("fact %s Lifecycle=%s, want archived", id, got.Lifecycle)
			}
		}
		for _, id := range []string{"h1", "l1", "p1"} {
			if !containsString(final, id) {
				t.Fatalf("protected fact %s must survive, final=%v", id, final)
			}
		}
		assertGolden(t, "consolidate_decay_archives.golden", mustJSON(t, snap))
	})

	t.Run("dedup_promotion", func(t *testing.T) {
		store := newMemStore(
			mk("d1", ScopeGlobal, "The sky is blue on a clear day.", CategoryFact, SourceConsolidated),
			capture("c1", "The sky is blue on a clear day."),
		)
		ex := fakeExtractor{
			extract: func(_ []string) []Candidate {
				return []Candidate{{Text: "The sky is blue on a clear day.", Category: CategoryFact}}
			},
		}
		rep, err := Consolidate(ctx, store, ex, ScopeGlobal, ConsolidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		snap := normalizeReport(rep)
		if len(snap.PromotedTexts) != 0 {
			t.Fatalf("duplicate candidate must not promote, got %v", snap.PromotedTexts)
		}
		assertGolden(t, "consolidate_dedup_promotion.golden", mustJSON(t, snap))
	})
}

func TestBaselineInterferenceBands(t *testing.T) {
	// Mirror the live caller (graph.go): lower=DefaultRelatedThreshold (0.3),
	// upper=DefaultDuplicateThreshold (0.6), no SimOption (pure token-Jaccard).
	pairs := DetectInterference(baselineCorpus(), DefaultRelatedThreshold, DefaultDuplicateThreshold, 50)
	assertGolden(t, "interference.golden", mustJSON(t, pairs))
}

// consolidateBaseRecords is the promote+reorg fixture: three reorganizable durable
// facts, three protected facts (human/locked/pinned) that must survive untouched, and
// two captures to distill.
func consolidateBaseRecords() []Record {
	d1 := mk("d1", ScopeGlobal, "Durable fact one about the alpha subsystem.", CategoryFact, SourceConsolidated)
	d2 := mk("d2", ScopeGlobal, "Durable fact two about the beta subsystem.", CategoryFact, SourceConsolidated)
	d3 := mk("d3", ScopeGlobal, "Durable fact three about the gamma subsystem.", CategoryFact, SourceConsolidated)
	h1 := mk("h1", ScopeGlobal, "Human authored ground truth fact.", CategoryFact, SourceHuman)
	l1 := mk("l1", ScopeGlobal, "Locked hand protected fact.", CategoryFact, SourceConsolidated)
	l1.Locked = true
	p1 := mk("p1", ScopeGlobal, "Pinned core context fact.", CategoryFact, SourceConsolidated)
	p1.Pinned = true
	c1 := capture("c1", "Episode mentioning the delta subsystem in detail.")
	c2 := capture("c2", "Episode mentioning the epsilon subsystem in detail.")
	return []Record{d1, d2, d3, h1, l1, p1, c1, c2}
}

// decayBaseRecords is the decay fixture: three stale durable facts (far-past
// UpdatedAt, never used) plus three protected facts. Under a populated DecayPolicy the
// stale three are pruned; the protected three are exempt.
func decayBaseRecords() []Record {
	mkStale := func(id, text string, src Source) Record {
		r := mk(id, ScopeGlobal, text, CategoryFact, src)
		r.UpdatedAt = staleLongAgo
		r.CreatedAt = staleLongAgo
		return r
	}
	d1 := mkStale("d1", "Stale durable fact one.", SourceConsolidated)
	d2 := mkStale("d2", "Stale durable fact two.", SourceConsolidated)
	d3 := mkStale("d3", "Stale durable fact three.", SourceConsolidated)
	h1 := mkStale("h1", "Human ground truth fact.", SourceHuman)
	l1 := mkStale("l1", "Locked fact.", SourceConsolidated)
	l1.Locked = true
	p1 := mkStale("p1", "Pinned fact.", SourceConsolidated)
	p1.Pinned = true
	return []Record{d1, d2, d3, h1, l1, p1}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
