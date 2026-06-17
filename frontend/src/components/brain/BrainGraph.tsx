import { useEffect, useMemo, useRef, useState } from "react";
import ForceGraph2D, {
  type ForceGraphMethods,
  type LinkObject,
  type NodeObject,
} from "react-force-graph-2d";
import type { Memory } from "~/lib/brain-api";

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
  val: number;
}

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

export function BrainGraph({
  memories,
  labelForScope,
}: {
  memories: Memory[];
  labelForScope: (scope: string) => string;
}) {
  const [showSimilar, setShowSimilar] = useState(true);
  const [hoverId, setHoverId] = useState<string | null>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [fg, setFg] = useState("#9ca3af");

  const wrapRef = useRef<HTMLDivElement>(null);
  const fgRef = useRef<ForceGraphMethods<GNode, GLink> | undefined>(undefined);
  const fitted = useRef(false);

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

  const { nodes, links } = useMemo<{ nodes: GNode[]; links: GLink[] }>(() => {
    const nodes: GNode[] = memories.map((m) => ({
      id: m.id,
      label: m.text.length > 40 ? `${m.text.slice(0, 40)}…` : m.text,
      scope: m.scope,
      scopeLabel: labelForScope(m.scope),
      category: m.category,
      source: m.source,
      uses: m.uses,
      pinned: m.pinned,
      val: 2 + Math.min(m.uses, 12) + (m.pinned ? 2 : 0),
    }));

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

    fitted.current = false; // re-frame when the data changes
    return { nodes, links };
  }, [memories, labelForScope, showSimilar]);

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
  // connected memories into legible clusters instead of one diffuse cloud.
  // (d3Force returns a ForceFn; cast through unknown to reach .strength/.distance.)
  useEffect(() => {
    const g = fgRef.current;
    if (!g || nodes.length === 0) return;
    (g.d3Force("charge") as unknown as { strength(n: number): void } | undefined)?.strength(-160);
    (g.d3Force("link") as unknown as { distance(n: number): void } | undefined)?.distance(45);
    g.d3ReheatSimulation();
  }, [nodes]);

  const neighbors = hoverId ? adjacency.get(hoverId) : null;
  const linkTouchesHover = (l: GLink) =>
    hoverId != null && (endId(l.source) === hoverId || endId(l.target) === hoverId);

  const scopeLegend = useMemo(() => {
    const scopes = [...new Set(nodes.map((n) => n.scope))];
    scopes.sort((a, b) => (a === "global" ? -1 : b === "global" ? 1 : a.localeCompare(b)));
    return scopes.map((s) => ({ scope: s, label: labelForScope(s), color: scopeColor(s) }));
  }, [nodes, labelForScope]);

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
            graphData={{ nodes, links }}
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
              ctx.fillStyle = scopeColor(node.scope);
              ctx.fill();
              if (node.pinned) {
                ctx.strokeStyle = "#facc15";
                ctx.lineWidth = 2 / scale;
                ctx.beginPath();
                ctx.arc(x, y, r + 2 / scale, 0, 2 * Math.PI);
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
              if (!fitted.current) {
                fgRef.current?.zoomToFit(400, 48);
                fitted.current = true;
              }
            }}
          />
        )
      )}

      {/* Controls */}
      <label className="absolute right-3 top-3 flex items-center gap-1.5 rounded-md border bg-card/80 px-2 py-1 text-xs backdrop-blur">
        <input
          type="checkbox"
          checked={showSimilar}
          onChange={(e) => setShowSimilar(e.target.checked)}
        />
        Similarity links
      </label>

      {/* Legend */}
      <div className="absolute bottom-3 left-3 max-w-[14rem] space-y-1 rounded-md border bg-card/80 p-2 text-xs backdrop-blur">
        <div className="mb-1 flex items-center gap-2 text-muted-foreground">
          <span className="inline-block h-0 w-5 border-t border-foreground/60" /> derived/related
          <span className="ml-1 inline-block h-0 w-5 border-t border-dashed border-foreground/60" />
          similar
        </div>
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
    </div>
  );
}
