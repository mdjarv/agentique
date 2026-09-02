import { Archive, ArchiveRestore, ArrowUp, Clock, Pin, PinOff } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { BranchSyncControl } from "~/components/chat/BranchSyncControl";
import { CreateChannelDialog } from "~/components/chat/dialogs/CreateChannelDialog";
import { DeleteSessionDialog } from "~/components/chat/dialogs/DeleteSessionDialog";
import { JoinChannelDialog } from "~/components/chat/dialogs/JoinChannelDialog";
import { RenameSessionDialog } from "~/components/chat/dialogs/RenameSessionDialog";
import { SessionActionMenu } from "~/components/chat/SessionActionMenu";
import { SessionIdentity } from "~/components/chat/SessionIdentity";
import { SessionLocation } from "~/components/chat/SessionLocation";
import { hasLiveWork, SessionWorkLine } from "~/components/chat/SessionWorkLine";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import {
  resolveSessionState,
  resolveStatusLabel,
  SessionBadge,
} from "~/components/layout/session/SessionBadge";
import {
  useMachineFault,
  useMachineStatus,
  useProjectMachine,
} from "~/components/machines/MachineChip";
import { untilText } from "~/components/schedules/schedule-format";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { useChannelManagement } from "~/hooks/session/useChannelManagement";
import { useSessionActions } from "~/hooks/session/useSessionActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useNow } from "~/hooks/useNow";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import { machineHue, machineWash } from "~/lib/machine-colors";
import type { ScheduleInfo } from "~/lib/schedule-actions";
import { archiveSession, setSessionPinned, unarchiveSession } from "~/lib/session/actions";
import { getErrorMessage, sessionShortId } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";
import { useScheduleStore } from "~/stores/schedule-store";

interface SessionHeaderProps {
  meta: SessionMetadata;
  hasPendingInput: boolean;
  /**
   * The session dock's toggle. Rendered here rather than built here: what the
   * dock has to show is derived from session state the panel already holds.
   */
  dockToggle?: React.ReactNode;
  /**
   * Subagents still out. Carried separately from `meta.state` because a
   * background agent outlives the turn that spawned it: the run settles to idle
   * while the work continues.
   */
  agentsInFlight?: number;
  /** Project accent color hex for the top border. */
  accentColor?: string;
  /** Git actions for the session — enables inline merge dropdown on desktop. */
  git?: ReturnType<typeof useGitActions>;
  /** Project-level git status — used to surface uncommitted-dirty warning on merge. */
  projectGitStatus?: ProjectGitStatus;
  /** The project's main branch, named in the rebase tooltip and the merge menu. */
  mainBranch?: string;
  /**
   * Sends a prompt to the session. The branch control needs it for one state
   * only — handing a conflicted branch to the agent, which is the established
   * answer to conflicts and not something the header can do itself.
   */
  onSendMessage?: (prompt: string) => void;
}

type ActiveDialog = "none" | "delete" | "create-channel" | "join-channel" | "rename";

