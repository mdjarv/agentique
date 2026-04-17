import type { AutoApproveMode } from "~/stores/chat-store";

export type EffortLevel = "" | "low" | "medium" | "high" | "xhigh" | "max";

export const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024; // 10 MB
export const MAX_ATTACHMENTS = 8;
export const ACCEPTED_TYPES = "image/*,application/pdf";

export function isAllowedType(mime: string): boolean {
  return mime.startsWith("image/") || mime === "application/pdf";
}

export function isImage(mime: string): boolean {
  return mime.startsWith("image/");
}

export const EFFORT_LEVELS: EffortLevel[] = ["max", "xhigh", "high", "medium", "low", ""];
export const EFFORT_LABELS: Record<EffortLevel, string> = {
  "": "Default",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "XHigh (recommended)",
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
