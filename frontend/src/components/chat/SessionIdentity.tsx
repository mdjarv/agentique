import { Archive, ArchiveRestore, Check, Copy, Pencil, Pin, PinOff } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { IconPicker } from "~/components/chat/IconPicker";
import { ProviderBadge } from "~/components/chat/ProviderBadge";
import { Button } from "~/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useWebSocket } from "~/hooks/useWebSocket";
import { EFFORT_LABELS, type EffortLevel } from "~/lib/composer-constants";
import { sessionModelLabel } from "~/lib/model-catalog";
import { archiveSession, setSessionPinned, unarchiveSession } from "~/lib/session/actions";
import { getSessionIconComponent } from "~/lib/session/icons";
import { cn, getErrorMessage } from "~/lib/utils";
import type { SessionMetadata } from "~/stores/chat-store";

interface SessionIdentityProps {
  meta: SessionMetadata;
  /** Fully-qualified `slug/shortId` reference, copyable from the popover. */
  sessionRef: string;
  /** Commit an inline rename (no-op when unchanged). */
  onRename: (name: string) => void;
  onIconChange: (icon: string | undefined) => void;
  /**
   * Stacked layout: the name wraps to two lines and a metadata `subline` sits
   * beneath it. Used on mobile, where the name gets the whole header width.
   */
  stacked?: boolean;
  /** Optional metadata line rendered under the name in stacked layout. */
  subline?: ReactNode;
}

/**
 * The header identity zone: session name with an inline editor, plus a detail
 * popover (ref / branch / path / effort / agent / icon). The same popover backs
 * both desktop and mobile — tapping the name on mobile opens it rather than
 * hitting a dead no-op button.
 */
export function SessionIdentity({
  meta,
  sessionRef,
  onRename,
  onIconChange,
  stacked = false,
  subline,
}: SessionIdentityProps) {
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(meta.name);
  const [popoverOpen, setPopoverOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const { copied: refCopied, copy: copyRef } = useCopyToClipboard();
  const { copied: branchCopied, copy: copyBranch } = useCopyToClipboard();
  const { copied: pathCopied, copy: copyPath } = useCopyToClipboard();

  const SessionIcon = getSessionIconComponent(meta.icon);
  const worktreeBranch = meta.worktreeBranch;
  const worktreePath = meta.worktreePath;
  const modelName = sessionModelLabel(meta.model, meta.resolvedModel);

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
      onRename(trimmed);
    } else {
      setEditName(meta.name);
    }
  };

  if (editing) {
    return (
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
    );
  }

  return (
    <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "flex min-w-0 rounded-md hover:bg-accent transition-colors cursor-pointer",
            stacked
              ? "flex-1 items-start gap-2 px-1 py-0.5 text-left"
              : "items-center gap-1.5 px-1.5 py-0.5",
          )}
        >
          <SessionIcon
            className={cn("text-agent shrink-0", stacked ? "size-4 mt-0.5" : "size-3.5")}
          />
          {stacked ? (
            <span className="flex flex-col min-w-0 gap-0.5">
              <span
                className={cn(
                  "line-clamp-2 font-medium text-sm leading-tight break-words",
                  !meta.name && "italic text-muted-foreground",
                )}
              >
                {meta.name || "Untitled"}
              </span>
              {subline}
            </span>
          ) : (
            <>
              <span
                className={cn(
                  "truncate font-medium text-sm",
                  !meta.name && "italic text-muted-foreground",
                )}
              >
                {meta.name || "Untitled"}
              </span>
              <ProviderBadge provider={meta.provider} size="sm" />
            </>
          )}
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

        {/* Branch */}
        {worktreeBranch && (
          <div className="space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
              Branch
            </span>
            <div className="flex items-center gap-1.5">
              <span
                className="text-xs font-mono text-muted-foreground truncate flex-1"
                title={worktreeBranch}
              >
                {worktreeBranch}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => copyBranch(worktreeBranch)}
                className="text-muted-foreground hover:text-foreground shrink-0"
              >
                {branchCopied ? <Check className="size-2.5" /> : <Copy className="size-2.5" />}
              </Button>
            </div>
          </div>
        )}

        {/* Local path */}
        {worktreePath && (
          <div className="space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
              Local path
            </span>
            <div className="flex items-center gap-1.5">
              <span
                className="text-xs font-mono text-muted-foreground truncate flex-1"
                title={worktreePath}
              >
                {worktreePath}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => copyPath(worktreePath)}
                className="text-muted-foreground hover:text-foreground shrink-0"
              >
                {pathCopied ? <Check className="size-2.5" /> : <Copy className="size-2.5" />}
              </Button>
            </div>
          </div>
        )}

        {/* Model */}
        {modelName && (
          <div className="space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
              Model
            </span>
            <div className="text-xs text-muted-foreground" title={meta.resolvedModel}>
              {modelName}
            </div>
          </div>
        )}

        {/* Effort */}
        {meta.effort && EFFORT_LABELS[meta.effort as EffortLevel] && (
          <div className="space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
              Effort
            </span>
            <div className="text-xs text-muted-foreground">
              {EFFORT_LABELS[meta.effort as EffortLevel]}
            </div>
          </div>
        )}

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
          <IconPicker value={meta.icon} onChange={onIconChange}>
            <button
              type="button"
              className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer"
            >
              <SessionIcon className="size-4" />
              <span>Change icon</span>
            </button>
          </IconPicker>
        </div>

        {/* Sidebar placement — the touch home for actions the thread sidebar
            only offers on hover (rows carry no buttons on mobile). */}
        <SidebarPlacementSection meta={meta} />
      </PopoverContent>
    </Popover>
  );
}

function SidebarPlacementSection({ meta }: { meta: SessionMetadata }) {
  const ws = useWebSocket();
  const archived = !!meta.archivedAt;
  const PinIcon = meta.pinned ? PinOff : Pin;

  const togglePin = () => {
    setSessionPinned(ws, meta.id, !meta.pinned, meta.pinned ? 0 : meta.pinOrder + 1).catch((err) =>
      toast.error(getErrorMessage(err, "Failed to update pin")),
    );
  };

  const toggleArchived = () => {
    const action = archived ? unarchiveSession : archiveSession;
    action(ws, meta.id).catch((err) =>
      toast.error(getErrorMessage(err, archived ? "Failed to unarchive" : "Failed to archive")),
    );
  };

  return (
    <div className="space-y-1">
      <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
        Sidebar
      </span>
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={togglePin}
          className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer"
        >
          <PinIcon className="size-4" />
          <span>{meta.pinned ? "Unpin" : "Pin"}</span>
        </button>
        <button
          type="button"
          onClick={toggleArchived}
          className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer"
        >
          {archived ? <ArchiveRestore className="size-4" /> : <Archive className="size-4" />}
          <span>{archived ? "Unarchive" : "Archive"}</span>
        </button>
      </div>
    </div>
  );
}
