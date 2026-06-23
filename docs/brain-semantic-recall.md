# Brain: semantic recall — why lexical is blunt, and the embedder path

Status: **Shipped (veto + vouch bar + semantic threading; verified live)** · 2026-06-22 ·
Sibling to [brain-memory.md](brain-memory.md), [brain-cross-scope-areas.md](brain-cross-scope-areas.md)

> **TL;DR of what shipped (2026-06-22).** The hybrid blend now lets semantics *exclude*, not
> just re-rank, an off-topic keyword survivor — via two complementary levers: a **vector veto**
> (drop a candidate the embedder scores as actively unrelated, regardless of keyword) for the
> multi-token case, and a **vouch bar** (the lexical lone-token guard is overridden only when
> the vector score clears the cosine *related* line, not the much lower `minVectorScore`) for
> the lone-token case. Semantic similarity is also threaded through `ApplyPlan`
> (links/communities), `DetectInterference`, and `ApplyGlobal`'s area refresh. Verified end-to-end
> against a **local all-MiniLM-L6-v2 (Ollama) + Chroma**: recall of "secrets and vars on github"
> returns only the on-topic fact, the GOPRIVATE fact is excluded via the vector path under
> shipped defaults. See **"What actually shipped"** and the **runbook** at the bottom.

> **Motivation (a real mis-recall).** In a `meta-spec` session wiring up Sentry for a TS/Vite
> sub-repo, the brain injected *"Private allbin Go modules require GOPRIVATE=github.com/allbin/\*
> plus git SSH config."* That fact is correct and correctly scoped (meta-spec is polyglot — it
> holds the Go `meta-link` CLI), but it was **irrelevant** to the question. The agent noticed and
> said so ("the recalled Go/GOPRIVATE note doesn't apply here"). The root cause is purely lexical:
> the match rested on the single token **`github`** (df 3/173 → moderate idf), while the query's
> actually-discriminating terms — `secrets`, `vars` (df 0) — matched nothing. Keyword recall can't
> know `github` is uninformative glue here; an embedder would never put "github secrets/vars" near
> "GOPRIVATE Go modules git SSH".

## Two parts: the blunt fix, and the real cure

> Both parts have now shipped (2026-06-22). This section is the original framing; see
> **"What actually shipped"** below for the as-built result and the vouch-bar refinement.

### Part 1 — lexical precision guard (the blunt fix, the `semantic=false` safeguard)

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
| **consolidation passes** | CPU only | first pass embeds the corpus; thereafter a text-hash cache (warmed from Chroma on restart) makes an unchanged corpus cost ~zero embeds |
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

> **Update (2026-06-22): option 1 shipped, but measurement showed it isn't sufficient alone —
> and revealed the *real* leak.** See "What actually shipped" below: for all-MiniLM the github
> fact scores ~0.36 (not near-zero), so a safe veto floor doesn't catch it; the actual fix is
> the **vouch bar**.

## What actually shipped

**The measurement (live, all-MiniLM-L6-v2 via Ollama).** Query `"secrets and vars on github"`
over a corpus mirroring the real scope:

| fact | cosine |
|---|---|
| Sentry DSN reads env **secrets and vars** (on-topic) | **0.4425** |
| release workflow → **github** actions | 0.4229 |
| GOPRIVATE=**github**.com/allbin Go modules (the mis-recall) | **0.3585** |
| nginx proxy manager TLS | 0.1335 |
| modernc.org/sqlite preference | 0.0453 |

Two facts: (a) the embedder *does* rank on-topic above off-topic (margin 0.084) — the hypothesis
holds; (b) MiniLM's distribution is **compressed** — the off-topic fact sits at 0.36, not
near-zero, and "related" is only ~0.44. So an absolute veto floor high enough to drop 0.36 (~0.40)
is fragile (it brushes the 0.42–0.44 real matches), and one safe enough to keep them (~0.15)
doesn't drop it. The veto-as-specified catches *clearly-unrelated* facts (nginx/sqlite level), not
this one.

