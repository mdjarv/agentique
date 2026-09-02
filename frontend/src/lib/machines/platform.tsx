/**
 * Platform vocabulary (multi-machine): which operating system a machine runs,
 * and the one mark each answer wears.
 *
 * The union is closed on the REST_GLYPH precedent — a new OS must choose its
 * mark rather than inherit a blank — and every surface that draws a machine
 * reads this table, so the rail cannot show a penguin where settings shows a
 * flag. The raw strings are Go's GOOS ("linux", "windows", "darwin"), which is
 * what the descriptor, /api/health and the machines catalog all speak; an
 * unknown or absent value parses to null and renders NO platform mark rather
 * than a guessed one — an older peer's silence says nothing about its OS.
 *
 * Platform is the machine's OWN fact, which is why it lives beside — not
 * inside — lib/machines/icons.ts: that registry is this host's presentation of
 * a machine, a user choice, and OS entries must not leak into the identity
 * dialog's picker grid.
 */
import { SiApple, SiLinux } from "@icons-pack/react-simple-icons";
import type { ComponentType } from "react";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";

export type Platform = "linux" | "darwin" | "windows";

export type PlatformGlyph = ComponentType<{ className?: string }>;

/**
 * Microsoft's four-pane mark, hand-authored.
 *
 * simple-icons carries SiApple and SiLinux but no Windows glyph — withdrawn
 * from that set like OpenAI's — so it lives here as its own component (the
 * CodexMark precedent), to be swapped for an official asset without touching
 * anything else. Four filled panes rather than the slanted flag: nothing rides
 * on an interior detail, so it survives the 10px tier where the machine marks
 * actually live.
 */
function WindowsMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M2 2h9.5v9.5H2zM12.5 2H22v9.5h-9.5zM2 12.5h9.5V22H2zM12.5 12.5H22V22h-9.5z" />
    </svg>
  );
}

export const PLATFORM_GLYPH: Record<Platform, PlatformGlyph> = {
  linux: SiLinux,
  darwin: SiApple,
  windows: WindowsMark,
};

/** Spoken name — tooltips, accessible labels, detail lines. */
export const PLATFORM_LABEL: Record<Platform, string> = {
  linux: "Linux",
  darwin: "macOS",
  windows: "Windows",
};

/**
 * Narrows a raw platform report to the closed union. Accepts the bare GOOS
 * ("linux") and the update checker's "GOOS/GOARCH" form ("linux/amd64").
 * Null means unknown, and unknown draws no mark.
 */
export function parsePlatform(raw: string | undefined | null): Platform | null {
  if (!raw) return null;
  const os = raw.toLowerCase().split("/", 1)[0];
  return os === "linux" || os === "darwin" || os === "windows" ? os : null;
}

/** The spoken name for a raw platform report, or "" when unknown — safe to
 *  drop straight into a detail line's filter(Boolean).join(" · "). */
export function platformLabel(raw: string | undefined | null): string {
  const platform = parsePlatform(raw);
  return platform ? PLATFORM_LABEL[platform] : "";
}

/** The mark for a raw platform report, or null when it parses to unknown. */
export function platformGlyph(raw: string | undefined | null): PlatformGlyph | null {
  const platform = parsePlatform(raw);
  return platform ? PLATFORM_GLYPH[platform] : null;
}

/**
 * The one resolver behind every machine glyph: an icon the user picked always
 * wins (presentation outranks fact — it is the name they gave the box), the
 * platform mark stands in where they picked nothing, and the generic server
 * glyph remains the floor for machines whose OS is unknown too.
 */
export function resolveMachineGlyph(
  iconId: string | undefined,
  platformOs: string | undefined,
): PlatformGlyph {
  return getMachineIcon(iconId ?? "") ?? platformGlyph(platformOs) ?? DEFAULT_MACHINE_ICON;
}
