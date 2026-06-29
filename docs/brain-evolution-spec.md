# Brain Evolution — Band 1 ("Migrate"): Final Implementation Spec

## 1. Title, mission, how to use this spec

**Mission.** The brain captures memories but does not *evolve* them: ~98% of facts carry `source=consolidated`, ~97% are frozen at confidence 0.80, scheduled consolidation passes an empty `DecayPolicy{}` so decay never fires, re-observing a known fact is a no-op, interference detection never reconciles, and learning only triggers on session *deletion*. Band 1 migrates the existing loop into an **ingest → churn → inject pipeline with an injection gate**, makes confidence a **living scalar that erodes on disuse** (computed at recall, persisted only at the archive transition), adds a **controlled-vocabulary label control plane**, and makes the whole thing **reversible** (markdown is source of truth; archive-not-delete; snapshot before every churn).

**How a developer agent uses this spec.** This document is self-contained. Execute the tasks in the order given in §6 (Sequencing) — **not** strictly by number: M6 (labels) is a hard predecessor of M5 (aging). Each task in §5 has: intent, exact file changes with signatures, behavioural contract, tests, acceptance checklist, and gotchas. §4 is the ground-truth contract reference — when any task body disagrees with §4, §4 wins. Where a fact could not be verified, the task carries an explicit **VERIFY FIRST** step; do that step, do not invent. Run the quality gates in §3 before every commit. Lock a behaviour baseline (M0) before touching production code.

---

## 2. Context & model

**The pipeline (target model).**
1. **Ingest** — cheap, frequent. Writes *raw* captures (`Source = capture`) that are **never injectable**. Session-end and clean-completion both stage captures.
2. **Churn** — the gate. The only path from `capture → consolidated`: it promotes, merges, updates, and re-checks labels, reviewing new + old facts together. Promotion stamps `DerivedFrom` (the capture IDs) — *provenance is the tier transition*, which is why a separate `SourceLearned` source is unnecessary.
3. **Inject** — recall returns only curated/injectable facts (`human`, `agent`, `consolidated`) plus pinned facts; captures and archived facts are excluded.

**The injection gate.** `capture` is excluded from `Recall` (recall.go) and from `OperatingContract`/`PinnedPreamble` upstream. Becoming injectable requires the churn. Humans optionally curate on top (Confirm → `human`).

**Disuse-confidence aging (computed, not materialized).** Confidence is a living scalar. *Effective confidence* = stored `ConfidenceScore` eroded by time-since-last-use on a volatility-keyed half-life, clamped up to an evidence floor. It is computed at recall time and **never written on a nudge**. Use (helped/corroborated/re-observed) refreshes `LastUsedAt` and raises the stored score; disuse erodes the effective value. Forgetting = **archive** at a confidence floor: a cold tier excluded from recall, kept on disk, restorable — **never auto-delete**. Human/pinned/locked/evergreen never erode and never archive. State is persisted exactly once, at the archive transition.

**Labels (control plane — logic branches on them).**
- `Evidence`: `user_stated | code_verified | corroborated | inferred | observed_once`.
- `Volatility`: `evergreen | slow | ephemeral` → decay rate.
- `Lifecycle`: `active | superseded | archived`.
- Typed `Relations`: `supersedes | contradicts | duplicates | generalizes | corroborates` (replaces the untyped `Related` list; `Related` retained for back-compat).
- `Keywords`: free-form, recall only, **no logic branches on them**.

**Reversibility safety model (not a shadow gate).** The brain is early/unproven, so we migrate the loop directly and shape it safely: markdown is SoT; archive-not-delete; the over-deletion guard holds; pinned/human are exempt; a brain snapshot precedes each churn (M1). Reversibility, not a second approval gate, is the safety property.

---

## 3. Conventions & quality gates

