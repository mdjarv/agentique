// Deterministic per-scope colours, shared by the graph view and the review surface so
// a project reads as the same colour everywhere. Global has a fixed colour; every
// other scope hashes into a fixed palette.

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

function hashStr(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return Math.abs(h);
}

export function scopeColor(scope: string): string {
  if (scope === "global") return GLOBAL_COLOR;
  return PALETTE[hashStr(scope) % PALETTE.length] ?? GLOBAL_COLOR;
}

// communityColor colours a node by its topic cluster. Community ids are scope-local,
// so the palette key mixes scope + community to keep clusters from different scopes
// distinct (RFC P3 — "color = community after P3").
export function communityColor(scope: string, community: number): string {
  return PALETTE[hashStr(`${scope}#${community}`) % PALETTE.length] ?? GLOBAL_COLOR;
}

// AREA_NONE_COLOR is the muted colour for facts in no cross-scope area.
const AREA_NONE_COLOR = "#6b7280";

// areaColor colours a node by its cross-scope topic area (the area label). Empty area
// (single-scope fact) reads muted so the real areas stand out.
export function areaColor(area: string): string {
  if (!area) return AREA_NONE_COLOR;
  return PALETTE[hashStr(area) % PALETTE.length] ?? GLOBAL_COLOR;
}

// shade lightens (amt > 0, toward white) or darkens (amt < 0, toward black) a "#rrggbb"
// hex by the given fraction, returning an "rgb(r, g, b)" string.
export function shade(hex: string, amt: number): string {
  const h = hex.replace("#", "");
  const t = amt < 0 ? 0 : 255;
  const p = Math.min(1, Math.abs(amt));
  const mix = (c: number) => Math.round(c + (t - c) * p);
  const r = mix(Number.parseInt(h.slice(0, 2), 16));
  const g = mix(Number.parseInt(h.slice(2, 4), 16));
  const b = mix(Number.parseInt(h.slice(4, 6), 16));
  return `rgb(${r}, ${g}, ${b})`;
}

// coreColor maps a fact's trust tier (+ confidence) to its "heat" colour — the brain
// graph's central encoding: white-hot = human ground truth, amber = needs review, else
// the scope hue brightened by confidence (a 0.65 fact sits dim, a ~1.0 fact glows).
// `base` is the scope/area hue the inferred tier brightens. Shared by the 2D and 3D graphs.
export function coreColor(
  trust: "human" | "review" | "normal",
  base: string,
  conf: number,
): string {
  if (trust === "human") return "#fbfdff"; // white-hot ground truth
  if (trust === "review") return "#fbbf24"; // amber — flagged / low confidence
  const norm = Math.max(0, Math.min(1, (conf - 0.6) / 0.4));
  return shade(base, 0.12 + 0.5 * norm); // inferred: dim → bright with confidence
}
