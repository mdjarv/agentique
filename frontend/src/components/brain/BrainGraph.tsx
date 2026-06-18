import { Check } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import ForceGraph2D, {
  type ForceGraphMethods,
  type LinkObject,
  type NodeObject,
} from "react-force-graph-2d";
import type { GraphReport, Memory } from "~/lib/brain-api";

// GraphMemory is a memory optionally carrying its server-computed centrality. When
// the graph endpoint has loaded, degree/betweenness are present and drive node
// sizing and the insights panel; before then the component degrades to the plain list.
type GraphMemory = Memory & { degree?: number; betweenness?: number };

// BrainGraph renders the brain as an Obsidian-style force-directed graph.
// Nodes are memories; edges come from three sources (cheapest first):
//   - provenance: `derivedFrom` links written by consolidation (solid).
//   - related:    curated `[[link]]` graph (solid) — empty until P1 of the RFC.
//   - similar:    Jaccard token overlap computed here (dashed), the stand-in
//                 for a structural link until `related` is populated.
// This is graph-view v1 from docs/brain-graph-layer.md: it reads only the
// existing API, no backend change. It also makes the "dead link graph" visible
// — isolated memories literally float free.

interface NodeData {
  id: string;
  label: string;
  scope: string;
  scopeLabel: string;
  category: string;
  source: string;
  uses: number;
  pinned: boolean;
  community: number;
  degree: number;
  val: number;
}

type ColorBy = "scope" | "community";

type EdgeKind = "provenance" | "related" | "similar";
interface LinkData {
  kind: EdgeKind;
}

type GNode = NodeObject<NodeData>;
type GLink = LinkObject<NodeData, LinkData>;

const NODE_REL_SIZE = 4;
const SIM_THRESHOLD = 0.18; // min Jaccard to draw a similarity edge
const SIM_MAX_NODES = 800; // skip the O(n^2) pass above this many nodes
const SIM_DEGREE_CAP = 4; // max similarity edges per node, keeps it from hairballing

const GLOBAL_COLOR = "#a78bfa";
const PALETTE = [
  "#60a5fa",
  "#34d399",
  "#fbbf24",
  "#f87171",
  "#f472b6",
  "#38bdf8",
  "#a3e635",
  "#fb923c",
  "#22d3ee",
  "#c084fc",
];

const STOPWORDS = new Set(
  "the and for are but not you all any can has have was with this that from they will would there their what when which while into over under more most some such only own same than too very our your".split(
    " ",
  ),
);

function hashStr(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return Math.abs(h);
}

function scopeColor(scope: string): string {
  if (scope === "global") return GLOBAL_COLOR;
  return PALETTE[hashStr(scope) % PALETTE.length] ?? GLOBAL_COLOR;
}

// communityColor colors a node by its topic cluster. Community ids are scope-local,
// so the palette key mixes scope + community to keep clusters from different scopes
// distinct (RFC P3 — "color = community after P3").
function communityColor(scope: string, community: number): string {
  return PALETTE[hashStr(`${scope}#${community}`) % PALETTE.length] ?? GLOBAL_COLOR;
}