export function SessionHeader({
  meta,
  hasPendingInput,
  dockToggle,
  agentsInFlight = 0,
  accentColor,
  git,
  projectGitStatus,
  mainBranch,
  onSendMessage,
}: SessionHeaderProps) {
  const ws = useWebSocket();
  const isMobile = useIsMobile();
  const isRunning = meta.state === "running";
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = isRunning;
  const canStop = meta.state === "idle" || meta.state === "running" || meta.state === "merging";
  const canRestart = canStop || meta.state === "stopped" || meta.state === "failed";
  const canArchive = meta.state !== "running" && meta.state !== "merging";

  const [activeDialog, setActiveDialog] = useState<ActiveDialog>("none");

  const actions = useSessionActions(ws, meta);
  const channel = useChannelManagement(ws, meta);

  const projectSlug = useAppStore((s) => s.projects.find((p) => p.id === meta.projectId)?.slug);
  const projectPath = useAppStore((s) => s.projects.find((p) => p.id === meta.projectId)?.path);
  const shortId = sessionShortId(meta.id);
  const sessionRef = projectSlug ? `${projectSlug}/${shortId}` : shortId;

  // The machine the session's project lives on — null for the primary. The
  // wash reads the same hue, so both come from one place.
  const machine = useProjectMachine(meta.projectId);
  const machineStatus = useMachineStatus(machine?.machineId);
  const machineFault = useMachineFault(machine?.machineId);
  const { resolvedTheme } = useTheme();
  const allMachineIds = useMachineStore(useShallow((s) => Object.keys(s.machines)));
  const hue = machineHue(
    machine?.machineId,
    allMachineIds,
    resolvedTheme === "dark" ? "dark" : "light",
  );
  // Recognition, not identification: the wash says "somewhere else, and which
  // somewhere" to a reader who has learned it, while the pill's first zone says
  // it in words to one who has not. It drains when the machine is away, in the
  // same moment the composer disables itself.
  const wash = machineWash(hue, { away: !!machine && machineStatus !== "connected" });

  // The overflow menu is identical on both layouts — declared once, placed in
  // whichever actions zone is rendered.
  const actionMenu = (
    <SessionActionMenu
      isMobile={isMobile}
      sessionRef={sessionRef}
      canStop={canStop}
      canRestart={canRestart}
      isWorktree={isWorktree}
      isBusy={isBusy}
      hasChannel={channel.hasChannel}
      cleaning={actions.cleaning}
      onStop={actions.handleStop}
      onRestart={actions.handleRestart}
      onResetConversation={actions.handleResetConversation}
      onRename={() => setActiveDialog("rename")}
      onClean={actions.handleClean}
      onLeaveChannel={channel.handleLeaveChannel}
      onCreateChannel={() => {
        channel.resetChannelForm();
        setActiveDialog("create-channel");
      }}
      onJoinChannel={async () => {
        if (await channel.openJoinChannel()) setActiveDialog("join-channel");
      }}
      onDelete={() => setActiveDialog("delete")}
    />
  );

  const placementActions = <SessionPlacementActions meta={meta} canArchive={canArchive} />;

  return (
    <>
      <PageHeader accentColor={accentColor} wash={wash}>
        {isMobile ? (
          // Mobile: the full name owns the header (up to two lines) with a dim
          // status/branch/ahead subline. Tapping opens the detail sheet. The
          // branch finish action lives on the strip below (see ChatPanel), and
          // the read-only effort/branch chips move into the sheet.
          <>
            <SessionIdentity
              meta={meta}
              sessionRef={sessionRef}
              onRename={actions.rename}
              onIconChange={actions.handleIconChange}
              stacked
              subline={
                <MobileSubline
                  meta={meta}
                  hasPendingApproval={hasPendingInput}
                  agentsInFlight={agentsInFlight}
                  projectBranch={projectGitStatus?.branch}
                />
              }
            />
            <div className="ml-auto flex items-center gap-1 shrink-0">
              {/* One navigation model, two presentations: the same control, and
                  on this layout it opens the dock as a sheet. Without it the
                  sheet is unreachable — only a `?dock=` deep link could open
                  it. */}
              {dockToggle}
              {placementActions}
              <ConnectionIndicator />
              {actionMenu}
            </div>
          </>
        ) : (
          <>
            {/* Identity zone: name + detail popover / inline rename */}
            <SessionIdentity
              meta={meta}
              sessionRef={sessionRef}
              onRename={actions.rename}
              onIconChange={actions.handleIconChange}
            />

            {/* Where this session's code lives — machine and worktree, one
                element, because they are two segments of one address and the
                expensive case is the one where both light at once. */}
            <SessionLocation
              machine={machine}
              status={machineStatus}
              fault={machineFault}
              worktreeBranch={meta.worktreeBranch}
              branchMissing={meta.branchMissing}
              worktreePath={meta.worktreePath}
              projectBranch={projectGitStatus?.branch}
              projectPath={projectPath}
            />

            {/* Actions zone */}
            <div className="ml-auto flex items-center gap-1.5">
              <ParkedScheduleChip sessionId={meta.id} state={meta.state} />

              {dockToggle}

              {/* One slot, naming the verb the branch needs — rebase when
                  behind, merge when ahead, and on a diverged branch rebase with
                  merge demoted behind its caret. Same control on both layouts;
                  the rule is `branchSync`, read from one place. */}
              <BranchSyncControl
                meta={meta}
                git={git}
                projectGitStatus={projectGitStatus}
                mainBranch={mainBranch}
                onSendMessage={onSendMessage}
                className="h-7 px-2 text-xs [&>button]:h-7 [&>button]:text-xs"
              />

              {placementActions}

              {actionMenu}
            </div>
          </>
        )}
      </PageHeader>

      <DeleteSessionDialog
        open={activeDialog === "delete"}
        onOpenChange={(open) => setActiveDialog(open ? "delete" : "none")}
        sessionName={meta.name}
        onDelete={async () => {
          if (await actions.handleDelete()) setActiveDialog("none");
        }}
        deleting={actions.deleting}
      />
      <RenameSessionDialog
        open={activeDialog === "rename"}
        onOpenChange={(open) => setActiveDialog(open ? "rename" : "none")}
        sessionId={meta.id}
        currentName={meta.name}
        onSubmit={async (name) => {
          if (await actions.handleRename(name)) setActiveDialog("none");
        }}
        saving={actions.renaming}
      />
      <CreateChannelDialog
        open={activeDialog === "create-channel"}
        onOpenChange={(open) => setActiveDialog(open ? "create-channel" : "none")}
        channelName={channel.channelName}
        onChannelNameChange={channel.setChannelName}
        channelRole={channel.channelRole}
        onChannelRoleChange={channel.setChannelRole}
        onSubmit={async () => {
          if (await channel.handleCreateChannel()) setActiveDialog("none");
        }}
      />
      <JoinChannelDialog
        open={activeDialog === "join-channel"}
        onOpenChange={(open) => setActiveDialog(open ? "join-channel" : "none")}
        channels={channel.availableChannels}
        selectedChannelId={channel.selectedChannelId}
        onSelectedChannelIdChange={channel.setSelectedChannelId}
        channelRole={channel.channelRole}
        onChannelRoleChange={channel.setChannelRole}
        onSubmit={async () => {
          if (await channel.handleJoinChannel()) setActiveDialog("none");
        }}
      />
    </>
  );
}