**The real leak — and the fix (the "vouch bar").** The hybrid path leaked the github fact because
the lexical lone-token guard was **skipped whenever `vs ≥ minVectorScore` (0.20)**. For a
compressed model 0.20 is below even the unrelated baseline, so *any* vector score "vouched" and
disabled the guard. The fix: the guard is now overridden only when the vector **genuinely vouches**
— `vs ≥ VectorVouchScore`, where the brain passes its cosine *related* line (`cosThresh`, default
`DefaultSemanticThreshold` 0.45). The github fact (0.36 < 0.45) no longer vouches, so its lone
`github` match is dropped by the lexical guard. No fragile hand-set floor needed.

The two levers cover **disjoint** classes and both ship:
- **Vector veto** (`Query.VectorVetoScore` / `DefaultVectorVetoScore` 0.15): drops a candidate the
  embedder scores as actively unrelated, regardless of keyword — handles the **multi-token**
  off-topic survivor the lexical guard can't (`kwMatches>1`). Only fires for candidates the vector
  actually *scored* (present in results); an unindexed/out-of-window one falls back to keyword.
  Recall also widens the vector search to cover the keyword candidate pool (`maxRecallVectorK` 200)
  so the veto can see survivors, not just top-k.
- **Vouch bar** (`Query.VectorVouchScore` = `cosThresh`): gates the lexical lone-token guard —
  handles the **lone-token** survivor (the github case).

Both default to the constants above (`VectorVetoScore`/`VectorVouchScore` left 0 → defaults);
`AGENTIQUE_BRAIN_VECTOR_VETO` overrides the veto floor. Result under shipped defaults: recall of
the github query returns **only the Sentry fact**.

