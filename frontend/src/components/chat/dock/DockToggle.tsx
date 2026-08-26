import { PanelRight, TriangleAlert, X } from "lucide-react";
import { Button } from "~/components/ui/button";
import type { DockAlert } from "~/lib/session/dock";
import { cn } from "~/lib/utils";

interface DockToggleProps {
  open: boolean;
  onToggle: () => void;
  /** Disabled when the session has nothing to dock — with a reason, not silently. */
  available: boolean;
  /**
   * The single mark the toggle is allowed to carry while the dock is shut.
   * Null while open: the tab row is right there saying it better.
   */
  alert: DockAlert | null;
}

const ALERT_LABEL: Record<DockAlert["kind"], (n: number) => string> = {
  blocked: (n) => `${n} ${n === 1 ? "loop is" : "loops are"} waiting on you`,
  failed: (n) => `${n} ${n === 1 ? "failure" : "failures"} to look at`,
  live: (n) => `${n} ${n === 1 ? "agent" : "agents"} still out`,
};

/**
 * The one control that opens the session dock.
 *
 * Collapsing the dock is the only thing in this design that costs information,
 * because the per-tab badges go with it. So the toggle carries one aggregate
 * mark in the app's usual ranking (`dockAlertState`) — waiting-on-you, then
 * failed, then live. One mark, never a summary: a button that tries to report
 * three states at once reports none of them.
 */
export function DockToggle({ open, onToggle, available, alert }: DockToggleProps) {
  const label = !available
    ? "Nothing to show beside this session yet"
    : open
      ? "Close the session dock"
      : alert
        ? `Open the session dock — ${ALERT_LABEL[alert.kind](alert.count)}`
        : "Open the session dock";

  return (
    <Button
      variant="ghost"
      size="sm"
      disabled={!available}
      onClick={onToggle}
      aria-label={label}
      aria-pressed={open}
      title={label}
      className={cn(
        "relative h-7 gap-1 px-2 text-xs",
        open ? "bg-muted/60 text-foreground" : "text-muted-foreground",
      )}
    >
      <PanelRight className="size-3.5" />
      {!open && alert && <AlertMark alert={alert} />}
    </Button>
  );
}

function AlertMark({ alert }: { alert: DockAlert }) {
  if (alert.kind === "live") {
    return (
      <span className="flex items-center gap-1">
        <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
        <span className="font-medium text-[10px] text-agent tabular-nums">{alert.count}</span>
      </span>
    );
  }
  // Same glyphs as everywhere else: the triangle is "someone is waiting on
  // you", the X is "it failed". One mark must mean one thing across surfaces.
  const Glyph = alert.kind === "blocked" ? TriangleAlert : X;
  return (
    <span
      className={cn(
        "flex items-center gap-0.5",
        alert.kind === "blocked" ? "text-warning" : "text-destructive",
      )}
    >
      <Glyph className="size-3" />
      <span className="font-medium text-[10px] tabular-nums">{alert.count}</span>
    </span>
  );
}
