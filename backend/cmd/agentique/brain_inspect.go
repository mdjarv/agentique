package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Read-only inspection commands for the brain (memory store). These never mutate
// the corpus — they list, show, search and summarize the markdown source of truth
// via the same brain.Service the server uses, so semantic recall (when configured)
// and keyword recall behave identically to production.

var (
	brainListScope    string
	brainListCategory string
	brainListLimit    int
	brainListSort     string
	brainListJSON     bool

	brainShowJSON bool

	brainSearchScope string
	brainSearchLimit int
	brainSearchJSON  bool

	brainStatsJSON bool
)

func init() {
	brainListCmd.Flags().StringVar(&brainListScope, "scope", "", "only this scope (e.g. global, project:<id>)")
	brainListCmd.Flags().StringVar(&brainListCategory, "category", "", "only this category (fact, identity, preference, …)")
	brainListCmd.Flags().IntVar(&brainListLimit, "limit", 50, "max memories to show (0 = all)")
	brainListCmd.Flags().StringVar(&brainListSort, "sort", "uses", "sort order: uses (most-used first) | new (newest first)")
	brainListCmd.Flags().BoolVar(&brainListJSON, "json", false, "emit JSON instead of a table")

	brainShowCmd.Flags().BoolVar(&brainShowJSON, "json", false, "emit JSON instead of formatted text")

	brainSearchCmd.Flags().StringVar(&brainSearchScope, "scope", "", "restrict to this scope plus global (default: all scopes)")
	brainSearchCmd.Flags().IntVar(&brainSearchLimit, "limit", 10, "max query-relevant matches to return")
	brainSearchCmd.Flags().BoolVar(&brainSearchJSON, "json", false, "emit JSON instead of formatted text")

	brainStatsCmd.Flags().BoolVar(&brainStatsJSON, "json", false, "emit JSON instead of a summary")

	brainCmd.AddCommand(brainListCmd)
	brainCmd.AddCommand(brainShowCmd)
	brainCmd.AddCommand(brainSearchCmd)
	brainCmd.AddCommand(brainStatsCmd)
}

// --- list -------------------------------------------------------------------

var brainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memories (id, scope, category, trust, uses, text)",
	Long: `List durable and episodic memories from the brain's markdown store.

Optionally filter by --scope and --category. Sorted most-used first by default
(--sort new for newest first), capped at --limit (0 = all). Use 'brain show <id>'
for a single memory's full text and provenance.`,
	RunE: runBrainList,
}

func runBrainList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return err
	}

	var scopes []memory.Scope
	if brainListScope != "" {
		scopes = []memory.Scope{memory.Scope(brainListScope)}
	}
	recs, err := svc.List(ctx, scopes...)
	if err != nil {
		return fmt.Errorf("list memories: %w", err)
	}

	if brainListCategory != "" {
		cat := memory.Category(brainListCategory)
		recs = filterRecords(recs, func(r memory.Record) bool { return r.Category == cat })
	}

	sortRecords(recs, brainListSort)

	total := len(recs)
	if brainListLimit > 0 && len(recs) > brainListLimit {
		recs = recs[:brainListLimit]
	}

	names := projectScopeNames(ctx)

	if brainListJSON {
		return emitJSON(memoriesToJSON(recs, names))
	}

	if total == 0 {
		fmt.Println("no memories found")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSCOPE\tCATEGORY\tTRUST\tUSES\tTEXT")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
			shortID(r.ID), scopeLabel(r.Scope, names), r.Category,
			trustLabel(r), r.Uses, truncateOneLine(r.Text, 70))
	}
	tw.Flush()
	if total > len(recs) {
		fmt.Printf("\nshowing %d of %d (raise --limit to see more)\n", len(recs), total)
	} else {
		fmt.Printf("\n%d memories\n", total)
	}
	return nil
}

// --- show -------------------------------------------------------------------

var brainShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a single memory's full text and all frontmatter",
	Long: `Print one memory's full text plus its provenance and indexes: scope, category,
source, confidence, uses/helped, derived-from, subsumed sources, related links,
community/area, and any review note. <id> may be a unique id prefix.`,
	Args: cobra.ExactArgs(1),
	RunE: runBrainShow,
}

