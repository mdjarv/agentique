import { forceCollide, forceX, forceY } from "d3-force";
import { Check, ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import ForceGraph2D, {
  type ForceGraphMethods,
  type LinkObject,
  type NodeObject,
} from "react-force-graph-2d";
import type { GraphLink, GraphReport, GraphTuning, Memory } from "~/lib/brain-api";
import { areaColor, communityColor, scopeColor } from "~/lib/scope-color";

// GraphMemory is a memory optionally carrying its server-computed centrality. When
// the graph endpoint has loaded, degree/betweenness are present and drive node
// sizing and the insights panel; before then the component degrades to the plain list.
type GraphMemory = Memory & {
  degree?: number;
  betweenness?: number;
};

// BrainGraph renders the brain as an Obsidian-style force-directed graph that *self-balances*:
// the backend supplies nodes and relationships (never positions) and the force simulation lays
// them out. Edges come from:
//   - provenance: `derivedFrom` links written by consolidation (solid).
//   - related:    curated `[[link]]` graph (solid) — empty until P1 of the RFC.
//   - similar:    semantic-similarity edges from the backend (each fact's nearest neighbours in
//                 embedding space) when in semantic mode; otherwise a lexical Jaccard fallback
//                 computed here. These are what pull related memories into organic clusters.
// Isolated memories (no edge) literally float free — the "dead link graph" made visible.

interface NodeData {
  id: string;
  label: string;
  // fullText is the untruncated memory text, shown in the hover tooltip (label is the
  // short on-canvas caption).
  fullText: string;
  scope: string;
  scopeLabel: string;
  category: string;
  source: string;
  uses: number;
  pinned: boolean;
  community: number;
  area: string;
  degree: number;
  val: number;
  // The per-project facts a cross-scope promotion merged into this one, shown on hover
  // (the merge inputs the Subsumed backfill restored). Empty for non-promoted facts.
  subsumed?: { scope: string; text: string }[];
}

type ColorBy = "scope" | "community" | "area";

type EdgeKind = "provenance" | "related" | "similar" | "area";
interface LinkData {
  kind: EdgeKind;
  // weight ∈ [0,1] is the normalized association strength for a semantic edge (from cosine
  // similarity, min-max scaled across the current edge set); undefined for structural edges,
  // which are treated as firm (weight 1). Drives both force strength and visual emphasis.
  weight?: number;
}

type GNode = NodeObject<NodeData>;
type GLink = LinkObject<NodeData, LinkData>;

const NODE_REL_SIZE = 4;
const SIM_THRESHOLD = 0.18; // min Jaccard to draw a similarity edge
const SIM_MAX_NODES = 800; // skip the O(n^2) pass above this many nodes
const SIM_DEGREE_CAP = 4; // max similarity edges per node, keeps it from hairballing
const REGION_MAX_NODES = 4000; // skip the per-frame hull pass above this many nodes (hull is ~n log n, cheap)
const REGION_PAD = 12; // world-unit breathing room around a region's outermost nodes

// Force-layout curve defaults — used when the backend graph payload carries no `tuning` (older
// backend, lexical mode, or a partial response). The backend's [brain.graph] config can override
// each of these; keep these in sync with brain's DefaultGraph* constants. A similar edge's link
// strength is base + span·weight and its distance is base − span·weight (weight ∈ [0,1]).
const LAYOUT_DEFAULTS: GraphTuning = {
  linkStrengthBase: 0.04,
  linkStrengthSpan: 0.32,
  linkDistanceBase: 90,
  linkDistanceSpan: 55,
  gravity: 0.045,
};

const STOPWORDS = new Set(
  "the and for are but not you all any can has have was with this that from they will would there their what when which while into over under more most some such only own same than too very our your".split(
    " ",
  ),
);

function nodeColor(node: NodeData, colorBy: ColorBy): string {
  if (colorBy === "area") return areaColor(node.area);
  if (colorBy === "community") return communityColor(node.scope, node.community);
  return scopeColor(node.scope);
}

function tokenize(text: string): Set<string> {
  const out = new Set<string>();
  for (const w of text.toLowerCase().split(/[^a-z0-9]+/)) {
    if (w.length >= 3 && !STOPWORDS.has(w)) out.add(w);
  }
  return out;
}

function jaccard(a: Set<string>, b: Set<string>): number {
  if (a.size === 0 || b.size === 0) return 0;
  const [small, big] = a.size < b.size ? [a, b] : [b, a];
  let inter = 0;
  for (const x of small) if (big.has(x)) inter++;
  return inter / (a.size + b.size - inter);
}

function endId(e: string | number | GNode | undefined): string {
  if (e == null) return "";
  return typeof e === "object" ? String((e as GNode).id) : String(e);
}

function esc(s: string): string {
  return s.replace(/[&<>]/g, (c) => (c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;"));
}

type Pt = { x: number; y: number };

function cross(o: Pt, a: Pt, b: Pt): number {
  return (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);
}

// convexHull returns the hull of `points` in CCW order (Andrew's monotone chain).
// Fewer than three points have no hull, so they are returned unchanged.
function convexHull(points: Pt[]): Pt[] {
  if (points.length < 3) return points;
  const sorted = points.slice().sort((p, q) => p.x - q.x || p.y - q.y);
  const build = (seq: Pt[]): Pt[] => {
    const chain: Pt[] = [];
    for (const p of seq) {
      while (chain.length >= 2) {
        const a = chain[chain.length - 2];
        const b = chain[chain.length - 1];
        if (!a || !b || cross(a, b, p) > 0) break;
        chain.pop();
      }
      chain.push(p);
    }
    chain.pop(); // last point is the first of the other chain — drop to avoid a dupe
    return chain;
  };
  return build(sorted).concat(build(sorted.slice().reverse()));
}

// withAlpha turns a "#rrggbb" hex into an rgba() string at the given opacity.
function withAlpha(hex: string, alpha: number): string {
  const h = hex.replace("#", "");
  const r = Number.parseInt(h.slice(0, 2), 16);
  const g = Number.parseInt(h.slice(2, 4), 16);
  const b = Number.parseInt(h.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

// INSIGHT_LIST_CAP bounds how many ids one section renders — some lists (notably
// isolated, which can be most of the corpus on a sparse link graph) are unbounded
// server-side. The full size is still shown in the header.
const INSIGHT_LIST_CAP = 12;

// InsightSection renders one click-to-focus list of node ids (god nodes / bridges).
// The list is capped at INSIGHT_LIST_CAP; when more exist the header shows the total
// and a trailing "+N more".
function InsightSection({
  title,
  hint,
  ids,
  labelFor,
  onPick,
}: {
  title: string;
  hint: string;
  ids: string[];
  labelFor: (id: string) => string;
  onPick: (id: string) => void;
}) {
  const shown = ids.slice(0, INSIGHT_LIST_CAP);
  const overflow = ids.length - shown.length;
  return (
    <div>
      <div className="mb-1 font-medium text-muted-foreground" title={hint}>
        {title}
        {ids.length > shown.length && (
          <span className="text-muted-foreground/70"> ({ids.length})</span>
        )}
      </div>
      <ul className="space-y-0.5">
        {shown.map((id) => (
          <li key={id}>
            <button
              type="button"
              className="w-full truncate text-left hover:text-foreground"
              onClick={() => onPick(id)}
              title={labelFor(id)}
            >
              {labelFor(id)}
            </button>
          </li>
        ))}
      </ul>
      {overflow > 0 && <div className="text-muted-foreground/60">+{overflow} more</div>}
    </div>
  );
}

export function BrainGraph({
  memories,
  links: semanticLinks,
  report,
  tuning,
  labelForScope,
  onConfirm,
  compact = false,
  focusId,
}: {
  memories: GraphMemory[];
  // Backend-supplied relationships (semantic-similarity edges); null/empty in lexical mode,
  // where the component falls back to computing lexical Jaccard similarity edges itself.
  links?: GraphLink[] | null;
  report: GraphReport | null;
  // Deployment-configurable force-layout curves; null falls back to LAYOUT_DEFAULTS.
  tuning?: GraphTuning | null;
  labelForScope: (scope: string) => string;
  onConfirm: (id: string) => void;
  // compact hides the controls + legend overlays so the canvas can be embedded as a
  // small focused subgraph (e.g. the isolated neighbourhood in the review surface).
  compact?: boolean;
  // focusId draws an emphasis ring on one node — the fact currently under review.
  focusId?: string;
}) {
  const [showSimilar, setShowSimilar] = useState(true);
  // Regions (the per-scope/area hulls) default OFF: at 1400+ facts the overlapping shaded
  // polygons read as visual noise more than structure. Opt in from the controls when wanted.
  const [showRegions, setShowRegions] = useState(false);
  const [colorBy, setColorBy] = useState<ColorBy>("scope");
  // Insights + legend default COLLAPSED: they're a dense wall of truncated text otherwise. The
  // collapsed insights panel shows a scannable row of count chips; expand for the lists.
  const [insightsOpen, setInsightsOpen] = useState(false);
  const [legendOpen, setLegendOpen] = useState(false);
  const [hoverId, setHoverId] = useState<string | null>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });

  const wrapRef = useRef<HTMLDivElement>(null);
  const fgRef = useRef<ForceGraphMethods<GNode, GLink> | undefined>(undefined);
  const fitted = useRef(false);
  // Carries the live (simulation-mutated) node objects forward across refetches so
  // positions survive a rebuild — keyed by memory id. See the nodes memo below.
  const prevNodesRef = useRef<Map<string, GNode>>(new Map());
  // The last emitted graphData plus its topology signature. We reuse this reference
  // (and only refresh display fields in place) whenever the topology is unchanged, so
  // react-force-graph doesn't reheat — it force-sets alpha(1) on every graphData ref
  // change (force-graph internals), which is what makes the layout jump.
  const graphDataRef = useRef<{ topo: string; data: { nodes: GNode[]; links: GLink[] } } | null>(
    null,
  );

  // Measure the container so the canvas fills it (and resizes with the window).
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const sync = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    sync();
    const ro = new ResizeObserver(sync);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const graphData = useMemo<{ nodes: GNode[]; links: GLink[] }>(() => {
    const prevById = prevNodesRef.current;

    // Carry the simulation state (x/y/vx/vy and any pinned fx/fy) forward by id so a
    // refetch doesn't re-randomize the layout. react-force-graph stores positions on
    // the node objects themselves; rebuilding fresh objects (as we must, to pick up
    // new centrality/use counts) would otherwise drop them and the engine would
    // re-place every node from scratch — the jump that a brain.updated burst causes.
    // Only ids with no previous entry (genuinely new memories) get placed fresh.
    const nodes: GNode[] = memories.map((m) => {
      const node: GNode = {
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
        // Size blends use-count, pinned, and structural degree so load-bearing "god
        // nodes" read bigger — the graphify signal made visual.
        val: 3 + Math.min(m.uses, 10) + (m.pinned ? 2 : 0) + Math.min(m.degree ?? 0, 8),
        subsumed: m.subsumed,
      };
      const prev = prevById.get(m.id);
      if (prev) {
        node.x = prev.x;
        node.y = prev.y;
        node.vx = prev.vx;
        node.vy = prev.vy;
        if (prev.fx != null) node.fx = prev.fx;
        if (prev.fy != null) node.fy = prev.fy;
      }
      return node;
    });

    const idSet = new Set(memories.map((m) => m.id));
    const links: GLink[] = [];
    const seen = new Set<string>(); // dedupe by kind+pair
    const structural = new Set<string>(); // pairs with a real edge, suppresses `similar`
    const pk = (a: string, b: string) => (a < b ? `${a}|${b}` : `${b}|${a}`);

    const addLink = (a: string, b: string, kind: EdgeKind, weight?: number) => {
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

    // Cross-scope area edges — only in "by area" colouring: star-link each area's members
    // to a representative so the force layout pulls a topic spanning projects into one
    // blob (areas come from text similarity, not structural edges, so without this their
    // members scatter). Bounded (size-1 per area). Rendered transparent — the labelled
    // hull + colour carry the area; the edges exist only to cluster the layout.
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

    // Similarity edges — what pull related memories into clusters. Prefer the backend's
    // SEMANTIC edges (embedding kNN): they encode meaning the lexical pass can't and have no
    // node-count cap. Only when the backend supplied none (lexical mode) do we fall back to the
    // local Jaccard pass (itself skipped above SIM_MAX_NODES, where O(n²) is too costly).
    if (showSimilar) {
      if (semanticLinks && semanticLinks.length > 0) {
        // Min-max normalize the cosine scores across this edge set so the strongest relation
        // reads as weight 1 and the weakest (at the backend threshold) as ~0 — independent of
        // the model's absolute cosine scale. Both force pull and visual emphasis scale with it.
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

    // Seed each brand-new node at the centroid of its already-placed neighbours so it
    // eases in from the right region rather than flying in from the origin during the
    // reheat a topology change triggers. Falls back to the engine's default placement
    // when it has no placed neighbour yet.
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      if (!n || n.x != null) continue;
      let sx = 0;
      let sy = 0;
      let cnt = 0;
      for (const l of links) {
        const s = endId(l.source);
        const t = endId(l.target);
        const other = s === n.id ? t : t === n.id ? s : null;
        if (!other) continue;
        const p = prevById.get(other);
        if (p?.x != null && p?.y != null) {
          sx += p.x;
          sy += p.y;
          cnt++;
        }
      }
      if (cnt > 0) {
        // Small index-derived jitter so several new nodes sharing a neighbour don't stack.
        n.x = sx / cnt + ((i % 5) - 2);
        n.y = sy / cnt + ((i % 3) - 1);
      }
    }

    // Topology signature: the node-id set plus the (kind+pair) link set, deliberately
    // excluding every display field. A refetch that only bumps uses/degree/centrality
    // leaves this unchanged, letting us reuse the prior graphData reference below.
    const topo = `${[...idSet].sort().join(",")}#${[...seen].sort().join(",")}`;

    const prev = graphDataRef.current;
    if (prev && prev.topo === topo) {
      // Topology unchanged: refresh display fields on the live (settled) node objects
      // in place and hand back the existing reference. react-force-graph then skips
      // its graphData digest entirely, so the simulation is never reheated — no jump.
      // The next render's nodeCanvasObject closure repaints with the updated values.
      const live = new Map(prev.data.nodes.map((n) => [String(n.id), n]));
      for (const n of nodes) {
        const l = live.get(String(n.id));
        if (!l) continue;
        l.label = n.label;
        l.fullText = n.fullText;
        l.scope = n.scope;
        l.scopeLabel = n.scopeLabel;
        l.category = n.category;
        l.source = n.source;
        l.uses = n.uses;
        l.pinned = n.pinned;
        l.community = n.community;
        l.area = n.area;
        l.degree = n.degree;
        l.val = n.val;
        l.subsumed = n.subsumed;
      }
      return prev.data;
    }

    // Topology changed: emit a fresh reference (surviving nodes keep their positions,
    // so only added/removed nodes drive the reheat) and remember the live objects so
    // the next rebuild can carry their positions forward.
    const data = { nodes, links };
    graphDataRef.current = { topo, data };
    prevNodesRef.current = new Map(nodes.map((n) => [String(n.id), n]));
    return data;
  }, [memories, semanticLinks, labelForScope, showSimilar, colorBy]);

  const { nodes, links } = graphData;

  // Neighbour map for hover highlighting. Built off `links` before the graph
  // engine rewrites source/target into node refs, so endId handles both.
  const adjacency = useMemo(() => {
    const m = new Map<string, Set<string>>();
    const link = (a: string, b: string) => {
      let set = m.get(a);
      if (!set) {
        set = new Set();
        m.set(a, set);
      }
      set.add(b);
    };
    for (const l of links) {
      const s = endId(l.source);
      const t = endId(l.target);
      link(s, t);
      link(t, s);
    }
    return m;
  }, [links]);

  // Tune the self-balancing force layout, weighted by the embeddings: each semantic edge's
  // cosine-derived weight makes a strong association pull HARDER (higher link strength) and sit
  // CLOSER (shorter link distance), so the layout's geometry mirrors how related the memories
  // actually are — a closer model of associative memory than uniform edges. Strong repulsion +
  // collision keep the clusters legible. Structural edges (provenance/related) are firm; area
  // edges are a faint clustering hint. Re-runs/reheats on topology change; `fitted` is cleared
  // so the next settle re-frames. (d3Force → ForceFn; cast through unknown for the setters.)
  // Force-layout curves come from the backend [brain.graph] config (deployment-tunable),
  // falling back to LAYOUT_DEFAULTS when the payload omits them. Resolved in render scope so the
  // effect can depend on the VALUES, not the per-refetch `tuning` object reference — a display-only
  // refetch is a fresh object with identical numbers and must not reheat the settled layout.
  const layout = tuning ?? LAYOUT_DEFAULTS;
  useEffect(() => {
    const g = fgRef.current;
    if (!g || nodes.length === 0) return;
    (g.d3Force("charge") as unknown as { strength(n: number): void } | undefined)?.strength(-160);
    const link = g.d3Force("link") as unknown as
      | { distance(fn: (l: GLink) => number): void; strength(fn: (l: GLink) => number): void }
      | undefined;
    link?.distance((l) => {
      // stronger → closer
      if (l.kind === "similar")
        return layout.linkDistanceBase - layout.linkDistanceSpan * (l.weight ?? 0.5);
      if (l.kind === "area") return 80;
      return 40; // provenance / related: tight structural ties
    });
    link?.strength((l) => {
      // stronger → tighter pull
      if (l.kind === "similar")
        return layout.linkStrengthBase + layout.linkStrengthSpan * (l.weight ?? 0.5);
      if (l.kind === "area") return 0.03;
      return 0.4; // provenance / related: firm
    });
    g.d3Force(
      "collide",
      forceCollide<GNode>()
        .radius((n) => Math.sqrt(n.val ?? 2) * NODE_REL_SIZE + 1.5)
        .iterations(2),
    );
    // Weak radial gravity toward the origin: without it, charge repulsion flings the isolated
    // facts (no edge to hold them) far out, which makes zoomToFit shrink the connected core to
    // an unreadable speck. Gravity keeps the whole graph compact so the clusters fill the frame.
    g.d3Force("x", forceX<GNode>(0).strength(layout.gravity));
    g.d3Force("y", forceY<GNode>(0).strength(layout.gravity));
    fitted.current = false; // re-frame after the new topology settles
    g.d3ReheatSimulation();
  }, [
    nodes,
    layout.linkStrengthBase,
    layout.linkStrengthSpan,
    layout.linkDistanceBase,
    layout.linkDistanceSpan,
    layout.gravity,
  ]);

  // Recoloring / toggling regions doesn't touch graphData, so the settled canvas won't
  // repaint on its own — nudge a redraw when either display dimension changes.
  // biome-ignore lint/correctness/useExhaustiveDependencies: `colorBy`/`showRegions` are trigger-only deps — they're read by the paint callbacks on the next frame; this effect just forces that frame.
  useEffect(() => {
    (fgRef.current as unknown as { refresh?: () => void } | undefined)?.refresh?.();
  }, [colorBy, showRegions]);

  const neighbors = hoverId ? adjacency.get(hoverId) : null;
  const linkTouchesHover = (l: GLink) =>
    hoverId != null && (endId(l.source) === hoverId || endId(l.target) === hoverId);

  const scopeLegend = useMemo(() => {
    const scopes = [...new Set(nodes.map((n) => n.scope))];
    scopes.sort((a, b) => (a === "global" ? -1 : b === "global" ? 1 : a.localeCompare(b)));
    return scopes.map((s) => ({ scope: s, label: labelForScope(s), color: scopeColor(s) }));
  }, [nodes, labelForScope]);

  // Region grouping for the shaded hulls. Membership tracks the active colour dimension:
  // by scope → one region per project (labelled with the project name); by community →
  // one per scope-local topic cluster (colour only); by area → one per cross-scope topic
  // area (labelled with the area's readable name, spanning projects). The live GNode
  // objects are kept so the hull reads their current x/y each frame. Recomputed only on
  // topology/colour change, never per frame.
  const regionGroups = useMemo(() => {
    const groups = new Map<string, { color: string; label: string; nodes: GNode[] }>();
    for (const n of nodes) {
      let key: string;
      let color: string;
      let label: string;
      if (colorBy === "area") {
        if (!n.area) continue; // facts in no cross-scope area form no region
        key = n.area;
        color = areaColor(n.area);
        label = n.area;
      } else if (colorBy === "community") {
        key = `${n.scope}#${n.community}`;
        color = communityColor(n.scope, n.community);
        label = "";
      } else {
        key = n.scope;
        color = scopeColor(n.scope);
        label = labelForScope(n.scope);
      }
      let g = groups.get(key);
      if (!g) {
        g = { color, label, nodes: [] };
        groups.set(key, g);
      }
      g.nodes.push(n);
    }
    return groups;
  }, [nodes, colorBy, labelForScope]);

  const nodeById = useMemo(() => new Map(nodes.map((n) => [String(n.id), n])), [nodes]);

  // focusNode pans+zooms to a node by id (the engine has stamped x/y on the node
  // objects after layout), so the insights lists are clickable shortcuts.
  const focusNode = (id: string) => {
    const n = nodeById.get(id);
    if (n?.x != null && n?.y != null) {
      fgRef.current?.centerAt(n.x, n.y, 500);
      fgRef.current?.zoom(4, 500);
      setHoverId(id);
    }
  };

  return (
    <div ref={wrapRef} className="relative h-full w-full text-foreground">
      {nodes.length < 2 ? (
        <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
          Need at least two memories to draw a graph.
        </div>
      ) : (
        size.w > 0 &&
        size.h > 0 && (
          <ForceGraph2D
            ref={fgRef}
            width={size.w}
            height={size.h}
            graphData={graphData}
            backgroundColor="rgba(0,0,0,0)"
            onRenderFramePre={(ctx, scale) => {
              // Shaded "areas": a translucent hull behind each region's nodes, drawn
              // pre-frame so it sits beneath the links and nodes. Padding/strokes are in
              // world units so a region tracks its nodes through zoom.
              if (!showRegions || compact || nodes.length > REGION_MAX_NODES) return;
              // In "by area" mode the region IS the membership (area edges cluster the
              // members), so wrap every member. In scope/community mode the region wraps
              // only the connected core — isolated facts scatter and would balloon the hull.
              const coreOnly = colorBy !== "area";
              ctx.lineJoin = "round";
              for (const g of regionGroups.values()) {
                const placed: { x: number; y: number; r: number }[] = [];
                let cx = 0;
                let cy = 0;
                let minY = Number.POSITIVE_INFINITY;
                for (const n of g.nodes) {
                  // Isolated facts (no structural links) scatter to the layout's edge;
                  // including them would balloon every hull to span the graph. They stay
                  // outside, floating — the honest "knowledge gap" signal.
                  if (n.x == null || n.y == null) continue;
                  if (coreOnly && (n.degree ?? 0) === 0) continue;
                  const r = Math.sqrt(n.val ?? 2) * NODE_REL_SIZE;
                  placed.push({ x: n.x, y: n.y, r });
                  cx += n.x;
                  cy += n.y;
                  minY = Math.min(minY, n.y);
                }
                if (placed.length === 0) continue;
                cx /= placed.length;
                cy /= placed.length;
                const expand = Math.max(...placed.map((p) => p.r)) + REGION_PAD;

                ctx.fillStyle = withAlpha(g.color, 0.07);
                ctx.strokeStyle = withAlpha(g.color, 0.3);
                ctx.lineWidth = 1.5 / scale;
                ctx.beginPath();
                if (placed.length < 3) {
                  // A point or a pair has no polygon — enclose it in a circle instead.
                  let rad = 0;
                  for (const p of placed) rad = Math.max(rad, Math.hypot(p.x - cx, p.y - cy));
                  ctx.arc(cx, cy, rad + expand, 0, 2 * Math.PI);
                } else {
                  // Push each hull vertex outward from the centroid so the polygon clears
                  // the node circles it wraps.
                  const hull = convexHull(placed).map((v) => {
                    const dx = v.x - cx;
                    const dy = v.y - cy;
                    const d = Math.hypot(dx, dy) || 1;
                    return { x: v.x + (dx / d) * expand, y: v.y + (dy / d) * expand };
                  });
                  let first = true;
                  for (const p of hull) {
                    if (first) {
                      ctx.moveTo(p.x, p.y);
                      first = false;
                    } else {
                      ctx.lineTo(p.x, p.y);
                    }
                  }
                  ctx.closePath();
                }
                ctx.fill();
                ctx.stroke();

                if (g.label && minY !== Number.POSITIVE_INFINITY) {
                  const fontSize = 12 / scale;
                  ctx.font = `600 ${fontSize}px sans-serif`;
                  ctx.textAlign = "center";
                  ctx.textBaseline = "bottom";
                  ctx.fillStyle = withAlpha(g.color, 0.85);
                  ctx.fillText(g.label, cx, minY - expand - fontSize * 0.4);
                }
              }
            }}
            nodeRelSize={NODE_REL_SIZE}
            nodeVal={(n) => n.val ?? 2}
            cooldownTicks={120}
            nodeLabel={(n) => {
              const head = `<div style="font-weight:600;margin-bottom:3px">${esc(n.fullText)}</div><span style="opacity:.6">${esc(n.scopeLabel)} · ${n.category} · used ${n.uses}×</span>`;
              let prov = "";
              if (n.subsumed?.length) {
                const items = n.subsumed
                  .slice(0, 6)
                  .map(
                    (s) =>
                      `<div style="opacity:.75">← ${esc(s.text.length > 64 ? `${s.text.slice(0, 64)}…` : s.text)}</div>`,
                  )
                  .join("");
                const more =
                  n.subsumed.length > 6
                    ? `<div style="opacity:.5">+${n.subsumed.length - 6} more</div>`
                    : "";
                prov = `<div style="margin-top:4px;opacity:.85">merged from ${n.subsumed.length} fact(s):</div>${items}${more}`;
              }
              return `<div style="font:12px sans-serif;max-width:300px">${head}${prov}</div>`;
            }}
            nodeCanvasObject={(node, ctx, scale) => {
              const x = node.x ?? 0;
              const y = node.y ?? 0;
              const r = Math.sqrt(node.val ?? 2) * NODE_REL_SIZE;
              const dim =
                hoverId != null && node.id !== hoverId && !neighbors?.has(String(node.id));
              ctx.globalAlpha = dim ? 0.12 : 1;
              // Dark "halo" moat: a slightly larger disc in the canvas background colour punched
              // under each node, so edges are cut away from the node's rim and the coloured nodes
              // read crisply on top of the link web instead of dissolving into it.
              ctx.beginPath();
              ctx.arc(x, y, r + 1.6 / scale, 0, 2 * Math.PI);
              ctx.fillStyle = "rgb(9,11,17)";
              ctx.fill();
              ctx.beginPath();
              ctx.arc(x, y, r, 0, 2 * Math.PI);
              ctx.fillStyle = nodeColor(node, colorBy);
              ctx.fill();
              if (node.pinned) {
                ctx.strokeStyle = "#facc15";
                ctx.lineWidth = 2 / scale;
                ctx.beginPath();
                ctx.arc(x, y, r + 2 / scale, 0, 2 * Math.PI);
                ctx.stroke();
              }
              if (focusId != null && String(node.id) === focusId) {
                // Emphasis ring on the fact under review (review surface).
                ctx.strokeStyle = "#22d3ee";
                ctx.lineWidth = 2.5 / scale;
                ctx.beginPath();
                ctx.arc(x, y, r + 4 / scale, 0, 2 * Math.PI);
                ctx.stroke();
              }
              // Labels are the main source of clutter at 1400+ nodes, so they are
              // deliberately sparse: the hovered node and its neighbours are always
              // labelled (that's where attention is), and once the user zooms in past
              // ~1.6× every node labels itself (legible at that scale). A dark pill behind
              // the text gives it contrast over nodes and edges instead of vanishing into them.
              const isHover = node.id === hoverId;
              const isNeighbor = neighbors?.has(String(node.id)) ?? false;
              if (!dim && (isHover || isNeighbor || scale > 1.6)) {
                const fontSize = (isHover ? 12.5 : 11) / scale;
                ctx.font = `${isHover ? 600 : 400} ${fontSize}px sans-serif`;
                ctx.textAlign = "center";
                ctx.textBaseline = "top";
                const tw = ctx.measureText(node.label).width;
                const ty = y + r + 3 / scale;
                const padX = 4 / scale;
                const padY = 2 / scale;
                ctx.fillStyle = "rgba(13,16,23,0.82)";
                ctx.beginPath();
                const bx = x - tw / 2 - padX;
                const by = ty - padY;
                const bw = tw + padX * 2;
                const bh = fontSize + padY * 2;
                if (ctx.roundRect) ctx.roundRect(bx, by, bw, bh, 3 / scale);
                else ctx.rect(bx, by, bw, bh);
                ctx.fill();
                ctx.fillStyle = isHover ? "#f8fafc" : "rgba(226,232,240,0.9)";
                ctx.fillText(node.label, x, ty);
              }
              ctx.globalAlpha = 1;
            }}
            nodePointerAreaPaint={(node, color, ctx) => {
              const r = Math.sqrt(node.val ?? 2) * NODE_REL_SIZE;
              ctx.fillStyle = color;
              ctx.beginPath();
              ctx.arc(node.x ?? 0, node.y ?? 0, r, 0, 2 * Math.PI);
              ctx.fill();
            }}
            linkColor={(l) => {
              // Area edges exist only to cluster the layout — invisible unless the hovered
              // node touches one (then they reveal the area's connections).
              if (l.kind === "area") {
                return hoverId != null && linkTouchesHover(l)
                  ? "rgba(250,204,21,0.7)"
                  : "rgba(0,0,0,0)";
              }
              if (hoverId != null) {
                return linkTouchesHover(l) ? "rgba(250,204,21,0.95)" : "rgba(140,140,150,0.05)";
              }
              // Coloured node clusters carry the picture; edges are a faint backbone. A semantic
              // edge's opacity rises STEEPLY with its association strength (weight²), so only the
              // strong relations read while the weak majority fade out of the web entirely — they
              // still shape the layout, just don't clutter it. Edges sharpen on hover.
              if (l.kind === "similar") {
                const w = l.weight ?? 0.5;
                const a = 0.42 * w * w;
                return a < 0.012 ? "rgba(0,0,0,0)" : `rgba(150,180,235,${a.toFixed(3)})`;
              }
              return "rgba(190,195,210,0.28)";
            }}
            linkWidth={(l) =>
              linkTouchesHover(l)
                ? 3.2
                : l.kind === "similar"
                  ? 0.3 + 1.7 * (l.weight ?? 0.5) ** 1.5 // thicker = stronger association
                  : 2
            }
            linkLineDash={(l) =>
              l.kind === "similar" ? [4, 3] : l.kind === "area" ? [2, 4] : null
            }
            onNodeHover={(n) => setHoverId(n ? String(n.id) : null)}
            onNodeClick={(n) => {
              if (n.x != null && n.y != null) {
                fgRef.current?.centerAt(n.x, n.y, 500);
                fgRef.current?.zoom(4, 500);
              }
            }}
            onEngineStop={() => {
              // Fit once, when the layout first settles after (re)mounting the graph
              // view. We never re-frame afterwards, so a memory change or a consolidation can't
              // yank a user who has zoomed/panned back out to the whole-graph view.
              if (!fitted.current) {
                fgRef.current?.zoomToFit(400, 48);
                fitted.current = true;
              }
            }}
          />
        )
      )}

      {/* Controls */}
      {!compact && (
        <div className="absolute right-3 top-3 flex flex-col items-end gap-1.5">
          <label className="flex items-center gap-1.5 rounded-md border bg-card/80 px-2 py-1 text-xs backdrop-blur">
            <input
              type="checkbox"
              checked={showSimilar}
              onChange={(e) => setShowSimilar(e.target.checked)}
            />
            Similarity links
          </label>
          <label
            className="flex items-center gap-1.5 rounded-md border bg-card/80 px-2 py-1 text-xs backdrop-blur"
            title="Shade an area behind each project (or topic cluster, in cluster colouring)"
          >
            <input
              type="checkbox"
              checked={showRegions}
              onChange={(e) => setShowRegions(e.target.checked)}
            />
            Regions
          </label>
          <label className="flex items-center gap-1.5 rounded-md border bg-card/80 px-2 py-1 text-xs backdrop-blur">
            <span className="text-muted-foreground">Color</span>
            <select
              value={colorBy}
              onChange={(e) => setColorBy(e.target.value as ColorBy)}
              className="bg-transparent outline-none"
              title="Colour by scope (project), by scope-local cluster, or by cross-scope topic area"
            >
              <option value="scope">by scope</option>
              <option value="community">by cluster</option>
              <option value="area">by area</option>
            </select>
          </label>
        </div>
      )}

      {/* Insights — graphify analyze.py analogs (RFC P2). Collapsed by default to a chip summary. */}
      {report &&
        (report.godNodes.length > 0 ||
          report.bridges.length > 0 ||
          report.needsConfirmation.length > 0 ||
          report.dueForReview.length > 0 ||
          report.isolated.length > 0 ||
          report.interference.length > 0) && (
          <div className="absolute left-3 top-3 w-64 overflow-hidden rounded-lg border bg-card/85 text-xs shadow-sm backdrop-blur">
            <button
              type="button"
              onClick={() => setInsightsOpen((o) => !o)}
              className="flex w-full items-center gap-1.5 px-2.5 py-1.5 hover:bg-muted/40"
              title="What the brain knows: load-bearing facts, topic bridges, gaps, and the confirm queue"
            >
              {insightsOpen ? (
                <ChevronDown className="size-3.5 text-muted-foreground" />
              ) : (
                <ChevronRight className="size-3.5 text-muted-foreground" />
              )}
              <span className="font-medium">Insights</span>
            </button>
            {!insightsOpen && (
              <div className="flex flex-wrap gap-1 px-2.5 pb-2">
                {[
                  { label: "Load-bearing", n: report.godNodes.length, cls: "" },
                  { label: "Bridges", n: report.bridges.length, cls: "" },
                  { label: "Confirm", n: report.needsConfirmation.length, cls: "text-amber-500" },
                  { label: "Due", n: report.dueForReview.length, cls: "" },
                  { label: "Isolated", n: report.isolated.length, cls: "" },
                  { label: "Confusable", n: report.interference.length, cls: "" },
                ]
                  .filter((c) => c.n > 0)
                  .map((c) => (
                    <span
                      key={c.label}
                      className={`rounded bg-muted/60 px-1.5 py-0.5 text-muted-foreground ${c.cls}`}
                    >
                      {c.label} <span className="font-medium text-foreground/80">{c.n}</span>
                    </span>
                  ))}
              </div>
            )}
            {insightsOpen && (
              <div className="max-h-[calc(100vh-13rem)] space-y-2 overflow-y-auto border-t px-2.5 py-2">
                {report.godNodes.length > 0 && (
                  <InsightSection
                    title="Load-bearing"
                    hint="Most-connected facts — much hangs off these"
                    ids={report.godNodes}
                    labelFor={(id) => nodeById.get(id)?.label ?? id}
                    onPick={focusNode}
                  />
                )}
                {report.bridges.length > 0 && (
                  <InsightSection
                    title="Bridges"
                    hint="Connect otherwise-separate topics — riskiest to lose"
                    ids={report.bridges}
                    labelFor={(id) => nodeById.get(id)?.label ?? id}
                    onPick={focusNode}
                  />
                )}
                {report.needsConfirmation.length > 0 && (
                  <div>
                    <div
                      className="mb-1 font-medium text-amber-600"
                      title="The brain's least-trusted facts — confirm to keep as ground truth, or delete"
                    >
                      Confirm?{" "}
                      <span className="text-muted-foreground">
                        ({report.needsConfirmation.length})
                      </span>
                    </div>
                    <ul className="space-y-1">
                      {report.needsConfirmation.map((id) => (
                        <li key={id} className="flex items-center gap-1">
                          <button
                            type="button"
                            className="min-w-0 flex-1 truncate text-left hover:text-foreground"
                            onClick={() => focusNode(id)}
                            title={nodeById.get(id)?.label ?? id}
                          >
                            {nodeById.get(id)?.label ?? id}
                          </button>
                          <button
                            type="button"
                            className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                            onClick={() => onConfirm(id)}
                            title="Confirm — keep as ground truth"
                          >
                            <Check className="size-3.5" />
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {report.dueForReview.length > 0 && (
                  <InsightSection
                    title="Due for review"
                    hint="Well-established facts gone cold — resurface before disuse fades them"
                    ids={report.dueForReview}
                    labelFor={(id) => nodeById.get(id)?.label ?? id}
                    onPick={focusNode}
                  />
                )}
                {report.isolated.length > 0 && (
                  <InsightSection
                    title="Isolated"
                    hint="No links to anything else — stray facts or knowledge gaps"
                    ids={report.isolated}
                    labelFor={(id) => nodeById.get(id)?.label ?? id}
                    onPick={focusNode}
                  />
                )}
                {report.interference.length > 0 && (
                  <div>
                    <div
                      className="mb-1 font-medium text-muted-foreground"
                      title="Similar but distinct — an agent could conflate these on recall"
                    >
                      Easily confused{" "}
                      <span className="text-muted-foreground/70">
                        ({report.interference.length})
                      </span>
                    </div>
                    <ul className="space-y-1">
                      {report.interference.map((p) => (
                        <li key={`${p.a}|${p.b}`} className="space-y-0.5">
                          {[p.a, p.b].map((id) => (
                            <button
                              key={id}
                              type="button"
                              className="block w-full truncate text-left hover:text-foreground"
                              onClick={() => focusNode(id)}
                              title={nodeById.get(id)?.label ?? id}
                            >
                              ↔ {nodeById.get(id)?.label ?? id}
                            </button>
                          ))}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

      {/* Legend — compact; the per-project colour key is collapsed behind a toggle. */}
      {!compact && (
        <div className="absolute bottom-3 left-3 max-w-[14rem] rounded-lg border bg-card/85 p-2 text-xs shadow-sm backdrop-blur">
          <div className="flex items-center gap-2 text-muted-foreground">
            <span className="inline-block h-0 w-4 border-t border-foreground/50" /> related
            <span className="ml-1 inline-block h-0 w-4 border-t border-dashed border-foreground/50" />
            similar
          </div>
          {colorBy === "area" ? (
            <div className="mt-1 text-muted-foreground">
              Coloured by cross-scope area — each shaded, labelled region is a topic that recurs
              across projects. Grey nodes belong to no area.
            </div>
          ) : colorBy === "community" ? (
            <div className="mt-1 text-muted-foreground">
              Coloured by topic cluster — facts in the same cluster consolidate together.
            </div>
          ) : (
            <div className="mt-1">
              <button
                type="button"
                onClick={() => setLegendOpen((o) => !o)}
                className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
              >
                {legendOpen ? (
                  <ChevronDown className="size-3" />
                ) : (
                  <ChevronRight className="size-3" />
                )}
                Colours by project ({scopeLegend.length})
              </button>
              {legendOpen && (
                <div className="mt-1 max-h-48 space-y-0.5 overflow-y-auto">
                  {scopeLegend.map((s) => (
                    <div key={s.scope} className="flex items-center gap-1.5 truncate">
                      <span
                        className="inline-block size-2.5 shrink-0 rounded-full"
                        style={{ backgroundColor: s.color }}
                      />
                      <span className="truncate">{s.label}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
