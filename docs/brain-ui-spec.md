# Brain UI — Implementation Spec (surface Band 1 → Band 3 frontend)

## 1. Title, mission, how to use this spec

**Mission.** Band 1 made the brain *evolve* (capture-tier ingest, reinforce, computed disuse-aging
with archive-not-delete, the label control plane, snapshots, a durable job queue) but was
**backend-only by design** (`docs/brain-evolution-spec.md` §7: "no frontend change in Band 1"). The
UI therefore can't see or manage any of it: the wire DTO stops at `confidence`/`reviewNote`, so
`lifecycle`/`evidence`/`volatility`/`corroborations`/`relations`/`keywords` never reach the client,
captures and archived facts appear in the memory list indistinguishable from live facts, and there
is no archive/restore/snapshot affordance. This spec brings the UI up to the Band 1 backend and
delivers the planned **Band 3 frontend** — E4 (typed-relation edges, lifecycle/archived filter,
evidence/volatility surfaced) and E2 (brain-health report).

**How to use this.** Self-contained; execute tasks in §6 order — **F0 (wire types) is the keystone
every other task reads from**. Each task in §5 has: intent, exact file changes, behavioural
contract, tests, acceptance checklist, gotchas. §4 is the ground-truth contract reference — when a
task body disagrees with §4, §4 wins. Run §3's gates before every commit.

---

## 2. Context & model

**The new record fields (Band 1, already persisted; absent from the wire today):**
- `lifecycle`: `active | superseded | archived` — archived = cold tier (out of recall, on disk, restorable).
- `evidence`: `user_stated | code_verified | corroborated | inferred | observed_once` — trust source.
- `volatility`: `evergreen | slow | ephemeral` — decay rate.
- `corroborations`: int — independent re-observations (distinct from `uses`/`helped`).
- `relations`: `[{type, target}]` typed edges (`supersedes|contradicts|duplicates|generalizes|corroborates`); the untyped `related` stays for back-compat.
- `keywords`: `string[]` — recall hints.
- `lastCurated` (time), `curatorNote` (string).
- `source` can now be `"capture"` — raw, never-injected, awaiting churn promotion.

**The UI model after this spec.**
1. **Visibility** — every memory row shows what tier it is in: a *capture* badge (raw/pending), an
   *archived* / *superseded* badge (cold/replaced), evidence + volatility chips, and a corroboration
   count. The default list shows only **live injectable** facts (`lifecycle=active`, non-capture);
   two toggles reveal captures and archived, with counts so nothing is silently hidden.
2. **Management** — a per-fact **Restore** action un-archives a cold fact (new backend endpoint over
   the existing un-archive path); a **Snapshots** panel lists/takes/restores brain snapshots (new
   backend endpoints over `brain.Snapshot`/`ListSnapshots`/`Restore`, with cachestore invalidation).
3. **Graph** — typed relations render as distinct edges; a lifecycle filter hides archived /
   dims superseded; colour-by gains `evidence`/`volatility`.
4. **Health** — a report panel over an extended `status` endpoint (counts per lifecycle/source/
   evidence/volatility/confidence tier, review-queue size, capture backlog).

**Key reality (§4 confirms):** the frontend brain types are **hand-written** in
`frontend/src/lib/brain-api.ts` — `memoryDTO` is NOT in the typegen registry, so `just typegen`
does **not** touch brain types. The wire contract is kept in sync **by hand**: every `memoryDTO`
change in `backend/internal/brain/http.go` has a mirror edit in `brain-api.ts`. This is the single
most important convention; F0 is built around it.

---

## 3. Conventions & quality gates

- **Gate:** `just check` (biome + tsc) must pass before every commit. For backend changes also run
  `cd backend && go test ./... -count=1 -race -short`. Frontend unit tests: `just test-frontend`
  (vitest) where a `__tests__` dir exists (BrainGraph, MemoryReview already have suites — extend them).