func runBrainShow(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return err
	}
	rec, err := resolveBrainMemory(ctx, svc, args[0])
	if err != nil {
		return err
	}
	names := projectScopeNames(ctx)

	if brainShowJSON {
		return emitJSON(memoryToJSON(rec, names))
	}

	fmt.Printf("%s\n\n", rec.Text)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 1, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s:\t%s\n", k, v) }
	row("id", rec.ID)
	row("scope", scopeLabel(rec.Scope, names))
	if l := scopeLabel(rec.Scope, names); l != string(rec.Scope) {
		row("scope (raw)", string(rec.Scope))
	}
	row("category", string(rec.Category))
	row("source", string(rec.Source))
	row("confidence", trustLabel(rec))
	row("uses", fmt.Sprintf("%d", rec.Uses))
	row("helped", fmt.Sprintf("%d", rec.Helped))
	row("pinned", fmt.Sprintf("%t", rec.Pinned))
	row("locked", fmt.Sprintf("%t", rec.Locked))
	row("created", rec.CreatedAt.Format(time.RFC3339))
	row("updated", rec.UpdatedAt.Format(time.RFC3339))
	if !rec.LastUsedAt.IsZero() {
		row("last used", rec.LastUsedAt.Format(time.RFC3339))
	}
	if rec.Community != 0 {
		row("community", fmt.Sprintf("%d", rec.Community))
	}
	if rec.Area != "" {
		row("area", rec.Area)
	}
	if len(rec.Related) > 0 {
		row("related", strings.Join(rec.Related, ", "))
	}
	if len(rec.DerivedFrom) > 0 {
		row("derived from", strings.Join(rec.DerivedFrom, ", "))
	}
	if rec.ReviewNote != "" {
		row("review note", rec.ReviewNote)
	}
	tw.Flush()

	if len(rec.Subsumed) > 0 {
		fmt.Printf("\nsubsumed sources (%d):\n", len(rec.Subsumed))
		for _, s := range rec.Subsumed {
			fmt.Printf("  ← [%s] %s\n", scopeLabel(s.Scope, names), truncateOneLine(s.Text, 100))
		}
	}
	return nil
}

// --- search -----------------------------------------------------------------

var brainSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memories via the production recall path (hybrid or keyword)",
	Long: `Run the query through the same recall path the live agent uses: a vector+keyword
hybrid when semantic recall is configured (Chroma + embeddings), or keyword-only
otherwise. Returns the query-relevant facts the brain would surface, ranked, plus
any associative neighbours recall folds in. Pinned (always-injected) facts are not
query-ranked — see 'brain list'.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runBrainSearch,
}

func runBrainSearch(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return err
	}
	query := strings.Join(args, " ")

	var scopes []memory.Scope
	if brainSearchScope != "" {
		sc := memory.Scope(brainSearchScope)
		if sc == memory.ScopeGlobal {
			scopes = []memory.Scope{memory.ScopeGlobal}
		} else {
			scopes = []memory.Scope{sc, memory.ScopeGlobal}
		}
	}

	res, err := svc.Recall(ctx, scopes, query, brainSearchLimit)
	if err != nil {
		return fmt.Errorf("recall: %w", err)
	}
	names := projectScopeNames(ctx)

	if brainSearchJSON {
		return emitJSON(map[string]any{
			"query":    query,
			"semantic": svc.SemanticEnabled(),
			"recalled": memoriesToJSON(res.Recalled, names),
		})
	}

	mode := "keyword"
	if svc.SemanticEnabled() {
		mode = "hybrid (vector + keyword)"
	}
	fmt.Printf("query: %q   mode: %s\n\n", query, mode)
	if len(res.Recalled) == 0 {
		fmt.Println("no matches")
		return nil
	}
	for i, r := range res.Recalled {
		fmt.Printf("%d. [%s] %s  (%s, uses %d)\n", i+1, scopeLabel(r.Scope, names), shortID(r.ID), trustLabel(r), r.Uses)
		fmt.Printf("   %s\n", truncateOneLine(r.Text, 100))
	}
	fmt.Printf("\n%d match(es)\n", len(res.Recalled))
	return nil
}

// --- stats ------------------------------------------------------------------

var brainStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Corpus summary: totals, per-scope, trust tiers, graph connectivity",
	Long: `Summarize the brain: total facts, per-scope counts, trust-tier breakdown
