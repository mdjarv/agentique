import {
  Check,
  Copy,
  Eraser,
  Loader2,
  LogOut,
  MoreHorizontal,
  PanelRightOpen,
  Pencil,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import { cleanSession, deleteSession, markSessionDone, renameSession } from "~/lib/session-actions";
import { type TeamInfo, createTeam, joinTeam, leaveTeam, listTeams } from "~/lib/team-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import type { SessionMetadata } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";

interface SessionHeaderProps {
  meta: SessionMetadata;
  hasPendingInput: boolean;
  showPanelButton?: boolean;
  onOpenPanel?: () => void;
}

export function SessionHeader({
  meta,
  hasPendingInput,
  showPanelButton,
  onOpenPanel,
}: SessionHeaderProps) {
  const ws = useWebSocket();
  const isMobile = useIsMobile();
  const isRunning = meta.state === "running";
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = isRunning;

  const [activeDialog, setActiveDialog] = useState<"none" | "delete" | "create-team" | "join-team">(
    "none",
  );
  const [deleting, setDeleting] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  const [teamName, setTeamName] = useState("");
  const [teamRole, setTeamRole] = useState("");
  const [availableTeams, setAvailableTeams] = useState<TeamInfo[]>([]);
  const [selectedTeamId, setSelectedTeamId] = useState("");
  const hasTeam = !!meta.teamId;
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(meta.name);
  const { copied: nameCopied, copy: copyName } = useCopyToClipboard();
  const inputRef = useRef<HTMLInputElement>(null);

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

  const handleOpenJoinTeam = useCallback(async () => {
    try {
      const teams = await listTeams(ws, meta.projectId);
      setAvailableTeams(teams);
      setSelectedTeamId(teams[0]?.id ?? "");
      setTeamRole("");
      setActiveDialog("join-team");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to load teams"));
    }
  }, [ws, meta.projectId]);

  const handleCreateTeam = useCallback(async () => {
    const name = teamName.trim();
    if (!name) return;
    try {
      const created = await createTeam(ws, meta.projectId, name);
      const team = await joinTeam(ws, meta.id, created.id, teamRole.trim());
      useTeamStore.getState().addTeam(team);
      setActiveDialog("none");
      setTeamName("");
      setTeamRole("");
      toast.success("Team created");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to create team"));
    }
  }, [ws, meta.projectId, meta.id, teamName, teamRole]);

  const handleJoinTeam = useCallback(async () => {
    if (!selectedTeamId) return;
    try {
      const team = await joinTeam(ws, meta.id, selectedTeamId, teamRole.trim());
      useTeamStore.getState().addTeam(team);
      setActiveDialog("none");
      setTeamRole("");
      toast.success("Joined team");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to join team"));
    }
  }, [ws, meta.id, selectedTeamId, teamRole]);

  const handleLeaveTeam = useCallback(async () => {
    try {
      await leaveTeam(ws, meta.id);
      toast.success("Left team");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to leave team"));
    }
  }, [ws, meta.id]);

  return (
    <>
      <PageHeader>
        <SessionStatusDot
          state={meta.state}
          connected={meta.connected}
          hasPendingApproval={hasPendingInput}
        />

        {/* Editable name */}
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
        ) : (
          <div className="group/name flex items-center gap-1 font-medium truncate">
            <button
              type="button"
              onClick={() => (isMobile ? undefined : setEditing(true))}
              className="flex items-center gap-1 truncate hover:text-foreground"
            >
              <span className={cn("truncate", !meta.name && "italic text-muted-foreground")}>
                {meta.name || "Untitled"}
              </span>
              {!isMobile && (
                <Pencil className="h-3 w-3 opacity-0 group-hover/name:opacity-50 transition-opacity shrink-0" />
              )}
            </button>
            {!isMobile && (
              <button
                type="button"
                onClick={() => copyName(meta.name || "Untitled")}
                className="p-0.5 rounded opacity-0 group-hover/name:opacity-50 hover:!opacity-100 text-muted-foreground transition-opacity shrink-0"
                aria-label="Copy session name"
              >
                {nameCopied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
              </button>
            )}
          </div>
        )}

        <div className="ml-auto flex items-center gap-1.5">
          {isMobile && <ConnectionIndicator />}

          {/* Session panel toggle (mobile) */}
          {showPanelButton && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-1.5 text-xs text-muted-foreground"
              title="Session panel"
              onClick={onOpenPanel}
            >
              <PanelRightOpen className="h-3.5 w-3.5" />
            </Button>
          )}

          {/* Mark done */}
          {(meta.state === "idle" || meta.state === "stopped" || meta.state === "failed") && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-1.5 text-xs text-muted-foreground hover:text-success"
              title="Mark done"
              onClick={() => {
                markSessionDone(ws, meta.id).catch((err) => {
                  toast.error(getErrorMessage(err, "Failed to mark done"));
                });
              }}
            >
              <Check className="h-3.5 w-3.5" />
            </Button>
          )}

          {/* Overflow menu — clean + delete */}
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
              {isMobile && (
                <>
                  <DropdownMenuItem onClick={() => setEditing(true)} className="text-xs gap-2">
                    <Pencil className="h-3.5 w-3.5" />
                    Rename
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => copyName(meta.name || "Untitled")}
                    className="text-xs gap-2"
                  >
                    {nameCopied ? (
                      <Check className="h-3.5 w-3.5" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                    Copy name
                  </DropdownMenuItem>
                </>
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
              {hasTeam ? (
                <DropdownMenuItem onClick={handleLeaveTeam} className="text-xs gap-2">
                  <LogOut className="h-3.5 w-3.5" />
                  Leave team
                </DropdownMenuItem>
              ) : (
                <>
                  <DropdownMenuItem
                    onClick={() => {
                      setTeamName("");
                      setTeamRole("");
                      setActiveDialog("create-team");
                    }}
                    className="text-xs gap-2"
                  >
                    <Users className="h-3.5 w-3.5" />
                    Create team...
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={handleOpenJoinTeam} className="text-xs gap-2">
                    <UserPlus className="h-3.5 w-3.5" />
                    Join team...
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

          {/* State label — hidden on mobile where status dot suffices */}
          {!isMobile && (
            <span className="text-xs text-muted-foreground capitalize">{meta.state}</span>
          )}
        </div>
      </PageHeader>

      {/* Delete confirmation */}
      <AlertDialog
        open={activeDialog === "delete"}
        onOpenChange={(open) => setActiveDialog(open ? "delete" : "none")}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete session</AlertDialogTitle>
            <AlertDialogDescription>
              Delete &quot;{meta.name || "Untitled"}&quot;? This removes the worktree, branch, and
              all session data.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Create team dialog */}
      <AlertDialog
        open={activeDialog === "create-team"}
        onOpenChange={(open) => setActiveDialog(open ? "create-team" : "none")}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Create team</AlertDialogTitle>
            <AlertDialogDescription>
              Create a new team and add this session as the first member.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-2 py-2">
            <input
              value={teamName}
              onChange={(e) => setTeamName(e.target.value)}
              placeholder="Team name"
              className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreateTeam();
              }}
            />
            <input
              value={teamRole}
              onChange={(e) => setTeamRole(e.target.value)}
              placeholder="Your role (optional)"
              className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleCreateTeam} disabled={!teamName.trim()}>
              Create
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Join team dialog */}
      <AlertDialog
        open={activeDialog === "join-team"}
        onOpenChange={(open) => setActiveDialog(open ? "join-team" : "none")}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Join team</AlertDialogTitle>
            <AlertDialogDescription>Select a team to join.</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-2 py-2">
            {availableTeams.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No teams yet. Create one from the menu.
              </p>
            ) : (
              <select
                value={selectedTeamId}
                onChange={(e) => setSelectedTeamId(e.target.value)}
                className="w-full text-sm bg-background border rounded px-3 py-1.5"
              >
                {availableTeams.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} ({t.members.length} members)
                  </option>
                ))}
              </select>
            )}
            <input
              value={teamRole}
              onChange={(e) => setTeamRole(e.target.value)}
              placeholder="Your role (optional)"
              className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleJoinTeam}
              disabled={!selectedTeamId || availableTeams.length === 0}
            >
              Join
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
