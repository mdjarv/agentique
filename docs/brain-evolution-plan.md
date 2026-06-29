# Brain Evolution — Implementation Plan

Status: draft for review · 2026-06-29 · companion to the design doc (rev 5).
Goal: close the gap the audit found — **capture works, evolution is dormant** — and
turn the store into the self-curating, ingest→churn→inject pipeline we designed.

## Is this the complete plan for all brain work?

It is the **complete plan for the brain-*evolution* work** (everything in the design doc:
the injection-gate pipeline, the Curator churn, disuse-confidence aging, typed relations +
evidence/volatility labels, two-stage local→global curation, archive-not-delete). Coverage
is end-to-end across three bands.

Resolution is deliberately uneven — and that's the point of "migrate and shape":

- **Band 1 (Migrate)** is specified file-by-file; we can start now.
- **Band 2 (Churn)** and **Band 3 (Extend)** are at design resolution. Their exact prompts,
  schemas, thresholds and rates are better fixed against real behaviour from Band 1 than
  argued up front.

It is **not** "every brain task that exists." Out of scope / separate tracks (see the last
section): the agentkit lift, the 3D-graph perf work (O(n²) kNN cache), graph config knobs,
and the semantic-recall quality soak. Some overlap (e.g. salience-gated consolidation is
subsumed by our aging model) and is called out where it does.

## Guiding constraints (from project conventions)

- **Reversibility is the safety model** — markdown stays source of truth; forgetting
  archives, never auto-deletes; over-deletion guard holds; pinned/human exempt; a brain
  snapshot precedes every churn. No shadow phase.
- **Lock a behaviour baseline before refactoring** so failures are attributable.
- Tests must pass before each commit: `cd backend && go test ./... -count=1 -short`, with
  `-race`; `just check` (biome + tsc) for any frontend. `just sqlc` after SQL changes;
  `just typegen` after wire-type changes.
- New tunables go in `config.toml [brain]` (env overrides), not env-only.
- Doc comments + `docs/brain-*.md` updates are part of completion.
- Prefer new focused files over swelling existing ones. Wrap errors with `%w`.

---

## Band 1 — Migrate (the foundation, build-ready)

Swaps the dormant machinery for the live pipeline. No LLM Curator yet, so nothing risky.

### M0 · Baseline lock
- **Goal:** capture current recall/consolidation behaviour so later changes are attributable.
- **Approach:** golden tests over `internal/memory` recall ranking + `Consolidate` on a fixed
  fixture corpus; snapshot current frontmatter schema.
- **Done:** tests green and committed before any behaviour change.

### M1 · Brain snapshot + rollback — ✅ SHIPPED
- **Goal:** one-shot, restorable snapshot of the brain dir before each churn / migration.
- **Files:** new `backend/internal/brain/snapshot.go`; call from `automation.go:runOnce`
  (top); CLI `agentique brain snapshot` + `restore` (`backend/cmd/agentique/brain_snapshot.go`).
- **Approach:** copy `brain/` → `brain/.snapshots/<ts>/`; retain N (`snapshot-retain`, default 7).
  Pure FS, stdlib only (manual `WalkDir`, not `os.CopyFS`). Time-injectable core (`snapshotAt`).
- **Done:** snapshot taken at the top of `runOnce` (WARN-and-proceed on failure); restore writes a
  pre-restore safety snapshot first and refuses against a live server unless `--force`; retention
  enforced; `.snapshots` proven invisible to `filestore.List`/`ListScopes`.

### M2 · Capture-tier ingest (the gate's input) — ✅ SHIPPED
- **Goal:** session ingest writes **raw, non-injectable captures**, not injectable facts.
- **Files:** `brain/brain.go` (`LearnFromTranscript` — change `SourceConsolidated` →
  `SourceCapture` at the write); `session/service.go` (ingest trigger); `server.go` (wire).
