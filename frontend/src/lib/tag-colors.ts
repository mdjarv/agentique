/** Predefined tag color palette (Tokyo Night-inspired). */
export const TAG_COLORS = [
  { id: "blue", label: "Blue", bg: "#7aa2f7", text: "#1a1b26" },
  { id: "teal", label: "Teal", bg: "#73daca", text: "#1a1b26" },
  { id: "green", label: "Green", bg: "#9ece6a", text: "#1a1b26" },
  { id: "yellow", label: "Yellow", bg: "#e0af68", text: "#1a1b26" },
  { id: "orange", label: "Orange", bg: "#ff9e64", text: "#1a1b26" },
  { id: "red", label: "Red", bg: "#f7768e", text: "#1a1b26" },
  { id: "purple", label: "Purple", bg: "#bb9af7", text: "#1a1b26" },
  { id: "cyan", label: "Cyan", bg: "#7dcfff", text: "#1a1b26" },
] as const;

export type TagColorId = (typeof TAG_COLORS)[number]["id"];

export function getTagColor(colorId: string) {
  return TAG_COLORS.find((c) => c.id === colorId) ?? TAG_COLORS[0];
}
