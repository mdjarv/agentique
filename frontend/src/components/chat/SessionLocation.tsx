/**
 * The location pill: which machine, and which worktree on it.
 *
 * One bordered object with two zones, because the two halves are two segments
 * of one address — and because the configuration that costs most, the *main*
 * worktree on a *remote* machine, is the one where both zones light at once.
 * As two separate chips with the dock toggle between them, nothing said that
 * they compound.
 *
 * What each zone says lives in `lib/session/location.ts`, which the sidebar can
 * read too; this file is only the drawing and the popovers.
 *
 * Zone 1 is always present, including on this machine. Absence is not a signal
 * you can trust — it reads the same as a bar that has not loaded — and an
 * address that is sometimes two segments and sometimes one cannot be compared
 * between two sessions at a glance. The local host is named in neutral ink with
 * no hue and no dot: stated, not announced.
 *
 * One subject per popover, so each zone opens its own. The exception is the
 * phone, where a 22px zone is under the 44px touch target: there the whole pill
 * is one target opening one sheet with both sections. `SessionIdentity` keeps
 * the session's own subjects (rename, icon, pin, archive) — the nine-subject
 * popover is what this splits up.
 */
import { Monitor } from "lucide-react";
import { memo } from "react";
import { useShallow } from "zustand/react/shallow";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useTheme } from "~/hooks/useTheme";
import { machineHue } from "~/lib/machine-colors";
import { getMachineIcon } from "~/lib/machines/icons";
import { platformGlyph, resolveMachineGlyph } from "~/lib/machines/platform";
import {
  machineTitle,
  WORKTREE_GLYPH,
  WORKTREE_LABEL,
  type WorktreeZone,
  worktreeZone,
} from "~/lib/session/location";
import { cn } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import { type MachineEntry, type MachineStatus, useMachineStore } from "~/stores/machine-store";

export interface SessionLocationProps {
  /** The machine this session runs on, or null for the primary. */
  machine: MachineEntry | null;
  status: MachineStatus;
  fault?: { detail: string } | null;
  worktreeBranch?: string | null;
  branchMissing?: boolean;
  worktreePath?: string | null;
  /** The project checkout's branch — the main worktree names it. */
  projectBranch?: string | null;
  /** Repo path, shown in the worktree popover. */
  projectPath?: string | null;
  className?: string;
  /** Smaller type, for the mobile subline. */
  compact?: boolean;
}

const ZONE_TONE: Record<WorktreeZone["tone"], string> = {
  quiet: "text-muted-foreground",
  warn: "bg-warning/15 text-warning font-medium",
  fault: "bg-destructive/15 text-destructive font-medium",
  hue: "",
};

/**
 * The id set hues are assigned from — sorted inside `machineHue`.
 *
 * `useShallow` is load-bearing: `Object.keys` allocates a new array per call,
 * and a selector returning one re-renders forever.
 */
function useAllMachineIds(): string[] {
  return useMachineStore(useShallow((s) => Object.keys(s.machines)));
}

