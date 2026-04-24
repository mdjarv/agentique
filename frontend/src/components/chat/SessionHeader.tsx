import {
  Check,
  Copy,
  Eraser,
  Gauge,
  GitBranch,
  Globe,
  Loader2,
  LogOut,
  MessageSquareX,
  MoreHorizontal,
  Pencil,
  RotateCcw,
  Square,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { CreateChannelDialog } from "~/components/chat/dialogs/CreateChannelDialog";
import { DeleteSessionDialog } from "~/components/chat/dialogs/DeleteSessionDialog";
import { JoinChannelDialog } from "~/components/chat/dialogs/JoinChannelDialog";
import { RenameSessionDialog } from "~/components/chat/dialogs/RenameSessionDialog";
import { IconPicker } from "~/components/chat/IconPicker";
import { MergeDropdown } from "~/components/chat/MergeDropdown";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { SessionStatusPill } from "~/components/layout/session/SessionStatusPill";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type ChannelInfo,
  createChannel,
  joinChannel,
  leaveChannel,
  listChannels,
} from "~/lib/channel-actions";
import { EFFORT_COLORS, EFFORT_LABELS, type EffortLevel } from "~/lib/composer-constants";
import {
  cleanSession,
  deleteSession,
  markSessionDone,
  renameSession,
  resetConversation,
  restartSession,
  setSessionIcon,
  stopSession,
} from "~/lib/session/actions";
import { getSessionIconComponent } from "~/lib/session/icons";
import { cn, getErrorMessage, sessionShortId } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { useBrowserStore } from "~/stores/browser-store";
import { useChannelStore } from "~/stores/channel-store";
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

  const [activeDialog, setActiveDialog] = useState<
    "none" | "delete" | "create-channel" | "join-channel" | "rename"
  >("none");
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  const [channelName, setChannelName] = useState("");
  const [channelRole, setChannelRole] = useState("");
  const [availableChannels, setAvailableChannels] = useState<ChannelInfo[]>([]);
  const [selectedChannelId, setSelectedChannelId] = useState("");
  const hasChannel = !!(meta.channelIds && meta.channelIds.length > 0);
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(meta.name);
  const [popoverOpen, setPopoverOpen] = useState(false);
  const { copied: refCopied, copy: copyRef } = useCopyToClipboard();

  const handleIconChange = useCallback(
    (icon: string | undefined) => {
      setSessionIcon(ws, meta.id, icon).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set icon"));
      });
    },
    [ws, meta.id],
  );
  const inputRef = useRef<HTMLInputElement>(null);
  const projectSlug = useAppStore((s) => s.projects.find((p) => p.id === meta.projectId)?.slug);
  const shortId = sessionShortId(meta.id);
  const sessionRef = projectSlug ? `${projectSlug}/${shortId}` : shortId;

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  useEffect(() => {
    if (!editing) setEditName(meta.name);
  }, [meta.name, editing]);

  const commitRename = () => {
    const trimmed = editName.trim();
    setEditing(false);
    if (trimmed && trimmed !== meta.name) {
      renameSession(ws, meta.id, trimmed).catch((err) => {
        toast.error(getErrorMessage(err, "Rename failed"));
      });
    } else {
      setEditName(meta.name);
    }
  };

  const handleRename = async (newName: string) => {
    setRenaming(true);
    try {
      await renameSession(ws, meta.id, newName);
      setActiveDialog("none");
    } catch (err) {
      toast.error(getErrorMessage(err, "Rename failed"));
    } finally {
      setRenaming(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteSession(ws, meta.id);
      setActiveDialog("none");
    } catch (err) {
      setDeleting(false);
      toast.error(getErrorMessage(err, "Delete failed"));
    }
  };

  const handleClean = useCallback(async () => {
    setCleaning(true);
    try {
      const r = await cleanSession(ws, meta.id);
      if (r.status === "cleaned") {
        toast.success("Cleaned");
      } else {
        toast.error(r.error ?? "Clean failed");
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Clean failed"));
    } finally {
      setCleaning(false);
    }
  }, [ws, meta.id]);

  const handleStop = useCallback(async () => {
    try {
      await stopSession(ws, meta.id);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to stop"));
    }
  }, [ws, meta.id]);

  const handleRestart = useCallback(async () => {
    try {
      await restartSession(ws, meta.id);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to restart"));
    }
  }, [ws, meta.id]);

  const handleResetConversation = useCallback(async () => {
    try {
      await resetConversation(ws, meta.id);
      toast.success("Conversation reset — next message starts fresh");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to reset conversation"));
    }
  }, [ws, meta.id]);

  const handleOpenJoinChannel = useCallback(async () => {
    try {
      const channels = await listChannels(ws, meta.projectId);
      setAvailableChannels(channels);
      setSelectedChannelId(channels[0]?.id ?? "");
      setChannelRole("");
      setActiveDialog("join-channel");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to load channels"));
    }
  }, [ws, meta.projectId]);

  const handleCreateChannel = useCallback(async () => {
    const name = channelName.trim();
    if (!name) return;
    try {
      const created = await createChannel(ws, meta.projectId, name);
      const ch = await joinChannel(ws, meta.id, created.id, channelRole.trim());
      useChannelStore.getState().addChannel(ch);
      setActiveDialog("none");
      setChannelName("");
      setChannelRole("");
      toast.success("Channel created");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to create channel"));
    }
  }, [ws, meta.projectId, meta.id, channelName, channelRole]);

  const handleJoinChannel = useCallback(async () => {
    if (!selectedChannelId) return;
    try {
      const ch = await joinChannel(ws, meta.id, selectedChannelId, channelRole.trim());
      useChannelStore.getState().addChannel(ch);
      setActiveDialog("none");
      setChannelRole("");
      toast.success("Joined channel");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to join channel"));
    }
  }, [ws, meta.id, selectedChannelId, channelRole]);

  const handleLeaveChannel = useCallback(async () => {
    try {
      const channelIds = meta.channelIds ?? [];
      await Promise.all(channelIds.map((chId) => leaveChannel(ws, meta.id, chId)));
      toast.success("Left channel");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to leave channel"));
    }
  }, [ws, meta.id, meta.channelIds]);

  const SessionIcon = getSessionIconComponent(meta.icon);

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

        {/* Identity zone: name (click for detail popover) */}
        {editing ? (
          <input
            ref={inputRef}
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") {
                setEditName(meta.name);
                setEditing(false);
              }
            }}
            className="font-medium truncate bg-transparent border-b border-border outline-none px-0 py-0 text-sm w-48"
          />
        ) : !isMobile ? (
          <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                className="flex items-center gap-1.5 min-w-0 rounded-md px-1.5 py-0.5 hover:bg-accent transition-colors cursor-pointer"
              >
                <SessionIcon className="size-3.5 text-agent shrink-0" />
                <span
                  className={cn(
                    "truncate font-medium text-sm",
                    !meta.name && "italic text-muted-foreground",
                  )}
                >
                  {meta.name || "Untitled"}
                </span>
              </button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-64 p-3 space-y-3">
              {/* Rename */}
              <div className="space-y-1">
                <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                  Name
                </span>
                <div className="flex items-center gap-1.5">
                  <span
                    className={cn(
                      "text-sm font-medium truncate flex-1",
                      !meta.name && "italic text-muted-foreground",
                    )}
                  >
                    {meta.name || "Untitled"}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => {
                      setPopoverOpen(false);
                      setEditing(true);
                    }}
                    className="text-muted-foreground hover:text-foreground shrink-0"
                  >
                    <Pencil className="size-3" />
                  </Button>
                </div>
              </div>

              {/* Session ref */}
              <div className="space-y-1">
                <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                  Reference
                </span>
                <div className="flex items-center gap-1.5">
                  <span className="text-xs font-mono text-muted-foreground">{sessionRef}</span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => copyRef(sessionRef)}
                    className="text-muted-foreground hover:text-foreground shrink-0"
                  >
                    {refCopied ? <Check className="size-2.5" /> : <Copy className="size-2.5" />}
                  </Button>
                </div>
              </div>

              {/* Agent profile */}
              {meta.agentProfileName && (
                <div className="space-y-1">
                  <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                    Agent
                  </span>
                  <div className="flex items-center gap-1.5 text-sm">
                    {meta.agentProfileAvatar && <span>{meta.agentProfileAvatar}</span>}
                    <span>{meta.agentProfileName}</span>
                  </div>
                </div>
              )}

              {/* Icon picker */}
              <div className="space-y-1">
                <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                  Icon
                </span>
                <IconPicker value={meta.icon} onChange={handleIconChange}>
                  <button
                    type="button"
                    className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer"
                  >
                    <SessionIcon className="size-4" />
                    <span>Change icon</span>
                  </button>
                </IconPicker>
              </div>
            </PopoverContent>
          </Popover>
        ) : (
          <button
            type="button"
            className="flex items-center gap-1.5 min-w-0 truncate"
            onClick={() => undefined}
          >
            <span
              className={cn(
                "truncate font-medium text-sm",
                !meta.name && "italic text-muted-foreground",
              )}
            >
              {meta.name || "Untitled"}
            </span>
          </button>
        )}

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
              onClick={() => {
                markSessionDone(ws, meta.id).catch((err) => {
                  toast.error(getErrorMessage(err, "Failed to mark done"));
                });
              }}
            >
              <Check className="h-3.5 w-3.5" />
              {!isMobile && <span>Done</span>}
            </Button>
          )}

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-1.5 text-xs text-muted-foreground"
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {canStop && (
                <DropdownMenuItem onClick={handleStop} className="text-xs gap-2">
                  <Square className="h-3.5 w-3.5" />
                  Stop session
                </DropdownMenuItem>
              )}
              {canRestart && (
                <DropdownMenuItem onClick={handleRestart} className="text-xs gap-2">
                  <RotateCcw className="h-3.5 w-3.5" />
                  Restart session
                </DropdownMenuItem>
              )}
              {canRestart && (
                <DropdownMenuItem onClick={handleResetConversation} className="text-xs gap-2">
                  <MessageSquareX className="h-3.5 w-3.5" />
                  Reset conversation
                </DropdownMenuItem>
              )}
              {(canStop || canRestart) && <DropdownMenuSeparator />}
              <DropdownMenuItem onClick={() => setActiveDialog("rename")} className="text-xs gap-2">
                <Pencil className="h-3.5 w-3.5" />
                Rename session...
              </DropdownMenuItem>
              {isMobile && (
                <DropdownMenuItem onClick={() => copyRef(sessionRef)} className="text-xs gap-2">
                  {refCopied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  Copy ref ({sessionRef})
                </DropdownMenuItem>
              )}
              {isWorktree && !isBusy && (
                <DropdownMenuItem
                  onClick={handleClean}
                  disabled={cleaning}
                  className="text-xs gap-2"
                >
                  {cleaning ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Eraser className="h-3.5 w-3.5" />
                  )}
                  Clean up worktree
                </DropdownMenuItem>
              )}
              <DropdownMenuSeparator />
              {hasChannel ? (
                <DropdownMenuItem onClick={handleLeaveChannel} className="text-xs gap-2">
                  <LogOut className="h-3.5 w-3.5" />
                  Leave channel
                </DropdownMenuItem>
              ) : (
                <>
                  <DropdownMenuItem
                    onClick={() => {
                      setChannelName("");
                      setChannelRole("");
                      setActiveDialog("create-channel");
                    }}
                    className="text-xs gap-2"
                  >
                    <Users className="h-3.5 w-3.5" />
                    Create channel...
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={handleOpenJoinChannel} className="text-xs gap-2">
                    <UserPlus className="h-3.5 w-3.5" />
                    Join channel...
                  </DropdownMenuItem>
                </>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() => setActiveDialog("delete")}
                className="text-xs gap-2 text-destructive focus:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete session
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </PageHeader>

      <DeleteSessionDialog
        open={activeDialog === "delete"}
        onOpenChange={(open) => setActiveDialog(open ? "delete" : "none")}
        sessionName={meta.name}
        onDelete={handleDelete}
        deleting={deleting}
      />
      <RenameSessionDialog
        open={activeDialog === "rename"}
        onOpenChange={(open) => setActiveDialog(open ? "rename" : "none")}
        sessionId={meta.id}
        currentName={meta.name}
        onSubmit={handleRename}
        saving={renaming}
      />
      <CreateChannelDialog
        open={activeDialog === "create-channel"}
        onOpenChange={(open) => setActiveDialog(open ? "create-channel" : "none")}
        channelName={channelName}
        onChannelNameChange={setChannelName}
        channelRole={channelRole}
        onChannelRoleChange={setChannelRole}
        onSubmit={handleCreateChannel}
      />
      <JoinChannelDialog
        open={activeDialog === "join-channel"}
        onOpenChange={(open) => setActiveDialog(open ? "join-channel" : "none")}
        channels={availableChannels}
        selectedChannelId={selectedChannelId}
        onSelectedChannelIdChange={setSelectedChannelId}
        channelRole={channelRole}
        onChannelRoleChange={setChannelRole}
        onSubmit={handleJoinChannel}
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
  if (!hasEffort && !isWorktree) return null;
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
      {isWorktree && (
        <span
          className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-muted/40 text-muted-foreground shrink-0 min-w-0"
          title={worktreeBranch ? `Worktree: ${worktreeBranch}` : "Worktree"}
        >
          <GitBranch className="h-2.5 w-2.5 shrink-0" />
          <span className="max-sm:hidden truncate max-w-[12ch]">
            {worktreeBranch ?? "worktree"}
          </span>
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