- **Wire types are hand-synced** (not typegen): `memoryDTO` (http.go) ↔ `Memory` (brain-api.ts).
  Never run `just typegen` expecting brain types to update; edit both files together. Add a one-line
  comment on `memoryDTO` pointing at `brain-api.ts` (and vice-versa) so the coupling is discoverable.
- **Zustand stable-selector rule (CLAUDE.md):** selectors must return stable references — never
  return `{}`/`[]`/`.map()`/`.filter()` from a selector. Derived/filtered lists live in component
  `useMemo` (as `BrainPage` already does for `groups`/`graphMemories`) or use `useShallow`; fallbacks
  use a module-level `const EMPTY: Foo[] = []`.
- **Path alias** `~/` → `frontend/src`. `routeTree.gen.ts` is generated — do not edit.
- **UI kit:** reuse `~/components/ui/badge` (`Badge`, cva `variant`s) for badges/chips, `~/components/ui/button`
  for actions, lucide-react for icons. Match the existing Tailwind idiom in `BrainPage.tsx`
  (`size-4`, `text-muted-foreground`, `tabular-nums`, `rounded-md border`). Add new badge `variant`s
  to `badgeVariants` rather than ad-hoc inline colours.
- **API client pattern** (brain-api.ts): every call is `fetch(\`${BASE}/...\`, {method, headers, body})`
  → `await throwIfNotOk(res, msg)` → `res.json()`. `BASE = "/api/brain"`. Mirror it for new endpoints.
- **Backend route registration:** add new routes in the brain block at `server.go:330-343` via
  `mux.Handle("METHOD /api/brain/...", httperror.HandlerFunc(bh.HandleX))`.
- **Costs stay hidden** (project rule): never surface `totalCost`/prices in any new UI.
- **Doc comments** on new exported Go symbols; update `docs/brain-memory.md` (REST API + Brain tab
  sections) as part of completion.

---

## 4. Verified code contracts (the reference of record)

**Backend HTTP (`backend/internal/brain/http.go`, routes `server.go:330-343`):**
```
memoryDTO (http.go:32-61): id, scope, text, category, source, pinned, locked, uses, helped,
  createdAt, updatedAt, derivedFrom[], related[], community, area, confidence, confidenceScore,
  reviewNote, subsumed[{scope,text}]   ← NO lifecycle/evidence/volatility/corroborations/relations/keywords
toDTO(r memory.Record) memoryDTO (http.go:68-80)   ← single mapping point
HandleList GET /api/brain/memories?scope=  (http.go:116) → toDTOs(Service.List(...))  ← NO filtering:
  returns captures AND archived (Service.List → store.List passes everything through)
Handlers: HandleCreate POST /memories; HandleGet GET /memories/{id}; HandleUpdate PUT /memories/{id}
  {text,category}; HandleDelete DELETE /memories/{id}; HandlePin/Lock POST /memories/{id}/pin|lock
  {pinned|locked}; HandleConfirm POST /memories/{id}/confirm; HandleFlag POST /memories/{id}/flag
  {reason}; HandleRefine POST /memories/{id}/refine; HandleSearch GET /search; HandleGraph GET /graph;
  Consolidate* POST /consolidate[...]; HandleStatus GET /status → {"semantic": bool}  ← minimal
brain.Handler struct: {Service *brain.Service, Runner, Bus}  (server.go:329)
brain.Service (Band 1): Snapshot() (SnapshotInfo,error); package brain.ListSnapshots(dir)([]SnapshotInfo,error),
  brain.Restore(dir,id,retain) error; Update(ctx,id,text,category) un-archives an archived fact
  (brain.go ~834: archived→active + LastUsedAt=now); store is a cachestore over filestore (write-
  invalidate; an EXTERNAL file rewrite — i.e. a snapshot restore — does NOT invalidate the cache).
  Service.dir is unexported; Service.SemanticEnabled() bool.
memory.Record labels (memory/labels.go): Evidence/Volatility/Lifecycle/RelationType string enums;
  TypedRelation{Type RelationType `json:"type"`; Target string `json:"target"`}; IsArchived(r).
```