export const SessionLocation = memo(function SessionLocation({
  machine,
  status,
  fault,
  worktreeBranch,
  branchMissing,
  worktreePath,
  projectBranch,
  projectPath,
  className,
  compact = false,
}: SessionLocationProps) {
  const isMobile = useIsMobile();
  const { resolvedTheme } = useTheme();
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  const primaryIcon = useFeatureStore((s) => s.machineIcon);
  const primaryPlatformOs = useFeatureStore((s) => s.machinePlatformOs);
  const allIds = useAllMachineIds();

  const hue = machineHue(machine?.machineId, allIds, resolvedTheme === "dark" ? "dark" : "light");
  const zone = worktreeZone({ worktreeBranch, branchMissing, projectBranch });
  const WorktreeGlyph = WORKTREE_GLYPH[zone.kind];
  // A user-picked icon wins; otherwise the machine's own OS marks it. The
  // primary keeps Monitor as its floor — "this machine" is not a Server.
  const MachineGlyph = machine
    ? resolveMachineGlyph(machine.icon, machine.platformOs)
    : (getMachineIcon(primaryIcon) ?? platformGlyph(primaryPlatformOs) ?? Monitor);

  const machineLabel = machine?.label ?? primaryLabel ?? "This machine";
  const hostTitle = machineTitle(machineLabel, {
    baseUrl: machine?.baseUrl,
    status,
    fault: fault?.detail,
  });

  const size = compact ? "text-[10px]" : "text-[11px]";
  const pad = compact ? "px-1.5 py-0.5" : "px-2 py-0.5";

  const hostZone = (
    <span
      className={cn(
        "inline-flex items-center gap-1 min-w-0 font-mono",
        size,
        pad,
        fault && "bg-destructive/15 text-destructive",
        // The primary machine is named, never announced: neutral ink, no
        // ground, no liveness dot. Only somewhere else gets a colour.
        !fault && !machine && "text-muted-foreground",
        !fault && machine && "font-medium",
      )}
      style={!fault && hue ? { backgroundColor: `${hue.bg}26`, color: hue.fg } : undefined}
    >
      <MachineGlyph className="size-2.5 shrink-0" />
      {/* Hostnames are routinely 12-16 characters, and "djarv01-…" answers
          nothing. The header has the room now that seven items left it; the
          mobile subline does not, so it keeps the tighter cap. */}
      <span className={cn("truncate", compact ? "max-w-[10ch]" : "max-w-[18ch]")}>
        {machineLabel}
      </span>
      {machine && (
        <span
          className={cn(
            "size-1.5 rounded-full shrink-0",
            fault && "bg-destructive",
            !fault && status === "connected" && "bg-success",
            !fault && status === "reconnecting" && "bg-warning animate-pulse",
            !fault && status === "disconnected" && "bg-muted-foreground",
          )}
        />
      )}
    </span>
  );

  // On the phone a *linked* worktree drops its branch and keeps its glyph. The
  // branch there is `session-<the session's own id>` — derived from the session
  // whose name is printed in full one line above — so it was 16ch of a 393px
  // band saying what the row already said. The kind still reads, because the
  // kind was always the glyph and the colour. The main-worktree case keeps its
  // words: that one names the *project's* branch, in amber, and it is the case
  // worth reading.
  const treeLabelled = !compact || zone.kind === "main" || zone.tone === "fault";

  const treeZone = (
    <span
      className={cn(
        "inline-flex items-center gap-1 min-w-0 font-mono border-l border-border/40",
        size,
        treeLabelled ? pad : compact ? "px-1 py-0.5" : pad,
        ZONE_TONE[zone.tone],
      )}
      title={treeLabelled ? undefined : zone.title}
    >
      <WorktreeGlyph className="size-2.5 shrink-0" />
      {treeLabelled && <span className="truncate max-w-[16ch]">{zone.label}</span>}
    </span>
  );

  const shell = cn(
    "inline-flex items-stretch min-w-0 rounded-md border border-border/40 bg-muted/30 overflow-hidden",
    // Compact is the phone's subline, where the pill shares 393px with a live
    // narration and has to be the thing that gives ground: `shrink-0` there
    // overflowed the identity button and painted the branch under the header's
    // own buttons. The desktop header has the room and keeps its full width.
    compact ? "shrink" : "shrink-0",
    className,
  );

  // One target, one sheet: a 22px zone is under the touch target, so the phone
  // never gets two.
  if (isMobile) {
    return (
      <Popover>
        <PopoverTrigger asChild>
          <button type="button" className={cn(shell, "cursor-pointer")} title={hostTitle}>
            {hostZone}
            {treeZone}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-64 p-3 space-y-3">
          <MachineSection machine={machine} label={machineLabel} status={status} fault={fault} />
          <div className="h-px bg-border/60" />
          <WorktreeSection zone={zone} worktreePath={worktreePath} projectPath={projectPath} />
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <span className={shell}>
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="cursor-pointer hover:brightness-110 transition-[filter] flex"
            title={hostTitle}
            aria-label={hostTitle}
          >
            {hostZone}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-64 p-3 space-y-3">
          <MachineSection machine={machine} label={machineLabel} status={status} fault={fault} />
        </PopoverContent>
      </Popover>
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="cursor-pointer hover:brightness-110 transition-[filter] flex min-w-0"
            title={zone.title}
            aria-label={zone.title}
          >
            {treeZone}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-64 p-3 space-y-3">
          <WorktreeSection zone={zone} worktreePath={worktreePath} projectPath={projectPath} />
        </PopoverContent>
      </Popover>
    </span>
  );
});

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
      {children}
    </span>
  );
}

function CopyRow({ value }: { value: string }) {
  const { copied, copy } = useCopyToClipboard();
  return (
    <button
      type="button"
      onClick={() => copy(value)}
      className="flex items-center gap-1.5 w-full text-left group cursor-pointer"
      title={copied ? "Copied" : `Copy ${value}`}
    >
      <span className="text-xs font-mono text-muted-foreground truncate flex-1">{value}</span>
      <span className="text-[10px] text-muted-foreground-faint group-hover:text-foreground shrink-0">
        {copied ? "copied" : "copy"}
      </span>
    </button>
  );
}

const STATUS_WORD: Record<MachineStatus, string> = {
  connected: "connected",
  reconnecting: "reconnecting",
  disconnected: "offline",
};

function MachineSection({
  machine,
  label,
  status,
  fault,
}: {
  machine: MachineEntry | null;
  label: string;
  status: MachineStatus;
  fault?: { detail: string } | null;
}) {
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between gap-2">
        <SectionLabel>Machine</SectionLabel>
        {machine && (
          <span
            className={cn(
              "text-[10px]",
              fault && "text-destructive",
              !fault && status === "connected" && "text-success",
              !fault && status !== "connected" && "text-muted-foreground",
            )}
          >
            {fault ? "credential rejected" : STATUS_WORD[status]}
          </span>
        )}
      </div>
      <div className="text-sm font-medium">{label}</div>
      {machine?.baseUrl && <CopyRow value={machine.baseUrl} />}
      {fault && <div className="text-[11px] text-destructive">{fault.detail}</div>}
      {!machine && (
        <div className="text-[11px] text-muted-foreground">
          The machine you are looking at — nothing routes over the network.
        </div>
      )}
    </div>
  );
}

function WorktreeSection({
  zone,
  worktreePath,
  projectPath,
}: {
  zone: WorktreeZone;
  worktreePath?: string | null;
  projectPath?: string | null;
}) {
  const Glyph = WORKTREE_GLYPH[zone.kind];
  return (
    <div className="space-y-1">
      <SectionLabel>Worktree</SectionLabel>
      <div className="flex items-center gap-1.5 text-sm font-medium">
        <Glyph className={cn("size-3.5 shrink-0", zone.kind === "main" && "text-warning")} />
        <span className="truncate">
          {WORKTREE_LABEL[zone.kind]}
          {zone.kind === "main" ? "" : ` · ${zone.label}`}
        </span>
      </div>
      <div className="text-[11px] text-muted-foreground">
        {zone.kind === "main"
          ? "Edits land in the checkout everything else is linked to."
          : "Edits are isolated from the project checkout."}
      </div>
      {worktreePath && <CopyRow value={worktreePath} />}
      {!worktreePath && projectPath && <CopyRow value={projectPath} />}
    </div>
  );
}
