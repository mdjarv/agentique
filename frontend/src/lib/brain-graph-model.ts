// Shared brain-graph data model: turns the raw memory list + backend semantic edges into
// the neutral { nodes, links } the graph views render. The 3D view (BrainGraph3D) builds
// directly off this; the 2D view (BrainGraph) keeps its own react-force-graph-coupled memo
// (it must carry simulation positions across refetches), but both agree on node fields,
// trust tiers, sizing, and the lexical-similarity fallback via the helpers exported here.

import { type GraphLink, inReviewQueue, type Memory } from "~/lib/brain-api";

// A memory optionally carrying its server-computed centrality (present once the graph
// endpoint has loaded; drives node sizing).
export type GraphMemory = Memory & { degree?: number; betweenness?: number };

export type BrainColorBy = "scope" | "community" | "area";
export type BrainEdgeKind = "provenance" | "related" | "similar" | "area";

export interface BrainNode {
  id: string;
  label: string; // short on-canvas caption
  fullText: string; // untruncated memory text (tooltip)
  scope: string;
  scopeLabel: string;
  category: string;
  source: string;
  uses: number;
  pinned: boolean;
  community: number;
  area: string;
  degree: number;
  val: number; // visual size — blends uses + pinned + structural degree
  // Trust tier driving the node's core heat (see coreColor): human = ground truth,
  // review = flagged / low-confidence, normal = inferred.
  trust: "human" | "review" | "normal";
  conf: number; // confidenceScore 0..1 — grades core brightness within "normal"
}

export interface BrainLink {
  source: string; // memory id
  target: string; // memory id
  kind: BrainEdgeKind;
  weight?: number; // normalized [0,1] semantic strength; undefined for structural edges
}

// Lexical-similarity fallback knobs (used only when the backend supplied no semantic edges).
export const SIM_THRESHOLD = 0.18; // min Jaccard to draw a similarity edge
export const SIM_MAX_NODES = 800; // skip the O(n²) pass above this many nodes
export const SIM_DEGREE_CAP = 4; // max similarity edges per node, keeps it from hairballing

const STOPWORDS = new Set(
  "the and for are but not you all any can has have was with this that from they will would there their what when which while into over under more most some such only own same than too very our your".split(
    " ",
  ),
);

export function tokenize(text: string): Set<string> {
  const out = new Set<string>();
  for (const w of text.toLowerCase().split(/[^a-z0-9]+/)) {
    if (w.length >= 3 && !STOPWORDS.has(w)) out.add(w);
  }
  return out;
}

export function jaccard(a: Set<string>, b: Set<string>): number {
  if (a.size === 0 || b.size === 0) return 0;
  const [small, big] = a.size < b.size ? [a, b] : [b, a];
  let inter = 0;
  for (const x of small) if (big.has(x)) inter++;
  return inter / (a.size + b.size - inter);
}

function trustOf(m: Memory): "human" | "review" | "normal" {
  if (m.source === "human" || m.confidence === "extracted") return "human";
  return inReviewQueue(m) ? "review" : "normal";
}

function toNode(m: GraphMemory, labelForScope: (s: string) => string): BrainNode {
  return {
    id: m.id,
    label: m.text.length > 40 ? `${m.text.slice(0, 40)}…` : m.text,
    fullText: m.text,
    scope: m.scope,
    scopeLabel: labelForScope(m.scope),
    category: m.category,
    source: m.source,
    uses: m.uses,
    pinned: m.pinned,
    community: m.community ?? 0,
    area: m.area ?? "",
    degree: m.degree ?? 0,
    val: 3 + Math.min(m.uses, 10) + (m.pinned ? 2 : 0) + Math.min(m.degree ?? 0, 8),
    trust: trustOf(m),
    conf: m.confidenceScore ?? 0.8,
  };
}