**Frontend data layer:**
```
Memory (brain-api.ts, HAND-WRITTEN): id,scope,text,category,source,pinned,locked,uses,createdAt,
  updatedAt,derivedFrom?,related?,community?,area?,confidence?,confidenceScore?,reviewNote?,
  subsumed?[{scope,text}]   ← mirrors memoryDTO; MISSING the Band-1 fields + `helped`
API client (brain-api.ts): getStatus()→BrainStatus; listMemories(scope?); createMemory; updateMemory
  (id,{text?,category?}); deleteMemory; setPinned; setLocked; confirmMemory; flagMemory(id,reason?);
  refineMemory; getGraph()→GraphData; consolidate fns. Pattern: fetch → throwIfNotOk → res.json().
useBrainStore (brain-store.ts, zustand): state {memories:Memory[], semantic, loaded, loading, graph,
  preview…, flareSeq}; actions load/loadGraph/create/update/remove/pin/lock/confirm/preview…/setJob/
  onBrainUpdated. upsert(list,m) keeps a fresh array ref (stable-ref contract). onBrainUpdated →
  flareSeq++ + debounced listMemories refetch (scheduleRefresh, 600ms). NO filter state in the store.
BrainPage.tsx (899 lines): Badge from ~/components/ui/badge; const [filter,setFilter]=useState("");
  groups = useMemo(...memories grouped by scope, sorted pinned/uses...); graphMemories = useMemo(...);
  reviewQueue = useMemo(memories.filter(inReviewQueue)); view toggle list|graph|graph3d; per-row
  actions pin/lock/confirm/flag/edit/delete; consolidate buttons; Add panel; MemoryReview modal.
MemoryReview.tsx: review queue with confirm/edit/flag/lock/pin (has __tests__).
useBrainSubscriptions.ts: subscribes to the brain.updated WS event → store.onBrainUpdated().
```

**Frontend graph (`frontend/src/lib/brain-graph-model.ts`):**
```
GraphMemory = Memory & {degree?, betweenness?}
BrainColorBy = "scope" | "community" | "area"
BrainEdgeKind = "provenance" | "related" | "similar" | "area"
BrainNode {id, label, scope, source, ..., area, val (size = uses+pinned+degree), trust ("human"|
  "review"|"normal"), conf (confidenceScore)}
BrainLink {source, target, kind: BrainEdgeKind, weight?}
buildBrainModel(memories, opts{colorBy, labelForScope, showSimilar}) → {nodes, links}; edges:
  provenance (derivedFrom), related (related[]), similar (lexical Jaccard fallback ≥ SIM_THRESHOLD
  0.18, capped SIM_MAX_NODES 800 / SIM_DEGREE_CAP 4), area (under "by area"). toNode: trust "human"
  when source==human || confidence==extracted; conf = confidenceScore ?? 0.8.
BrainGraph.tsx (2D) + BrainGraph3D.tsx (3D, carries sim positions across refetches) both render
  {nodes,links}; have __tests__/BrainGraph.test.tsx.
```

> **VERIFY FIRST before coding each task:** re-confirm the line numbers above (they drift); confirm
> `Memory` in `brain-api.ts` still lacks the new fields; confirm `HandleList`/`Service.List` still
> return captures+archived unfiltered; confirm no other consumer of `Memory` breaks when fields are added.

---

## 5. Tasks F0..F6

### F0 · Wire the Band-1 fields end-to-end (KEYSTONE)

**Intent.** Put `lifecycle`, `evidence`, `volatility`, `corroborations`, `relations`, `keywords`,
`lastCurated`, `curatorNote` (and the already-persisted `helped`) on the wire and into the frontend
`Memory` type, so every later task can read them. Pure plumbing — no behaviour change.

