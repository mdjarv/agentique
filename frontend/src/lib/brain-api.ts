import { throwIfNotOk } from "~/lib/http";

const BASE = "/api/brain";

// Memory is the HAND-WRITTEN mirror of `memoryDTO` in backend/internal/brain/http.go.
// Brain wire types are NOT in the typegen registry — `just typegen` does not touch them.
// Every field here must match a memoryDTO json tag exactly; edit both files together.
// See brain-ui-spec.md §3.
export interface Memory {
  id: string;
  scope: string;
  text: string;
  category: string;
  source: string;
  pinned: boolean;
  locked: boolean;
  uses: number;
  // Helped counts confirmed-useful outcomes (MemoryUsed) — a stronger signal than a bare
  // injection (uses). Distinct from corroborations (independent re-observations).
  helped?: number;
  createdAt: string;
  updatedAt: string;
  derivedFrom?: string[];
  related?: string[];
  // Derived topic-cluster id within the scope (set by consolidation). Scope-local:
  // only comparable among memories of the same scope.
  community?: number;
  // Cross-scope topic "area" this fact belongs to (AssignAreas) — a readable label
  // comparable across the whole brain; empty when the fact is single-scope.
  area?: string;
  // Confidence tier (extracted | inferred | ambiguous) + its 0..1 score (RFC P2).
  confidence?: string;
  confidenceScore?: number;
  // Set when a fact was flagged contradicted on recall (RFC-LD D2): the reason it
  // needs review. Present → the fact belongs in the review queue regardless of score.
  reviewNote?: string;
  // The per-project facts a cross-scope promotion merged into this one (RFC P5),
  // snapshotted before the originals were deleted — the merge inputs for review.
  subsumed?: { scope: string; text: string }[];

  // --- Band-1 controlled-vocabulary labels (mirrors memoryDTO; see labels.go). The
  // list/graph surfaces read these for tier badges, chips, filters, and typed edges.
  // lifecycle/evidence/volatility are always set server-side; typed as optional only
  // because an older backend (pre-F0) wouldn't send them.
  // Lifecycle: active = live/injectable, superseded = replaced, archived = cold tier.
  lifecycle?: "active" | "superseded" | "archived";
  // Evidence (trust source) — one of EVIDENCE_VALUES.
  evidence?: string;
  // Volatility (decay rate) — one of VOLATILITY_VALUES.
  volatility?: string;
  // Corroborations counts independent re-observations (distinct from uses/helped).
  corroborations?: number;
  // Typed link graph (supersedes/contradicts/duplicates/generalizes/corroborates); the
  // untyped `related` stays for back-compat. Churn-populated (Band 2) — empty today.
  relations?: { type: string; target: string }[];
  // Free-form recall hints; no logic branches on them.
  keywords?: string[];
  // When a churn/human last reviewed the fact; absent when never curated.
  lastCurated?: string;
  // Free-form human annotation.
  curatorNote?: string;
}

// Controlled-vocabulary enums (mirror memory/labels.go), exported for badges/filters so
// the strings live in one place. Order is rough trust/stability tiers (strongest first).
export const EVIDENCE_VALUES = [
  "user_stated",
  "code_verified",
  "corroborated",
  "inferred",
  "observed_once",
] as const;
export const VOLATILITY_VALUES = ["evergreen", "slow", "ephemeral"] as const;
export const LIFECYCLE_VALUES = ["active", "superseded", "archived"] as const;

// NEEDS_CONFIRMATION_SCORE mirrors the backend's NeedsConfirmationScore: facts at or
// below it (and not pinned/locked/human) are the ones the brain offers up to confirm.
export const NEEDS_CONFIRMATION_SCORE = 0.65;

// needsConfirmation reports whether a memory is a candidate for the "confirm what I'm
// unsure about" UX: a non-protected, low-confidence fact.
export function needsConfirmation(m: Memory): boolean {
  return (
    !m.pinned &&
    !m.locked &&
    m.source !== "human" &&
    (m.confidenceScore ?? 1) <= NEEDS_CONFIRMATION_SCORE
  );
}

// inReviewQueue is the predicate for the dedicated review surface: a low-confidence
// fact, OR any fact explicitly flagged contradicted (reviewNote) — even a protected
// one, since a flagged ground-truth fact still warrants a human look.
export function inReviewQueue(m: Memory): boolean {
  return needsConfirmation(m) || !!m.reviewNote;
}

