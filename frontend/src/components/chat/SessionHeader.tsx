import { GitBranch } from "lucide-react";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
  session: SessionData;
}

export function SessionHeader({ session }: SessionHeaderProps) {
  const { meta } = session;

  // Sum up cost from all result events across all turns
  let totalCost = 0;
  for (const turn of session.turns) {
    for (const e of turn.events) {
      if (e.type === "result" && e.cost) {
        totalCost += e.cost;
      }
    }
  }

  return (
    <div className="border-b px-4 py-2 flex items-center gap-3 text-sm shrink-0">
      <SessionStatusDot state={meta.state} hasUnseenCompletion={session.hasUnseenCompletion} />
      <span className="font-medium truncate">{meta.name}</span>
      {meta.worktreeBranch && (
        <span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
          <GitBranch className="h-3 w-3" />
          {meta.worktreeBranch}
        </span>
      )}
      {meta.worktreePath && (
        <span className="text-xs text-muted-foreground truncate hidden lg:block">
          {meta.worktreePath}
        </span>
      )}
      <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground shrink-0">
        {totalCost > 0 && <span>${totalCost.toFixed(4)}</span>}
        <span className="capitalize">{meta.state}</span>
      </span>
    </div>
  );
}