**Exact changes.**
- `backend/internal/brain/http.go`: extend `memoryDTO` with the new JSON fields (use `omitempty`
  for the optional ones; `lifecycle`/`evidence`/`volatility` are always set so non-omit), add a
  `relationDTO{Type,Target string}` (mirror `subsumedDTO`), and map all of them in `toDTO`
  (`Lifecycle: string(r.Lifecycle)`, …, `Relations: toRelationDTOs(r.Relations)`, `Helped: r.Helped`
  already present). Add a comment: "keep in sync with frontend/src/lib/brain-api.ts Memory".
- `frontend/src/lib/brain-api.ts`: extend the hand-written `Memory` interface with the matching
  optional fields (`lifecycle?: "active"|"superseded"|"archived"`, `evidence?: string`,
  `volatility?: string`, `corroborations?: number`, `relations?: {type: string; target: string}[]`,
  `keywords?: string[]`, `lastCurated?: string`, `curatorNote?: string`, and add `helped?: number`).
  Add exported string-union helper consts for the enums (`EVIDENCE_VALUES`, `VOLATILITY_VALUES`,
  `LIFECYCLE_VALUES`) used by badges/filters. Add a comment mirroring the backend coupling note.
- No `just typegen` (brain types are hand-written — confirmed §4). No new endpoint here.

**Behavioural contract.** `GET /api/brain/memories` now returns the new fields on every record;
existing UI is unaffected (it just ignores them). Legacy records (pre-backfill) still decode (labels
were backfilled by `brain backfill-labels`, but normalize-on-load fills any gaps server-side, so the
DTO always carries coherent values).

**Tests.** Backend: extend a `http_test.go` (or add one) asserting `toDTO` carries lifecycle/evidence/
volatility/corroborations/relations for a fully-labelled record, and that an archived record serializes
with `lifecycle:"archived"`. Frontend: a small `brain-api` type test isn't required (tsc covers it);
ensure `just check` passes.

**Acceptance.** [ ] memoryDTO + toDTO + relationDTO extended; [ ] Memory interface mirrors it exactly
(field names/casing match the JSON tags); [ ] enums exported; [ ] `go test ./... -race -short` + `just check`
green; [ ] coupling comment in both files.

**Gotchas.** JSON casing: Go `json:"confidenceScore"` ↔ TS `confidenceScore`. `relations` items use
`{type,target}` (lowercase) — match the `TypedRelation` json tags. Do NOT add to typegen.

---

### F1 · Memory-list visibility (badges + chips + corroborations)

**Intent.** Make every memory row self-describing: capture / archived / superseded badges, evidence
+ volatility chips, a corroboration count. Display-only.

**Exact changes.**
- `frontend/src/components/ui/badge.tsx`: add semantic `variant`s to `badgeVariants` (e.g. `capture`,
  `archived`, `superseded`, `evidence`, `volatility`) OR keep variants generic and pass Tailwind
  colour classes via `className` — match how existing badges are styled. Prefer adding named variants
  so colours are centralized.
- `BrainPage.tsx` (the per-memory row renderer — find the `MemoryItem`/inline row near the list
  render): add, next to the existing confidence/source/category badges:
  - `source === "capture"` → a `capture` badge ("capture — pending promotion").
  - `lifecycle === "archived"` → an `archived` badge; `=== "superseded"` → a `superseded` badge.
  - evidence chip (compact, e.g. `code_verified` → "✓ code", `corroborated` → "corroborated") and a
    volatility chip (`ephemeral`/`slow`/`evergreen`) — small, muted.
  - corroborations: when `> 0`, a tiny `×N` indicator beside `uses`/`helped` (`tabular-nums`).
- `MemoryReview.tsx`: surface the same evidence/volatility chips on review rows (read-only).

**Behavioural contract.** No data changes; archived/superseded/capture rows are now visually
distinct. A row with no special state looks exactly as today.

**Tests.** Extend `MemoryReview` / add a `BrainPage` render test: a capture record shows the capture
badge; an archived record shows the archived badge; evidence/volatility chips render their label.