(human / inferred / ambiguous, plus flagged-for-review), graph connectivity
(connected vs isolated facts) and the semantic-edge count when semantic recall
is configured.`,
	RunE: runBrainStats,
}

// brainStats is the computed corpus summary (also the --json shape).
type brainStats struct {
	Total         int            `json:"total"`            // durable facts (excludes captures)
	Captures      int            `json:"captures"`         // raw episodic material, not injected
	Pinned        int            `json:"pinned"`           // always-injected
	Locked        int            `json:"locked"`           // exempt from consolidation/decay
	FlaggedReview int            `json:"flaggedForReview"` // ReviewNote set (contradicted on recall)
	Semantic      bool           `json:"semantic"`         // vector recall active
	SemanticEdges int            `json:"semanticEdges"`    // embedding kNN edges (0 in keyword mode)
	Connected     int            `json:"connected"`        // durable facts with ≥1 graph edge
	Isolated      int            `json:"isolated"`         // durable facts with no edges
	ByScope       []scopeCount   `json:"byScope"`
	ByTrust       map[string]int `json:"byTrust"`  // confidence tier -> count
	BySource      map[string]int `json:"bySource"` // source -> count
	ByCategory    map[string]int `json:"byCategory"`
}

type scopeCount struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

func runBrainStats(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return err
	}
	all, err := svc.List(ctx)
	if err != nil {
		return fmt.Errorf("list memories: %w", err)
	}
	names := projectScopeNames(ctx)

	stats := computeBrainStats(all)
	stats.Semantic = svc.SemanticEnabled()

	// Graph connectivity over durable facts: structural edges (Related + DerivedFrom)
	// unioned with the semantic kNN. SemanticEdges is nil in keyword mode, so the count
	// degrades to structural connectivity only.
	durable := filterRecords(all, func(r memory.Record) bool { return r.Source != memory.SourceCapture })
	edges, eerr := svc.SemanticEdges(ctx, durable)
	if eerr != nil {
		fmt.Fprintf(os.Stderr, "warning: semantic edges unavailable: %v\n", eerr)
	}
	stats.SemanticEdges = len(edges)
	cent := memory.ComputeCentralityWithEdges(durable, edges)
	for _, r := range durable {
		if cent[r.ID].Degree > 0 {
			stats.Connected++
		} else {
			stats.Isolated++
		}
	}

	// Resolve display labels for per-scope counts.
	for i := range stats.ByScope {
		stats.ByScope[i].Label = scopeLabel(memory.Scope(stats.ByScope[i].Scope), names)
	}

	if brainStatsJSON {
		return emitJSON(stats)
	}
	printBrainStats(stats)
	return nil
}

// computeBrainStats tallies the corpus counts. Pure (no IO), so it is unit-testable.
func computeBrainStats(all []memory.Record) brainStats {
	s := brainStats{
		ByTrust:    map[string]int{},
		BySource:   map[string]int{},
		ByCategory: map[string]int{},
	}
	scopeCounts := map[string]int{}
	for _, r := range all {
		if r.Source == memory.SourceCapture {
			s.Captures++
			continue
		}
		s.Total++
		scopeCounts[string(r.Scope)]++
		r = memory.NormalizeConfidence(r)
		s.ByTrust[string(r.Confidence)]++
		s.BySource[string(r.Source)]++
		s.ByCategory[string(r.Category)]++
		if r.Pinned {
			s.Pinned++
		}
		if r.Locked {
			s.Locked++
		}
		if r.ReviewNote != "" {
			s.FlaggedReview++
		}
	}
	for scope, n := range scopeCounts {
		s.ByScope = append(s.ByScope, scopeCount{Scope: scope, Count: n})
	}
	// Largest scope first, then by scope id for stable output.
	sort.Slice(s.ByScope, func(i, j int) bool {
		if s.ByScope[i].Count != s.ByScope[j].Count {
			return s.ByScope[i].Count > s.ByScope[j].Count
		}
		return s.ByScope[i].Scope < s.ByScope[j].Scope
	})
	return s
}

func printBrainStats(s brainStats) {
	fmt.Printf("Brain corpus\n")
	fmt.Printf("  total facts:   %d  (+%d captures awaiting consolidation)\n", s.Total, s.Captures)
	fmt.Printf("  pinned:        %d\n", s.Pinned)
	fmt.Printf("  locked:        %d\n", s.Locked)
	fmt.Printf("  flagged:       %d  (contradicted on recall, awaiting review)\n", s.FlaggedReview)

	fmt.Printf("\nGraph connectivity\n")
	fmt.Printf("  connected:     %d\n", s.Connected)
	fmt.Printf("  isolated:      %d\n", s.Isolated)
	if s.Semantic {
		fmt.Printf("  semantic edges: %d\n", s.SemanticEdges)
	} else {
		fmt.Printf("  semantic edges: (keyword mode — semantic recall disabled)\n")
	}

	fmt.Printf("\nTrust tiers\n")
	for _, tier := range []memory.ConfidenceTier{memory.ConfidenceExtracted, memory.ConfidenceInferred, memory.ConfidenceAmbiguous} {
		fmt.Printf("  %-10s %d\n", string(tier)+":", s.ByTrust[string(tier)])
	}

	fmt.Printf("\nBy source\n")
	printCountMap(s.BySource)

	fmt.Printf("\nBy category\n")
	printCountMap(s.ByCategory)

	fmt.Printf("\nBy scope\n")
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, sc := range s.ByScope {
		fmt.Fprintf(tw, "  %s\t%d\n", sc.Label, sc.Count)
	}
	tw.Flush()
}

// printCountMap prints a name->count map sorted by descending count.
func printCountMap(m map[string]int) {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	for _, p := range pairs {
		fmt.Printf("  %-13s %d\n", p.k+":", p.v)
	}
}

// --- shared helpers ---------------------------------------------------------

// brainMemoryJSON is the CLI's JSON view of a memory. It mirrors the Brain REST
// DTO's camelCase shape (internal/brain/http.go) for consistency, minus the derived
// embedding vector. scopeLabel resolves project scopes to a human name for display.
type brainMemoryJSON struct {
	ID              string              `json:"id"`
	Scope           string              `json:"scope"`
	ScopeLabel      string              `json:"scopeLabel,omitempty"`
	Text            string              `json:"text"`
	Category        string              `json:"category"`
	Source          string              `json:"source"`
	Pinned          bool                `json:"pinned"`
	Locked          bool                `json:"locked"`
	Uses            int                 `json:"uses"`
	Helped          int                 `json:"helped"`
	Confidence      string              `json:"confidence"`
	ConfidenceScore float64             `json:"confidenceScore"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	LastUsedAt      *time.Time          `json:"lastUsedAt,omitempty"`
	DerivedFrom     []string            `json:"derivedFrom,omitempty"`
	Related         []string            `json:"related,omitempty"`
	Subsumed        []brainSubsumedJSON `json:"subsumed,omitempty"`
	Community       int                 `json:"community"`
	Area            string              `json:"area,omitempty"`
	ReviewNote      string              `json:"reviewNote,omitempty"`
}

