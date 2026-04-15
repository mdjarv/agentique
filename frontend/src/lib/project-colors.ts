import { COLORS } from "~/lib/color-palette";

export interface ProjectColor {
  /** Bright variant — use for tinted backgrounds (`${bg}20`), full-opacity dots. */
  bg: string;
  /** Theme-appropriate accent — use for text, borders, outlines. */
  fg: string;
  /** Text color ON a full-opacity colored background. */
  text: string;
}

type ResolvedTheme = "light" | "dark";

function resolve(c: (typeof COLORS)[number], resolvedTheme: ResolvedTheme): ProjectColor {
  return {
    bg: c.bg,
    fg: resolvedTheme === "dark" ? c.bg : c.fgLight,
    text: c.text,
  };
}

/** Returns color for a project. Explicit color wins, otherwise deterministic auto-assign. */
export function getProjectColor(
  colorId: string,
  projectId: string,
  allProjectIds: string[],
  resolvedTheme: ResolvedTheme,
): ProjectColor {
  if (colorId) {
    const c = COLORS.find((p) => p.id === colorId);
    if (c) return resolve(c, resolvedTheme);
  }
  const sorted = [...allProjectIds].sort();
  const idx = sorted.indexOf(projectId);
  const c = COLORS[idx >= 0 ? idx % COLORS.length : 0] ?? COLORS[0];
  return resolve(c, resolvedTheme);
}