- **Tests before every commit:** `cd backend && go test ./... -count=1 -race -short` (run directly, not via the justfile; `-short` skips the live-CLI integration test; `-race` is mandatory). M0 also adds `-race -short` to the `test-backend` justfile recipe.
- **Gate:** `just check` (gofmt, vet, lint, biome, tsc) must pass.
- **After editing `backend/db/queries/` or `backend/db/migrations/`:** run `just sqlc` to regenerate `backend/internal/store/*.sql.go`. Never hand-edit generated files.
- **After changing Go wire types:** run `just typegen`. (Band 1 changes `memory.Record` and adds an internal `store.BrainJob`; neither is a registered wire type — typegen is **not** required unless you also extend `brain/http.go`'s `memoryDTO`, which Band 1 does not.)
- **Tunables go in `config.toml [brain]`** with `AGENTIQUE_BRAIN_*` env overrides (env wins; `firstNonEmpty`/`envIntOr`/`envFloatOr`/`envBoolOr`); never env-only.
- **Docs are part of completion:** doc comments on every exported symbol, plus the relevant `docs/brain-*.md` updates.
- **Errors:** wrap with `%w`; never swallow; guard clauses / early returns; one job per function (separate IO from logic).
- **Lock a behaviour baseline before refactoring** (M0, and per-task baseline locks).
- **Prefer new focused files** over swelling existing ones.

---

## 4. Verified code contracts (the reference of record)

**`backend/internal/memory` (core — stdlib + yaml/uuid only):**

```go
type Record struct {
  ID string; Scope Scope; Text string; Category Category; Source Source
  Pinned bool; Locked bool; Uses int; Helped int
  CreatedAt, UpdatedAt, LastUsedAt time.Time
  DerivedFrom []string; Subsumed []SubsumedSource; Related []string
  Community int; Area string
  Confidence ConfidenceTier; ConfidenceScore float64
  ReviewNote string; Embedding []float32
}
func New(scope Scope, text string, category Category, source Source) Record // sets ID, trims Text, ConfidenceForSource, CreatedAt=UpdatedAt=now.UTC()

const ( SourceHuman="human"; SourceAgent="agent"; SourceConsolidated="consolidated"; SourceCapture="capture" ) // only first three injectable
const ( ConfidenceExtracted="extracted"; ConfidenceInferred="inferred"; ConfidenceAmbiguous="ambiguous" )
const ( ScoreGroundTruth=1.0; DefaultInferredScore=0.8; CrossProjectInferredScore=0.65; AmbiguousScoreThreshold=0.55; ActOnConfidence=0.85 )

type Store interface { Put(ctx,Record) error; Get(ctx,id) (Record,error); Delete(ctx,id) error; List(ctx,...Scope) ([]Record,error) }
func ConfidenceForSource(Source) (ConfidenceTier, float64)   // human→(extracted,1.0); else (inferred,0.8)
func TierForScore(Source, float64) ConfidenceTier
func NormalizeConfidence(Record) Record                       // backfill+reconcile on load
func StorageStrength(Record) float64                          // pinned→1.0; blend conf/use/provenance; never decays
func RetrievalStrength(Record, now) float64                   // StorageStrength × 2^(-days/30); days from lastSeen
func lastSeen(Record) time.Time                              // LastUsedAt if non-zero else UpdatedAt
func Salience(Record) float64; func salienceDecayFactor(Record) float64
func isProtected(Record) bool                                 // Pinned||Locked||SourceHuman  (consolidate.go:228)
func isContradicted(Record) bool; func isStronglyCorroborated(Record) bool // Helped>=2 && !contradicted
func reorgRetained(Record) bool; func clamp01(float64) float64 // strength.go:125

type DecayPolicy struct { MaxAge time.Duration; MinUses int; ConfidenceWeighted, StrengthWeighted, SalienceWeighted bool }
func (DecayPolicy) effectiveMaxAge(Record) time.Duration
func (DecayPolicy) shouldDecay(Record, now) bool             // KEPT in Band 1 (still referenced by tests); ApplyPlan stops calling it in M5
func (DecayPolicy) staleAge(Record, now) time.Duration

type Extractor interface { Extract(ctx,[]string)([]Candidate,error); Reorganize(ctx,[]Fact)([]Fact,error) }
type ConsolidateOptions struct { PrevFingerprint string; Force bool; Decay DecayPolicy; DuplicateThreshold,MinSurvivorRatio float64; MinPromotionScopes int; PrevManifest map[string]string; DryRun bool; Progress func(int,int); OnError func(error); SimOptions []SimOption }
type Report struct { Scope Scope; Promoted []Record; CapturesConsumed []string; Rewritten []Change; Abstracted,Deleted,Decayed []Record; Skipped,ReorgRefused bool; Fingerprint string }
func PlanConsolidation(ctx,Store,Extractor,Scope,ConsolidateOptions)(Plan,error)
func ApplyPlan(ctx,Store,Scope,Plan,ConsolidateOptions)(Report,error)   // decay block at consolidate.go:379-394 currently Deletes
func Consolidate(ctx,Store,Extractor,Scope,ConsolidateOptions)(Report,error) // NOTE: decay is opts.Decay, NOT a positional param
func writePromoted(...); func applyReorg(...)                            // over-deletion guard: minFacts=8, ratio=0.5
func FindDuplicate(text string, existing []Record, threshold float64)(Record,bool) // DefaultDuplicateThreshold=0.6

type Query struct { Text string; Scopes []Scope; K int; VectorVetoScore, VectorVouchScore float64 }
type Result struct { Pinned []Record; Recalled []Record }
func Recall(ctx,Store,Query)(Result,error) // recall.go:91-99: PINNED branch runs FIRST (line 92-95), THEN capture skip (96-98); now:=time.Now().UTC() at recall.go:224 (non-injectable today)
func DetectInterference(records []Record, lower, upper float64, limit int, ...SimOption) []InterferencePair // captures excluded; A<B
// Thresholds: DefaultRelatedThreshold=0.3 (link.go:12), DefaultDuplicateThreshold=0.6 (dedup.go:7),
//             DefaultCommunityThreshold=0.15 (community.go:16), DefaultSemanticThreshold=0.45 (similarity.go:9)
// LIVE interference caller graph.go:267 passes (DefaultRelatedThreshold, DefaultDuplicateThreshold, maxInterference)
```

**`backend/internal/memory/filestore`** — `frontmatter` struct (yaml.v3), keys: `id, scope, category, source, pinned, locked, uses, helped, created, updated, last_used, derived_from, related, community, area, confidence, confidence_score, review_note, subsumed`. `FileStore.mu sync.Mutex` serializes `Put`/`Delete`; `List` is **non-recursive** (reads only direct `*.md` of each top-level scope dir; filestore.go:335) and sorted by CreatedAt+ID; atomic temp+rename write; `toRecord` calls `NormalizeConfidence`. Layout: `root/{sanitized_scope}/{id}.md`.

**`backend/internal/memory/cachestore`** — read-through, `mu sync.RWMutex`, write-invalidate. Has a documented double-check-lock race; safe only under single-server-process serialization. **`backend/internal/memory/chroma`** — v2 HTTP (goroutine-safe), cosine HNSW, best-effort index (never fails durable write), `SourceCapture` never indexed; metadata keys today: `scope, category, source`.

**`backend/internal/brain`:**
```go
func New(ctx, cfg Config)(*Service,error)
func (s *Service) Add(ctx, scope, text, category, source)(Record,error) // brain.go:436; dedups DURABLE-only (FindDuplicate@459); returns dup UNCHANGED@460; identity auto-pinned@463; NO lock today
func (s *Service) Capture(ctx, scope, text)(Record,error)               // brain.go:474; Source=capture; hardcodes CategoryFact; NO dedup, NO pin today
func (s *Service) LearnFromTranscript(ctx, scope, []TranscriptEvent, Extractor)(int,error) // brain.go:721; line 730 calls s.Add(...,SourceConsolidated)
func (s *Service) Consolidate(ctx, scope, Extractor, decay DecayPolicy, dryRun bool, ConsolidateOpts)(Report,error) // brain.go:853; TAKES s.mu
func (s *Service) MarkHelped/Flag/Confirm(...)                          // brain.go:785-825
func (s *Service) OperatingContract(ctx, projectID) string             // brain.go:526; pref-only, excludes capture, ReviewNote; gate ActOnConfidence=0.85
func (s *Service) RecallBlock(ctx, projectID, prompt, exclude)(string,[]string) // brain.go:590; BumpUses@629
func (s *Service) PinnedPreamble(ctx, projectID) string                // brain.go:501
func ScopeForProject(projectID) Scope                                   // brain.go:418; ""→ScopeGlobal else "project:"+id
func recallScopes(Scope) []Scope                                        // [scope, global]
// reconcile family lives in reconsolidate.go: MarkHelped/MarkContradicted + CorroborationCeiling, AutoCorroborationGapClose, corroborationGapClose
```
**Automation** (`automation.go`): `runOnce`@91; **line 107 passes empty `DecayPolicy{}`** (the audit finding); broadcasts `EventBrainUpdated`@120; `AssignAreas`@124; `initialDelay=30s`. `NewAutomation(svc, runner, bus, interval, model)`@41.

**Session/server hooks:** `Service.onSessionEnd func(projectID string, events []store.SessionEvent)` (service.go:199); `SetOnSessionEnd`@205; `DeleteSession` captures `endEvents`@1104-1109, deletes row@1111, fires `go s.onSessionEnd(...)`@1121-1124 gated by `const minEventsToEncode = 8`@1130. `handleRuntimeStateChange`@68 sets `completedAt` and `SetSessionCompleted` on `runtime.StateDone`@92-96; idle trigger `go s.flushPendingMessages()`@110. `Session.recallFn` field ~line 160, `SetRecallFn` ~611. `Manager.MemoryRecallFn`/`wireRecall`@262, callers Create@313 / Resume@466 / Reconnect@558. server.go: `bus := eventbus.New()`@138; brain wiring 264-417; session-end closure 368-395 (calls `LearnFromTranscript` + `ApplyOutcomesFromTranscript`); `queries *store.Queries` in scope; `NewAutomation`@412.

**DB/config:** migrations `NNN_description.sql` goose Up/Down (latest `036_add_provider.sql`); queries `-- name: X :one|:many|:exec`; `just sqlc` → `backend/internal/store/*.sql.go` + `models.go`. `BrainConfig` (config.go:127) with `toml:"..."` + `AGENTIQUE_BRAIN_*` overrides in serve.go; `server.Config` Brain* fields. `RunMigrations(db, db.Migrations)` at serve.go:274.

---

## 5. Tasks M0..M7 (presented in numeric order; see §6 for execution order — **M6 before M5**)

---

### M0 · Baseline lock

#### Intent
Freeze today's `Recall` ranking, `Consolidate` report, `DetectInterference` banding, and the on-disk frontmatter as deterministic golden tests over a fixed fixture corpus — including current *broken* behaviour (empty `DecayPolicy{}` → no decay; decay-on → `Delete`, not archive) — so every later change yields an attributable golden diff. Test-only, plus the one justfile flag edit.

#### Exact changes
All goldens are deterministic/offline (keyword-only recall, canned `fakeExtractor`). Reuse existing white-box helpers in `package memory` tests (`memStore`/`newMemStore`, `rec`, `mk`, `capture`, `fakeExtractor`, `contains`) — do not redeclare them.

**New `backend/internal/memory/baseline_test.go` (`package memory`):**
- `var updateGolden = flag.Bool("update", false, "rewrite golden files")` (one per package).
- `var fixedNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)`.
- `baselineCorpus() []Record` — ~10 records spanning every `Category`, every injectable `Source`, one `SourceCapture` (must be excluded), one pinned identity fact. **Stable hand-assigned IDs `f01..f10`, fixed text, fixed timestamps. Build keyword scores well-separated (no near-ties)** so the small recency tiebreaker cannot flip ordering across runs (see Gotchas — recall has a non-injectable wall clock).
- `normalizeReport(Report) reportSnapshot` — JSON-stable projection with **sorted** string slices (kills `memStore.List` map-order nondeterminism):
  ```go
  type reportSnapshot struct {
    PromotedTexts, CapturesConsumed, RewrittenIDs, AbstractedTexts, DeletedIDs, DecayedIDs []string // all sorted
    Skipped, ReorgRefused bool
  }
  ```
- `assertGolden(t, name, got []byte)` — read `testdata/baseline/<name>`; on `-update` write; else `t.Fatalf` on `!bytes.Equal`. `json.MarshalIndent(v,"","  ")` + trailing `\n`.

**New `backend/internal/memory/filestore/schema_golden_test.go` (`package filestore`):** own `updateGolden` flag; `fullRecord()` sets **every** frontmatter-mapped field non-zero (so no `omitempty` key is suppressed); `TestBaselineFrontmatterSchema` does a byte-exact round-trip against `testdata/schema.golden.md`.

**Modify `backend/justfile`** — change `test-backend` to `cd backend && go test ./... -count=1 -race -short` (verify no recipe already passes these flags before editing).

**New committed goldens:** `testdata/baseline/recall_<case>.golden`, `testdata/baseline/consolidate_<scenario>.golden`, `testdata/baseline/interference.golden`, `testdata/schema.golden.md`.

#### Behavioural contract
Goldens encode *current* semantics: empty `DecayPolicy{}` → `DecayedIDs: []`; populated `DecayPolicy{MaxAge>0,MinUses}` → stale fact in `DecayedIDs` **and removed from the store** (current delete). M5 will flip these and the diff is the audit trail. Locked edge cases: empty/whitespace query (Recalled empty, Pinned returned); capture exclusion; pinned-identity exemption; dedup-on-promotion (`PromotedTexts: []`); human/locked survival.

#### Tests (all `-race`)
- `TestBaselineRecallRanking` (table, keyword-only): `build_tooling`, `empty_query`, `identity`. **Binding assertion form (single, unambiguous): top-K recalled ID *set membership* + top-1 ID spot-check** — *not* full strict byte order — because `rank` uses a non-injectable `time.Now()` and the recency term scales per-record by `StorageStrength` (see Gotchas). Marshal `{pinned (set), recalledTop1, recalledSet}` and `assertGolden`.
- `TestBaselineConsolidateReport` (canned `fakeExtractor`):
  - `promote_reorg_decay_off`: `memory.Consolidate(ctx, store, ex, scope, ConsolidateOptions{})` → `normalizeReport == golden`, `DecayedIDs` empty, protected (human/locked/pinned) IDs present in sorted `finalStoreIDs`.
  - `decay_on_deletes`: `memory.Consolidate(ctx, store, ex, scope, ConsolidateOptions{Decay: DecayPolicy{MaxAge: 24*time.Hour, MinUses: 1}})` with a `fixedNow`-relative stale `UpdatedAt` → stale ID in `DecayedIDs` and **absent** from `finalStoreIDs` (locks delete-not-archive).
  - `dedup_promotion`: candidate duplicates a durable fact → `PromotedTexts: []`.
  - Assert only via sorted `normalizeReport` + sorted `finalStoreIDs`; never raw `List` order.
- `TestBaselineInterferenceBands`: `DetectInterference(baselineCorpus(), memory.DefaultRelatedThreshold /*0.3*/, memory.DefaultDuplicateThreshold /*0.6*/, 50)` with **no** `SimOption` — **mirrors the live caller `graph.go:267`** (lower=related=0.3, upper=duplicate=0.6). Byte-equal `interference.golden` (`A<B` deterministic; captures excluded).
- `TestBaselineFrontmatterSchema`: byte-exact `fullRecord()` round-trip.

#### Acceptance checklist
- [ ] New test files + `testdata/baseline/*.golden` + `testdata/schema.golden.md` committed.
- [ ] `cd backend && go test ./internal/memory/... -count=1 -race -short` green; full `./...` green; `just check` green.
- [ ] `go test ./internal/memory/... -run TestBaseline -update && git diff --exit-code` → no diff (stable).
- [ ] No production `.go` file under `internal/memory`/`internal/brain` changed (M0 is test-only + the justfile flag).
- [ ] `test-backend` recipe includes `-race -short`.
- [ ] Interference golden uses **0.3 / 0.6** (matching `graph.go:267`).
- [ ] Decay-on golden shows stale fact removed from `finalStoreIDs`; decay-off golden shows `DecayedIDs: []`.
- [ ] Lands **before** any behaviour-changing task.

#### Gotchas / VERIFY FIRST
- **Recall has a non-injectable wall clock.** `rank` calls `time.Now().UTC()` (recall.go:224) and the 0.05 recency term `RetrievalStrength(c, now)` scales per-record by `StorageStrength` — it is **not** constant across records and shrinks as wall-clock advances. Do **not** claim "identical across records." The binding contract is **top-K set membership + top-1 spot-check** with a well-separated fixture (keyword 0.95 dominates), so the tiebreaker cannot flip the answer. (If you prefer strict ordering, add an unexported clock seam to recall first; otherwise use the set form. State which one — it is the set form here.)
- **Map-order:** always normalize via sorted slices; never golden raw `List`/`CaptureIDs`/`DerivedFrom` order.
- **`Consolidate` signature:** decay is `ConsolidateOptions.Decay`, **not** a positional arg (the positional `DecayPolicy` exists only on the `brain.Consolidate` wrapper, which M0 does not use).
- **`fakeExtractor` already exists** (may also have a `Promote` method); reuse it — the core `Extractor` is only `Extract`+`Reorganize`.
- **`omitempty` trap:** `fullRecord()` must set every optional frontmatter field non-zero. When a later task adds a frontmatter key, extend `fullRecord()` and regenerate with `-update` — the diff is the schema-change audit.
- **VERIFY FIRST:** the exported field names on `memory.SubsumedSource` (`Scope`, `Text`) before constructing it in `fullRecord()`.
- **Scope:** the brain-level "Add returns dup unchanged" audit lives in `brain.Add` (M4), not in these memory goldens.

---

### M1 · Brain snapshot + rollback

#### Intent
A one-shot, restorable filesystem snapshot taken automatically before every churn and on demand from the CLI, so any consolidation/migration is reversible. Pure FS copy under `brain/.snapshots/<ts>/`, retain N (config), no new deps. **This is the single snapshot mechanism for the whole band — M5 and M6 reuse `brain.Snapshot`, they do not invent their own.**

#### Exact changes
**NEW `backend/internal/brain/snapshot.go`** (package `brain`, stdlib only: `os, io, path/filepath, sort, strings, time, fmt, errors`):
```go
const (
  snapshotsDir          = ".snapshots"
  snapshotTSFormat      = "20060102T150405Z" // UTC, FS-safe, lexically == chronological
  defaultSnapshotRetain = 7
)
type SnapshotInfo struct { ID, Path string; CreatedAt time.Time; Files int; Bytes int64 }

// Snapshot copies the brain tree (every top-level entry EXCEPT .snapshots, INCLUDING
// .fingerprints.json / .global-manifest.json) into brain/.snapshots/<ts>/, then prunes
// to `retain` newest. retain<=0 → defaultSnapshotRetain. Empty brain → Files==0, no error.
func Snapshot(brainDir string, retain int) (SnapshotInfo, error) { return snapshotAt(brainDir, retain, time.Now().UTC()) }

// snapshotAt is the time-injectable core (tests pass deterministic, distinct `now`s
// without sleeping; production passes time.Now().UTC()).
func snapshotAt(brainDir string, retain int, now time.Time) (SnapshotInfo, error)

func ListSnapshots(brainDir string) ([]SnapshotInfo, error) // newest-first; missing dir → (nil,nil)
func Restore(brainDir, id string, retain int) error          // safety-Snapshot first, then make tree == id
// helpers:
func copyTree(src, dst string, skipTop map[string]struct{}) (files int, bytes int64, err error)
func pruneSnapshots(brainDir string, retain int) error
```
- `copyTree`: `filepath.WalkDir`; skip top-level names in `skipTop` (always `{snapshotsDir:{}}` for Snapshot, `{}` for Restore); dirs `0o755`, files `O_WRONLY|O_CREATE|O_TRUNC 0o644` + `io.Copy`; wrap every error with `%w`.
- `snapshotAt`: `id := now.Format(snapshotTSFormat)`; `dst := filepath.Join(brainDir, snapshotsDir, id)`; `MkdirAll`; `copyTree(brainDir, dst, {snapshotsDir})`; `pruneSnapshots`.
- `Restore`: validate snapshot path exists (`fmt.Errorf("brain: snapshot %q not found: %w", id, os.ErrNotExist)`); `Snapshot(brainDir, retain)` for the pre-restore safety copy; remove each live top-level entry except `snapshotsDir`; `copyTree(<snapPath>, brainDir, {})`.

**MODIFY `backend/internal/brain/brain.go`** — `Config`: add `SnapshotRetain int` (doc: `0 = default 7`, env `AGENTIQUE_BRAIN_SNAPSHOT_RETAIN`). `Service`: add `snapshotRetain int` (read-only after `New`). `New`: set it. Add `func (s *Service) Snapshot() (SnapshotInfo, error) { return Snapshot(s.dir, s.snapshotRetain) }`.

**MODIFY `backend/internal/brain/automation.go`** — `runOnce`, after `ListScopes` and **only when `len(scopes) > 0`**, before the scope loop:
```go
if len(scopes) > 0 {
  if info, err := a.svc.Snapshot(); err != nil {
    slog.Warn("brain: scheduled consolidation: snapshot failed", "error", err)
  } else {
    slog.Info("brain: pre-churn snapshot", "id", info.ID, "files", info.Files)
  }
}
```
Decision: snapshot failure logs WARN and the churn proceeds (does not hard-block) — see VERIFY FIRST.

**MODIFY `backend/cmd/agentique/brain.go`** — extract a **testable core** (mirror M6's pattern) and add two thin cobra subcommands wired in `init()` via `brainCmd.AddCommand(...)`, resolving `brainDir := filepath.Join(filepath.Dir(resolveDBPath()), "brain")`:
- `agentique brain snapshot` (`--retain int`, default 0) → core calls `brain.Snapshot`; prints new ID, file count, and `ListSnapshots` retained list. Pure FS; no `brain.New`/Chroma.
- `agentique brain restore <id>` (`--retain int`, `-f/--force`) → require exactly one arg; if missing/unknown, print available IDs from `ListSnapshots`; confirm `[y/N]` unless `--force`; call `brain.Restore`; report the pre-restore safety snapshot ID. Keep the IO-free decision/format logic in a `runSnapshot`/`runRestore` core returning a result struct for testing.

**MODIFY config/wiring:** `config.go` `BrainConfig`: `SnapshotRetain int \`toml:"snapshot-retain"\`` (doc: 0 = built-in default 7; do not also default to 7 in config — keep the default in one place). `serve.go`: `BrainSnapshotRetain: envIntOr("AGENTIQUE_BRAIN_SNAPSHOT_RETAIN", fileCfg.Brain.SnapshotRetain)`. `server.go` `Config`: `BrainSnapshotRetain int`; pass `SnapshotRetain: cfg.BrainSnapshotRetain` into `brain.New`.

**DOCS:** add Snapshots/rollback subsection to `docs/brain-memory.md`; mark M1 in `docs/brain-evolution-plan.md`; add `snapshot-retain` to the sample `config.toml [brain]`.

#### Behavioural contract
Every `runOnce` with ≥1 scope creates `brain/.snapshots/<ts>/` (all scope `.md` + both dotfiles) before any consolidation write. `.snapshots` is a sibling of scope dirs and **invisible to recall/consolidation**: VERIFIED — `filestore.List` is non-recursive (filestore.go:335), reads only direct `*.md` of each top-level dir, so `.snapshots` (no direct `.md`) yields zero records and never enters `ListScopes`/`Recall`. Retention: newest `retain` (default 7) kept; older `RemoveAll`-pruned; `retain<=0`→7. `Restore(id)` makes the tree exactly match `id` and first writes a fresh pre-restore snapshot. Edge cases: empty brain → automation skips; direct `Snapshot` yields `Files==0`, no error. Same-second IDs collide (1s resolution; `O_TRUNC` overwrites) — acceptable; tests avoid this via `snapshotAt`. Byte-for-byte over the markdown SoT, so archived/superseded/pinned/human are copied verbatim (no exemption logic). Per-file consistency guaranteed by filestore atomic temp+rename; the snapshot is a near-point-in-time set, not a global transaction. **Restore is offline-only for M1** (rewrites files under a running server's cachestore).

#### Tests (`backend/internal/brain/snapshot_test.go`, `-race`)
- `TestSnapshotCreatesCopy` — byte-identical copies of every scope file + dotfile; `Files`/`Bytes` match seeded totals.
- `TestSnapshotExcludesSnapshotsDir` — two snapshots; second contains no nested `.snapshots` (no exponential blowup).
- `TestListInvisibleToFilestore` (load-bearing) — `filestore.New(dir).List(ctx)` identical before/after `Snapshot`; `ListScopes` unchanged.
- `TestSnapshotRetention` — use `snapshotAt` with **distinct injected `now`s** to create `retain+3` snapshots without sleeping; assert exactly `retain` newest remain; `retain<=0` keeps 7.
- `TestRestoreRoundTrip` — `snapshotAt(t0)`; mutate (add/delete/edit); `Restore`; tree matches pre-mutation; assert one **more** snapshot than before (the pre-restore safety copy). Use injected times so IDs are distinct.
- `TestRestoreUnknownIDError` — `errors.Is(err, os.ErrNotExist)`; live tree unchanged.
- `TestSnapshotEmptyBrain` — `nil` error, `Files==0`.
- `TestRunOnceSnapshotsBeforeChurn` — `Service` over temp dir with ≥1 scope; `runOnce` with `model==""`; ≥1 snapshot dir afterward matching pre-churn facts.
- `TestSnapshotCLI_Core` — drive the extracted `runSnapshot`/`runRestore` cores: snapshot-by-id round-trip, unknown-id produces the available-ID listing + `os.ErrNotExist`, and `restore` reports the pre-restore snapshot ID (assert on the returned result struct / captured output, not on cobra plumbing).

#### Acceptance checklist
- [ ] `snapshot.go` exists, package `brain`, stdlib only (no new go.mod deps); `snapshotAt` time-injectable core present.
- [ ] `runOnce` snapshots before the loop when scopes exist; failure WARN-logged, non-fatal.
- [ ] `brain snapshot` prints new ID + file count + retained list; `brain restore <id>` confirms unless `--force`, lists IDs on unknown id, and reports the pre-restore safety snapshot — all verified by `TestSnapshotCLI_Core`.
- [ ] Retention enforced (default 7; `snapshot-retain` + env override via `envIntOr`).
- [ ] `.snapshots` never in `List`/`ListScopes`/`Recall` (`TestListInvisibleToFilestore`).
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass. No `just sqlc`/`just typegen`.
- [ ] Errors `%w`; doc comments on exports; docs + sample config updated.
- [ ] M0 baselines still green.

#### Gotchas / VERIFY FIRST
- **Do NOT place snapshots inside a scope dir or make `List` recursive** — the sibling design is safe only because `List` is non-recursive (filestore.go:335). Keep `TestListInvisibleToFilestore` as the guard.
- **Skip `.snapshots` in `copyTree`'s top level** or each snapshot copies all prior ones.
- **Include both dotfiles** (`.fingerprints.json`, `.global-manifest.json`) in snapshot and restore — restoring `.md` without the manifest desyncs the next consolidation skip logic.
- **VERIFY FIRST (failure policy):** spec defaults to WARN-and-proceed; confirm with the reviewer whether snapshot failure should hard-block the pass (if so, `return` from `runOnce` on error).
- **VERIFY FIRST (Go version):** `os.CopyFS` (Go 1.23+) errors if dst exists and can't skip a subtree; the manual `WalkDir` copy is version-safe — confirm `go.mod` before considering `os.CopyFS`.
- **VERIFY FIRST (restore guard):** `backend/cmd/agentique/pidfile.go` exists; decide whether `brain restore` refuses/warns when a server pidfile is live (restore is unsafe against a running cachestore), or just document the offline requirement.

---

### M2 · Capture-tier ingest

#### Intent
Session ingest must stage RAW, non-injectable captures (`Source = capture`) instead of injectable `consolidated` facts. This turns ingest into tier-1; promotion becomes the churn's exclusive job. No `SourceLearned` — `SourceCapture` exists and recall already excludes it.

#### Exact changes
**`backend/internal/brain/brain.go`**

**(a) Re-route the ingest write — the root of the audit finding.** In `LearnFromTranscript` (718–736), replace the line-730 `s.Add(...,SourceConsolidated)` with the capture path:
```go
if _, err := s.Capture(ctx, scope, c.Text, c.Category); err == nil {
  staged++
}
```
Rename `added → staged`; return the count of captures staged. Rewrite the doc comment to: "stages raw captures from a finished session's transcript for later promotion by the churn. Captures are never injected; only consolidation promotes them (`capture → consolidated`, with `DerivedFrom` provenance)." Keep the function name. **Do not** route ingest through `s.Add` with `SourceCapture` — `Add` dedups against durable and auto-pins `CategoryIdentity`; a pinned capture is wrong and capture-vs-durable dedup is the churn's job.

**(b) Capture carries the candidate's category.** Change the signature:
```go
func (s *Service) Capture(ctx context.Context, scope memory.Scope, text string, category memory.Category) (memory.Record, error)
```
Body: default `category` to `memory.CategoryFact` when empty (mirror Add); `r := memory.New(scope, text, category, memory.SourceCapture)`. Leave `Pinned=false` always — **never** auto-pin a capture, even `CategoryIdentity`. In M2, `Capture` has **no dedup** (genuinely-new captures accumulate; capture-vs-capture never dedups). **Note for M4:** M4 adds capture-vs-*durable* reinforcement on top of this signature (dedup set stays durable-only) — so M2's "no dedup" is specifically "no capture-vs-capture dedup."

**`backend/internal/server/server.go`** — session-end closure (368–395): no functional change; update only the success log to reflect the tier, e.g. `slog.Info("brain: staged captures from ended session", "project", projectID, "captures", n)`. Keep the `bus.Broadcast(brain.EventBrainUpdated, …)` (captures are visible in the Brain list, just not injected).

**`backend/internal/session/service.go`** — **no change.** The transcript-capture + `go s.onSessionEnd(...)` seam (1104–1124, gated by `minEventsToEncode=8`) already delivers the transcript. Confirm only.

**Recall exclusion — confirm, do not modify.** `Recall` (recall.go:91–99): the `if r.Pinned { res.Pinned = append(...); continue }` branch runs **first** (92–95), then the `SourceCapture` skip (96–98) runs only for non-pinned records. **Therefore the actual guarantee that captures never inject is `Capture` leaving `Pinned=false`** — not recall ordering. (A capture that were ever pinned *would* land in `res.Pinned` and inject; the capture skip never sees it.) Keep `TestCaptureDoesNotPinIdentity` as the load-bearing guard.

#### Behavioural contract
| Aspect | Before | After |
|---|---|---|
| Session end → ingest | `SourceConsolidated`, injectable | `SourceCapture`, not injectable until churn promotes |
| `LearnFromTranscript` return | durable count | captures staged |
| Dedup at ingest | deduped (`Add`) | no capture-vs-capture dedup (M4 adds capture-vs-durable reinforce) |
| Identity candidate | auto-pinned, injected | category preserved, `Pinned=false`, not injected |
| `Capture` category | always `CategoryFact` | candidate's category (default `CategoryFact`) |

Edge cases: empty/trivial input → loop writes nothing, returns 0 (the `minEventsToEncode=8` upstream gate filters trivial sessions). Pinned/human/locked durable untouched (ingest only writes fresh-UUID capture rows). Concurrency: the hook runs in a goroutine concurrent with live `Add`/`Recall`; each capture gets a fresh UUID (no write-write collision; filestore serializes `Put`). The cachestore double-check-lock race is pre-existing; M4 adds the `s.mu` ingest locking that closes the churn-vs-ingest window.

#### Tests (`backend/internal/brain/capture_ingest_test.go`, `-race`)
Use a tiny in-test `memory.Extractor` stub (canned `Extract`, no-op `Reorganize`).
- `TestLearnFromTranscriptStagesCaptures` — 2 candidates (fact + preference); return count 2; `s.List(ctx, scope)` returns 2 records, all `Source==capture`, `Pinned==false`, categories preserved.
- `TestLearnFromTranscriptCapturesNotInjected` — after ingest with a query-matching candidate: `RecallBlock` empty block + empty ids; `PinnedPreamble == ""`; `OperatingContract == ""` even for a `CategoryPreference` capture.
- `TestCaptureDoesNotPinIdentity` — `Capture(ctx, scope, "User's name is Mathias.", memory.CategoryIdentity)` → `Pinned==false`, `Source==capture` (contrast `Add` which would pin). Guards against re-routing ingest through `Add`.
- `TestCaptureDefaultsCategory` — `Capture(ctx, scope, "x", "")` → `Category==CategoryFact`.
- `TestCaptureConcurrentWithRecall` (`-race`) — N `Capture` vs M `RecallBlock`; no panic; no capture text ever in any recall block.
- Lock baseline: confirm `memory.TestRecallExcludesCaptures` passes before and after.

#### Acceptance checklist
- [ ] `LearnFromTranscript` writes via `Capture` (`SourceCapture`), never `SourceConsolidated`/`SourceAgent` (grep clean).
- [ ] `Capture` is 4-arg `(ctx, scope, text, category)`; all call sites updated; `grep "\.Capture("` shows no 3-arg form.
- [ ] An ended session (≥8 events, learn model set) produces `Source: capture` rows on disk.
- [ ] Those captures absent from `RecallBlock`/`PinnedPreamble`/`OperatingContract` (tests).
- [ ] No capture is ever `Pinned==true`.
- [ ] Doc comments on `LearnFromTranscript` + `Capture` updated; `docs/brain-memory.md` ingest section says ingest is capture-tier, promotion is the churn's job.
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass.
- [ ] `TestRecallExcludesCaptures` green.

#### Gotchas / VERIFY FIRST
- **PIPELINE DEPENDENCY (highest risk):** after M2, ingest no longer injects directly — only the churn promotes `capture → consolidated`. A deployment with `BrainLearnModel` set but **no** scheduled consolidation (`BrainConsolidateInterval` empty / `Extractor==nil`) stages captures that never inject — a regression vs. today. M2 must land with/after a churn that runs promotion; require scheduled consolidation enabled. Call this out in the PR and `docs/brain-memory.md`.
- **VERIFY FIRST:** `grep "\.Capture("` across `backend/` before changing the signature to confirm `LearnFromTranscript` is the only caller (current read: only the definition). Update any caller in the same change.
- **No sqlc/typegen/migration** (markdown-only).
- **Do not** add capture-vs-capture dedup or promotion logic here.
- **Provenance forward-compat:** `writePromoted` already stamps `DerivedFrom=captureIDs` and deletes consumed captures, so capture IDs become provenance automatically once promotion runs — captures must carry their real category (change (b)) for promoted facts to keep it.

---

### M3 · Learn on completion (not only delete)

#### Intent
Fire the brain ingest sink when a session's CLI exits cleanly (`runtime.StateDone`), so fresh captures flow without requiring deletion. The delete path stays as the safety net; a per-session high-water marker makes the two paths idempotent.

#### Exact changes
M3 changes **when** ingest fires, not **what** it writes — the sink is the already-wired `Service.onSessionEnd` closure (server.go:369). Do not touch the closure body (M2 owns the capture write).

**Trigger (decided):** hook `runtime.StateDone` ("CLI exited cleanly. Conversation is complete."), **NOT** per-turn idle. `StateIdle` would fire every turn and need delta aggregation (spam) — out of scope.

**(a) `backend/internal/session/session.go`** — add a Session-local callback mirroring `recallFn` (field ~line 160, comment ~155) / `SetRecallFn` (~611), guarded by `s.mu`:
```go
onComplete func() // fired once per clean completion (StateDone); nil disables
func (s *Session) SetOnComplete(fn func()) { s.mu.Lock(); s.onComplete = fn; s.mu.Unlock() }
```

**(b) `backend/internal/session/runtime_bridge.go`** — in `handleRuntimeStateChange`, inside the existing `if ev.To == runtime.StateDone {` block (after `SetSessionCompleted`@92-96), snapshot under lock and fire async (mirror `go s.flushPendingMessages()`@110):
```go
s.mu.Lock(); cb := s.onComplete; s.mu.Unlock()
if cb != nil { go cb() }
```
The two early returns above (`StateMerging`@71-74; `StateFailed→Done`@81-84) intentionally skip the fire — the delete path nets those.

**(c) `backend/internal/session/manager.go`** — mirror `MemoryRecallFn`/`wireRecall`:
```go
OnSessionComplete func(projectID, sessionID string) // async, best-effort; nil disables
func (m *Manager) wireCompletion(sess *Session, projectID string) {
  if m.OnSessionComplete == nil || projectID == "" { return }
  id := sess.ID
  sess.SetOnComplete(func() { m.OnSessionComplete(projectID, id) })
}
```
Call `m.wireCompletion(sess, <projectID>)` immediately after each `m.wireRecall(...)`: Create@313 (`params.ProjectID`), Resume@466 (`p.ProjectID`), Reconnect@558 (`p.ProjectID`) — all three.

**(d) `backend/internal/session/service.go`** — shared idempotency marker + entry point:
```go
learnMu        sync.Mutex
learnHighWater map[string]int // sessionID → #events already ingested this process
```
Init in `NewService`: `learnHighWater: make(map[string]int)`.
```go
func (s *Service) claimLearn(sessionID string, count int) bool {
  if count < minEventsToEncode { return false }
  s.learnMu.Lock(); defer s.learnMu.Unlock()
  if count <= s.learnHighWater[sessionID] { return false }
  s.learnHighWater[sessionID] = count
  return true
}
func (s *Service) HandleSessionComplete(projectID, sessionID string) {
  if s.onSessionEnd == nil { return }
  ctx := context.Background()
  evs, err := s.queries.ListEventsBySession(ctx, sessionID)
  if err != nil { slog.Warn("learn-on-completion: list events failed", "session_id", sessionID, "error", err); return }
  if !s.claimLearn(sessionID, len(evs)) { return }
  s.onSessionEnd(projectID, evs)
}
```
DeleteSession (1121-1124): route through the same marker:
```go
if s.onSessionEnd != nil && s.claimLearn(sessionID, len(endEvents)) {
  projectID := dbSess.ProjectID
  go s.onSessionEnd(projectID, endEvents)
}
s.learnMu.Lock(); delete(s.learnHighWater, sessionID); s.learnMu.Unlock()
```

**(e) `backend/internal/server/server.go`** — wire the Manager hook **inside** the existing `if encodeEx != nil || outcomeJudge != nil { ... }` block, after `svc.SetOnSessionEnd(...)`:
```go
mgr.OnSessionComplete = svc.HandleSessionComplete
```
**M7 NOTE (preserve this line):** M7 rewrites this block into a job-queue version — it **must** retain `mgr.OnSessionComplete = svc.HandleSessionComplete`, or learn-on-completion is silently disabled.

No migration/sqlc/typegen/config (in-process marker only; durable idempotency is M7).

#### Behavioural contract
| Scenario | After |
|---|---|
| StateDone, ≥8 events | `onSessionEnd` runs once; session **not** deleted, events untouched |
| StateDone, <8 events | no ingest; delete remains the net |
| Complete → delete (no new events) | ingest once total (delete skips via high-water; marker pruned on delete) |
| Never completes → delete, ≥8 | unchanged (safety net) |
| Complete@N → resume → complete@M>N | ingests again (high-water N→M); brain text-dedup keeps facts unique |
| StateDone twice, no new events | second is a no-op |
| Merging, or Failed→Done | fire skipped (early returns); delete nets |

Concurrency: `onComplete` snapshotted under `s.mu` before `go cb()`; `claimLearn` fully mutex-guarded so concurrent completion+delete fire `onSessionEnd` at most once. Completion ingest now runs concurrently with live `Add()` — funneled through the single `brain.Service` (M4 adds the `s.mu` ingest locking).

#### Tests (`backend/internal/session/completion_learn_test.go`, `-race`)
Reuse the suite harness (`s.svc`, `s.Queries`, `s.mgr`, `mock_cli_test.go`).
- `TestClaimLearn_HighWaterMark` — table: `(7)→false`, `(8)→true`, `(8)→false`, `(12)→true`, `(5)→false`.
- `TestClaimLearn_AtomicUnderRace` (`-race`) — N goroutines `claimLearn(id,8)`; **exactly one** returns true.
- `TestHandleSessionComplete_FiresOnceAndKeepsSession` — ≥8 events, recording `onSessionEnd`; after `HandleSessionComplete`, recorder once, `GetSession` succeeds, events present.
- `TestHandleSessionComplete_GatedByMinEvents` — <8 → never called.
- `TestCompletionThenDelete_NoDoubleCapture` (`-race`) — completion then delete → recorder once total.
- `TestDeleteWithoutCompletion_SafetyNet` — baseline: delete with ≥8 events → once. **Lock this first.**
- `TestReingestAfterGrowth` — complete@8 (fires), grow to 12, complete again → fires with 12.
- `TestManagerWiresCompletionOnStateDone` — set `mgr.OnSessionComplete`; drive mock CLI to `StateDone` → fires once `(projectID, sessionID)`; drive Running→Idle first → does **not** fire on Idle. VERIFY FIRST that `mock_cli_test.go` can emit the event mapping to `StateDone`; else test `handleRuntimeStateChange` directly with a synthesized `runtime.StateChangeEvent{To: runtime.StateDone}`.

#### Acceptance checklist
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass.
- [ ] `Session.onComplete`+`SetOnComplete`; `Manager.OnSessionComplete`+`wireCompletion` at all three sites; `Service.claimLearn`+`HandleSessionComplete`; `learnHighWater` initialized — exact signatures above.
- [ ] `onComplete` fired only on `StateDone`, snapshotted under `s.mu`, dispatched via `go`.
- [ ] DeleteSession ingest gated by `claimLearn`; marker pruned on delete.
- [ ] Completion does not delete the session; events remain.
- [ ] No double-capture (test-proven, `-race`); delete-only net unchanged.
- [ ] `minEventsToEncode` honored both paths.
- [ ] Doc comments; `%w` on new wrapped errors; `docs/brain-evolution-plan.md` M3 marked (trigger=`StateDone`, idempotency=event high-water); one-line note in `docs/brain-learning-dynamics.md`.

#### Gotchas / VERIFY FIRST
- **Lock the baseline first:** author `TestDeleteWithoutCompletion_SafetyNet` against current behaviour before editing DeleteSession.
- **`StateDone` is terminal, not per-turn** (verified). Do not hook `StateIdle`. Idle-then-torn-down sessions never hit Done; the delete net covers them (expected).
- **Other `SetSessionCompleted` sites** (`session.go:934 MarkDone`, `service_approval.go:96`, `git_service.go:289/628`) set completed without routing through the fire. M3 hooks only the runtime Done path. VERIFY FIRST whether `MarkDone` is a genuine conversation-complete to learn from; firing `s.onComplete` there too is **safe** (`claimLearn` dedups) but confirm it isn't git/merge bookkeeping.
- **In-process marker only:** a restart loses `learnHighWater`; a post-restart delete of a previously-completed session re-runs extraction once (brain text-dedup prevents dup facts). Durable idempotency is M7.
- **Map growth:** `learnHighWater` grows per session per process; pruned on delete; optionally sweep in the existing `sweepIdempotencyCache` loop.

---

### M4 · Reinforce-on-re-observe

#### Intent
Re-observing text that duplicates an existing *durable* memory must strengthen it (count the corroboration, refresh recency, nudge confidence toward the 0.95 ceiling) and return the strengthened record — instead of the silent dup-return that discards the signal. Genuinely-new text continues to stage as a capture. **Builds on M2's 4-arg `Capture` signature.** Also introduces the `s.mu` ingest locking that fixes the churn-vs-ingest race (required before M5 activates the churn).

#### Exact changes
**(a) `backend/internal/memory/record.go` — new persisted counter.** After `Helped int`:
```go
// Corroborations counts independent RE-OBSERVATIONS: an ingest/Add saw text duplicating
// this durable fact. Distinct from Helped (agent acknowledged a *recalled* fact, MemoryUsed)
// and Uses (bare injection). Raises ConfidenceScore toward CorroborationCeiling via Reinforce.
Corroborations int
```
Do **not** change `New()` (zero is correct). Do **not** add it to `StorageStrength` in M4 (keep weights stable; confidence rise already propagates via `storageConfWeight`).

**(b) `backend/internal/memory/reconsolidate.go` — the reinforce helper** (lives here, beside `MarkHelped`/`MarkContradicted`, reusing `CorroborationCeiling`/`AutoCorroborationGapClose`/`isProtected`/`NormalizeConfidence`):
```go
// Reinforce: the same fact was independently observed again. Increments Corroborations,
// stamps LastUsedAt (drives retrieval recency + decay-by-disuse), and for a non-protected
// fact raises ConfidenceScore toward CorroborationCeiling by AutoCorroborationGapClose of the
// remaining gap (a dup match is a machine inference → gentle weight, not firsthand 0.5).
// Protected facts keep their score but still accrue the count. Leaves UpdatedAt untouched
// (re-observation is retrieval, not a content edit — like MarkHelped). now stamps LastUsedAt.
func Reinforce(r Record, now time.Time) Record {
  r.Corroborations++
  r.LastUsedAt = now
  if !isProtected(r) && r.ConfidenceScore < CorroborationCeiling {
    r.ConfidenceScore += AutoCorroborationGapClose * (CorroborationCeiling - r.ConfidenceScore)
  }
  return NormalizeConfidence(r)
}
```

**(c) `backend/internal/memory/filestore/filestore.go` — persist.** Add to `frontmatter` (after `Helped`): `Corroborations int \`yaml:"corroborations,omitempty"\``. Map in `toFrontmatter` (`Corroborations: r.Corroborations`) and `toRecord` (`Corroborations: m.Corroborations`). Old files (no key) decode to 0.

**(d) `backend/internal/brain/brain.go` — the `Add` dup branch (PRIMARY, the DoD)** + **`s.mu` locking.** Wrap the `Add` critical section (List → dedup → Put / New → Put) in `s.mu` (see the race fix below), and replace the silent return at 459-461:
```go
if dup, ok := memory.FindDuplicate(text, existing, memory.DefaultDuplicateThreshold); ok {
  reinforced := memory.Reinforce(dup, time.Now().UTC())
  if err := s.store.Put(ctx, reinforced); err != nil {
    return memory.Record{}, fmt.Errorf("brain: reinforce duplicate %s: %w", dup.ID, err)
  }
  return reinforced, nil
}
```
`existing` is already durable-only (captures filtered at 454-456). `time`/`fmt` already imported — VERIFY they remain after edit.

**(e) `backend/internal/brain/brain.go` — `Capture()` durable-dup reinforce (SECONDARY), on M2's 4-arg signature.** This is the reconciliation of M2's "no capture-vs-capture dedup" with reinforce-on-re-observe: a re-observation arriving on the ingest tier reinforces the *durable* fact instead of stacking a redundant capture; only genuinely-new text stays a capture. Dedup set stays **durable-only**, so capture-vs-capture still never dedups (M2's invariant intact). Build on the post-M2 body:
```go
func (s *Service) Capture(ctx context.Context, scope memory.Scope, text string, category memory.Category) (memory.Record, error) {
  text = strings.TrimSpace(text)
  if text == "" { return memory.Record{}, fmt.Errorf("brain: empty capture text") }
  if category == "" { category = memory.CategoryFact }
  s.mu.Lock(); defer s.mu.Unlock() // race fix: atomic List→dedup→Put vs concurrent ingest/churn
  all, err := s.store.List(ctx, recallScopes(scope)...)
  if err != nil { return memory.Record{}, err }
  existing := make([]memory.Record, 0, len(all))
  for _, r := range all { if r.Source != memory.SourceCapture { existing = append(existing, r) } }
  if dup, ok := memory.FindDuplicate(text, existing, memory.DefaultDuplicateThreshold); ok {
    reinforced := memory.Reinforce(dup, time.Now().UTC())
    if err := s.store.Put(ctx, reinforced); err != nil {
      return memory.Record{}, fmt.Errorf("brain: reinforce duplicate %s: %w", dup.ID, err)
    }
    return reinforced, nil
  }
  r := memory.New(scope, text, category, memory.SourceCapture) // genuinely-new: stage as capture
  if err := s.store.Put(ctx, r); err != nil { return memory.Record{}, fmt.Errorf("brain: capture: %w", err) }
  return r, nil
}
```
**Do NOT** flip `Add()`'s no-dup branch to write a capture (Add is the intentional durable path: MCP `MemoryAdd`@mcp.go:38, transcript-learn@brain.go:730).

**Race fix (major — churn vs ingest).** Verified: `Service.Consolidate` **takes `s.mu`** but `Add`/`Capture` take **none** — one-sided locking gives zero mutual exclusion, so concurrent ingest (M3 fires lock-free `Capture` on every clean completion) and the churn (M5 archives/rewrites/deletes) can lose `Reinforce` increments (RMW) and trigger the cachestore stale-cache install. Fix: take `s.mu` across the full `List → dedup/Reinforce → Put` critical section in **both** `Add` and `Capture` (hold across List→Put only; **no nested caller lock**). Because `Consolidate` already holds `s.mu`, this gives ingest-vs-churn mutual exclusion. **M4 must precede M5.** VERIFY FIRST that no caller of `Add`/`Capture` already holds `s.mu` (avoid self-deadlock) before adding the locks.

**(f) Docs.** Add `Reinforce` (third reconsolidation verb: re-observation) to `docs/brain-outcome-signal.md`; tick M4 in `docs/brain-evolution-plan.md`; update M2's `docs/brain-memory.md` note to state M4 adds capture-vs-durable reinforcement.

#### Behavioural contract
| Path | After |
|---|---|
| `Add` dup of durable | `Corroborations++`, `LastUsedAt=now`, score `+= AutoCorroborationGapClose×(0.95−score)` (non-protected), persisted, returns strengthened record (same ID) |
| `Add` new text | durable write (unchanged) |
| `Capture` dup of durable | reinforces the durable fact, writes **no** capture |
| `Capture` new text | capture written (unchanged) |

Edge cases: empty input → still errors before any List. Protected dup (pinned/locked/human) → `Corroborations` + `LastUsedAt` update, `ConfidenceScore` never touched (`isProtected`); `UpdatedAt` unchanged in all cases. Ceiling asymptotic; a fact already ≥ ceiling stays. Capture dups never reinforce a capture (dedup set durable-only). Concurrency: now atomic via `s.mu`. The AGING "don't rewrite on every nudge" rule applies to *passive disuse* (M5), not here — re-observation is a discrete event like `MarkHelped`, so persisting per occurrence is correct.

#### Tests (`-race`)
`reconsolidate_test.go`:
- `TestReinforceRaisesConfidenceTowardCeiling` — `New(scope,"x",fact,SourceConsolidated)` (0.8); `Reinforce` 3×; score strictly increases each call, `≤ CorroborationCeiling+1e-9`, `Corroborations==3`, `LastUsedAt==now`.
- `TestReinforceSparesProtectedScore` — human (1.0): score unchanged, `Corroborations==1`, `LastUsedAt` stamped.
- `TestReinforceDoesNotTouchUpdatedAt`.

`filestore_test.go`:
- `TestFilestoreCorroborationsRoundTrip` — `Corroborations=3` round-trips; `0` round-trips to 0 (optionally assert no `corroborations:` key — omitempty).

`brain_test.go` (use `newSvc(t)`):
- `TestAddReinforcesDurableDuplicate` — `Add(...SourceAgent)` → `r1` (0 corro, 0.8); re-`Add` near-dup → `r2`: `r2.ID==r1.ID`, `Corroborations==1`, `ConfidenceScore > r1`, list count unchanged; `Get(r2.ID)` confirms persistence.
- `TestAddDoesNotReinforceCaptureOnlyMatch` — `Capture` then `Add` of same text → new durable record (id ≠ capture, `Source==SourceAgent`).
- `TestCaptureReinforcesDurableDuplicate` — seed durable via `Add`; `Capture` dup → returns durable with `Corroborations==1`, **no** capture added; `Capture` new text → one new `SourceCapture` record.
- `TestAddCaptureReinforceRace` (`-race`) — concurrent `Add`/`Capture` of the same durable text from many goroutines; assert no lost increments beyond serialization (`s.mu` makes it deterministic) and no race.
- Extend the existing `TestAddDedupAndIdentityPin` to also assert `r2.Corroborations==1` (its `r1.ID==r2.ID` invariant survives).

#### Acceptance checklist
- [ ] `Record.Corroborations int` added; `New()` and `StorageStrength` weights unchanged.
- [ ] `memory.Reinforce(r, now)` per spec; `isProtected` skips score; `UpdatedAt` untouched; returns `NormalizeConfidence(r)`.
- [ ] Frontmatter persists `corroborations` (omitempty); legacy files → 0.
- [ ] `Add` dup branch reinforces + `Put`s + returns strengthened record (same ID); `%w` error.
- [ ] `Capture` reinforces durable dups (durable-only set), stages only genuinely-new text; M2's 4-arg signature retained; no 3-arg call introduced.
- [ ] **`s.mu` wraps the List→dedup/Reinforce→Put critical section in both `Add` and `Capture`**; `Consolidate` already holds `s.mu`; no nested-lock deadlock (verified).
- [ ] New tests (memory + filestore + brain) green, including the `-race` concurrency test.
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass.
- [ ] Doc comments; `docs/brain-outcome-signal.md` + `docs/brain-evolution-plan.md` + M2 doc note updated.

#### Gotchas / VERIFY FIRST
- **No sqlc/typegen** (markdown field).
- **Reinforce re-embeds unchanged text** via `chroma.index()` on `Put` — wasteful but harmless (best-effort). Skip-re-embed-when-Text-unchanged is out of scope; note it.
- **Locking is now required** (overrides the original "don't add locking" recommendation): the race fix is part of M4's DoD. Hold `s.mu` across List→Put only.
- **Gap-close weight** reuses `AutoCorroborationGapClose` (machine inference), not the firsthand `corroborationGapClose`. If product wants it tunable, add a `[brain]` key later (env override) — not required for M4.
- **Sibling dedup paths** (`ImportRecords` bulk dedup-and-skip) intentionally get no reinforcement; confirm M4 edits don't route through it.
- **Forward-compat with archive (M5):** when lifecycle/archive lands, the dedup set should exclude archived rows OR a re-observation of an archived fact should *restore* it. M5 handles archived exclusion from the dedup set; M4 takes no action (no archive yet) — flag the interaction for M5.

---

### M5 · Computed disuse-confidence aging (archive, not delete)

> **HARD DEPENDENCY: implement M6 first.** M6 is the sole owner of the `Evidence`/`Volatility`/`Lifecycle` enums, their `Record` fields, and the filestore frontmatter keys. M5 **only** adds aging behaviour + the `ArchiveFloor` knob and **reads** M6's labels — it must **not** declare any of those types/fields/frontmatter (doing so is a duplicate-declaration compile error). M5 also requires M4 (the `s.mu` ingest locking) and M1 (pre-churn snapshot).

#### Intent
Make confidence a living scalar: at recall time compute effective confidence = stored score eroded by disuse (volatility half-life, clamped to an evidence floor), so unused inferred facts fade out of recall **without any file rewrite**; persist exactly once at the archive transition (`Lifecycle=LifecycleArchived`: excluded from recall, kept on disk, restorable). Human/pinned/locked/evergreen never erode and never archive. Replace the empty `DecayPolicy{}` in scheduled consolidation with a real archive-transition policy that swaps the current `Delete` for an archive `Put`.

#### Exact changes
**A. `backend/internal/memory/aging.go` — NEW FILE (pure functions; stdlib only).** Reads M6's `Record.Volatility`/`Record.Evidence`/`Record.Lifecycle` directly (M6 is a hard predecessor — **no Category/Source fallbacks**):
```go
package memory
import ("math"; "time")
const (
  halfLifeSlowDays      = 90.0
  halfLifeEphemeralDays = 14.0
  DefaultArchiveConfidenceFloor = 0.35
  floorTrusted  = 0.50 // user_stated/code_verified/corroborated (> archive line → never archives)
  floorInferred = 0.30 // ordinary inference (< archive line → can fade out)
  floorObserved = 0.15 // observed_once
)

// EffectiveConfidence: stored ConfidenceScore eroded by time-since-last-use via the
// volatility half-life, clamped up to the evidence floor. Protected/evergreen return the
// stored score unchanged. Pure read — NEVER writes. now is injected (shared clock + testable).
func EffectiveConfidence(r Record, now time.Time) float64 {
  base := NormalizeConfidence(r).ConfidenceScore
  if base <= 0 || base > 1 { base = DefaultInferredScore }
  if isProtected(r) || isEvergreen(r) { return base }
  days := now.Sub(lastSeen(r)).Hours() / 24
  if days < 0 { days = 0 }
  eff := base * math.Pow(0.5, days/volatilityHalfLifeDays(r))
  if fl := evidenceFloor(r); eff < fl { eff = fl }
  return clamp01(eff)
}

// shouldArchive: the archive-transition test as a DecayPolicy method. MaxAge is reused as a
// HARD minimum disuse age (and the on/off switch when 0). Idempotent (archived → false);
// protected/evergreen never archive.
func (d DecayPolicy) shouldArchive(r Record, now time.Time) bool {
  if d.MaxAge <= 0 { return false }
  if isProtected(r) || isEvergreen(r) || isArchived(r) { return false }
  if now.Sub(lastSeen(r)) < d.MaxAge { return false }
  floor := d.ArchiveFloor
  if floor <= 0 { floor = DefaultArchiveConfidenceFloor }
  return EffectiveConfidence(r, now) <= floor
}

func isEvergreen(r Record) bool { return r.Volatility == VolatilityEvergreen } // M6 field
func volatilityHalfLifeDays(r Record) float64 {
  switch r.Volatility {
  case VolatilityEphemeral: return halfLifeEphemeralDays
  case VolatilityEvergreen: return math.Inf(1)
  default:                  return halfLifeSlowDays // slow / unset
  }
}
func evidenceFloor(r Record) float64 {
  switch r.Evidence {
  case EvidenceUserStated, EvidenceCodeVerified, EvidenceCorroborated: return floorTrusted
  case EvidenceObservedOnce: return floorObserved
  default:                   return floorInferred // inferred / unset
  }
}
```
Reuse existing `clamp01`/`lastSeen`/`isProtected`; `isArchived`/`IsArchived` are provided by **M6** (labels.go).

**B. `backend/internal/memory/consolidate.go` — `DecayPolicy.ArchiveFloor` + decay block archives.** Add (additive):
```go
// ArchiveFloor: effective-confidence line below which a faded fact is archived. <=0 → DefaultArchiveConfidenceFloor.
ArchiveFloor float64
```
Rewrite the decay block in `ApplyPlan` (379-394) — same loop shape, **archive not delete**, predicate `shouldArchive`:
```go
if opts.Decay.MaxAge > 0 {
  kept := reorgInput[:0]
  for _, r := range reorgInput {
    if opts.Decay.shouldArchive(r, now) {
      if !opts.DryRun {
        ar := r; ar.Lifecycle = LifecycleArchived; ar.UpdatedAt = now
        if err := store.Put(ctx, ar); err != nil { return rep, fmt.Errorf("archive %s: %w", r.ID, err) }
      }
      rep.Decayed = append(rep.Decayed, r) // "Decayed" now means archived; keep the field name
      continue
    }
    kept = append(kept, r)
  }
  reorgInput = kept
}
```
**Exclude archived from the durable working set** in two places:
- `ApplyPlan` (332-337): change `if r.Source != SourceCapture` → `if r.Source != SourceCapture && !isArchived(r)`.
- `PlanConsolidation` (261): the filter is the **`else` branch** of `if r.Source == SourceCapture { captures... } else { durable... }` (consolidate.go:266-272) — add `&& !isArchived(r)` to the `else` path (i.e. `else if !isArchived(r) { durable = append(...) }`).

Keep `shouldDecay` (still referenced by `confidence_test.go`/`salience_test.go`/`strength_test.go`); `ApplyPlan` simply stops calling it. Add a doc note that the live archive path is `shouldArchive`.

**C. `backend/internal/memory/recall.go` — exclude archived; fade by effective confidence (computed, never persisted).** Add `ArchiveFloor float64` to `Query` (store.go, mirrors `VectorVetoScore`). In `Recall`'s candidate loop add, as the **first** check (before the pinned branch — so an archived fact never reaches `Pinned`):
```go
if isArchived(r) { continue } // cold tier never injected
```
In `rank`, thread the floor through the signature and add the **read-time fade gate** (reversible, no write), active only when opted in:
```go
if q.ArchiveFloor > 0 && !isProtected(c) && !isEvergreen(c) && EffectiveConfidence(c, now) <= q.ArchiveFloor {
  continue // faded out of recall; still active on disk until the churn archives it
}
```
Gating on `q.ArchiveFloor > 0` is the **recall-cliff defense** (see Gotchas): when archiving is disabled (floor 0), recall does **not** hard-drop faded facts. Keep `rec := RetrievalStrength(c, now)` as the recency tiebreaker.

**D. Archived must be excluded from ALL non-recall consumers (ship in the same commit).** Add an `isArchived` skip at each verified `store.List` consumer where cold facts must not appear:
- `promote.go:101` and `promote.go:208` — cross-scope promotion must not resurrect an archived fact as a new consolidated fact.
- `strength.go DueForReview` — do not surface archived facts for human review.
- `areas.go:91, 116`; `community.go:26`; `link.go:27`; `global_graph.go:162` — graph/areas exclude the cold tier.
- `brain.go OperatingContract` (534-544) — add the archived check via exported `memory.IsArchived(r)` (brain cannot call unexported `isArchived`).
- **HTTP memories list** (`brain/http.go`): do **not** silently hide archived — keep returning them with the `lifecycle` field so a future "show archived" toggle works (a separate view, not implicit hiding). Surface `lifecycle` in `memoryDTO` only if the UI needs it now; if you do, run typegen (see checklist).

**E. `backend/internal/memory/filestore/filestore.go`** — **no new frontmatter declaration in M5** (M6 already added the `lifecycle` key and the `Record.Lifecycle` round-trip). M5 relies on M6's filestore wiring.

**F. `backend/internal/brain/automation.go` — real archive-transition policy.** Add `archiveAfter time.Duration` and `archiveFloor float64` to `Automation`; extend `NewAutomation` to accept them (update the single caller, server.go:412). In `runOnce` replace the empty policy (107):
```go
decay := memory.DecayPolicy{MaxAge: a.archiveAfter, ArchiveFloor: a.archiveFloor}
rep, err := a.svc.Consolidate(ctx, scope, ex, decay, false, ConsolidateOpts{})
```
`archiveAfter <= 0` → inert (archive disabled), preserving today's behaviour until configured. **M1's pre-churn snapshot already runs at the top of `runOnce`** — M5 relies on it; do not add a parallel snapshot (drop any "TODO: snapshot" note).

**G. `backend/internal/brain/brain.go` — recall passes the floor; restore resets the clock.** Add `archiveFloor float64` to `Service`, set in `New` from `cfg`. In `RecallBlock` set `q.ArchiveFloor = s.archiveFloor` on the `memory.Query`. In `OperatingContract` add the `memory.IsArchived` skip. In the `Update`/un-archive/edit path: when an edit flips `Lifecycle` from archived back to active, also stamp `LastUsedAt = time.Now().UTC()` (restore must not immediately re-archive). Add `Config.ArchiveFloor float64` (and the `archiveAfter` plumbing below).

**H. Config + wiring.** `config.go` `BrainConfig`: `ArchiveAfter string \`toml:"archive-after"\`` (e.g. `"720h"`; `""`=off) and `ArchiveFloor float64 \`toml:"archive-confidence-floor"\`` (0 = `DefaultArchiveConfidenceFloor`). `serve.go`: `BrainArchiveAfter: firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_ARCHIVE_AFTER"), fileCfg.Brain.ArchiveAfter)`, `BrainArchiveFloor: envFloatOr("AGENTIQUE_BRAIN_ARCHIVE_FLOOR", fileCfg.Brain.ArchiveFloor)`. `server.go` `Config`: `BrainArchiveAfter string`, `BrainArchiveFloor float64`; parse `BrainArchiveAfter` to `time.Duration` (warn + 0 on parse error), pass `ArchiveFloor` into `brain.Config` and `archiveAfter`/`archiveFloor` into `NewAutomation`.

**I. Regenerate M0 goldens (audit trail).** M5 flips decay from delete to archive, so the M0 consolidate goldens change. In the same commit run `go test ./internal/memory/... -run TestBaseline -update` and commit the diff — exactly the delete→archive audit trail M0 was built to capture.

**DOCS:** `docs/brain-learning-dynamics.md` (D1 disuse/aging), `docs/brain-memory.md` (archived cold tier, archive-not-delete, recall-cliff gate); sample `config.toml`.

#### Behavioural contract
| Case | After |
|---|---|
| Inferred fact unused N days | eff. conf decays (90d slow / 14d ephemeral half-life); once `≤ floor` **and archive enabled** it is excluded from recall (computed, file untouched); next churn sets `Lifecycle=archived` |
| Then `MemoryUsed`/re-observed | `MarkHelped`/`Reinforce` refresh `LastUsedAt=now` + raise score → eff. conf jumps back → reappears; not archived |
| Human/pinned/locked/evergreen | exempt: eff. conf == stored; `shouldArchive` false |
| Archived fact | excluded from recall candidates, durable set, reorg, dedup-target, promotion, DueForReview, areas/community/link/graph, operating contract; `shouldArchive` false (idempotent); restorable via lifecycle→active edit (which stamps `LastUsedAt`) |
| Faded fact during churn, `DryRun` | reported in `rep.Decayed`, nothing written |
| Forgetting | `store.Put` with `Lifecycle=archived` — nothing ever deleted |
| Archiving disabled (default) | no recall fade-out, no archive; behaviour == today |

Edge cases: empty store → no-op; clock skew (`now < lastSeen`) → 0 days; captures already non-injectable/excluded from durable, never age. Concurrency: `EffectiveConfidence`/recall are pure reads (race-safe); archive writes go through `Consolidate` (holds `s.mu`), and **M4 made `Add`/`Capture` take `s.mu` too**, so churn-vs-ingest is mutually exclusive — do **not** claim the Consolidate-only lock alone protects against ingest (it does not; M4's ingest locking is the other half).

#### Tests (all `-race`)
`aging_test.go`:
- `TestEffectiveConfidence_DisuseErosion` — slow inferred base 0.8: ~0.8@0d, ~0.4@90d, monotone down; @365d clamped to `floorInferred` (0.30).
- `TestEffectiveConfidence_EphemeralFadesFasterThanSlow`.
- `TestEffectiveConfidence_EvergreenNeverErodes`; `TestEffectiveConfidence_HumanProtectedNeverErodes`.
- `TestEffectiveConfidence_HelpedRevives` — fade to near floor, `MarkHelped`, eff_after > eff_before and > `DefaultArchiveConfidenceFloor`.
- `TestShouldArchive_FadedInferred`; `_RecentNotArchived`; `_EvergreenAndProtectedNever`; `_AlreadyArchivedIdempotent`; `_DisabledWhenMaxAgeZero`.

`recall_test.go`:
- `TestRecall_ExcludesArchived` — archived fact absent from `Recalled` and `Pinned`.
- `TestRecall_FadedFactDropsWhenArchiveEnabled` — with `Query.ArchiveFloor>0`, active faded fact absent; `store.Get(id).Lifecycle == ""` afterward (computed, not persisted).
- `TestRecall_NoFadeWhenArchiveDisabled` — `Query.ArchiveFloor==0`, same faded fact still recalled (cliff defense).
- `TestRecall_FreshFactOutranksColdFact`.

`consolidate_test.go` (update the baseline at :452 from delete→archive — see the precise assertion-change note in Gotchas):
- `TestApplyPlan_ArchivesNotDeletes` — `store.Get(id)` still returns it with `Lifecycle==archived`; in `rep.Decayed`; no `Delete`.
- `TestApplyPlan_DryRunArchiveWritesNothing`.
- `TestApplyPlan_ProtectedAndEvergreenSurviveArchive`.
- `TestApplyPlan_ArchivedExcludedFromDurable` — a capture duplicating an *archived* fact promotes a fresh consolidated fact; archived not reorganized.

Per-consumer exclusion tests (D):
- `TestPromote_SkipsArchived`; `TestDueForReview_SkipsArchived`; `TestAreas_SkipArchived`; `TestCommunity_SkipArchived`; `TestLink_SkipArchived`; `TestGlobalGraph_SkipArchived`; `TestOperatingContract_SkipsArchived`.

Restore:
- `TestRestoreActiveRefreshesLastUsedAt` — archive a fact, edit `Lifecycle` back to active via the brain `Update` path, assert `LastUsedAt ≈ now` and a subsequent `shouldArchive` returns false.

Brain behavioural:
- `TestService_Consolidate_ArchivesFaded` — seed a faded inferred fact, `Consolidate` with `DecayPolicy{MaxAge>0, ArchiveFloor:0.35}` → archived (not deleted), `EventBrainUpdated` broadcast.

`filestore` (M6 already covers `lifecycle` round-trip; M5 adds nothing here).

#### Acceptance checklist
- [ ] `aging.go` with doc-commented `EffectiveConfidence`, `DecayPolicy.shouldArchive`, `isEvergreen`, `volatilityHalfLifeDays`, `evidenceFloor`; reuses `clamp01`/`lastSeen`/`isProtected`; uses M6's `isArchived`/labels (no Category/Source fallbacks).
- [ ] **No `Lifecycle`/`Evidence`/`Volatility` type/const/field/frontmatter declared in M5** (owned by M6); package compiles with M6+M5 both applied.
- [ ] `Recall` excludes archived AND drops facts with eff. conf ≤ floor **only when `Query.ArchiveFloor>0`**, without writing; protected/evergreen never dropped.
- [ ] `ApplyPlan` archives (`Put` lifecycle=archived) instead of deleting; archived excluded from durable/reorg/dedup; `DryRun` writes nothing; `%w` errors.
- [ ] Archived excluded from `promote.go` (both sites), `DueForReview`, `areas`/`community`/`link`/`global_graph`, `OperatingContract` — each with a named test.
- [ ] HTTP memories list still returns archived (not silently hidden).
- [ ] `automation.go` passes a non-empty `DecayPolicy` (MaxAge=`archiveAfter`, ArchiveFloor=config); inert when `archiveAfter<=0`; relies on M1's pre-churn snapshot.
- [ ] `[brain] archive-after` + `archive-confidence-floor` with env overrides; threaded config→serve→server.Config→{Automation, brain.Service}.
- [ ] Restore via lifecycle→active stamps `LastUsedAt` (`TestRestoreActiveRefreshesLastUsedAt`).
- [ ] **M0 goldens regenerated** with `-update` and the delete→archive diff committed.
- [ ] No deletion anywhere in the aging path.
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass. `just sqlc` not required; `just typegen` not required unless `lifecycle` added to `memoryDTO` (then confirm registry + run it).
- [ ] Doc comments; `docs/brain-learning-dynamics.md` + `docs/brain-memory.md` updated.
- [ ] Baseline locked: see the precise per-assertion change list in Gotchas.

#### Gotchas / VERIFY FIRST
- **M6 is a hard predecessor** (resolves the duplicate-declaration blocker). M5 reads `r.Volatility`/`r.Evidence`/`r.Lifecycle` and uses M6's `isArchived`/`IsArchived`; it declares none of them. Every M5 reference to labels is **M6**, not "M4" (M4 is reinforce-on-re-observe).
- **`shouldDecay` stays** (still referenced by `confidence_test.go`/`salience_test.go`/`strength_test.go`); only `ApplyPlan` stops calling it.
- **Baseline-lock precision:** only tests asserting the record is **gone after decay** must change to assert `store.Get(id).Lifecycle == archived` (and no `ErrNotFound`). Tests asserting only on `rep.Decayed` (consolidate_test.go:452, salience_test.go:79-101, strength_test.go:98-104) are **unaffected** (M5 still populates `Decayed`). Direct `shouldDecay` tests (salience_test.go:50-57/115-121) are unchanged.
- **Recall cliff (deploy safety):** the read-time fade applies the disuse half-life against the whole ~1,482-fact corpus on first deploy using historical timestamps. Two defenses, both in: (1) the fade gate is keyed on `q.ArchiveFloor > 0`, and `archive-after` defaults to `""` (off) — so nothing fades or archives until the operator opts in after curating; (2) **M6's backfill stamps `LastUsedAt=now` where it is zero**, starting the disuse clock at the migration boundary, so when archiving is later enabled, facts are measured from the backfill, not ancient `UpdatedAt`. Document both; do not let the historical clock retroactively evict live facts.
- **Half-life constants** (90d/14d/0.35/floors) are documented in-code constants (precedent: recall.go's non-config calibration); only `archive-after` and `archive-confidence-floor` are config. `archive-after` is the **hard minimum disuse** before any archive.
- **VERIFY FIRST:** re-confirm the exact line numbers of the `store.List` consumers in D before editing (promote.go:101/208, areas.go:91/116, community.go:26, link.go:27, global_graph.go:162, `DueForReview`) — add the `isArchived` skip per call.

---

### M6 · Label fields + one-time backfill

> **This task is the SOLE owner of `Evidence`/`Volatility`/`Lifecycle` (enums, `Record` fields, filestore frontmatter, `isArchived`/`IsArchived`) and the typed `Relations`. It MUST be implemented and merged before M5** (which reads these labels). M5 adds only aging behaviour on top.

#### Intent
Add the controlled-vocabulary control plane (`Evidence`, `Volatility`, `Lifecycle`, typed `Relations`, `Keywords`, `LastCurated`, `CuratorNote`) to `memory.Record`; make every record carry coherent defaults via load-time normalization (mirroring `NormalizeConfidence`); persist those defaults to the ~1,482 existing markdown files and into Chroma metadata with a snapshot-first, idempotent one-time CLI pass. **No churn logic branches on labels yet** — they are seeded and round-tripped.

#### Exact changes
**(a) New `backend/internal/memory/labels.go`** — types + pure default derivation (stdlib only):
```go
package memory

type Evidence string
const ( EvidenceUserStated="user_stated"; EvidenceCodeVerified="code_verified"; EvidenceCorroborated="corroborated"; EvidenceInferred="inferred"; EvidenceObservedOnce="observed_once" )

type Volatility string
const ( VolatilityEvergreen="evergreen"; VolatilitySlow="slow"; VolatilityEphemeral="ephemeral" )

type Lifecycle string
const ( LifecycleActive="active"; LifecycleSuperseded="superseded"; LifecycleArchived="archived" )

type RelationType string
const ( RelationSupersedes="supersedes"; RelationContradicts="contradicts"; RelationDuplicates="duplicates"; RelationGeneralizes="generalizes"; RelationCorroborates="corroborates" )

type TypedRelation struct { Type RelationType `json:"type"`; Target string `json:"target"` } // replaces untyped Related (Related retained)

func EvidenceForSource(s Source) Evidence {
  switch s {
  case SourceHuman:   return EvidenceUserStated
  case SourceCapture: return EvidenceObservedOnce
  default:            return EvidenceInferred
  }
}
func VolatilityForCategory(c Category) Volatility {
  switch c {
  case CategoryIdentity: return VolatilityEvergreen
  case CategoryTask:     return VolatilityEphemeral
  default:               return VolatilitySlow
  }
}

// withDefaultLabels FILLS EMPTY label fields only — never overwrites an explicit value
// (idempotent + human-curation safe), exactly mirroring withDerivedConfidence. Unknown
// non-empty vocab values are left intact (forward-compat).
func (r Record) withDefaultLabels() Record {
  if r.Evidence == "" {
    r.Evidence = EvidenceForSource(r.Source)
    if r.Source != SourceHuman && isStronglyCorroborated(r) { r.Evidence = EvidenceCorroborated }
  }
  if r.Volatility == "" { r.Volatility = VolatilityForCategory(r.Category) }
  if r.Lifecycle == ""  { r.Lifecycle = LifecycleActive }
  return r
}
func NormalizeLabels(r Record) Record { return r.withDefaultLabels() }

func isArchived(r Record) bool { return r.Lifecycle == LifecycleArchived }
func IsArchived(r Record) bool { return isArchived(r) } // exported for the brain layer (M5)
```
`isStronglyCorroborated` already lives in `salience.go` (`Helped≥2 && !isContradicted`).

**(b) `backend/internal/memory/record.go`** — add fields (after `Related []string`, before `Confidence`); keep `Related`:
```go
Evidence    Evidence
Volatility  Volatility
Lifecycle   Lifecycle
Relations   []TypedRelation // typed link graph; empty until the churn populates it
Keywords    []string        // free-form recall hints; no logic branches on them
LastCurated time.Time       // last review (churn or human); zero until first curated
CuratorNote string          // free-form human annotation
```
In `New()` set the fresh defaults (`Helped==0`, so source-only base is correct):
```go
Evidence:   EvidenceForSource(source),
Volatility: VolatilityForCategory(category),
Lifecycle:  LifecycleActive,
```
Leave `Relations`/`Keywords` nil, `LastCurated` zero, `CuratorNote` empty.

**(c) `backend/internal/memory/filestore/filestore.go`** — extend `frontmatter` (after `Related`), add `relationFM`, wire converters, call `NormalizeLabels` on load:
```go
Evidence    string       `yaml:"evidence,omitempty"`
Volatility  string       `yaml:"volatility,omitempty"`
Lifecycle   string       `yaml:"lifecycle,omitempty"`
Relations   []relationFM `yaml:"relations,omitempty"`
Keywords    []string     `yaml:"keywords,omitempty"`
LastCurated time.Time    `yaml:"last_curated,omitempty"`
CuratorNote string       `yaml:"curator_note,omitempty"`
```
```go
type relationFM struct { Type string `yaml:"type"`; Target string `yaml:"target"` }
func toRelationFM(rs []memory.TypedRelation) []relationFM   // nil-safe, mirrors toSubsumedFM
func fromRelationFM(rs []relationFM) []memory.TypedRelation // nil-safe, mirrors fromSubsumedFM
```
In `toFrontmatter` map the new fields (`string(r.Evidence)`, …, `LastCurated: r.LastCurated.UTC()`, `Relations: toRelationFM(r.Relations)`). In `toRecord` populate them and wrap the existing `NormalizeConfidence(...)`:
```go
return memory.NormalizeLabels(memory.NormalizeConfidence(memory.Record{
  /* existing fields */,
  Evidence: memory.Evidence(m.Evidence), Volatility: memory.Volatility(m.Volatility),
  Lifecycle: memory.Lifecycle(m.Lifecycle), Relations: fromRelationFM(m.Relations),
  Keywords: m.Keywords, LastCurated: m.LastCurated, CuratorNote: m.CuratorNote,
}))
```

**(d) New `backend/internal/memory/filestore/backfill.go`** — the persist engine (idempotent by byte-compare; reuses unexported `decode`/`encode`/`atomicWrite`/`scopeDir` under `f.mu`):
```go
// RewriteNormalized rewrites every record file whose on-disk bytes differ from its
// normalized canonical form (NormalizeConfidence + NormalizeLabels), persisting derived
// labels/confidence that were missing, AND — for the M5 migration grace — stamping
// LastUsedAt=now where it is currently zero (so the disuse clock starts at backfill, not
// ancient UpdatedAt). Idempotent: a canonical, already-stamped file is skipped (a second
// pass finds no zero LastUsedAt and no missing labels → rewrites nothing). Never deletes,
// never mutates Text. Returns (scanned, rewritten).
func (f *FileStore) RewriteNormalized(ctx context.Context, now time.Time, dryRun bool) (scanned, rewritten int, err error)
```
Walk every `*.md` per scope dir: read bytes → `decode` (normalizes labels+confidence) → if `LastUsedAt.IsZero()` set `LastUsedAt = now` → `encode` → if `!bytes.Equal(orig, new)` increment `rewritten` and (unless `dryRun`) `atomicWrite`. Take `f.mu` for the write phase. (The `LastUsedAt`-where-zero stamp is **only** in this one-time engine, never in `NormalizeLabels`/`toRecord` — load-time stamping would corrupt every read.)

**(e) `backend/internal/memory/chroma/store.go`** — extract a shared metadata builder used by `index()` (113-117) and `Reindex()` (165-169), adding `volatility`/`lifecycle`:
```go
func metadataFor(r memory.Record) map[string]any {
  r = memory.NormalizeLabels(r)
  return map[string]any{
    "scope": string(r.Scope), "category": string(r.Category), "source": string(r.Source),
    "volatility": string(r.Volatility), "lifecycle": string(r.Lifecycle),
  }
}
```
**Lock a baseline first:** extract `metadataFor` in a no-behaviour-change commit (existing chroma tests green) before adding the new keys / the CLI.

**(f) New `backend/cmd/agentique/brain_labels.go`** — one-time CLI (testable core + thin cobra `RunE`; register in `brain.go init()` via `brainCmd.AddCommand(backfillLabelsCmd)`):
```go
var backfillLabelsCmd = &cobra.Command{ Use: "backfill-labels", Short: "One-time: seed Evidence/Volatility/Lifecycle defaults + start the disuse clock + Chroma", RunE: runBrainBackfillLabels }
// flags: --brain-dir (default live brain), --dry-run, -f/--force, --no-reindex
```
Flow: resolve `brainDir`; if `!dryRun` call `brain.Snapshot(brainDir, retain)` (**M1's snapshot — do not invent a parallel mechanism**) and print the snapshot ID/path; `fs := filestore.New(brainDir)`; `scanned, rewritten, err := fs.RewriteNormalized(ctx, time.Now().UTC(), dryRun)`; print counts; if `!dryRun && !noReindex` build the service and call `svc.Reindex(ctx)` when `svc.SemanticEnabled()` so Chroma picks up the new keys. Keep the decision/format logic in an IO-free core for testing.

#### Behavioural contract
- **Before:** no label fields; old files lack the keys; Chroma metadata = `{scope,category,source}`.
- **After:** every record read carries coherent labels (load-time `NormalizeLabels`); after the CLI runs, files physically carry them, every record has a non-zero `LastUsedAt`, and Chroma carries `volatility`/`lifecycle`.
- **Defaults:** evidence — human→`user_stated`, capture→`observed_once`, else→`inferred`, except non-human with `Helped≥2 && !contradicted`→`corroborated`; volatility — identity→`evergreen`, task→`ephemeral`, else→`slow`; lifecycle — always `active` on backfill.
- **Fill-empty only:** never overwrites a non-empty label (hand-set `lifecycle: archived` survives); unknown future vocab values intact.
- **Empty input:** `RewriteNormalized` → `(0,0,nil)`; CLI prints "nothing to backfill".
- **Idempotency:** a second pass rewrites 0 files (canonical + no zero `LastUsedAt`); Reindex is a no-op upsert.
- **Pinned/human/locked:** untouched semantically (no delete, no Text edit, no archive); human → `user_stated`.
- **Reversibility:** M1's pre-write snapshot is the restore point (restore = `brain restore <id>`).
- **Concurrency:** run with the server idle and **restart afterward** so the cachestore reloads and Chroma metadata is picked up (cross-process writes don't invalidate the live cache; normalize-on-load heals it anyway).

#### Tests (`-race`)
`memory/labels_test.go`:
- `TestEvidenceForSource`; `TestVolatilityForCategory`.
- `TestNormalizeLabelsFillsEmpty` — empties get defaults; consolidated `Helped=2`, empty `ReviewNote` → `corroborated`; capture → `observed_once`.
- `TestNormalizeLabelsPreservesExplicit` — `{code_verified, ephemeral, archived}` unchanged.
- `TestNormalizeLabelsIdempotent` — `NormalizeLabels(NormalizeLabels(r))` deep-equals `NormalizeLabels(r)`.
- `TestNewSetsLabels` — `New(scope, txt, CategoryIdentity, SourceAgent)` → `Volatility==evergreen`, `Lifecycle==active`, `Evidence==inferred`.
- `TestIsArchived` / `TestIsArchivedExported`.

`filestore/filestore_test.go`:
- `TestRoundTripLabels` — Put/Get with 2 relations, 2 keywords, non-zero `LastCurated`, a `CuratorNote`, `Lifecycle: superseded`, `Evidence: corroborated`; all fields equal (order preserved).
- `TestDecodeBackfillsLabels` — write a legacy (label-less) file via raw bytes, `List`, assert derived labels.

`filestore/backfill_test.go`:
- `TestRewriteNormalizedPersistsLabels` — legacy file; `RewriteNormalized(ctx, fixedNow, false)` → `rewritten==1`; raw bytes now contain `evidence:`,`volatility:`,`lifecycle:`,`last_used:`; second call → `rewritten==0`.
- `TestRewriteNormalizedDryRunWritesNothing`.
- `TestRewriteNormalizedNoOpOnCanonical` — a fully-labeled, `LastUsedAt`-stamped record → `rewritten==0`.
- `TestRewriteNormalizedStampsDisuseClockWhereZero` — record with zero `LastUsedAt` gets `LastUsedAt==fixedNow`; a record with non-zero `LastUsedAt` is left unchanged.

`chroma/store_test.go`:
- `TestMetadataForIncludesLabels` — contains `volatility`+`lifecycle` (+scope/category/source) and normalizes an un-labeled input (pure builder; no HTTP).

`cmd/agentique/brain_labels_test.go`:
- `TestBackfillLabelsSnapshotAndIdempotent` — temp brain with a legacy file; run the testable core; assert a snapshot (via `brain.Snapshot`) was created and contains the original file, the live file is now labeled + `last_used`-stamped, and a second run rewrites 0.

#### Acceptance checklist
- [ ] `memory.Record` has `Evidence`, `Volatility`, `Lifecycle`, `Relations []TypedRelation`, `Keywords []string`, `LastCurated time.Time`, `CuratorNote string`; `Related` retained.
- [ ] `New()` stamps `Evidence`/`Volatility`/`Lifecycle`; `NormalizeLabels` fills empties only, idempotent; `IsArchived` exported.
- [ ] filestore round-trips all new fields; legacy files load with derived labels.
- [ ] `RewriteNormalized` persists missing labels, stamps `LastUsedAt`-where-zero (M5 grace), byte-idempotent, never deletes/edits Text, honors `dryRun`.
- [ ] Chroma `index()` and `Reindex()` both emit `volatility`+`lifecycle` via shared `metadataFor`.
- [ ] `agentique brain backfill-labels` snapshots via `brain.Snapshot` (single snapshot mechanism), prints the snapshot ID, runs read-only under `--dry-run`, reindexes unless `--no-reindex`; re-running is a no-op.
- [ ] Doc comments on all new fields; `docs/brain-memory.md` + `docs/brain-graph-layer.md` (typed relations vs `Related`) updated.
- [ ] `cd backend && go test ./... -count=1 -race -short` + `just check` pass; `%w` errors.

#### Gotchas / VERIFY FIRST
- **No DB/sqlc/migration** (labels live in markdown, not SQLite).
- **No typegen / no UI:** verified `cmd/typegen` registers no brain/memory types and `memoryDTO` is hand-written/unregistered. Per domain rules, do **not** extend `memoryDTO` here.
- **Fill-empty discipline is load-bearing:** if `NormalizeLabels` ever overwrote a non-empty value it would stop being idempotent and silently reclassify human-curated labels. Reclassification is deferred to the churn.
- **`corroborated`-on-backfill is a one-shot classification** (reads `Helped` only while Evidence is empty); it will not re-fire later. Intentional for M6 (no churn) — note it in the doc comment.
- **Byte-idempotency** depends on stable YAML marshaling; `TestRewriteNormalizedNoOpOnCanonical` guards it. The pass also heals any missing pre-P2 confidence (benign).
- **`LastUsedAt` stamp is migration policy, not normalization** — keep it strictly inside `RewriteNormalized`, never in `NormalizeLabels`/`toRecord`.
- **VERIFY FIRST — Chroma value type:** v2 accepts string metadata (existing keys are strings); `metadataFor` normalizes first so no empty-string label reaches `index()`.
- **VERIFY FIRST — snapshot location:** M1's `brain.Snapshot` writes `brain/.snapshots/<ts>/` (already proven invisible to `List`). Use it; do not write a sibling `brain-snapshots/`.

---

### M7 · Durable retry queue

#### Intent
Session-end learning/outcome passes today run in a bare `go s.onSessionEnd(...)` goroutine — a restart mid-LLM-extraction silently loses the work, with no retry. Replace the inline passes with a durable, at-least-once, idempotent job queue backed by `brain_jobs` in `agentique.db`: enqueue on session end, drain on startup + on enqueue, retry with a bounded budget, then dead-letter (retain row + ERROR log) — never silent loss. **Must preserve M3's `mgr.OnSessionComplete` wiring.**

#### Exact changes
**(a) New migration `backend/db/migrations/037_create_brain_jobs.sql`** (goose; next after `036`):
```sql
-- +goose Up
CREATE TABLE brain_jobs (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,                       -- "learn" | "outcome"
    scope      TEXT NOT NULL,
    payload    TEXT NOT NULL,                       -- JSON: {project_id, events:[]TranscriptEvent}
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_brain_jobs_created ON brain_jobs(created_at, id);
-- +goose Down
DROP TABLE IF EXISTS brain_jobs;
```
`last_error NOT NULL DEFAULT ''` so sqlc maps to `string`, not `sql.NullString`.

**(b) New `backend/db/queries/brain_jobs.sql`** — then `just sqlc`:
```sql
-- name: CreateBrainJob :one
INSERT INTO brain_jobs (id, kind, scope, payload, attempts) VALUES (?, ?, ?, ?, ?) RETURNING *;
-- name: ListBrainJobs :many
SELECT * FROM brain_jobs ORDER BY created_at ASC, id ASC;
-- name: UpdateBrainJobAttempts :exec
UPDATE brain_jobs SET attempts = ?, last_error = ? WHERE id = ?;
-- name: DeleteBrainJob :exec
DELETE FROM brain_jobs WHERE id = ?;
```
Generates `store.BrainJob{ID,Kind,Scope,Payload string; Attempts int64; LastError,CreatedAt string}` and methods on `*store.Queries`. `ORDER BY created_at ASC, id ASC` — second-resolution `created_at` needs the `id` tiebreak for deterministic drain order.

**(c) New `backend/internal/brain/jobqueue.go`** (focused file; decoupled from `*store.Queries` via interface, from `*Service` via injected handlers):
```go
package brain
const ( JobKindLearn="learn"; JobKindOutcome="outcome"; defaultJobMaxAttempts=5 )

type jobStore interface {
  CreateBrainJob(ctx context.Context, arg store.CreateBrainJobParams) (store.BrainJob, error)
  ListBrainJobs(ctx context.Context) ([]store.BrainJob, error)
  UpdateBrainJobAttempts(ctx context.Context, arg store.UpdateBrainJobAttemptsParams) error
  DeleteBrainJob(ctx context.Context, id string) error
}
type Job struct { ID, Kind string; Scope memory.Scope; ProjectID string; Events []TranscriptEvent; Attempts int }
type JobHandler func(ctx context.Context, j Job) (changed bool, err error)
type jobPayload struct { ProjectID string `json:"project_id"`; Events []TranscriptEvent `json:"events"` }

type JobQueue struct {
  db jobStore; bus eventbus.Broadcaster; handlers map[string]JobHandler; maxAttempts int
  mu sync.Mutex; draining, requeued bool
}
func NewJobQueue(db jobStore, bus eventbus.Broadcaster, maxAttempts int, handlers map[string]JobHandler) *JobQueue
func (q *JobQueue) Enqueue(ctx context.Context, kind, projectID string, events []TranscriptEvent) error
func (q *JobQueue) Drain(ctx context.Context)
```
- `NewJobQueue`: `if maxAttempts <= 0 { maxAttempts = defaultJobMaxAttempts }`.
- `Enqueue`: marshal `jobPayload`; `CreateBrainJob` with `ID: uuid.NewString()`, `Kind`, `Scope: string(ScopeForProject(projectID))`, `Payload`, `Attempts: 0`; wrap insert errors with `%w`; on success `go q.Drain(context.Background())`. The insert is synchronous (job durable before `Enqueue` returns).
- `Drain`: single-flight with a follow-up pass:
```go
func (q *JobQueue) Drain(ctx context.Context) {
  q.mu.Lock()
  if q.draining { q.requeued = true; q.mu.Unlock(); return }
  q.draining = true; q.mu.Unlock()
  for {
    q.drainPass(ctx)
    q.mu.Lock()
    if !q.requeued { q.draining = false; q.mu.Unlock(); return }
    q.requeued = false; q.mu.Unlock()
  }
}
```
- `drainPass`: `ListBrainJobs`; per row: skip if `Attempts >= maxAttempts` (dead-lettered); skip without burning an attempt if `handlers[kind]==nil` (model disabled this run); else decode payload → `Job`, run handler. Success: `DeleteBrainJob`; if `changed`, broadcast `EventBrainUpdated` once per pass. Error: `attempts++`, `UpdateBrainJobAttempts(attempts, err.Error())`; when `attempts >= maxAttempts` log `slog.Error("brain: job dead-lettered", "id",…,"kind",…,"attempts",…,"error",err)`. One pass = at most one attempt per job.

**(d) `backend/internal/server/server.go` — rewire the session-end hook (350-395), PRESERVING M3's wiring.** `queries` is in scope. Build handlers from the existing `encodeEx`/`outcomeJudge` (unchanged), construct the queue, drain on startup, enqueue instead of run inline, and **keep `mgr.OnSessionComplete`**:
```go
handlers := map[string]brain.JobHandler{}
if encodeEx != nil {
  handlers[brain.JobKindLearn] = func(ctx context.Context, j brain.Job) (bool, error) {
    n, err := brainSvc.LearnFromTranscript(ctx, j.Scope, j.Events, encodeEx); return n > 0, err
  }
}
if outcomeJudge != nil {
  handlers[brain.JobKindOutcome] = func(ctx context.Context, j brain.Job) (bool, error) {
    rep, err := brainSvc.ApplyOutcomesFromTranscript(ctx, j.Scope, j.Events, outcomeJudge)
    return rep.Helped > 0 || rep.Flagged > 0, err
  }
}
if len(handlers) > 0 {
  jq := brain.NewJobQueue(queries, bus, cfg.BrainRetryMax, handlers)
  go jq.Drain(context.Background()) // startup recovery: resume crash-left jobs
  svc.SetOnSessionEnd(func(projectID string, events []store.SessionEvent) {
    tevents := make([]brain.TranscriptEvent, len(events))
    for i, e := range events { tevents[i] = brain.TranscriptEvent{Type: e.Type, Data: e.Data} }
    if _, ok := handlers[brain.JobKindLearn]; ok {
      if err := jq.Enqueue(context.Background(), brain.JobKindLearn, projectID, tevents); err != nil {
        slog.Warn("brain: enqueue learn job failed", "project", projectID, "error", err)
      }
    }
    if _, ok := handlers[brain.JobKindOutcome]; ok {
      if err := jq.Enqueue(context.Background(), brain.JobKindOutcome, projectID, tevents); err != nil {
        slog.Warn("brain: enqueue outcome job failed", "project", projectID, "error", err)
      }
    }
  })
  mgr.OnSessionComplete = svc.HandleSessionComplete // <-- M3 wiring: MUST be retained
}
```
Two **independent** jobs (a learn failure can't force an outcome re-run). Handler bodies call the exact same `Service` methods as today — the behaviour baseline, routed through durability. M3's completion ingest also flows through `svc.onSessionEnd`, so it gains queue durability automatically.

**(e) Config `BrainRetryMax`** — `config.go` `BrainConfig`: `RetryMax int \`toml:"retry-max"\`` (doc: 0 → default 5; env `AGENTIQUE_BRAIN_RETRY_MAX`). `serve.go`: `BrainRetryMax: envIntOr("AGENTIQUE_BRAIN_RETRY_MAX", fileCfg.Brain.RetryMax)`. `server.go` `Config`: `BrainRetryMax int`.

#### Behavioural contract
- **After:** `onSessionEnd` writes one durable row per configured pass and returns fast. A drainer executes them; a failure increments `attempts` + persists `last_error`, retried on next enqueue/startup drain; at `attempts >= BrainRetryMax` the row is **dead-lettered** (kept, ERROR-logged, excluded from future drains). On startup the drainer replays every pending row.
- **Delivery:** at-least-once (row deleted only after a successful handler). A crash after the LLM pass mutated memory but before `DeleteBrainJob` replays on restart. `LearnFromTranscript` is self-healing under replay (`Add` dedups + M4 reinforces). `ApplyOutcomesFromTranscript` is **not** fully idempotent — a replay can re-increment `Helped`; this is an accepted at-least-once bound (document; do not attempt exactly-once).
- **Edge cases:** empty/trivial input unreachable from the live path (`service.go:1121` gates `>= minEventsToEncode`); if reached, handler runs on empty transcript → `changed=false` → row deleted. Unknown/disabled kind skipped without burning an attempt (resumes when re-enabled). Duplicate facts handled by dedup. Archived/pinned/human/locked unchanged (queue adds no bypass). Concurrency: `Drain` single-flight + follow-up pass; SQLite serialises DB writes; cachestore single-server-process assumption holds (all draining in one process under `JobQueue.mu`). **Do not run two server processes against the same DB+brain dir.**

#### Tests (`backend/internal/brain/jobqueue_test.go`, `-race`)
In-memory fake `jobStore` (map by id) + synthetic handlers; no LLM/Service.
- `TestJobQueue_EnqueueDrainSuccess` — enqueue a `learn` job; handler signals a channel/`WaitGroup` the test waits on (and/or poll until the row is deleted from the fake store) **before** asserting. Asserts: handler invoked exactly once with round-tripped `Events` and `Scope==ScopeForProject(projectID)`; row gone; `EventBrainUpdated` once. (The sync point makes the assertions deterministic despite `Enqueue`'s `go Drain`.)
- `TestJobQueue_PayloadRoundTrip` — persisted `Payload` JSON decodes to byte-identical `Events` + `ProjectID`; `Kind=="learn"`, `Scope==string(ScopeForProject(projectID))`.
- `TestJobQueue_RetryThenDeadLetter` — handler always errors, `maxAttempts=3`, call `Drain` 5×: handler invoked exactly 3×; row present with `Attempts==3` and non-empty `LastError`; never deleted; one ERROR log (injected `*slog.Logger`/test handler).
- `TestJobQueue_ResumeAfterRestart` (**binding "resumes after restart" criterion**) — pre-seed a pending row (`Attempts==1`, valid payload); fresh `JobQueue`; `Drain` → handler runs with the persisted payload; row deleted.
- `TestJobQueue_UnknownKindSkipped` — no registered handler → `Attempts` unchanged, row retained.
- `TestJobQueue_SingleFlight` (`-race`) — handler blocks on a channel; two `Drain` goroutines + a concurrent `Enqueue`; release. No race; in-flight handler ran once; the job enqueued during the active pass is processed by the follow-up loop (not stranded).
- `TestBrainJobStore_RoundTrip` — in-memory SQLite (`:memory:`), `store.RunMigrations(db, db.Migrations)`, exercise `CreateBrainJob → ListBrainJobs → UpdateBrainJobAttempts → DeleteBrainJob`; assert round-trip and `created_at ASC, id ASC` order. (`-short`-safe.)

#### Acceptance checklist
- [ ] `037_create_brain_jobs.sql` with goose Up/Down; `last_error NOT NULL DEFAULT ''`; `idx_brain_jobs_created` on `(created_at, id)`.
- [ ] `backend/db/queries/brain_jobs.sql` present; `just sqlc` re-run; generated `brain_jobs.sql.go` + `store.BrainJob` committed (no hand-edits).
- [ ] `brain/jobqueue.go` depends on `jobStore` interface + injected `JobHandler`s, not `*Service`.
- [ ] `server.go` enqueues two independent jobs, no longer runs passes inline, wires startup `go jq.Drain(...)`, and **retains `mgr.OnSessionComplete = svc.HandleSessionComplete`** (M3 not regressed).
- [ ] `BrainRetryMax` plumbed (`retry-max` toml → `envIntOr` → `server.Config`); default when `<= 0`.
- [ ] Dead-letter retains the row, persists `last_error`, logs ERROR once, excluded from later drains.
- [ ] **Binding restart criterion:** `TestJobQueue_ResumeAfterRestart` green (the kill -9 / sqlite3 procedure is an optional smoke-test note, not a CI gate).
- [ ] All new tests pass under `cd backend && go test ./... -count=1 -race -short`; `just check` green.
- [ ] Doc comments on exports; `docs/brain-learning-dynamics.md` (durability) + `docs/tech-debt.md` (at-least-once outcome-double-count caveat) updated; `%w` errors.

#### Gotchas / VERIFY FIRST
- **VERIFY FIRST (no import cycle):** `jobqueue.go` imports `internal/store`. `store` does not import `brain` — `grep -R "internal/brain" backend/internal/store` to confirm no back-edge before committing.
- **VERIFY FIRST (generated names):** after `just sqlc`, confirm `ListBrainJobs`, `UpdateBrainJobAttemptsParams{Attempts int64; LastError string; ID string}`, field types `int64`/`string`; adjust the `jobStore` interface to match verbatim.
- **No `just typegen`** (no wire type crosses to the frontend; `store.BrainJob` is internal).
- **Residual loss window (document):** `DeleteSession` captures `endEvents` in memory, deletes the row, then fires `go s.onSessionEnd` → enqueue. A `kill -9` in the gap between the row-delete commit and the `Enqueue` insert loses that one transcript (in-memory only copy). Pre-existing and smaller than the bug M7 fixes; keep `Enqueue` to a single fast insert to minimise it; note as a known limitation.
- **Single-flight follow-up pass is load-bearing** — `Enqueue` kicks a `Drain` that usually finds `draining==true`; without the `requeued` pass that job would wait until the next session end. Keep it; cover in `TestJobQueue_SingleFlight`.
- **At-least-once vs outcome double-count:** if the `Helped` re-increment on replay is unacceptable, the follow-up (not M7) is an applied-marker (per-`(scope, fact-id)` idempotency key); for M7, document the bound.
- **Determinism:** rely on `ORDER BY created_at ASC, id ASC` (the `id` tiebreak is required for the round-trip ordering assertion).

---

## 6. Sequencing & dependencies

**Execution order (NOT strictly numeric):**

```
M0  (baseline lock — MUST be first; no production code yet)
 │
M1  (snapshot/rollback — the single snapshot mechanism; M5 & M6 reuse brain.Snapshot)
 │
M2  (capture-tier ingest — 4-arg Capture; ingest no longer injects)
 │
M3  (learn on completion — fires ingest on StateDone; preserve mgr.OnSessionComplete)
 │
M4  (reinforce-on-re-observe — builds on M2's Capture; ADDS s.mu ingest locking;
 │   MUST precede M5 because it closes the churn-vs-ingest race)
 │
M6  (labels control plane — SOLE owner of Evidence/Volatility/Lifecycle/Relations;
 │   backfill stamps LastUsedAt to start the disuse clock; MUST precede M5)
 │
M5  (computed aging + archive-not-delete — reads M6 labels; regenerates M0 goldens;
 │   relies on M1 snapshot + M4 locking)
 │
M7  (durable retry queue — wraps the M2/M3 ingest sink; must re-add M3's wiring)
```

**Hard dependencies (the load-bearing ones):**
- **M0 first** — lock behaviour before any production change.
- **M6 before M5** — M6 owns the `Lifecycle`/`Evidence`/`Volatility` types/fields/frontmatter that M5 reads; declaring them in both is a duplicate-declaration compile error. (Resolves the blockers.)
- **M2 before M4** — M4's `Capture` edit is written against M2's 4-arg signature.
- **M4 before M5** — M4's `s.mu` ingest locking is what makes the M5 churn safe against concurrent ingest (M3 fires lock-free ingest otherwise).
- **M1 before M5 and M6** — both reuse `brain.Snapshot` (no parallel snapshot infra).
- **M3's `mgr.OnSessionComplete` line must survive M7's server.go rewrite.**

**Parallelizable (independent surfaces, given the order above):**
- **M1** (snapshot, pure FS + CLI) is independent of the ingest/aging chain and can be developed in parallel with M2/M3 once M0 lands; it only needs to merge before M5/M6.
- **M7** (DB/queue) touches `db/` + `server.go` wiring and the queue file — independent of the `memory` core changes (M5/M6); it can be developed in parallel and merged last (it must rebase onto M3's wiring).
- Within tasks, the per-package test work (memory vs filestore vs chroma vs brain) can be parallelized.

**Running multiple dev agents (file-ownership split).** The honest partition is **two** worktree-isolated agents — not more. The core is largely serial because M4's ingest-locking *and* M6's labels both gate M5, so the critical path (M0→M1→M2→M3→M4→M6→M5) belongs to one owner:
- **Agent A — core (critical path):** M0 → M1 → M2 → M3 → M4 → M6 → M5. Owns `internal/memory/*`, `internal/brain/*`, `session/service.go`, and the ingest wiring in `server.go`.
- **Agent B — infra (side-branch):** M7 only — `db/migrations`, `db/queries`, `internal/store` (sqlc), `internal/brain/jobqueue.go`. Starts once M0 lands; **rebases onto Agent A's `server.go` wiring (M3's `mgr.OnSessionComplete`) before merge.** `server.go` is the sole merge point.
- M1 *could* go to a third agent (pure FS + CLI), but it must land before M5/M6, so the coordination rarely pays off.

So: realistically **2 agents** (core + infra), not the 3 loosely-coupled tracks an early sketch suggested — the verified dependencies (M4-locking→M5, M6-labels→M5) make the spine more serial than it first looked.

---

## 7. Cross-cutting

**New `config.toml [brain]` keys (all with `AGENTIQUE_BRAIN_*` env overrides via `firstNonEmpty`/`envIntOr`/`envFloatOr`):**
| Key | Type | Env | Default | Task |
|---|---|---|---|---|
| `snapshot-retain` | int | `_SNAPSHOT_RETAIN` | 7 | M1 |
| `archive-after` | string (dur) | `_ARCHIVE_AFTER` | `""` (off) | M5 |
| `archive-confidence-floor` | float | `_ARCHIVE_FLOOR` | 0.35 | M5 |
| `retry-max` | int | `_RETRY_MAX` | 5 | M7 |

Threading for each: `config.go BrainConfig` → `serve.go server.Config{...}` literal → `server.go Config` struct → consumer (`brain.New` / `NewAutomation` / `NewJobQueue`).

**Docs to update:** `docs/brain-evolution-plan.md` (tick each band as shipped); `docs/brain-memory.md` (capture-tier ingest, snapshots/rollback, archived cold tier + archive-not-delete, label control plane, backfill command); `docs/brain-learning-dynamics.md` (D1 disuse/aging; learn-on-completion; durability); `docs/brain-outcome-signal.md` (`Reinforce` as the third reconsolidation verb); `docs/brain-graph-layer.md` (typed `Relations` vs untyped `Related`); `docs/tech-debt.md` (at-least-once outcome double-count caveat). Sample `config.toml [brain]` block.

**DB / sqlc:** only **M7** touches `backend/db/` (migration `037_create_brain_jobs.sql` + `queries/brain_jobs.sql`) → run `just sqlc`, commit generated `store/brain_jobs.sql.go` + updated `models.go`. No other task adds SQL (labels/captures/lifecycle are all markdown-only).

**typegen:** **not required** for Band 1 — `memory.Record` and `store.BrainJob` are not registered wire types and Band 1 does not extend `brain/http.go`'s `memoryDTO`. Only if a follow-up surfaces `lifecycle`/labels on the brain wire model do you confirm the typegen registry and run `just typegen`.

**Frontend:** **no frontend change in Band 1.** Captures and archived facts may appear in the existing Brain memories list (the HTTP list intentionally keeps returning archived with its `lifecycle` field), but a dedicated "show archived" toggle / label UI is a deliberate follow-up, not part of this band. (Costs/`totalCost` remain hidden per project rules.)

---

## 8. Definition of done for Band 1 + Risks

**Definition of done (Band 1 = "Migrate"):**
- [ ] All of M0–M7 merged in the §6 order; each commit passed `cd backend && go test ./... -count=1 -race -short` and `just check`.
- [ ] **Pipeline:** session ingest (on clean completion *and* on delete) writes `Source=capture` (non-injectable); the scheduled churn is the exclusive path `capture → consolidated` with `DerivedFrom` provenance; recall injects only `human`/`agent`/`consolidated` + pinned.
- [ ] **Aging:** confidence is computed-eroded at recall (never rewritten on a nudge); forgetting is **archive** (`Lifecycle=archived`, excluded everywhere non-recall, kept on disk, restorable), never delete; human/pinned/locked/evergreen never erode/archive; archiving is opt-in (`archive-after`) with a deploy-safe recall-cliff gate + backfill disuse-clock reset.
- [ ] **Reinforcement:** re-observing a durable fact strengthens it (`Corroborations++`, `LastUsedAt`, score → ceiling) under `s.mu` (no lost increments, no churn-vs-ingest race).
- [ ] **Labels:** every record carries coherent `Evidence`/`Volatility`/`Lifecycle` (+typed `Relations`/`Keywords`); the ~1,482 existing files + Chroma metadata are backfilled, idempotently, snapshot-first.
- [ ] **Durability:** learn/outcome passes survive a mid-extraction crash (drain on startup) and dead-letter (never silent loss) after `retry-max`.
- [ ] **Safety/reversibility:** markdown is SoT; `brain.Snapshot` precedes every churn and the backfill; `brain restore <id>` works; over-deletion guard intact; nothing in the band ever auto-deletes a record.
- [ ] M0 goldens regenerated by M5 stand as the committed delete→archive audit trail.
- [ ] All `docs/brain-*.md` updated; the new `[brain]` tunables documented.

**Risks (lead: the churn-vs-`Add`/`Capture` concurrency check):**
1. **Churn vs ingest concurrency (highest).** `Consolidate` holds `s.mu` but pre-M4 `Add`/`Capture` do **not** — a one-sided lock is no mutual exclusion, and M3 fires lock-free `Capture` on every clean completion concurrently with the M5 churn that archives/rewrites. Reachable failures: lost `Reinforce` increments (RMW) and the cachestore double-check-lock stale-cache install. **Mitigation (mandatory, in M4 before M5):** take `s.mu` across `List → dedup/Reinforce → Put` in both `Add` and `Capture` (hold List→Put only, no nested caller lock); verified `Consolidate` already holds `s.mu`. Verify no caller already holds `s.mu` to avoid self-deadlock.
2. **Recall cliff on M5 deploy.** The read-time fade against ~1,482 facts using historical timestamps could silently evict a large fraction at once. Mitigation: fade gated on `Query.ArchiveFloor>0` with `archive-after` defaulting off + M6 backfill stamping `LastUsedAt=now`-where-zero. Verify both before enabling archiving in prod.
3. **Pipeline regression if churn is off.** Post-M2, ingest no longer injects directly; a deployment with a learn model but no scheduled consolidation stages captures that never inject. Mitigation: require/runbook scheduled consolidation enabled; documented in M2.
4. **At-least-once outcome double-count.** Crash-replay can re-increment `Helped` (outcome pass not idempotent). Accepted bound for Band 1; documented in `docs/tech-debt.md`; applied-marker is a follow-up.
5. **Archived leakage.** Archived facts must be excluded from every non-recall consumer (promotion, DueForReview, areas/community/link/graph, operating contract), not just recall — else cold facts resurface or get re-promoted. Mitigation: M5 ships those `isArchived` skips with named tests in the same commit (not deferred).
6. **Migration order / duplicate declarations.** M5 and M6 both touch `Lifecycle`; wrong order = compile failure. Mitigation: M6 is the sole owner and is sequenced before M5; an acceptance item asserts the package compiles with both applied.
7. **Cross-process cache staleness on backfill/restore.** The M6 CLI and `brain restore` mutate files outside the server process; the live cachestore won't see them. Mitigation: run with the server idle and restart afterward (documented); normalize-on-load heals labels regardless.