- **Note — provenance simplification:** the capture tier *makes a separate `SourceLearned`
  unnecessary*. Ingest = `capture`; the churn promotes `capture → consolidated` with
  `derived_from`. Provenance is the tier transition. (Supersedes the design doc's
  "add SourceLearned" line.)
- **Recall already excludes `capture`** (`recall.go`) — verified — so captures never inject.
- **Done:** an ended session produces `capture` records; recall does not surface them.

### M3 · Learn on completion (not only on delete)
- **Goal:** fresh input flows without deleting sessions.
- **Files:** `session/service.go` — add a session-completion / `TurnCompletedEvent` hook
  alongside the existing `DeleteSession` path (kept as the capture-before-cascade safety net).
- **Approach:** sub-task to pick the exact lifecycle event (runtime →idle / session
  "completed" status / turn-completed). Keep `minEventsToEncode` gate. Fire async.
- **Done:** captures appear on completion; no double-capture vs the delete path (dedup by
  session id + last-ingested marker).

### M4 · Reinforce-on-re-observe
- **Goal:** re-encountering a known fact **strengthens** it instead of being silently dropped.
- **Files:** `memory/record.go` (`Corroborations int`); `brain/brain.go` (`Add` dedup branch);
  `memory/strength.go` (reinforce helper, reuse the 0.95 corroboration ceiling).
- **Approach:** in `Add`, when a capture/observation duplicates a **durable** fact → bump
  `Corroborations`, refresh `LastUsedAt`, nudge confidence toward the ceiling, and return it
  (no new record). Genuinely-new text → write a capture.
- **Done:** repeated observation raises a durable fact's confidence/corroborations.

### M5 · Computed disuse-confidence aging (not materialized)
- **Goal:** confidence erodes with disuse and rises with use, **without** rewriting files for
  every nudge; archiving is a *transition*, not a timer.
- **Files:** new `memory/aging.go` (`EffectiveConfidence(r, now, policy)` pure fn);
  `recall.go` (rank by effective confidence; exclude `lifecycle=archived`);
  `automation.go` (replace the empty `DecayPolicy{}` with a real
  `{StrengthWeighted, SalienceWeighted, …}` used only for the **archive transition**);
  `record.go` (`Lifecycle` enum).
- **Approach:** effective = stored × f(time-since-last-*helped*, evidence, volatility), floor
  by evidence/volatility (human/evergreen floor high, never archive). Persist only when a
  fact crosses the floor → `lifecycle=archived` (kept on disk, out of recall, restorable).
- **Done:** unused inferred facts fade out of recall over time; a single `helped` revives;
  human/evergreen never erode; nothing is deleted.

### M6 · Label fields + one-time backfill
- **Goal:** the controlled vocabulary the churn and aging branch on.
- **Files:** `memory/record.go` (`Evidence`, `Volatility`, `Lifecycle`, `Relations
  []TypedRelation`, `Keywords`, `LastCurated`, `CuratorNote`); filestore yaml tags; a one-time
  backfill.