function SessionPlacementActions({
  meta,
  canArchive,
}: {
  meta: SessionMetadata;
  canArchive: boolean;
}) {
  const ws = useWebSocket();
  const archived = !!meta.archivedAt;
  const pinLabel = meta.pinned ? "Unpin session" : "Pin session";
  const archiveLabel = archived ? "Unarchive session" : "Archive session";
  const PinIcon = meta.pinned ? PinOff : Pin;

  const togglePin = () => {
    setSessionPinned(ws, meta.id, !meta.pinned, meta.pinned ? 0 : meta.pinOrder + 1).catch((err) =>
      toast.error(getErrorMessage(err, "Failed to update pin")),
    );
  };

  const toggleArchive = () => {
    const action = archived ? unarchiveSession : archiveSession;
    action(ws, meta.id).catch((err) =>
      toast.error(
        getErrorMessage(
          err,
          archived ? "Failed to unarchive session" : "Failed to archive session",
        ),
      ),
    );
  };

  return (
    <fieldset className="flex items-center gap-0.5" aria-label="Session placement">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={togglePin}
        aria-label={pinLabel}
        aria-pressed={meta.pinned}
        title={pinLabel}
        className={
          meta.pinned ? "bg-primary/10 text-primary hover:bg-primary/15" : "text-muted-foreground"
        }
      >
        <PinIcon className="size-3.5" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={toggleArchive}
        disabled={!archived && !canArchive}
        aria-label={
          !archived && !canArchive
            ? "Archive session, unavailable while session is running"
            : archiveLabel
        }
        aria-pressed={archived}
        title={
          !archived && !canArchive ? "Archive unavailable while session is running" : archiveLabel
        }
        className="text-muted-foreground"
      >
        {archived ? <ArchiveRestore className="size-3.5" /> : <Archive className="size-3.5" />}
      </Button>
    </fieldset>
  );
}

