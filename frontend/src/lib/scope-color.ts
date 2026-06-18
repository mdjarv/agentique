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
