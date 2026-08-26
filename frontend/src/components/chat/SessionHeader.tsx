import {
  Archive,
  ArchiveRestore,
  ArrowUp,
  Clock,
  FolderGit2,
  Gauge,
  GitBranch,
  Server,
} from "lucide-react";
import { useState } from "react";
import { CreateChannelDialog } from "~/components/chat/dialogs/CreateChannelDialog";
import { DeleteSessionDialog } from "~/components/chat/dialogs/DeleteSessionDialog";
import { JoinChannelDialog } from "~/components/chat/dialogs/JoinChannelDialog";
import { RenameSessionDialog } from "~/components/chat/dialogs/RenameSessionDialog";
import { MergeDropdown } from "~/components/chat/MergeDropdown";
import { SessionActionMenu } from "~/components/chat/SessionActionMenu";
import { SessionIdentity } from "~/components/chat/SessionIdentity";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { ProjectGitPill } from "~/components/layout/git/ProjectGitPill";
import { PageHeader } from "~/components/layout/PageHeader";
import {
  resolveSessionState,
  resolveStatusLabel,
  SessionBadge,
} from "~/components/layout/session/SessionBadge";
import { SessionStatusPill } from "~/components/layout/session/SessionStatusPill";
import { MachineChip, useProjectMachine } from "~/components/machines/MachineChip";
import { untilText } from "~/components/schedules/schedule-format";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { useChannelManagement } from "~/hooks/session/useChannelManagement";
import { useSessionActions } from "~/hooks/session/useSessionActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useNow } from "~/hooks/useNow";
import { useWebSocket } from "~/hooks/useWebSocket";
import { EFFORT_COLORS, EFFORT_LABELS, type EffortLevel } from "~/lib/composer-constants";
import type { ScheduleInfo } from "~/lib/schedule-actions";
import { cn, sessionShortId } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useScheduleStore } from "~/stores/schedule-store";

interface SessionHeaderProps {
  meta: SessionMetadata;
  hasPendingInput: boolean;
  /**
   * The session dock's toggle. Rendered here rather than built here: what the
   * dock has to show is derived from session state the panel already holds.
   */
  dockToggle?: React.ReactNode;
  /** Project accent color hex for the top border. */
  accentColor?: string;
  /** Git actions for the session — enables inline merge dropdown on desktop. */
  git?: ReturnType<typeof useGitActions>;
  /** Project-level git status — used to surface uncommitted-dirty warning on merge. */
  projectGitStatus?: ProjectGitStatus;
  /**
   * Take the user to the pending approval or question. Omitted when they are
   * already looking at it, which is what keeps the pill from offering a jump
   * that goes nowhere.
   */
  onGoToPendingInput?: () => void;
}

type ActiveDialog = "none" | "delete" | "create-channel" | "join-channel" | "rename";

