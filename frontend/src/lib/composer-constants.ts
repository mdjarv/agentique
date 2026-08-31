import type { AutoApproveMode } from "~/stores/chat-store";

export type EffortLevel = "" | "low" | "medium" | "high" | "xhigh" | "max";

export const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
export const MAX_ATTACHMENTS = 4;
export const ACCEPTED_TYPES = "image/*,application/pdf";

export function isAllowedType(mime: string): boolean {
  return mime.startsWith("image/") || mime === "application/pdf";
}

export function isImage(mime: string): boolean {
  return mime.startsWith("image/");
}

export const EFFORT_LEVELS: EffortLevel[] = ["max", "xhigh", "high", "medium", "low", ""];

/**
 * The same levels as a ramp, weakest first, with "Default" left out.
 *
 * A menu puts the strongest option at the top; a ramp climbs, and it cannot
 * carry an unset value as a rung — "Default" is the absence of a choice, not a
 * quantity between Low and Medium. So the meter reads an unset effort as an
 * empty ramp rather than inventing a position for it.
 */
export const RAMP_LEVELS: EffortLevel[] = ["low", "medium", "high", "xhigh", "max"];
export const EFFORT_LABELS: Record<EffortLevel, string> = {
  "": "Default",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "XHigh",
  max: "Max",
};
export const EFFORT_COLORS: Record<EffortLevel, string> = {
  "": "text-muted-foreground",
  low: "text-info",
  medium: "text-primary",
  high: "text-orange",
  xhigh: "text-orange",
  max: "text-destructive",
};

export const PERMISSION_MODES: AutoApproveMode[] = ["manual", "auto", "fullAuto"];
export const PERMISSION_LABELS: Record<AutoApproveMode, string> = {
  manual: "Manual",
  auto: "Auto",
  fullAuto: "Full Auto",
};
/**
 * The verb phrasing, for menus. A menu row is an action you are about to take,
 * so it reads better as one — "Manually approve" over "Manual". The nouns
 * survive in `PERMISSION_LABELS` for the places that label a *state* rather
 * than offer a choice (templates, team profiles).
 */
export const PERMISSION_VERBS: Record<AutoApproveMode, string> = {
  manual: "Manually approve",
  auto: "Automatically approve",
  fullAuto: "Skip all approvals",
};

export const PERMISSION_DESCRIPTIONS: Record<AutoApproveMode, string> = {
  manual: "Approve every tool use individually",
  auto: "Auto-approve reads and writes, prompt for shell commands",
  fullAuto: "Auto-approve all operations including shell commands",
};
export const PERMISSION_COLORS: Record<AutoApproveMode, string> = {
  manual: "text-muted-foreground",
  auto: "text-success",
  fullAuto: "text-warning",
};
export const PERMISSION_BG: Record<AutoApproveMode, string> = {
  manual: "",
  auto: "bg-success/10",
  fullAuto: "bg-warning/10",
};