function nodeColor(node: NodeData, colorBy: ColorBy): string {
  return colorBy === "community"
    ? communityColor(node.scope, node.community)
    : scopeColor(node.scope);
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

// InsightSection renders one click-to-focus list of node ids (god nodes / bridges).
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
  return (
    <div>
      <div className="mb-1 font-medium text-muted-foreground" title={hint}>
        {title}
      </div>
      <ul className="space-y-0.5">
        {ids.map((id) => (
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
    </div>
  );
}

export function BrainGraph({
  memories,
  report,
  labelForScope,
  onConfirm,
  compact = false,
  focusId,
}: {
  memories: GraphMemory[];
  report: GraphReport | null;
  labelForScope: (scope: string) => string;
  onConfirm: (id: string) => void;
  // compact hides the controls + legend overlays so the canvas can be embedded as a
  // small focused subgraph (e.g. the isolated neighbourhood in the review surface).
  compact?: boolean;
  // focusId draws an emphasis ring on one node — the fact currently under review.
  focusId?: string;
}) {
  const [showSimilar, setShowSimilar] = useState(true);
  const [colorBy, setColorBy] = useState<ColorBy>("scope");
  const [hoverId, setHoverId] = useState<string | null>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [fg, setFg] = useState("#9ca3af");

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
  // Custom forces are applied once and reheated once to take effect; later topology
  // changes reheat via the graphData digest, with the forces already in place.
  const forcesReady = useRef(false);

  // Measure the container so the canvas fills it (and resizes with the window).
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const sync = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    sync();
    setFg(getComputedStyle(el).color || "#9ca3af");
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
        scope: m.scope,
        scopeLabel: labelForScope(m.scope),
        category: m.category,
        source: m.source,
        uses: m.uses,
        pinned: m.pinned,
        community: m.community ?? 0,
        degree: m.degree ?? 0,
        // Size blends use-count, pinned, and structural degree so load-bearing "god
        // nodes" read bigger — the graphify signal made visual.
        val: 2 + Math.min(m.uses, 10) + (m.pinned ? 2 : 0) + Math.min(m.degree ?? 0, 8),
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

    const addLink = (a: string, b: string, kind: EdgeKind) => {
      if (a === b || !idSet.has(a) || !idSet.has(b)) return;
      const key = `${kind}#${pk(a, b)}`;
      if (seen.has(key)) return;
      seen.add(key);
      links.push({ source: a, target: b, kind });
      if (kind !== "similar") structural.add(pk(a, b));
    };

    for (const m of memories) {
      for (const d of m.derivedFrom ?? []) addLink(m.id, d, "provenance");
      for (const r of m.related ?? []) addLink(m.id, r, "related");
    }

    if (showSimilar && memories.length <= SIM_MAX_NODES) {
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
        l.scope = n.scope;
        l.scopeLabel = n.scopeLabel;
        l.category = n.category;
        l.source = n.source;
        l.uses = n.uses;
        l.pinned = n.pinned;
        l.community = n.community;
        l.degree = n.degree;
        l.val = n.val;
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
  }, [memories, labelForScope, showSimilar]);

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

  // Tighten the layout: a stronger repulsion with a short link distance pulls
  // connected memories into legible clusters instead of one diffuse cloud. Forces
  // persist on the simulation across data changes, so we set them (idempotently) and
  // reheat exactly once to apply them to the initial layout. Subsequent topology
  // changes reheat via react-force-graph's own graphData digest (forces already set);
  // data-only refetches keep the graphData reference stable and so never reheat.
  // (d3Force returns a ForceFn; cast through unknown to reach .strength/.distance.)
  useEffect(() => {
    const g = fgRef.current;
    if (!g || nodes.length === 0) return;
    (g.d3Force("charge") as unknown as { strength(n: number): void } | undefined)?.strength(-160);
    (g.d3Force("link") as unknown as { distance(n: number): void } | undefined)?.distance(45);
    if (!forcesReady.current) {
      forcesReady.current = true;
      g.d3ReheatSimulation();
    }
  }, [nodes]);

  // Recoloring doesn't touch graphData, so the settled canvas won't repaint on its
  // own — nudge a redraw when the color dimension changes.
  // biome-ignore lint/correctness/useExhaustiveDependencies: `colorBy` is a trigger-only dep — the new color is read by nodeCanvasObject on the next paint; this effect just forces that paint.
  useEffect(() => {
    (fgRef.current as unknown as { refresh?: () => void } | undefined)?.refresh?.();
  }, [colorBy]);

  const neighbors = hoverId ? adjacency.get(hoverId) : null;
  const linkTouchesHover = (l: GLink) =>
    hoverId != null && (endId(l.source) === hoverId || endId(l.target) === hoverId);

  const scopeLegend = useMemo(() => {
    const scopes = [...new Set(nodes.map((n) => n.scope))];
    scopes.sort((a, b) => (a === "global" ? -1 : b === "global" ? 1 : a.localeCompare(b)));
    return scopes.map((s) => ({ scope: s, label: labelForScope(s), color: scopeColor(s) }));
  }, [nodes, labelForScope]);

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
            nodeRelSize={NODE_REL_SIZE}
            nodeVal={(n) => n.val ?? 2}
            cooldownTicks={120}
            nodeLabel={(n) =>
              `<div style="font:12px sans-serif;max-width:260px">${esc(n.label)}<br/><span style="opacity:.6">${esc(n.scopeLabel)} · ${n.category} · used ${n.uses}×</span></div>`
            }
            nodeCanvasObject={(node, ctx, scale) => {
              const x = node.x ?? 0;
              const y = node.y ?? 0;
              const r = Math.sqrt(node.val ?? 2) * NODE_REL_SIZE;
              const dim =
                hoverId != null && node.id !== hoverId && !neighbors?.has(String(node.id));
              ctx.globalAlpha = dim ? 0.15 : 1;
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
              if (scale > 1.1 || node.pinned || node.uses >= 3 || node.id === hoverId) {
                const fontSize = 12 / scale;
                ctx.font = `${fontSize}px sans-serif`;
                ctx.textAlign = "center";
                ctx.textBaseline = "top";
                ctx.fillStyle = fg;
                ctx.fillText(node.label, x, y + r + 2 / scale);
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
              if (hoverId != null) {
                return linkTouchesHover(l) ? "rgba(250,204,21,0.95)" : "rgba(140,140,150,0.06)";
              }
              return l.kind === "similar" ? "rgba(150,180,235,0.5)" : "rgba(190,195,210,0.8)";
            }}
            linkWidth={(l) => (linkTouchesHover(l) ? 3.2 : l.kind === "similar" ? 1.4 : 2)}
            linkLineDash={(l) => (l.kind === "similar" ? [4, 3] : null)}
            onNodeHover={(n) => setHoverId(n ? String(n.id) : null)}
            onNodeClick={(n) => {
              if (n.x != null && n.y != null) {
                fgRef.current?.centerAt(n.x, n.y, 500);
                fgRef.current?.zoom(4, 500);
              }
            }}
            onEngineStop={() => {
              // Fit once, when the layout first settles after (re)mounting the graph
              // view. We never re-frame afterwards, so a memory change or a Tidy can't
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
          <label className="flex items-center gap-1.5 rounded-md border bg-card/80 px-2 py-1 text-xs backdrop-blur">
            <span className="text-muted-foreground">Color</span>
            <select
              value={colorBy}
              onChange={(e) => setColorBy(e.target.value as ColorBy)}
              className="bg-transparent outline-none"
              title="Color nodes by scope, or by topic cluster (community) detected during consolidation"
            >
              <option value="scope">by scope</option>
              <option value="community">by cluster</option>
            </select>
          </label>
        </div>
      )}

      {/* Insights — graphify analyze.py analogs (RFC P2). */}
      {report &&
        (report.godNodes.length > 0 ||
          report.bridges.length > 0 ||
          report.needsConfirmation.length > 0) && (
          <div className="absolute left-3 top-3 max-h-[calc(100%-1.5rem)] w-60 space-y-2 overflow-y-auto rounded-md border bg-card/80 p-2 text-xs backdrop-blur">
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
                  <span className="text-muted-foreground">({report.needsConfirmation.length})</span>
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
          </div>
        )}

      {/* Legend */}
      {!compact && (
        <div className="absolute bottom-3 left-3 max-w-[14rem] space-y-1 rounded-md border bg-card/80 p-2 text-xs backdrop-blur">
          <div className="mb-1 flex items-center gap-2 text-muted-foreground">
            <span className="inline-block h-0 w-5 border-t border-foreground/60" /> derived/related
            <span className="ml-1 inline-block h-0 w-5 border-t border-dashed border-foreground/60" />
            similar
          </div>
          {colorBy === "community" ? (
            <div className="text-muted-foreground">
              Colored by topic cluster — facts in the same cluster consolidate together.
            </div>
          ) : (
            scopeLegend.map((s) => (
              <div key={s.scope} className="flex items-center gap-1.5 truncate">
                <span
                  className="inline-block size-2.5 shrink-0 rounded-full"
                  style={{ backgroundColor: s.color }}
                />
                <span className="truncate">{s.label}</span>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