**Acceptance.** [ ] badges/chips render for capture/archived/superseded/evidence/volatility/corroborations;
[ ] active non-capture rows visually unchanged; [ ] `just check` green; [ ] no inline magic colours
(use badge variants or a shared map).

**Gotchas.** Keep it compact — rows are dense. Don't show `evidence:"inferred"`/`volatility:"slow"`
(the defaults) as loud chips; render defaults subtly or omit (decide and be consistent).

---

### F2 · Filters (default-hide cold/raw; reveal with counts)

**Intent.** The default memory list is "what the brain will actually inject": `lifecycle=active`,
non-capture. Two toggles reveal captures and archived, each with a count so nothing is silently hidden.

**Exact changes.**
- `BrainPage.tsx`: add component-local `const [showArchived,setShowArchived]=useState(false)` and
  `const [showCaptures,setShowCaptures]=useState(false)` (component state, not the store — mirrors the
  existing `filter` text state; keeps the stable-selector rule intact). Thread them into the `groups`
  and `graphMemories` useMemos: by default drop `source==="capture"` and `lifecycle==="archived"`;
  include them when the matching toggle is on. Render two toggle chips in the toolbar (near the
  list/graph view switch) showing the hidden counts (e.g. "Captures (12)", "Archived (3)") derived
  from a `useMemo` over `memories`.
- Keep `reviewQueue` unaffected (review is its own surface).

**Behavioural contract.** Default view excludes captures + archived. Toggling shows them (badged via
F1). The graph view's `graphMemories` excludes captures + archived by default too (consistent with
the backend graph, which already drops captures + archived).

**Tests.** `BrainPage` test: with both toggles off, a capture and an archived record are absent from
the rendered list and the toolbar shows their counts; toggling reveals them.

**Acceptance.** [ ] default list hides captures + archived; [ ] toggles reveal them with counts;
[ ] graphMemories excludes captures + archived by default; [ ] filter state is component-local (no
store selector returns a fresh array); [ ] `just check` green.

**Gotchas.** The text `filter` and the new toggles compose (apply both). Don't move filtering into a
zustand selector (would violate the stable-ref rule).

---

### F3 · Per-fact Restore (un-archive) action

**Intent.** Let a human pull an archived fact back into the live set from the UI — the cold tier is
restorable. Backend already un-archives on edit (`Update`); add a dedicated, no-edit restore path.

**Exact changes (backend).**
- `brain.Service`: add `Restore(ctx, id) (memory.Record, error)` — `Get` → if `IsArchived`, set
  `Lifecycle=active` + `LastUsedAt=now` (mirror the `Update` un-archive branch) → `Put` under `s.mu`
  (reuse `mutate`); a non-archived id is a no-op returning the record. (Goes through the normal Put
  path, so the cachestore stays consistent — unlike F4 snapshot-restore.)
- `http.go`: `HandleRestore POST /api/brain/memories/{id}/restore` → `Service.Restore` → `toDTO`.
- `server.go:330-343` block: register the route.
**Exact changes (frontend).**
- `brain-api.ts`: `restoreMemory(id): Promise<Memory>` (POST `/memories/{id}/restore`).
- `brain-store.ts`: `restore: (id) => Promise<void>` (calls `restoreMemory`, `upsert`s the result).
- `BrainPage.tsx`: on an archived row (shown when "show archived" is on), a **Restore** button
  (lucide `ArchiveRestore`/`Undo2`) → `store.restore(id)`.

**Behavioural contract.** Restore flips a single archived fact to active and restarts its disuse
clock, so it re-enters recall; nothing else changes; idempotent on an active fact.

**Tests.** Backend: `TestService_Restore` (archived → active + LastUsedAt bumped; active → unchanged).
HTTP: restore endpoint returns the active record. Frontend: archived row shows Restore; clicking calls
the store action and the row leaves the archived set.

**Acceptance.** [ ] `Service.Restore` under `s.mu`, archived→active+clock; [ ] route + api + store +
button; [ ] `go test -race -short` + `just check` green; [ ] doc comment + `docs/brain-memory.md` REST
section updated.