// Parked-state presentation (docs/scheduled-loops.md, "The parked state must
// not read as dead"): the earliest enabled schedule with a queued next fire
// for this session, or null. Returns a store object reference (or null) —
// stable across renders, safe as a zustand selector.
function useNextSchedule(sessionId: string): ScheduleInfo | null {
  return useScheduleStore((s) => {
    let best: ScheduleInfo | null = null;
    for (const sched of Object.values(s.schedules)) {
      if (sched.sessionId !== sessionId || !sched.enabled || !sched.nextRunAt) continue;
      if (!best || sched.nextRunAt < best.nextRunAt) best = sched;
    }
    return best;
  });
}

// Desktop read-only chip for a parked loop session: stopped/evicted but with a
// schedule that will resume it — the header must not read as dead.
function ParkedScheduleChip({ sessionId, state }: { sessionId: string; state: string }) {
  const next = useNextSchedule(sessionId);
  const now = useNow();
  if (state !== "stopped" || !next) return null;
  return (
    <span
      className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-muted/40 text-muted-foreground shrink-0"
      title={`Parked — "${next.name}" fires ${untilText(next.nextRunAt, now)}`}
    >
      <Clock className="h-2.5 w-2.5 shrink-0" />
      <span>Next {untilText(next.nextRunAt, now)}</span>
    </span>
  );
}

// The dim metadata line under the name on mobile: a status dot + label, the
// branch, and the commits-ahead count — the essentials that were scattered
// across chips before, now glanceable without a tap.
function MobileSubline({
  meta,
  hasPendingApproval,
  agentsInFlight,
  projectBranch,
}: {
  meta: SessionMetadata;
  hasPendingApproval: boolean;
  agentsInFlight: number;
  projectBranch?: string;
}) {
  const badgeState = resolveSessionState({ state: meta.state, hasPendingApproval });
  const label = resolveStatusLabel({ state: meta.state, badgeState, connected: meta.connected });
  const ahead = meta.commitsAhead ?? 0;
  const machine = useProjectMachine(meta.projectId);
  const machineStatus = useMachineStatus(machine?.machineId);
  const machineFault = useMachineFault(machine?.machineId);
  // Parked loop session: "Stopped" would read as dead — show the next fire
  // and which schedule owns it instead.
  const nextSchedule = useNextSchedule(meta.id);
  const now = useNow();
  const parked = meta.state === "stopped" ? nextSchedule : null;
  // While there is work to narrate, the narration wins the line: "editing
  // derive.ts · 12 tool calls" is what the operator came to know, and the
  // branch is one tap away in the detail sheet.
  const live = !parked && hasLiveWork({ state: meta.state, agentsInFlight });
  return (
    <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground min-w-0">
      <SessionBadge state={badgeState} size="sm" bare />
      {live ? (
        <SessionWorkLine
          sessionId={meta.id}
          state={meta.state}
          agentsInFlight={agentsInFlight}
          className="min-w-0 flex-1"
        />
      ) : (
        <span className="truncate">
          {parked ? `next ${untilText(parked.nextRunAt, now)} · ${parked.name}` : label}
        </span>
      )}
      {/* The same element the desktop header carries, at the smaller size —
          one location, one reading, whichever layout you are on. */}
      <SessionLocation
        machine={machine}
        status={machineStatus}
        fault={machineFault}
        worktreeBranch={meta.worktreeBranch}
        branchMissing={meta.branchMissing}
        worktreePath={meta.worktreePath}
        projectBranch={projectBranch}
        compact
      />
      {ahead > 0 && (
        <span className="flex items-center gap-0.5 shrink-0 text-success">
          <ArrowUp className="size-2.5" />
          {ahead}
        </span>
      )}
      {ahead > 0 && (
        <span className="flex items-center gap-0.5 shrink-0 text-success">
          <ArrowUp className="size-2.5" />
          {ahead}
        </span>
      )}
    </span>
  );
}
