package memory

import (
	"context"
	"sort"
	"strings"
)

// DefaultMinPromotionScopes is the minimum number of distinct project scopes a
// topic community must span before its facts are offered for global promotion. It
// is the "transferable pattern" guardrail (RFC P5): a fact that recurs across ≥2
// projects is a candidate for a cross-project rule, whereas a fact living in a
// single scope is codebase-specific and stays put. 2 is the floor (a pattern shared
// by even two projects is transferable); raise it to demand stronger recurrence
// (the RFC allows up to 3). Tunable.
const DefaultMinPromotionScopes = 2

// isPromotable reports whether a record may be lifted into the global scope: a
// non-protected, non-capture project fact. Global facts already live there;
// captures are raw episodic material; pinned/locked/human facts are off-limits. The
// cross-scope filter, the candidate gather (PlanGlobalPromotion), the apply-time
// staleness guard and the incremental manifest all key on this one predicate so
// they can never drift out of agreement.
func isPromotable(r Record) bool {
	return r.Scope != ScopeGlobal && r.Source != SourceCapture && !isProtected(r)
}

// CrossScopeGroup is a topic community whose member facts recur across two or more
// distinct project scopes — the unit of transferable knowledge. It is the brain's
// analogue of a shared "node" in graphify's global graph (global_graph.py): the
// same tooling/preference fact seen in N projects collapses to one cross-project
// entry. Scopes lists the distinct project scopes the group touches (sorted);
// Members are the facts, ordered by (label, scope, id) so identical facts sit
// adjacent — see labelFor.
type CrossScopeGroup struct {
	Scopes  []Scope
	Members []ScopedFact
}

// CrossScopeGroups partitions candidate project facts into topic communities and
// returns only those spanning at least minScopes distinct project scopes — the
// transferable-pattern guardrail of RFC P5.
//
// It is the brain analogue of graphify's cross-repo merge (global_graph.py): facts
// are grouped by topic ("dedup shared nodes by label"), and only groups shared
// across projects survive; the rest are codebase-specific and never reach the
// Promoter. Codebase facts live in a single scope, so they fall out here with no
// content heuristic — the scope span is the signal.
//
// Communities come from DetectCommunities over token-Jaccard at the given threshold.
// Related edges are deliberately not used: RelinkScope only ever links within a
// scope, so the cross-scope recurrence signal is entirely textual similarity, which
// is exactly what we want to detect across projects.
//
// Deterministic: the partition (DetectCommunities) and every sort here are
// order-independent, so the same candidate set always yields the same groups.
func CrossScopeGroups(candidates []ScopedFact, threshold float64, minScopes int) []CrossScopeGroup {
	if minScopes < 2 {
		minScopes = 2
	}
	if len(candidates) < minScopes {
		return nil // can't span minScopes scopes with fewer facts than that
	}

	recs := make([]Record, len(candidates))
	scopeByID := make(map[string]Scope, len(candidates))
	factByID := make(map[string]ScopedFact, len(candidates))
	for i, c := range candidates {
		recs[i] = Record{ID: c.ID, Scope: c.Scope, Text: c.Text, Category: c.Category}
		scopeByID[c.ID] = c.Scope
		factByID[c.ID] = c
	}
	comm := DetectCommunities(recs, threshold)

	byComm := map[int][]string{}
	for id, c := range comm {
		byComm[c] = append(byComm[c], id)
	}
	commIDs := make([]int, 0, len(byComm))
	for c := range byComm {
		commIDs = append(commIDs, c)
	}
	sort.Ints(commIDs)

	var groups []CrossScopeGroup
	for _, c := range commIDs {
		ids := byComm[c]
		scopeSet := map[Scope]struct{}{}
		for _, id := range ids {
			scopeSet[scopeByID[id]] = struct{}{}
		}
		if len(scopeSet) < minScopes {
			continue // single-scope (codebase-specific) topic — never promote
		}
		scopes := make([]Scope, 0, len(scopeSet))
		for s := range scopeSet {
			scopes = append(scopes, s)
		}
		sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })

		members := make([]ScopedFact, 0, len(ids))
		for _, id := range ids {
			members = append(members, factByID[id])
		}
		sortByLabel(members)
		groups = append(groups, CrossScopeGroup{Scopes: scopes, Members: members})
	}
	return groups
}

// crossScopeCandidates narrows a candidate set to only the facts in cross-scope
// communities, flattened with each group's members kept contiguous so a topic that
// fits in a single batch is never split across two model calls (a split recurrence
// is only caught on a later pass).
func crossScopeCandidates(candidates []ScopedFact, threshold float64, minScopes int) []ScopedFact {
	groups := CrossScopeGroups(candidates, threshold, minScopes)
	if len(groups) == 0 {
		return nil
	}
	var out []ScopedFact
	for _, g := range groups {
		out = append(out, g.Members...)
	}
	return out
}

// labelFor is the dedup-by-label key of a fact: its significant tokens, deduped,
// sorted and joined. Two facts that say the same thing in the same words share a
// label, so they cluster into one community (token-Jaccard 1.0) and sort adjacent
// within their cross-scope group — graphify's "dedup shared nodes by label"
// (global_graph.py). Colocating the copies lets the Promoter collapse them into a
// single global node (one Promotion subsuming the per-project copies).
func labelFor(text string) string {
	toks := uniqueTokens(text)
	sort.Strings(toks)
	return strings.Join(toks, " ")
}

// sortByLabel orders facts by (label, scope, id) so identical facts are adjacent and
// the order is fully deterministic regardless of the caller's input order.
func sortByLabel(facts []ScopedFact) {
	sort.Slice(facts, func(i, j int) bool {
		li, lj := labelFor(facts[i].Text), labelFor(facts[j].Text)
		if li != lj {
			return li < lj
		}
		if facts[i].Scope != facts[j].Scope {
			return facts[i].Scope < facts[j].Scope
		}
		return facts[i].ID < facts[j].ID
	})
}

// ScopeManifest returns a per-scope content hash of every promotable fact in the
// store — the cross-scope "manifest" mirroring graphify's global manifest
// (global_graph.py). It lets an incremental global pass skip the (expensive) model
// run when no project has changed since the last one. The hash is the same
// order-independent fingerprint used as the apply-time staleness guard, computed
// over exactly the promotable set (isPromotable), so a manifest entry and the
// matching GlobalPlan fingerprint are always byte-identical for an unchanged scope.
func ScopeManifest(ctx context.Context, store Store) (map[string]string, error) {
	all, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	byScope := map[Scope][]Record{}
	for _, r := range all {
		if isPromotable(r) {
			byScope[r.Scope] = append(byScope[r.Scope], r)
		}
	}
	m := make(map[string]string, len(byScope))
	for scope, rs := range byScope {
		m[string(scope)] = fingerprint(rs)
	}
	return m, nil
}

// manifestsEqual reports whether two scope→hash manifests are identical (same
// scopes, same hashes). An empty manifest is never equal to anything, so a
// first-ever pass — or one after the manifest file was cleared — always runs the
// model rather than skipping it.
func manifestsEqual(a, b map[string]string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