**Gotchas.** Restore is a normal write (cachestore-consistent). Do NOT route it through the
snapshot-restore path. `mutate` already takes `s.mu` (M4 review fix) — reuse it.

---

### F4 · Snapshots panel (list / take / restore)

**Intent.** Surface brain snapshots (today CLI-only) in the UI: list them, take one on demand, and
roll the whole brain back. This is an **admin/destructive** surface — guard it.

**Exact changes (backend).**
- `brain.Service`: `ListSnapshots() ([]SnapshotInfo, error)` and `RestoreSnapshot(id string) error`
  wrapping `brain.ListSnapshots(s.dir)` / `brain.Restore(s.dir, id, s.snapshotRetain)`. **Critical:**
  a snapshot restore rewrites files *underneath the live cachestore*, which write-invalidates only on
  its own `Put`/`Delete` — so after `brain.Restore`, the Service MUST force a cache rebuild. Add an
  `Invalidate()` method to `cachestore` (it already has an unexported `invalidate()`; expose it) and
  call it from `RestoreSnapshot` after the restore. (Without this, the UI would show stale memories
  until the next write/restart — the M1 "restore is offline-only" caveat.)
- `http.go`: `HandleListSnapshots GET /api/brain/snapshots` → `{id,createdAt,files,bytes}[]`;
  `HandleCreateSnapshot POST /api/brain/snapshots` → the new `SnapshotInfo`; `HandleRestoreSnapshot
  POST /api/brain/snapshots/{id}/restore` → 204 + a follow-up `EventBrainUpdated` broadcast so all
  tabs refetch.
- `server.go`: register the three routes.
**Exact changes (frontend).**
- `brain-api.ts`: `Snapshot{id,createdAt,files,bytes}` type + `listSnapshots()`, `createSnapshot()`,
  `restoreSnapshot(id)`.
