import { Check, FolderGit2, Gauge, GitBranch, Globe } from "lucide-react";
import { useState } from "react";
import { CreateChannelDialog } from "~/components/chat/dialogs/CreateChannelDialog";
import { DeleteSessionDialog } from "~/components/chat/dialogs/DeleteSessionDialog";
import { JoinChannelDialog } from "~/components/chat/dialogs/JoinChannelDialog";
import { RenameSessionDialog } from "~/components/chat/dialogs/RenameSessionDialog";
import { MergeDropdown } from "~/components/chat/MergeDropdown";
import { SessionActionMenu } from "~/components/chat/SessionActionMenu";
import { SessionIdentity } from "~/components/chat/SessionIdentity";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { SessionStatusPill } from "~/components/layout/session/SessionStatusPill";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { useChannelManagement } from "~/hooks/session/useChannelManagement";
import { useSessionActions } from "~/hooks/session/useSessionActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import { EFFORT_COLORS, EFFORT_LABELS, type EffortLevel } from "~/lib/composer-constants";
import { cn, sessionShortId } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { useBrowserStore } from "~/stores/browser-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useUIStore } from "~/stores/ui-store";

interface SessionHeaderProps {
  meta: SessionMetadata;
  hasPendingInput: boolean;
  /** Tab bar rendered inline in the header on desktop. */
  tabBar?: React.ReactNode;
  /** Project accent color hex for the top border. */
  accentColor?: string;
  /** Git actions for the session — enables inline merge dropdown on desktop. */
  git?: ReturnType<typeof useGitActions>;
  /** Project-level git status — used to surface uncommitted-dirty warning on merge. */
  projectGitStatus?: ProjectGitStatus;
}

type ActiveDialog = "none" | "delete" | "create-channel" | "join-channel" | "rename";

export function SessionHeader({
  meta,
  hasPendingInput,
  tabBar,
  accentColor,
  git,
  projectGitStatus,
}: SessionHeaderProps) {
  const ws = useWebSocket();
  const isMobile = useIsMobile();
  const browserEnabled = useFeatureStore((s) => s.features.browser);
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

  return (
    <>
      <PageHeader accentColor={accentColor}>
        <SessionStatusPill
          state={meta.state}
          connected={meta.connected}
          hasPendingApproval={hasPendingInput}
          compact={isMobile}
        />

        {/* Identity zone: name + detail popover / inline rename */}
        <SessionIdentity
          meta={meta}
          sessionRef={sessionRef}
          onRename={actions.rename}
          onIconChange={actions.handleIconChange}
        />

        {/* Navigation zone: tab bar (desktop only, with extra left spacing) */}
        {!isMobile && tabBar && (
          <div className="flex items-stretch gap-1 ml-6 self-stretch">{tabBar}</div>
        )}

        {/* Actions zone */}
        <div className="ml-auto flex items-center gap-1.5">
          <ReadOnlyIndicators
            effort={meta.effort as EffortLevel | undefined}
            isWorktree={isWorktree}
            worktreeBranch={meta.worktreeBranch}
          />

          {isMobile && <ConnectionIndicator />}

          {!isMobile && browserEnabled && <BrowserToggle sessionId={meta.id} />}

          {!isMobile && git && canMerge && (
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

          {(meta.state === "idle" || meta.state === "stopped" || meta.state === "failed") && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs text-muted-foreground hover:text-success gap-1"
              title="Mark done"
              onClick={actions.handleMarkDone}
            >
              <Check className="h-3.5 w-3.5" />
              {!isMobile && <span>Done</span>}
            </Button>
          )}

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
        </div>
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

function BrowserToggle({ sessionId }: { sessionId: string }) {
  const rightPanelCollapsed = useUIStore((s) => s.rightPanelCollapsed);
  const setRightPanelCollapsed = useUIStore((s) => s.setRightPanelCollapsed);
  const launched = useBrowserStore((s) => s.sessions[sessionId]?.launched ?? false);

  const handleToggle = () => {
    setRightPanelCollapsed(!rightPanelCollapsed);
  };

  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn(
        "h-7 px-2 text-xs gap-1 relative",
        rightPanelCollapsed ? "text-muted-foreground" : "text-foreground",
      )}
      title="Toggle browser panel"
      onClick={handleToggle}
    >
      <Globe className="h-3.5 w-3.5" />
      {launched && <span className="absolute top-0.5 right-0.5 size-2 rounded-full bg-green-500" />}
    </Button>
  );
}
