import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/schedule-actions";

/** Human-readable cadence for a schedule ("every 30 min", "daily 09:00", …). */
export function humanCadence(s: ScheduleInfo): string {
  if (s.mode === "once") return "once";
  if (s.mode === "dynamic") return "self-paced";
  return cronToHuman(s.cron);
}

/** Best-effort 5-field cron → phrase; falls back to the raw expression. */
export function cronToHuman(cron: string): string {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return cron;
  const [min, hour, dom, mon, dow] = parts;
  const pad = (v: string) => v.padStart(2, "0");
  if (mon === "*" && dom === "*") {
    if (hour === "*" && dow === "*") {
      if (min === "*") return "every minute";
      const step = min.match(/^\*\/(\d+)$/);
      if (step) return `every ${step[1]} min`;
      if (/^\d+$/.test(min)) return `hourly at :${pad(min)}`;
    }
    const hourStep = hour.match(/^\*\/(\d+)$/);
    if (hourStep && /^\d+$/.test(min)) return `every ${hourStep[1]} h at :${pad(min)}`;
    if (/^\d+$/.test(hour) && /^\d+$/.test(min)) {
      const at = `${pad(hour)}:${pad(min)}`;
      if (dow === "*") return `daily ${at}`;
      if (dow === "1-5") return `weekdays ${at}`;
      return `${at} on dow ${dow}`;
    }
  }
  return cron;
}

/** Compact relative time until an RFC3339 instant ("in 25m", "in 3h", "now"). */
export function untilText(rfc3339: string, now: Date = new Date()): string {
  if (!rfc3339) return "";
  const target = new Date(rfc3339).getTime();
  if (Number.isNaN(target)) return "";
  const diffSec = Math.round((target - now.getTime()) / 1000);
  if (diffSec <= 0) return "now";
  if (diffSec < 60) return `in ${diffSec}s`;
  const min = Math.round(diffSec / 60);
  if (min < 60) return `in ${min}m`;
  const h = Math.floor(min / 60);
  if (h < 24) return `in ${h}h ${min % 60 ? `${min % 60}m` : ""}`.trim();
  return `in ${Math.round(h / 24)}d`;
}

/** Compact relative time since an RFC3339 instant ("2m ago", "3h ago"). */
export function agoText(rfc3339: string, now: Date = new Date()): string {
  if (!rfc3339) return "";
  const t = new Date(rfc3339).getTime();
  if (Number.isNaN(t)) return "";
  const sec = Math.max(0, Math.round((now.getTime() - t) / 1000));
  if (sec < 60) return "just now";
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function formatRunDuration(ms: number): string {
  if (ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.round(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  return `${min}m ${sec % 60}s`;
}

export interface RunStatusMeta {
  label: string;
  /** Tailwind text/background classes for a status dot or badge. */
  dotClass: string;
  textClass: string;
}

/** Visual vocabulary for run statuses — single source of truth for B/C UIs. */
export function runStatusMeta(run: Pick<ScheduleRunInfo, "status" | "overdue">): RunStatusMeta {
  if (run.overdue && run.status === "running") {
    return { label: "overdue", dotClass: "bg-amber-500", textClass: "text-amber-500" };
  }
  switch (run.status) {
    case "ok":
      return { label: "ok", dotClass: "bg-emerald-500", textClass: "text-emerald-500" };
    case "action_needed":
      return { label: "needs you", dotClass: "bg-amber-500", textClass: "text-amber-500" };
    case "error":
      return { label: "error", dotClass: "bg-destructive", textClass: "text-destructive" };
    case "deferred":
      return { label: "deferred", dotClass: "bg-sky-500", textClass: "text-sky-500" };
    case "interrupted":
      return { label: "interrupted", dotClass: "bg-muted-foreground", textClass: "text-muted-foreground" };
    case "skipped":
      return { label: "skipped", dotClass: "bg-muted-foreground/50", textClass: "text-muted-foreground" };
    case "running":
      return { label: "running", dotClass: "bg-blue-500 animate-pulse", textClass: "text-blue-500" };
    case "firing":
    case "queued":
      return { label: "queued", dotClass: "bg-muted-foreground/70", textClass: "text-muted-foreground" };
    default:
      return { label: run.status, dotClass: "bg-muted-foreground/50", textClass: "text-muted-foreground" };
  }
}

/** Short human text for why a schedule is paused. */
export function pauseReasonText(reason: string): string {
  switch (reason) {
    case "user":
      return "paused";
    case "completed":
      return "finished";
    case "expired":
      return "expired";
    case "session-completed":
      return "session completed";
    case "auto-failures":
      return "paused after repeated failures";
    case "dynamic-ended":
      return "loop ended";
    case "pending-approval":
      return "awaiting approval";
    case "invalid-schedule":
      return "invalid schedule";
    default:
      return reason;
  }
}