- `BrainPage.tsx` (or a new `BrainSnapshots.tsx` modal opened from a toolbar button): list snapshots
  (newest first, with file/byte counts + relative time), a "Take snapshot" button, and per-row
  "Restore" with a **confirm dialog** ("This rolls the entire brain back to <ts>. A safety snapshot
  is taken first."). After restore, the `EventBrainUpdated` push refetches the list.

**Behavioural contract.** Taking a snapshot is non-destructive; restoring writes a pre-restore safety
snapshot then makes the tree match the chosen id and invalidates the live cache so the UI reflects it
immediately. Pinned/human/archived are all restored verbatim (snapshot is byte-level).

**Tests.** Backend: `TestService_RestoreSnapshot_InvalidatesCache` — seed, snapshot, mutate (via the
Service so the cache is warm), restore, assert `Service.List` reflects the restored tree (not the
stale cache). HTTP round-trip for the three endpoints. Frontend: snapshot list renders; restore shows
the confirm and calls the api.

**Acceptance.** [ ] `cachestore.Invalidate()` exposed + called post-restore (the load-bearing fix);
[ ] three endpoints + Service methods + api + panel with confirm; [ ] restore broadcasts
EventBrainUpdated; [ ] `go test -race -short` + `just check` green; [ ] docs updated.

**Gotchas.** **The cachestore staleness is the whole risk** — a restore that doesn't invalidate the
cache silently lies to the UI. Keep the confirm dialog (irreversible-feeling). Don't allow restore
while a consolidation job is running (check the store's job state or just warn).

---

### F5 · Graph: typed relations + lifecycle filter + evidence/volatility colour (Band 3 E4)

**Intent.** The knowledge graph reads the new labels: typed relations as distinct edges, a lifecycle
filter, and colour-by evidence/volatility.

**Exact changes (`frontend/src/lib/brain-graph-model.ts` + the two graph components).**
- `BrainEdgeKind`: add the typed-relation kinds (`supersedes|contradicts|duplicates|generalizes|
  corroborates`) — or add a single `typed` kind carrying the relation `type` for styling. In
  `buildBrainModel`, emit a link per `m.relations` entry (in addition to / instead of the untyped
  `related` backbone), styled distinctly (e.g. `contradicts` red dashed, `supersedes` arrowed).
- `BrainColorBy`: add `"evidence"` and `"volatility"`; extend `toNode` + the colour map so nodes can
  be coloured by evidence tier or volatility. Surface `lifecycle`, `evidence`, `volatility`,
  `corroborations` on `BrainNode` for hover/detail.
- Lifecycle filter: exclude `lifecycle==="archived"` nodes by default (the graph already gets the
  F2-filtered `graphMemories`, so this may be free); add a control to *dim* `superseded` nodes.
- `BrainGraph.tsx` / `BrainGraph3D.tsx`: render the new edge styles + colour-by options; add the new
  fields to the node hover/detail panel.

**Behavioural contract.** With no typed relations present (today's data — the churn hasn't populated
`relations` yet, that's Band 2 C3), the graph looks as it does now (typed-edge code is a no-op on
empty `relations`). Colour-by evidence/volatility is opt-in via the existing colour-by control.

**Tests.** Extend `__tests__/BrainGraph.test.tsx`: `buildBrainModel` emits a typed link for a record
with a `relations` entry; archived nodes are excluded; colour-by evidence assigns the expected bucket.

**Acceptance.** [ ] typed-relation edges from `relations[]`; [ ] colour-by evidence/volatility;
[ ] archived excluded / superseded dimmable; [ ] new fields in hover/detail; [ ] graph unchanged when
`relations` is empty; [ ] `just check` + graph tests green.

**Gotchas.** Respect the O(n²) `similar`-edge guards (`SIM_MAX_NODES`/`SIM_DEGREE_CAP`) — typed
relations are explicit edges, cheap; don't add another O(n²) pass. Keep the 3D view's
position-carry-across-refetch behaviour.

---

### F6 · Brain-health report (Band 3 E2)

**Intent.** A small dashboard answering "what state is the brain in?" — counts that make the Band 1
pipeline legible: how many captures await promotion, how much is archived, the evidence/volatility/
confidence distribution, the review backlog.

**Exact changes (backend).**
- `http.go HandleStatus`: extend the response beyond `{semantic}` with a `counts` object computed
  from `Service.List(ctx)`: `total`, `byLifecycle{active,superseded,archived}`, `bySource{human,
  agent,consolidated,capture}`, `byEvidence{...}`, `byVolatility{...}`, `byConfidenceTier{extracted,
  inferred,ambiguous}`, `reviewQueue` (count with a non-empty `reviewNote`), `corroboratedTotal`.
  Keep it a cheap single-pass aggregation. (No new endpoint — extend the existing one.)
**Exact changes (frontend).**
- `brain-api.ts`: extend `BrainStatus` with the `counts` shape; `getStatus` already returns it.
- `brain-store.ts`: keep `status`/`counts` in state (load with `load()`; refresh on `onBrainUpdated`).
- `BrainPage.tsx`: a compact header strip or a "Health" popover showing the key numbers
  (captures-pending, archived, review-queue, evidence/volatility mini-distribution). Reuse the
  existing toolbar count style (`tabular-nums`, muted).

**Behavioural contract.** Read-only; the strip updates on the `brain.updated` event (debounced
refetch already exists). No counts surface costs.

**Tests.** Backend: `TestHandleStatus_Counts` over a seeded corpus (one of each tier). Frontend:
the health strip renders the counts from a mocked status.

**Acceptance.** [ ] status endpoint returns the counts; [ ] BrainStatus + store + health UI;
[ ] refreshes on brain.updated; [ ] `go test -race -short` + `just check` green; [ ] docs updated.

**Gotchas.** Single-pass aggregation — don't N+1 the store. This is the natural surface to later show
the Band-2 Curator's per-pass changelog; structure the component so a "recent churn" list can slot in.

---

## 6. Sequencing & dependencies

```
F0 (wire types — KEYSTONE, everything reads the new fields)
 ├─ F1 (badges/chips)            ─┐
 ├─ F2 (filters)                  ├─ list surface; F1+F2 land together (filters need lifecycle)
 ├─ F3 (per-fact restore)         │  (backend Restore endpoint; independent)
 ├─ F5 (graph: relations/filter)  │  (needs F0's relations/lifecycle)
 └─ F6 (health report)            ┘  (needs F0; extends status)
F4 (snapshots panel) — independent backend-heavy admin task; can land any time after F0
```
- **F0 first** — no field is on the wire until it lands; all others read it.
- **F1 + F2 together** — badges are most useful with the filters (and vice-versa); ship as one list-surface commit or two adjacent ones.
- **F3 / F4 / F5 / F6 are mutually independent** after F0. F4 (snapshots) is the riskiest (cachestore invalidation) — do it deliberately. F5/F6 are the Band-3 items.
- Each task is one commit; each is independently shippable and behind the existing Brain tab.

---

## 7. Cross-cutting

- **Wire sync:** the ONLY contract that spans backend+frontend is `memoryDTO` ↔ `Memory` (F0). Every
  later task that adds a field (e.g. snapshot DTO, status counts) edits BOTH the Go DTO and the TS type
  by hand. No `just typegen` for brain.
- **New endpoints (all under `/api/brain`, registered `server.go:330-343`):** `POST /memories/{id}/restore`
  (F3); `GET /snapshots`, `POST /snapshots`, `POST /snapshots/{id}/restore` (F4); `GET /status` extended
  (F6). Each via `httperror.HandlerFunc(bh.HandleX)`.
- **New Service methods:** `Restore` (F3), `ListSnapshots`/`RestoreSnapshot` (F4) — and `cachestore.Invalidate()`.
- **Live updates:** the `brain.updated` WS event + `onBrainUpdated`'s debounced refetch already exist;
  F3/F4 should broadcast `EventBrainUpdated` after a mutation so all tabs refresh (the per-memory
  actions already upsert locally; snapshot-restore must broadcast).
- **Tests:** backend `go test ./... -race -short`; frontend vitest in the brain `__tests__` dirs; `just
  check` is the hard gate. The 3D graph is hard to unit-test — cover `buildBrainModel` (pure) and the
  2D `BrainGraph` instead.
- **Docs:** update `docs/brain-memory.md` (REST API list + a "Brain tab" UI subsection) and tick the
  Band-3 frontend items in `docs/brain-evolution-plan.md` (E2/E4) as they ship.

---

## 8. Definition of done (Brain UI caught up to Band 1 + Band 3 frontend)

- [ ] **Visible:** every memory row shows its tier — capture/archived/superseded badges,
  evidence/volatility chips, corroboration count (F0+F1); the new fields are on the wire and in `Memory`.
- [ ] **Filterable:** default list = live injectable facts only; captures + archived are toggle-revealable
  with counts (F2); the graph excludes them by default.
- [ ] **Manageable:** a human can restore a single archived fact (F3) and list/take/restore brain
  snapshots from the UI, with the live cache correctly invalidated on restore (F4).
- [ ] **Graph (Band 3 E4):** typed relations render as distinct edges, archived is filtered, colour-by
  gains evidence/volatility, new labels show in hover (F5).
- [ ] **Health (Band 3 E2):** a status report surfaces the lifecycle/source/evidence/volatility/
  confidence distributions, capture backlog, and review-queue size (F6).
- [ ] Every commit passed `cd backend && go test ./... -count=1 -race -short` (for backend changes) and
  `just check`; costs stay hidden; `memoryDTO`↔`Memory` kept in hand-sync throughout.

**Out of scope (later / other bands):** the Band-2 Curator and its per-pass changelog (the health
panel is built to slot it in later); a dedicated captures-pipeline view; typed-relation *editing* from
the UI (relations are churn-populated, read-only here); `backfill-labels` from the UI (a one-time
admin CLI op). Snapshot restore remains a guarded admin action, not an everyday undo.