- **Approach:** keep `Related []string` for back-compat; add typed `Relations`. Backfill the
  existing ~1,482: volatility from category (goal/task→ephemeral, preference/identity→
  evergreen, else slow); evidence from source (human→user_stated, else inferred);
  lifecycle=active. No logic keys off them yet (that's Band 2).
- **Done:** all records carry defaulted labels; round-trips through filestore; Chroma metadata
  extended with volatility/lifecycle for filtered recall.

### M7 · Durable retry queue
- **Goal:** a restart mid-extraction never loses a learn/outcome job.
- **Files:** goose migration + sqlc for `brain_jobs(id, kind, scope, payload, attempts,
  created)`; new `brain/jobqueue.go`; `server.go` wiring; drain on startup + on enqueue.
- **Approach:** app-state (not memory content) → lives in `agentique.db`. Enqueue instead of
  bare goroutine; idempotent; bounded retries then dead-letter + log.
- **Done:** kill the server mid-ingest → job resumes after restart; no silent loss.
- ⚠️ Adds a DB table (migration) — schema change only; no writes to the live DB outside the app.

**Band 1 exit:** the store visibly *lives* — captures gate, re-observation reinforces, disuse
fades to archive — with zero LLM curation and full reversibility.

---

## Band 2 — Churn / the Curator (design resolution; sharpen during build)

### C1 · Two-stage structure
Replace `automation.runOnce` with Stage 1 (per-project, grounded) → Stage 2 (global sweep).
Snapshot (M1) first. Stage 1 cadence nightly + drift; Stage 2 ~weekly.

### C2 · The Curator reviewer
New `brain/curator.go`: load a scope's **captures + curated + neighbours + stats** into one
opus context; return **schema-validated per-memory verdicts** (useful / redundant / noise /
currency → keep/strengthen/merge/rewrite/supersede/abstract/archive/flag + rationale). Reuse
`writePromoted` / `applyReorg` + the over-deletion guard to apply.

### C3 · Typed relations + interference→reconcile
Wire `DetectInterference` (today graph-view-only) into the churn so candidate pairs become
typed `Relations` with an action (supersede/merge/abstract/contradict→flag).

### C4 · Evidence + volatility assignment
Churn sets/re-checks `evidence` and `volatility` (the labels are self-correcting because the
churn revisits them each pass).

### C5 · Stage-1 lightweight grounding
While the project is in context, cheap checks: do `applies_to` paths/files still exist? →
flag stale. (Deep semantic grounding is Band 3.)

### C6 · Promotion to global
Stage 2 promotes recurring cross-scope facts to global (existing `MinPromotionScopes` path).

---

## Band 3 — Extend

- **E1 · Drift triggers + fast-path** — churn a scope after N new captures; explicit
  "remember this" / user corrections promote immediately (skip the wait).
- **E2 · Brain-health report** — per-pass changelog + trend dashboard (recall precision,
  churn %, age/confidence distributions, contract self-earned vs human). Brain UI + digest.
- **E3 · Deep repo grounding** — Curator verifies factual claims against live code.
- **E4 · Frontend** — typed-relation edges in the 3D `BrainGraph.tsx`; lifecycle/archived
  filter; evidence/volatility surfaced. `just typegen` for new wire types; `just check`.

---

## Cross-cutting

- **Config:** new `[brain]` keys (decay rates + floors, drift thresholds, stage cadences,
  curator model) — config.toml with env override; enable in the user's
  `~/.config/agentique/config.toml`, restart to apply.
- **Tests:** `-race` throughout; golden recall/consolidation tests extended per band; integration
  test gated by a live model stays `-short`-skippable.
- **Docs:** update `docs/brain-memory.md` + the relevant `brain-*.md`; doc-comment new exported
  symbols.

## Sequencing & dependencies

```
M0 → M1 → (M2,M3) → M4 → M5 → M6 → M7   [Band 1, mostly linear; M2/M3 parallel]
                                  └────→ C1 → C2 → (C3,C4,C5,C6)   [Band 2 needs M6 labels]
                                                              └──→ E1..E4   [Band 3]
```

Each task is an independent commit; each band is independently shippable.

## Out of scope / adjacent brain tracks (not this plan)

- **agentkit lift** of `internal/memory` (do after these contracts settle).
- **3D graph perf** — fingerprint-cache the O(n²) kNN; make cap/threshold/weights configurable.
- **Semantic-recall quality soak** — multi-turn judge + recall-precision tuning.
- **D3 salience-gated consolidation** — largely *subsumed* by M5's salience-aware aging.

## Risks

- **Concurrency:** churn writing while a live `Add()` runs → filestore race. Verify/lock
  regardless of storage choice (storage-independent reliability bug). *Check early.*
- **Recall regression during migration:** keep the read path working as the write/churn path
  changes; the keyword fallback is the floor.
- **Capture pile-up** between churns: bounded by drift triggers (E1); captures never inject so
  pressure is on context size, not correctness.
