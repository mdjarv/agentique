# Brain: semantic recall — why lexical is blunt, and the embedder path

Status: Planned · 2026-06-22 · Sibling to [brain-memory.md](brain-memory.md),
[brain-cross-scope-areas.md](brain-cross-scope-areas.md)

> **Motivation (a real mis-recall).** In a `meta-spec` session wiring up Sentry for a TS/Vite
> sub-repo, the brain injected *"Private allbin Go modules require GOPRIVATE=github.com/allbin/\*
> plus git SSH config."* That fact is correct and correctly scoped (meta-spec is polyglot — it
> holds the Go `meta-link` CLI), but it was **irrelevant** to the question. The agent noticed and
> said so ("the recalled Go/GOPRIVATE note doesn't apply here"). The root cause is purely lexical:
> the match rested on the single token **`github`** (df 3/173 → moderate idf), while the query's
> actually-discriminating terms — `secrets`, `vars` (df 0) — matched nothing. Keyword recall can't
> know `github` is uninformative glue here; an embedder would never put "github secrets/vars" near
> "GOPRIVATE Go modules git SSH".

## Two parts: the shipped blunt fix, and the real cure

### Shipped now — lexical precision guard (the blunt fix)

`recall.go` now applies a **lone-token guard**: when a multi-token query overlaps a candidate on a
*single* distinct token and there's no strong vector signal, that token must carry
≥ `singleTokenMinShare` (0.40) of the query's idf mass — "the one word you matched must be most of
what you asked about." This drops the `github` case (share ~0.14) while keeping a genuinely dominant
single-keyword match (e.g. a query that is essentially one rare term, share ~0.6).

**It is honestly blunt.** Lexical scoring fundamentally cannot tell a *good* lone-token match (`just`
answering "build tool just") from a *bad* one (`github` answering "secrets and vars") — they are
structurally identical and only differ *semantically*. In a real corpus the distinction usually
falls out of idf (common words like `build`/`tool` have low weight, so the rare on-topic term
dominates), but in small/sparse scopes the guard can drop a legitimate weak single-token match.
That residual false-negative risk is the price of killing the false positives, and it is acceptable
because per-turn recall is *background context* (the agent can always `MemorySearch`) and a visible
false positive erodes trust in the recall block more than a quiet miss. **The guard is a mitigation,
not the cure.**

### The cure — semantic (vector) recall

The codebase already has the whole semantic path built; it is **dormant only because no embedder is
configured** (live runs as `semantic=false`). Enabling it is config, not new architecture:

| env var | meaning |
|---|---|
| `AGENTIQUE_BRAIN_CHROMA_URL` | Chroma (vector DB) base URL |
| `AGENTIQUE_BRAIN_EMBED_URL` | embedding endpoint (OpenAI-compatible `/embeddings`) |
| `AGENTIQUE_BRAIN_EMBED_MODEL` | embedding model id |
| `AGENTIQUE_BRAIN_EMBED_KEY` | optional API key |
| `AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD` | cosine link threshold (default 0.45, measured on all-MiniLM-L6-v2) |

When all three of ChromaURL/EmbedURL/EmbedModel are set and Chroma answers a heartbeat,
`brain.New` switches `store` to the Chroma-backed store and recall becomes a **hybrid**
(`recall.go`): `final = 0.55·vector + 0.40·keyword + 0.05·recency`.

## What enabling the embedder costs (the "process intensive" question)

| | keyword (today) | + embedder |
|---|---|---|
| **recall hot path** (every turn) | `List` (cached) + O(n) idf scoring; ~1 ms; no network | + 1 query-embed call + 1 Chroma vector search **per turn** |
| **per-turn latency added** | — | ~10–50 ms with a local model; ~100–500 ms via a cloud API |
| **infra to operate** | none | a running Chroma + an embedding endpoint |
| **consolidation passes** | CPU only | re-embeds the **whole corpus each pass** — no `(id, text-hash)` cache yet (tech-debt) |
| **tuning** | none | cosine threshold is model-specific, hand-calibrated |
| **failure mode** | n/a | degrades gracefully: a slow/down backend → keyword fallback within `recallTimeout` (3 s) |

**Recommendation: run a *local* embedder.** all-MiniLM-L6-v2 (what the default threshold was
measured against) via Ollama embeddings or a small sidecar keeps both the per-turn query-embed and
the per-pass corpus-embed fast and free. The latency/cost concern only bites with a cloud embedding
API on the per-turn hot path. Costs are irrelevant per project policy, but per-turn latency on the
recall path is a real UX consideration — local keeps it negligible.

## The blend-tightening lever (do this *with* the embedder)

A subtlety found while tracing the mis-recall: **even with the embedder on, the current hybrid blend
would still leak this exact fact.** The survival gate drops a candidate only when *both* vector and
keyword are weak (`vs < minVectorScore(0.20) && kw < minKeywordNorm(0.08)`). The github fact has
~0 vector score but kw ≈ 0.23 → it survives on keyword, and `0.40·0.23 + recency ≈ 0.13` clears the
`minFinalScore` floor. So the embedder would *re-rank it lower* but not necessarily *exclude* it.

To make semantics genuinely decisive, pair the embedder rollout with one of:

1. **Veto on near-zero vector score:** when a vector signal *is available* for the query, drop any
   candidate whose `vs` is below a floor regardless of keyword overlap (semantics says "not
   related" → trust it). I.e. in hybrid mode, gate on vector, not "either signal".
2. **Lower the keyword weight in hybrid mode** (e.g. 0.40 → 0.25) so a keyword-only survivor can't
   clear `minFinalScore` on its own without at least a weak vector corroboration.

Option 1 is cleaner and directly targets this failure class. Either way, the lexical guard above
stays as the keyword-only-mode (`semantic=false`) safeguard — they are complementary, not redundant.

## Sequencing

1. ✅ **Lexical lone-token guard** — shipped (`recall.go`, `singleTokenMinShare`). Works in both
   modes; the only precision lever while `semantic=false`.
2. **Stand up a local embedder** (Ollama/MiniLM + Chroma), set the `AGENTIQUE_BRAIN_EMBED_*` env.
   This also revives semantic *clustering/areas* (today lexical even in semantic mode — tech-debt
   "semantic activated only for areas").
3. **Tighten the hybrid blend** (option 1 above) in the same change, so the vector signal can veto
   an off-topic keyword survivor.
4. **Add the `(id, text-hash)` embedding cache** (tech-debt) before the corpus grows, so passes
   stop re-embedding unchanged facts.

## References

- `internal/memory/recall.go` (`rank`, `keywordScores`, `singleTokenMinShare`, the hybrid blend).
- `internal/memory/similarity.go` (`DefaultSemanticThreshold`).
- `internal/brain/brain.go` (`New` semantic wiring, `embedRecords`), `internal/memory/chroma`,
  `internal/memory/embedhttp`.
- tech-debt: "semantic similarity is activated only for areas", "embeddings re-embed the whole
  corpus every pass", "cosine threshold is model-specific and hand-tuned".