type brainSubsumedJSON struct {
	Scope string `json:"scope"`
	Text  string `json:"text"`
}

func memoryToJSON(r memory.Record, names map[string]string) brainMemoryJSON {
	r = memory.NormalizeConfidence(r)
	out := brainMemoryJSON{
		ID: r.ID, Scope: string(r.Scope), Text: r.Text, Category: string(r.Category),
		Source: string(r.Source), Pinned: r.Pinned, Locked: r.Locked, Uses: r.Uses, Helped: r.Helped,
		Confidence: string(r.Confidence), ConfidenceScore: r.ConfidenceScore,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		DerivedFrom: r.DerivedFrom, Related: r.Related,
		Community: r.Community, Area: r.Area, ReviewNote: r.ReviewNote,
	}
	if label := scopeLabel(r.Scope, names); label != string(r.Scope) {
		out.ScopeLabel = label
	}
	if !r.LastUsedAt.IsZero() {
		t := r.LastUsedAt
		out.LastUsedAt = &t
	}
	for _, s := range r.Subsumed {
		out.Subsumed = append(out.Subsumed, brainSubsumedJSON{Scope: string(s.Scope), Text: s.Text})
	}
	return out
}

func memoriesToJSON(rs []memory.Record, names map[string]string) []brainMemoryJSON {
	out := make([]brainMemoryJSON, 0, len(rs))
	for _, r := range rs {
		out = append(out, memoryToJSON(r, names))
	}
	return out
}