export interface BrainStatus {
  semantic: boolean;
}

export interface SearchResult {
  pinned: Memory[];
  recalled: Memory[];
}

export interface ConsolidateReport {
  scope: string;
  promoted: Memory[] | null;
  rewritten: { before: Memory; after: Memory }[] | null;
  abstracted: Memory[] | null;
  deleted: Memory[] | null;
  decayed: Memory[] | null;
  capturesConsumed: string[] | null;
  skipped: boolean;
  reorgRefused: boolean;
}

export async function getStatus(): Promise<BrainStatus> {
  const res = await fetch(`${BASE}/status`);
  await throwIfNotOk(res, "Failed to load brain status");
  return res.json();
}

export async function listMemories(scope?: string): Promise<Memory[]> {
  const q = scope ? `?scope=${encodeURIComponent(scope)}` : "";
  const res = await fetch(`${BASE}/memories${q}`);
  await throwIfNotOk(res, "Failed to list memories");
  return res.json();
}

export interface CreateMemoryInput {
  scope?: string;
  text: string;
  category?: string;
}

export async function createMemory(input: CreateMemoryInput): Promise<Memory> {
  const res = await fetch(`${BASE}/memories`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  await throwIfNotOk(res, "Failed to create memory");
  return res.json();
}

export async function updateMemory(
  id: string,
  input: { text?: string; category?: string },
): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  await throwIfNotOk(res, "Failed to update memory");
  return res.json();
}

export async function deleteMemory(id: string): Promise<void> {
  const res = await fetch(`${BASE}/memories/${id}`, { method: "DELETE" });
  await throwIfNotOk(res, "Failed to delete memory");
}

export async function setPinned(id: string, pinned: boolean): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/pin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pinned }),
  });
  await throwIfNotOk(res, "Failed to update pin");
  return res.json();
}

export async function setLocked(id: string, locked: boolean): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/lock`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ locked }),
  });
  await throwIfNotOk(res, "Failed to update lock");
  return res.json();
}

// confirmMemory accepts a low-confidence fact as ground truth (the confirm UX): it
// becomes human-authored/EXTRACTED and is thereafter protected from consolidation.
export async function confirmMemory(id: string): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/confirm`, { method: "POST" });
  await throwIfNotOk(res, "Failed to confirm memory");
  return res.json();
}

// flagMemory marks a fact as contradicted (RFC-LD D2): weakens it into the review
// band and records an optional reason. Mirrors the agent MemoryFlag tool — used by the
// "Outdated" action on a recalled-memory card.
export async function flagMemory(id: string, reason?: string): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/flag`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ reason: reason ?? "" }),
  });
  await throwIfNotOk(res, "Failed to flag memory");
  return res.json();
}

// restoreMemory un-archives a cold-tier fact, pulling it back into the live (injectable)
// set and restarting its disuse clock so it re-enters recall. Idempotent on a non-archived
// fact. A normal write (cachestore-consistent) — distinct from a snapshot restore.
export async function restoreMemory(id: string): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/restore`, { method: "POST" });
  await throwIfNotOk(res, "Failed to restore memory");
  return res.json();
}

