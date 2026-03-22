import { FolderOpen, GitBranch } from "lucide-react";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
  session: SessionData;
}

export function SessionHeader({ session }: SessionHeaderProps) {
  const { meta } = session;

  return (
    <div className="border-b px-4 py-2 flex items-center gap-3 text-sm shrink-0">
      <SessionStatusDot state={meta.state} hasUnseenCompletion={session.hasUnseenCompletion} />
      <span className="font-medium truncate">{meta.name}</span>
      {meta.worktreeBranch ? (
        <span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
          <GitBranch className="h-3 w-3" />
          {meta.worktreeBranch}
        </span>
      ) : (
        <span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
          <FolderOpen className="h-3 w-3" />
          Local
        </span>
      )}
      <span className="ml-auto text-xs text-muted-foreground shrink-0 capitalize">
        {meta.state}
      </span>
    </div>
  );
}