// resolveBrainMemory fetches a memory by exact id, falling back to a unique id-prefix
// match so users can paste the short ids printed by 'brain list'.
func resolveBrainMemory(ctx context.Context, svc *brain.Service, idOrPrefix string) (memory.Record, error) {
	if r, err := svc.Get(ctx, idOrPrefix); err == nil {
		return r, nil
	} else if !errors.Is(err, memory.ErrNotFound) {
		return memory.Record{}, err
	}
	all, err := svc.List(ctx)
	if err != nil {
		return memory.Record{}, err
	}
	var matches []memory.Record
	for _, r := range all {
		if strings.HasPrefix(r.ID, idOrPrefix) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return memory.Record{}, fmt.Errorf("no memory matches id %q", idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "ambiguous id prefix %q matches %d memories:\n", idOrPrefix, len(matches))
		for _, r := range matches {
			fmt.Fprintf(&b, "  %s  %s\n", shortID(r.ID), truncateOneLine(r.Text, 60))
		}
		return memory.Record{}, errors.New(strings.TrimRight(b.String(), "\n"))
	}
}

// projectScopeNames maps each project scope ("project:<id>") to the project's display
// name, for readable scope columns. Best-effort: a DB-open failure yields an empty map
// and callers fall back to raw scope strings, so inspection still works.
func projectScopeNames(ctx context.Context) map[string]string {
	names := map[string]string{}
	db, err := store.Open(resolveDBPath())
	if err != nil {
		return names
	}
	defer db.Close()
	projects, err := store.New(db).ListProjects(ctx)
	if err != nil {
		return names
	}
	for _, p := range projects {
		names[string(brain.ScopeForProject(p.ID))] = p.Name
	}
	return names
}

// scopeLabel renders a scope for humans: "global" as-is, a known project scope as its
// project name, anything else as the raw scope string.
func scopeLabel(scope memory.Scope, names map[string]string) string {
	if name, ok := names[string(scope)]; ok && name != "" {
		return name
	}
	return string(scope)
}

// trustLabel renders a record's confidence tier and score, e.g. "extracted 1.00".
func trustLabel(r memory.Record) string {
	r = memory.NormalizeConfidence(r)
	return fmt.Sprintf("%s %.2f", r.Confidence, r.ConfidenceScore)
}

func filterRecords(rs []memory.Record, keep func(memory.Record) bool) []memory.Record {
	out := rs[:0:0]
	for _, r := range rs {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// sortRecords orders records in place: "new" = newest CreatedAt first, anything else
// ("uses", the default) = most-used first. Ties break on CreatedAt then id so output
// is deterministic.
func sortRecords(rs []memory.Record, order string) {
	switch order {
	case "new", "newest", "created":
		sort.Slice(rs, func(i, j int) bool {
			if !rs[i].CreatedAt.Equal(rs[j].CreatedAt) {
				return rs[i].CreatedAt.After(rs[j].CreatedAt)
			}
			return rs[i].ID < rs[j].ID
		})
	default:
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].Uses != rs[j].Uses {
				return rs[i].Uses > rs[j].Uses
			}
			if !rs[i].CreatedAt.Equal(rs[j].CreatedAt) {
				return rs[i].CreatedAt.After(rs[j].CreatedAt)
			}
			return rs[i].ID < rs[j].ID
		})
	}
}

// truncateOneLine collapses newlines and clips to max runes with an ellipsis, keeping
// table rows on one line.
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