**Semantic threading (#2).** `ConsolidateOptions.SimOptions` threads embedding-blended similarity
through `memory.ApplyPlan` → `RelinkScope` + `AssignCommunities` (per-scope links/communities now
semantic); `DetectInterference` blends cosine into its related-lower bound (dup-exclusion stays
lexical); `ApplyGlobal` refreshes areas via the semantic `s.AssignAreas`. The brain computes the
SimOptions *before* taking `s.mu` so the embed never blocks under the lock.

## Sequencing

1. ✅ **Lexical lone-token guard** — shipped (`recall.go`, `singleTokenMinShare`). The
   `semantic=false` safeguard; still in place.
2. ✅ **Local embedder** — Ollama + all-MiniLM-L6-v2 (`/v1/embeddings`) + Chroma v2, wired via the
   `AGENTIQUE_BRAIN_*` env. Revived semantic clustering/areas through `ApplyPlan` (#2).
3. ✅ **Tighten the hybrid blend** — vector veto (option 1) **plus** the vouch bar (the measured
   real fix). Verified live.
4. ✅ **Embedding cache** — shipped an in-process text-hash cache (`Service.embedCache`): after the
   first pass an unchanged corpus costs zero embed calls. **Cold start closed** (`warmEmbedCache`):
   the first clustering embed after a restart seeds the cache from the vectors Chroma already holds
   via a bulk-vector read (`chroma.Client.GetEmbeddings` → `chroma.Store.LoadVectors`, keyed by
   `embedKey(document)`), so a restart re-embeds nothing for an unchanged corpus — runs once per
   process, retries on a transient Chroma error. **Pruning closed** (`pruneEmbedCache`, at the
   `AssignAreas` whole-brain checkpoint): stale entries from edited/deleted texts are dropped so the
   cache is bounded by the live corpus. Verified live (`TestWarmEmbedCacheLiveZeroReembedAfterRestart`).
5. ✅ **Auto-calibration** — the veto/vouch/cosine floors are derivable from the corpus's OWN
   pairwise cosine distribution instead of hand-tuned per model. Shipped: a pure
   `memory.Calibrate` (`internal/memory/calibrate.go`) samples the pairwise cosines and reads
   the **cosine "related" line from a high percentile (p99) and the veto floor from a low one
   (p25)**; `brain.New` opts in via `AGENTIQUE_BRAIN_AUTOCAL=1`, embeds the live corpus (through
   the text-hash cache) and overrides the hand defaults for the knobs the operator didn't pin.
   See "Auto-calibration" below.

## Auto-calibration (sequencing #5)

The three thresholds are model-specific because cosine distributions differ per embedder — and
even on one model the hand value can be wrong for a corpus whose breadth differs from the sample
it was tuned on. Rather than hand-pick a constant, **measure the corpus's own pairwise cosine
distribution and read thresholds off its percentiles.** The shape is the lever: most fact pairs
are unrelated (different topics), so the bulk sits low and only a thin tail is genuinely related —
hence the related line is a *high* percentile and the veto floor a *low* one.

**Measured live (all-MiniLM, the real 1509-fact brain, 227k sampled pairs, via `brain calibrate`):**

| percentile | cosine | role |
|---|---|---|
| p25 | **0.0455** | → vector veto floor (clearly-unrelated band; cf. sqlite 0.05 / nginx 0.13) |
| p50 | 0.108 | (median pair — the unrelated bulk) |
| p99 | **0.4187** | → cosine "related" line = link threshold + vouch bar |
| p99.5 | 0.470 | |

Derived: **cosineThreshold 0.4187 (vs hand 0.45), vectorVeto 0.0455 (vs hand 0.15)**. The related
line stays above the off-topic-keyword survivor (~0.36) so the github fact still can't vouch —
**verified: recall of "secrets and vars on github" returns only the Sentry fact under the
auto-derived thresholds** (`TestBrainAutoCalibrateExcludesGithub`). A finding worth noting: the
hand veto 0.15 sits at ~p63 of the real corpus — it was tuned on the 5-fact example and is
over-aggressive on a broad brain; auto-cal corrects it *downward*, and because the github
exclusion rides on the **vouch bar** (not the veto) this is safe.

- **Helper** (`internal/memory/calibrate.go`, pure/liftable): `SampleCosineDistribution` (sorted,
  deterministic stride sampling, empty-vector drop, `MaxPairs` cap 200k), `DeriveThresholds`
  (percentile→threshold, veto clamped into `[0, cosThresh)`, thin-sample `OK=false` fallback),
  and the one-call `Calibrate`. `DefaultRelatedPercentile` 0.99, `DefaultVetoPercentile` 0.25.
- **Wiring**: `brain.Config.Calibrate` / `Service.Calibrate` embed the durable corpus (through the
  text-hash embed cache — calibration also *warms* it for the next pass) and call `memory.Calibrate`.
  `brain.New` runs it at boot when `AGENTIQUE_BRAIN_AUTOCAL=1`, bounded by a 2-minute timeout, and
  overrides only the knobs left unset. **Precedence: explicit
  `AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD`/`_VECTOR_VETO` > auto-cal > hand default.** Any failure
  (no embedder, thin corpus, embed error) keeps the defaults and logs why — boot never breaks.
- **Inspect without booting**: `agentique brain calibrate` prints the distribution + derived
  thresholds next to today's defaults (writes nothing) — the measure-first tool the runbook used
  to fake by running the integration test.

## Runbook — stand up the local embedder + Chroma and verify

```bash
# 1. Ollama (CPU is fine for embeddings). Download the static binary, serve, pull the model.
#    (the install script needs root; the tarball does not)
curl -fSL https://github.com/ollama/ollama/releases/latest/download/ollama-linux-amd64.tar.zst \
  | tar --use-compress-program=unzstd -xf - -C /tmp/ollama       # extracts bin/ + lib/
OLLAMA_HOST=127.0.0.1:11434 OLLAMA_MODELS=/tmp/ollama/models \
  LD_LIBRARY_PATH=/tmp/ollama/lib /tmp/ollama/bin/ollama serve &
/tmp/ollama/bin/ollama pull all-minilm                            # 45 MB, 384-dim

# 2. Chroma v2 (the client uses /api/v2). Docker is simplest (no pip needed):
docker run -d --name chroma -p 127.0.0.1:8000:8000 chromadb/chroma:latest

# 3. Point the brain at them (the server reads these in serve.go):
export AGENTIQUE_BRAIN_CHROMA_URL=http://127.0.0.1:8000
export AGENTIQUE_BRAIN_EMBED_URL=http://127.0.0.1:11434/v1/embeddings
export AGENTIQUE_BRAIN_EMBED_MODEL=all-minilm
# optional: AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD, AGENTIQUE_BRAIN_VECTOR_VETO (pin a knob)
# optional: AGENTIQUE_BRAIN_AUTOCAL=1 to derive both from the live corpus at boot (#5)
# On boot, look for: "brain: semantic recall enabled ... cosineThreshold=0.45 vectorVeto=0.15"
# and, with AUTOCAL: "brain: semantic thresholds auto-calibrated ... cosineThreshold=0.42 ..."

# 4. Verify (env-gated integration tests — they re-measure + assert the github case):
CHROMA_TEST_URL=http://127.0.0.1:8000 \
EMBED_TEST_URL=http://127.0.0.1:11434/v1/embeddings \
EMBED_TEST_MODEL=all-minilm \
  go test ./internal/memory/chroma/ -run TestSemanticRecallVetoesGithubMisRecall -v
# and the production-wiring test:
#   go test ./internal/brain/ -run TestBrainSemanticWiring -v
```

Calibration note: the cosine/veto floors above are all-MiniLM-specific. For another model you no
longer have to read them off by hand — run `agentique brain calibrate` (prints the corpus's own
cosine distribution + the percentile-derived thresholds) or set `AGENTIQUE_BRAIN_AUTOCAL=1` to have
the server derive them at boot (#5). The hand defaults remain the fallback; an explicit
`AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD`/`_VECTOR_VETO` still wins per-knob.

Index maintenance: the Chroma collection is maintained **lazily** (each `Put` indexes one fact), so a
bulk hand-edit of the markdown files or an embedding-model change leaves vectors stale or missing
until a later pass touches each fact. Rebuild the whole collection in one shot with
`agentique brain reindex` (re-embeds the durable corpus from the markdown source of truth; needs the
same embedder + Chroma config the server uses — env or the `[brain]` config keys). The slow self-heal
is the **scheduled-consolidation** pass, which also refreshes the semantic graph; it now runs once
shortly after server start (a short initial delay) and then on `consolidate-interval`, so a
frequently-restarted server can no longer defer that refresh indefinitely (a bare interval timer used
to reset on every restart).

## References

- `internal/memory/recall.go` (`rank`, the veto `DefaultVectorVetoScore`/`Query.VectorVetoScore`,
  the vouch bar `Query.VectorVouchScore`, `singleTokenMinShare`, `maxRecallVectorK`, the hybrid blend).
- `internal/memory/store.go` (`Query.VectorVetoScore`, `Query.VectorVouchScore`).
- `internal/memory/similarity.go` (`DefaultSemanticThreshold`, `Similarity.interference`),
  `internal/memory/{consolidate,interference}.go` (`ConsolidateOptions.SimOptions` threading).
- `internal/memory/calibrate.go` (auto-calibration #5: `Calibrate`, `SampleCosineDistribution`,
  `DeriveThresholds`, `CalibrationSample`/`Result`, the `Default*Percentile` constants),
  `internal/brain/brain.go` (`Config.Calibrate`, `Service.Calibrate`, `New` opt-in via
  `AGENTIQUE_BRAIN_AUTOCAL`), `cmd/agentique/brain.go` (`brain calibrate`).
- `internal/brain/brain.go` (`New` semantic wiring incl. `vetoScore`; `scopeSimOptions`;
  veto+vouch threaded into `Recall`/`RecallBlock`; `ApplyPlan`/`Consolidate`/`ApplyGlobal`;
  `Service.Reindex` → `chroma.Store.Reindex`), `internal/brain/graph.go` (semantic interference),
  `internal/memory/chroma`, `internal/memory/embedhttp`.
- Index maintenance: `cmd/agentique/brain.go` (`brain reindex`, config-aware `newBrainService`),
  `internal/brain/automation.go` (near-boot first pass via the initial-delay timer in `loop`).
- Live integration tests (env-gated): `internal/memory/chroma/semantic_recall_integration_test.go`,
  `internal/brain/semantic_integration_test.go` (`TestBrainSemanticWiring`,
  `TestBrainAutoCalibrateExcludesGithub`). Unit: `internal/memory/calibrate_test.go`.
- tech-debt: "semantic similarity is activated only for areas" (now CLOSED), "embeddings re-embed the
  whole corpus every pass" (open, more pressing), "cosine threshold is model-specific and hand-tuned"
  (3 coupled knobs now), "lexical recall precision … → semantic cure SHIPPED".