// refineMemory asks the model to rewrite a fact per an instruction (informed by the
// sources it was merged from) and returns the DRAFT text. It writes nothing — the
// caller shows the draft and the user decides whether to save it.
export async function refineMemory(
  id: string,
  body: { text: string; instruction: string; model: string },
): Promise<string> {
  const res = await fetch(`${BASE}/memories/${id}/refine`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  await throwIfNotOk(res, "Failed to refine memory");
  const data = (await res.json()) as { text: string };
  return data.text;
}

// GraphNode is a memory enriched with its structural-graph centrality (RFC P2):
// degree (load-bearing "god nodes") and normalized betweenness (bridge facts).
export interface GraphNode extends Memory {
  degree: number;
  betweenness: number;
}

// GraphReport is the derived "what the brain knows" panel. Each list holds node ids
// resolved against GraphData.nodes.
export interface GraphReport {
  godNodes: string[];
  bridges: string[];
  needsConfirmation: string[];
  isolated: string[];
  // Well-established facts that have gone cold (high storage, low retrieval) — the
  // spaced-review queue (RFC-LD D6): resurface before disuse decays them.
  dueForReview: string[];
  // Similar-but-not-duplicate fact pairs an agent could conflate (RFC-LD D5). Each
  // references two node ids; the client resolves them against GraphData.nodes.
  interference: { a: string; b: string; similarity: number }[];
}

// GraphLink is a backend-supplied relationship between two nodes (by id). Currently semantic-
// similarity edges (each fact's nearest neighbours in embedding space); present only in semantic
// mode. The client force layout self-balances them — the backend sends relationships, not positions.
export interface GraphLink {
  source: string;
  target: string;
  kind: string;
  // Cosine similarity that produced a semantic edge (omitted/0 for non-semantic edges). The
  // graph weights both the layout force and the edge's visual strength by it.
  score?: number;
}

// GraphTuning carries the force-layout curve parameters (deployment-configurable via the
// backend's [brain.graph] config) the graph applies to its d3 simulation. Absent fields fall
// back to the component's built-in defaults, so an old backend or a partial payload still works.
export interface GraphTuning {
  // A similar edge's link force is linkStrengthBase + linkStrengthSpan·weight (weight ∈ [0,1]).
  linkStrengthBase: number;
  linkStrengthSpan: number;
  // A similar edge's rest length is linkDistanceBase − linkDistanceSpan·weight.
  linkDistanceBase: number;
  linkDistanceSpan: number;
  // Radial pull toward the origin that keeps isolated facts from flinging out.
  gravity: number;
}

export interface GraphData {
  nodes: GraphNode[];
  // Backend relationships (semantic-similarity edges); empty/omitted in lexical mode, where the
  // client falls back to computing lexical similarity edges itself.
  links?: GraphLink[];
  report: GraphReport;
  // Force-layout tuning (deployment-configurable); omitted by older backends.
  tuning?: GraphTuning;
}

// getGraph fetches the force-graph payload: every durable memory annotated with
// centrality, plus the derived insight report. Computed server-side, request-time.
export async function getGraph(scope?: string): Promise<GraphData> {
  const q = scope ? `?scope=${encodeURIComponent(scope)}` : "";
  const res = await fetch(`${BASE}/graph${q}`);
  await throwIfNotOk(res, "Failed to load brain graph");
  return res.json();
}

export async function searchMemories(q: string, scope?: string): Promise<SearchResult> {
  const params = new URLSearchParams({ q });
  if (scope) params.set("scope", scope);
  const res = await fetch(`${BASE}/search?${params.toString()}`);
  await throwIfNotOk(res, "Failed to search memories");
  return res.json();
}

export async function consolidate(scope: string): Promise<ConsolidateReport> {
  const res = await fetch(`${BASE}/consolidate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scope }),
  });
  await throwIfNotOk(res, "Failed to consolidate");
  return res.json();
}

// Fact is the id+text+category view of a memory the reorganization model returns.
export interface Fact {
  id: string;
  text: string;
  category: string;
}

// ConsolidationPlan is the model's proposal from a preview. The client holds it
// and posts it back to apply, so the model runs once and apply commits exactly
// what was previewed. Opaque to the UI today; typed so a future "edit before
// apply" feature can mutate it.
export interface ConsolidationPlan {
  scope: string;
  inputFingerprint: string;
  reorganized: Fact[] | null;
  reorganizeRan: boolean;
  reorganizeSkipped: boolean;
  promoted: { text: string; category: string }[] | null;
  captureIds: string[] | null;
}

// ConsolidationJob is the server-side state of a (potentially long) preview that
// runs in the background. Progress and the final {report, plan} arrive over the
// WebSocket — never via the kickoff request — so a request hiccup can't kill the
// model run, and every tab sees the same job.
export interface ConsolidationJob {
  id: string;
  kind: "scope" | "global" | "all";
  scope?: string;
  model?: string;
  phase: "running" | "done" | "error";
  current: number;
  total: number;
  report?: ConsolidateReport;
  plan?: ConsolidationPlan | GlobalConsolidationPlan;
  changes?: number; // aggregate change count for kind "all"
  error?: string;
}

// ConsolidateMode selects the reorganize strategy: "conservative" merges only true
// duplicates; "aggressive" collapses families of granular facts into broad rules.
export type ConsolidateMode = "conservative" | "aggressive";

// startScopePreview kicks off a per-scope preview job and returns its initial
// (running) state. The result arrives over the WS bus. Empty model = deterministic.
// mode picks the reorganize strategy; force re-runs even if the scope is unchanged
// since the last pass (otherwise an already-tidied scope previews as "skipped").
export async function startScopePreview(
  scope: string,
  model: string,
  mode: ConsolidateMode = "conservative",
  force = false,
): Promise<ConsolidationJob> {
  const res = await fetch(`${BASE}/consolidate/preview`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scope, model, mode, force }),
  });
  await throwIfNotOk(res, "Failed to start preview");
  return (await res.json()).job;
}

// startConsolidateAll kicks off a bulk consolidation of every scope (auto-applied). The
// model runs in the background; progress arrives over the WS bus.
export async function startConsolidateAll(model: string): Promise<ConsolidationJob> {
  const res = await fetch(`${BASE}/consolidate/all`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ model }),
  });
  await throwIfNotOk(res, "Failed to start consolidate all");
  return (await res.json()).job;
}

// getConsolidationJob returns the current/most-recent job so a freshly opened tab
// can resync to an in-flight (or just-finished) consolidation.
export async function getConsolidationJob(): Promise<ConsolidationJob | null> {
  const res = await fetch(`${BASE}/consolidate/job`);
  await throwIfNotOk(res, "Failed to load consolidation job");
  return (await res.json()).job;
}

// applyConsolidate applies a previewed plan deterministically (no model call).
// Throws on a 409 stale plan ("the brain changed…") — the caller should re-preview.
export async function applyConsolidate(plan: ConsolidationPlan): Promise<ConsolidateReport> {
  const res = await fetch(`${BASE}/consolidate/apply`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plan }),
  });
  await throwIfNotOk(res, "Failed to apply consolidation");
  return res.json();
}

// GlobalConsolidationPlan is the model's cross-scope promotion proposal: facts to
// lift into global, each naming the per-project copies it subsumes. Held by the
// client and posted back to apply (no second model call).
export interface GlobalConsolidationPlan {
  promotions: { text: string; category: string; subsumes: string[] }[] | null;
  fingerprints: Record<string, string>;
}

// startGlobalPreview kicks off the cross-scope promotion preview as a background
// job (it scans all projects — potentially many model batches). The result arrives
// over the WS bus. Throws 409 if a consolidation is already running.
export async function startGlobalPreview(model: string): Promise<ConsolidationJob> {
  const res = await fetch(`${BASE}/consolidate/global/preview`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ model }),
  });
  await throwIfNotOk(res, "Failed to start global preview");
  return (await res.json()).job;
}

// applyGlobalConsolidate applies a global plan deterministically. 409 if a project
// changed since the preview — the caller should re-preview.
export async function applyGlobalConsolidate(
  plan: GlobalConsolidationPlan,
): Promise<ConsolidateReport> {
  const res = await fetch(`${BASE}/consolidate/global/apply`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plan }),
  });
  await throwIfNotOk(res, "Failed to apply global consolidation");
  return res.json();
}

// Snapshot is the wire shape of a brain snapshot (HAND-SYNCED with snapshotDTO in
// backend/internal/brain/http.go). A snapshot is a byte-level copy of the whole markdown
// brain tree, taken before each churn and on demand; restoring rolls the entire brain back.
export interface Snapshot {
  id: string;
  createdAt: string;
  files: number;
  bytes: number;
}

// listSnapshots returns the brain snapshots, newest-first.
export async function listSnapshots(): Promise<Snapshot[]> {
  const res = await fetch(`${BASE}/snapshots`);
  await throwIfNotOk(res, "Failed to list snapshots");
  return res.json();
}

// createSnapshot takes a brain snapshot on demand (non-destructive).
export async function createSnapshot(): Promise<Snapshot> {
  const res = await fetch(`${BASE}/snapshots`, { method: "POST" });
  await throwIfNotOk(res, "Failed to take snapshot");
  return res.json();
}

// restoreSnapshot rolls the ENTIRE brain back to a snapshot (a safety snapshot is taken
// first, and the live cache is invalidated server-side). Admin/destructive.
export async function restoreSnapshot(id: string): Promise<void> {
  const res = await fetch(`${BASE}/snapshots/${id}/restore`, { method: "POST" });
  await throwIfNotOk(res, "Failed to restore snapshot");
}
