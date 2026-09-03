import {
  Archive,
  ArchiveRestore,
  Check,
  Copy,
  Eraser,
  Loader2,
  LogOut,
  MessageSquareX,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  RotateCcw,
  Square,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";

interface SessionActionMenuProps {
  isMobile: boolean;
  /** Fully-qualified `slug/shortId` reference, copyable from the mobile menu. */
  sessionRef: string;
  canStop: boolean;
  canRestart: boolean;
  isWorktree: boolean;
  isBusy: boolean;
  hasChannel: boolean;
  cleaning: boolean;
  onStop: () => void;
  onRestart: () => void;
  onResetConversation: () => void;
  /** Open the rename dialog. */
  onRename: () => void;
  onClean: () => void;
  onLeaveChannel: () => void;
  /** Open the create-channel dialog. */
  onCreateChannel: () => void;
  /** Open the join-channel dialog. */
  onJoinChannel: () => void;
  /** Open the delete-session dialog. */
  onDelete: () => void;
  /**
   * Pin and archive as menu rows, for the layout with no room to draw them.
   * Absent means the surface draws its own buttons (desktop) and the rows are
   * left out — the same control, never twice on one screen.
   */
  placement?: { pinned: boolean; archived: boolean; canArchive: boolean };
  onTogglePin: () => void;
  onToggleArchive: () => void;
}

/**
 * The overflow (⋯) menu for a session: lifecycle (stop/restart/reset), rename,
 * worktree cleanup, channel membership, and delete. Purely presentational —
 * every action is a callback supplied by the header.
 */
export function SessionActionMenu({
  isMobile,
  sessionRef,
  canStop,
  canRestart,
  isWorktree,
  isBusy,
  hasChannel,
  cleaning,
  onStop,
  onRestart,
  onResetConversation,
  onRename,
  onClean,
  onLeaveChannel,
  onCreateChannel,
  onJoinChannel,
  onDelete,
  placement,
  onTogglePin,
  onToggleArchive,
}: SessionActionMenuProps) {
  const { copied: refCopied, copy: copyRef } = useCopyToClipboard();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="h-7 px-1.5 text-xs text-muted-foreground">
          <MoreHorizontal className="h-3.5 w-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {placement && (
          <>
            <DropdownMenuItem onClick={onTogglePin} className="text-xs gap-2">
              {placement.pinned ? (
                <PinOff className="h-3.5 w-3.5" />
              ) : (
                <Pin className="h-3.5 w-3.5" />
              )}
              {placement.pinned ? "Unpin session" : "Pin session"}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={onToggleArchive}
              disabled={!placement.archived && !placement.canArchive}
              className="text-xs gap-2"
            >
              {placement.archived ? (
                <ArchiveRestore className="h-3.5 w-3.5" />
              ) : (
                <Archive className="h-3.5 w-3.5" />
              )}
              {placement.archived ? "Unarchive session" : "Archive session"}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        {canStop && (
          <DropdownMenuItem onClick={onStop} className="text-xs gap-2">
            <Square className="h-3.5 w-3.5" />
            Stop session
          </DropdownMenuItem>
        )}
        {canRestart && (
          <DropdownMenuItem onClick={onRestart} className="text-xs gap-2">
            <RotateCcw className="h-3.5 w-3.5" />
            Restart session
          </DropdownMenuItem>
        )}
        {canRestart && (
          <DropdownMenuItem onClick={onResetConversation} className="text-xs gap-2">
            <MessageSquareX className="h-3.5 w-3.5" />
            Reset conversation
          </DropdownMenuItem>
        )}
        {(canStop || canRestart) && <DropdownMenuSeparator />}
        <DropdownMenuItem onClick={onRename} className="text-xs gap-2">
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
          <DropdownMenuItem onClick={onClean} disabled={cleaning} className="text-xs gap-2">
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
          <DropdownMenuItem onClick={onLeaveChannel} className="text-xs gap-2">
            <LogOut className="h-3.5 w-3.5" />
            Leave channel
          </DropdownMenuItem>
        ) : (
          <>
            <DropdownMenuItem onClick={onCreateChannel} className="text-xs gap-2">
              <Users className="h-3.5 w-3.5" />
              Create channel...
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onJoinChannel} className="text-xs gap-2">
              <UserPlus className="h-3.5 w-3.5" />
              Join channel...
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={onDelete}
          className="text-xs gap-2 text-destructive focus:text-destructive"
        >
          <Trash2 className="h-3.5 w-3.5" />
          Delete session
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