export function SessionHeader({
  meta,
  hasPendingInput,
  dockToggle,
  accentColor,
  git,
  projectGitStatus,
  onGoToPendingInput,
}: SessionHeaderProps) {
  const ws = useWebSocket();
  const isMobile = useIsMobile();
  const isRunning = meta.state === "running";
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = isRunning;
  const canStop = meta.state === "idle" || meta.state === "running" || meta.state === "merging";
  const canRestart = canStop || meta.state === "stopped" || meta.state === "failed";

  const [activeDialog, setActiveDialog] = useState<ActiveDialog>("none");

  const actions = useSessionActions(ws, meta);
  const channel = useChannelManagement(ws, meta);

  const projectSlug = useAppStore((s) => s.projects.find((p) => p.id === meta.projectId)?.slug);
  const shortId = sessionShortId(meta.id);
  const sessionRef = projectSlug ? `${projectSlug}/${shortId}` : shortId;

  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;
  const isMerged = meta.worktreeMerged && ahead === 0 && behind === 0;
  const canMerge = !!git && isWorktree && !meta.branchMissing && !isMerged && ahead > 0 && !isBusy;
  const projectDirty = !!projectGitStatus && projectGitStatus.uncommittedCount > 0;
  const hasUncommitted = !!git?.uncommittedFiles && git.uncommittedFiles.length > 0;

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

  return (
    <>
      <PageHeader accentColor={accentColor}>
        {isMobile ? (
          // Mobile: the full name owns the header (up to two lines) with a dim
          // status/branch/ahead subline. Tapping opens the detail sheet. The
          // finish action lives on the tab strip below (see ChatPanel), and the
          // read-only effort/branch chips move into the sheet.
          <>
            <SessionIdentity
              meta={meta}
              sessionRef={sessionRef}
              onRename={actions.rename}
              onIconChange={actions.handleIconChange}
              stacked
              subline={<MobileSubline meta={meta} hasPendingApproval={hasPendingInput} />}
            />
            <div className="ml-auto flex items-center gap-1 shrink-0">
              {projectSlug && !isWorktree && (
                <ProjectGitPill
                  projectId={meta.projectId}
                  projectSlug={projectSlug}
                  gitStatus={projectGitStatus}
                />
              )}
              <ConnectionIndicator />
              {actionMenu}
            </div>
          </>
        ) : (
          <>
            <SessionStatusPill
              state={meta.state}
              connected={meta.connected}
              hasPendingApproval={hasPendingInput}
              compact={false}
              // The pill is outside the tab switch, so it reports a blocked
              // session from every tab — but the approve/deny buttons only
              // exist on the chat branch. Being told you are blocked and left
              // to find your own way back is half an answer.
              onActivate={hasPendingInput ? onGoToPendingInput : undefined}
              activateHint="open the chat to respond"
            />

            {/* Identity zone: name + detail popover / inline rename */}
            <SessionIdentity
              meta={meta}
              sessionRef={sessionRef}
              onRename={actions.rename}
              onIconChange={actions.handleIconChange}
            />

            {/* Actions zone */}
            <div className="ml-auto flex items-center gap-1.5">
              <ParkedScheduleChip sessionId={meta.id} state={meta.state} />
              <MachineChip projectId={meta.projectId} />
              <ReadOnlyIndicators
                effort={meta.effort as EffortLevel | undefined}
                isWorktree={isWorktree}
                worktreeBranch={meta.worktreeBranch}
              />

              {dockToggle}

              {git && canMerge && (
                <MergeDropdown
                  git={git}
                  projectDirty={projectDirty}
                  className={cn(
                    "h-7 px-2 text-xs border",
                    meta.mergeStatus === "clean" && !hasUncommitted
                      ? "bg-success/10 text-success border-success/30 hover:bg-success/20"
                      : "hover:bg-accent",
                  )}
                />
              )}

              {/* Push belongs to the checkout you are standing in. A worktree
                  session has two different "ahead" counts — its branch vs
                  main, and main vs origin — so it talks only about its own
                  branch (merge, above) and leaves the project checkout to the
                  sidebar's sync dock. */}
              {projectSlug && !isWorktree && (
                <ProjectGitPill
                  projectId={meta.projectId}
                  projectSlug={projectSlug}
                  gitStatus={projectGitStatus}
                  labelled
                />
              )}

              {/* Files the session away; it does not end a run, which is why
                  the server refuses it mid-turn and this hides there. */}
              {(meta.state === "idle" || meta.state === "stopped" || meta.state === "failed") && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-xs text-muted-foreground hover:text-success gap-1"
                  title={meta.archivedAt ? "Unarchive session" : "Archive session"}
                  onClick={meta.archivedAt ? actions.handleUnarchive : actions.handleArchive}
                >
                  {meta.archivedAt ? (
                    <ArchiveRestore className="h-3.5 w-3.5" />
                  ) : (
                    <Archive className="h-3.5 w-3.5" />
                  )}
                  <span>{meta.archivedAt ? "Unarchive" : "Archive"}</span>
                </Button>
              )}

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
}: {
  meta: SessionMetadata;
  hasPendingApproval: boolean;
}) {
  const badgeState = resolveSessionState({ state: meta.state, hasPendingApproval });
  const label = resolveStatusLabel({ state: meta.state, badgeState, connected: meta.connected });
  const branch = meta.worktreeBranch;
  const ahead = meta.commitsAhead ?? 0;
  const machine = useProjectMachine(meta.projectId);
  // Parked loop session: "Stopped" would read as dead — show the next fire
  // and which schedule owns it instead.
  const nextSchedule = useNextSchedule(meta.id);
  const now = useNow();
  const parked = meta.state === "stopped" ? nextSchedule : null;
  return (
    <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground min-w-0">
      <SessionBadge state={badgeState} size="sm" bare />
      <span className="truncate">
        {parked
          ? `next ${untilText(parked.nextRunAt, now)} · ${parked.name}`
          : `${label}${branch ? ` · ${branch}` : ""}`}
      </span>
      {machine && (
        <span className="flex items-center gap-0.5 shrink-0" title={`Runs on ${machine.label}`}>
          <Server className="size-2.5" />
          <span className="truncate max-w-[10ch]">{machine.label}</span>
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

function ReadOnlyIndicators({
  effort,
  isWorktree,
  worktreeBranch,
}: {
  effort: EffortLevel | undefined;
  isWorktree: boolean;
  worktreeBranch?: string;
}) {
  const effortLabel = effort ? EFFORT_LABELS[effort] : undefined;
  const effortColor = effort ? EFFORT_COLORS[effort] : undefined;
  const hasEffort = !!effort && !!effortLabel;
  return (
    <>
      {hasEffort && (
        <span
          className={cn(
            "inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-muted/40 shrink-0",
            effortColor,
          )}
          title={`Reasoning effort: ${effortLabel}`}
        >
          <Gauge className="h-2.5 w-2.5" />
          <span className="max-sm:hidden">{effortLabel}</span>
        </span>
      )}
      {isWorktree ? (
        <span
          className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-muted/40 text-muted-foreground shrink-0 min-w-0"
          title={worktreeBranch ? `Worktree: ${worktreeBranch}` : "Worktree"}
        >
          <GitBranch className="h-2.5 w-2.5 shrink-0" />
          <span className="truncate max-w-[8ch] sm:max-w-[12ch]">
            {worktreeBranch ?? "worktree"}
          </span>
        </span>
      ) : (
        <span
          className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border border-warning/40 bg-warning/10 text-warning shrink-0"
          title="Running directly in the project's working directory (no worktree isolation)"
        >
          <FolderGit2 className="h-2.5 w-2.5 shrink-0" />
          <span className="max-sm:hidden">local</span>
        </span>
      )}
    </>
  );
}