// buildBrainModel assembles the node + edge set. Edges:
//   - provenance / related: structural backbone (firm).
//   - area: cross-scope topic clustering hint, only under "by area" colouring.
//   - similar: the backend's semantic kNN (preferred) or a local lexical Jaccard fallback.
export function buildBrainModel(
  memories: GraphMemory[],
  semanticLinks: GraphLink[] | null | undefined,
  opts: { colorBy: BrainColorBy; labelForScope: (s: string) => string; showSimilar?: boolean },
): { nodes: BrainNode[]; links: BrainLink[] } {
  const { colorBy, labelForScope, showSimilar = true } = opts;
  const nodes = memories.map((m) => toNode(m, labelForScope));

  const idSet = new Set(memories.map((m) => m.id));
  const links: BrainLink[] = [];
  const seen = new Set<string>();
  const structural = new Set<string>(); // pairs with a real edge — suppresses a `similar` dupe
  const pk = (a: string, b: string) => (a < b ? `${a}|${b}` : `${b}|${a}`);

  const addLink = (a: string, b: string, kind: BrainEdgeKind, weight?: number) => {
    if (a === b || !idSet.has(a) || !idSet.has(b)) return;
    const key = `${kind}#${pk(a, b)}`;
    if (seen.has(key)) return;
    seen.add(key);
    links.push({ source: a, target: b, kind, weight });
    if (kind !== "similar") structural.add(pk(a, b));
  };

  for (const m of memories) {
    for (const d of m.derivedFrom ?? []) addLink(m.id, d, "provenance");
    for (const r of m.related ?? []) addLink(m.id, r, "related");
  }

  // Cross-scope area edges (only when colouring by area): star-link each area's members to
  // a representative so the layout pulls a topic spanning projects together.
  if (colorBy === "area") {
    const areaHub = new Map<string, string>();
    for (const m of memories) {
      if (!m.area) continue;
      const hub = areaHub.get(m.area);
      if (hub === undefined) {
        areaHub.set(m.area, m.id);
        continue;
      }
      addLink(m.id, hub, "area");
    }
  }

  if (showSimilar) {
    if (semanticLinks && semanticLinks.length > 0) {
      // Min-max normalize cosine scores so the strongest reads as weight 1, the weakest ~0.
      let lo = Number.POSITIVE_INFINITY;
      let hi = Number.NEGATIVE_INFINITY;
      for (const l of semanticLinks) {
        const s = l.score ?? 0;
        if (s < lo) lo = s;
        if (s > hi) hi = s;
      }
      const range = hi - lo;
      for (const l of semanticLinks) {
        if (structural.has(pk(l.source, l.target))) continue;
        const w = range > 0 ? ((l.score ?? lo) - lo) / range : 0.5;
        addLink(l.source, l.target, "similar", w);
      }
    } else if (memories.length <= SIM_MAX_NODES) {
      const toks = memories.map((m) => tokenize(m.text));
      const cands: { a: string; b: string; s: number }[] = [];
      for (let i = 0; i < memories.length; i++) {
        const mi = memories[i];
        const ti = toks[i];
        if (!mi || !ti) continue;
        for (let j = i + 1; j < memories.length; j++) {
          const mj = memories[j];
          const tj = toks[j];
          if (!mj || !tj) continue;
          const s = jaccard(ti, tj);
          if (s >= SIM_THRESHOLD && !structural.has(pk(mi.id, mj.id))) {
            cands.push({ a: mi.id, b: mj.id, s });
          }
        }
      }
      cands.sort((x, y) => y.s - x.s);
      const deg = new Map<string, number>();
      for (const c of cands) {
        const da = deg.get(c.a) ?? 0;
        const db = deg.get(c.b) ?? 0;
        if (da >= SIM_DEGREE_CAP || db >= SIM_DEGREE_CAP) continue;
        addLink(c.a, c.b, "similar");
        deg.set(c.a, da + 1);
        deg.set(c.b, db + 1);
      }
    }
  }

  return { nodes, links };
}
