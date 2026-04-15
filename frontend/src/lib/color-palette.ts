/**
 * Predefined color palette (Tokyo Night-inspired), ordered by hue.
 *
 * `bg`      — bright variant, the color's identity. Used for tinted backgrounds (`${bg}20`).
 * `fgLight` — darker variant of the same hue for text/accents on light backgrounds.
 * `text`    — text color ON a full-opacity colored background.
 */
export const COLORS = [
  { id: "red", label: "Red", bg: "#f7768e", fgLight: "#a82545", text: "#1a1b26" },
  { id: "rose", label: "Rose", bg: "#ffa0a0", fgLight: "#a83838", text: "#1a1b26" },
  { id: "pink", label: "Pink", bg: "#ff7eb3", fgLight: "#a82860", text: "#1a1b26" },
  { id: "orange", label: "Orange", bg: "#ff9e64", fgLight: "#954a1c", text: "#1a1b26" },
  { id: "yellow", label: "Yellow", bg: "#e0af68", fgLight: "#7a5518", text: "#1a1b26" },
  { id: "lime", label: "Lime", bg: "#c3e88d", fgLight: "#4a7518", text: "#1a1b26" },
  { id: "green", label: "Green", bg: "#9ece6a", fgLight: "#3a6215", text: "#1a1b26" },
  { id: "teal", label: "Teal", bg: "#73daca", fgLight: "#186852", text: "#1a1b26" },
  { id: "cyan", label: "Cyan", bg: "#7dcfff", fgLight: "#155f85", text: "#1a1b26" },
  { id: "blue", label: "Blue", bg: "#7aa2f7", fgLight: "#284a90", text: "#1a1b26" },
  { id: "indigo", label: "Indigo", bg: "#7b8bf5", fgLight: "#353d90", text: "#1a1b26" },
  { id: "purple", label: "Purple", bg: "#bb9af7", fgLight: "#6038a5", text: "#1a1b26" },
  { id: "slate", label: "Slate", bg: "#8b95b0", fgLight: "#3d4560", text: "#1a1b26" },
] as const;

export type ColorId = (typeof COLORS)[number]["id"];

export type Color = (typeof COLORS)[number];

export function getColor(colorId: string) {
  return COLORS.find((c) => c.id === colorId) ?? COLORS[0];
}
