import { Check, Users2, X } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval } from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";
import type { PendingApproval } from "~/stores/chat-store";

interface SpawnWorkerApprovalBannerProps {
  sessionId: string;
  approval: PendingApproval;
}

interface SpawnWorkerInput {
  channelName?: string;
  workers: { name: string; role?: string; prompt: string }[];
}

export function SpawnWorkerApprovalBanner({ sessionId, approval }: SpawnWorkerApprovalBannerProps) {
  const ws = useWebSocket();
  const [resolving, setResolving] = useState(false);

  const input = approval.input as SpawnWorkerInput;
  const workers = input?.workers ?? [];

  const handleResolve = useCallback(
    async (allow: boolean) => {
      setResolving(true);
      try {
        await resolveApproval(ws, sessionId, approval.approvalId, allow);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to resolve approval"));
        setResolving(false);
      }
    },
    [ws, sessionId, approval.approvalId],
  );

  return (
    <div className="mx-4 mb-3 rounded-lg border border-primary/30 bg-primary/5 p-3 space-y-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <Users2 className="h-4 w-4 text-primary" />
        Spawn {workers.length} worker{workers.length !== 1 ? "s" : ""}
        {input?.channelName && (
          <span className="text-muted-foreground font-normal">
            in channel &ldquo;{input.channelName}&rdquo;
          </span>
        )}
      </div>

      <div className="space-y-1.5">
        {workers.map((w) => (
          <div key={w.name} className="rounded border border-border/50 bg-background px-3 py-2">
            <div className="text-xs font-medium">
              {w.name}
              {w.role && <span className="text-muted-foreground font-normal ml-1">({w.role})</span>}
            </div>
            <div className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{w.prompt}</div>
          </div>
        ))}
      </div>

      <div className="flex items-center gap-2 pt-1">
        <Button size="xs" disabled={resolving} onClick={() => handleResolve(true)}>
          <Check className="h-3 w-3" />
          Approve
        </Button>
        <Button
          size="xs"
          variant="outline"
          disabled={resolving}
          onClick={() => handleResolve(false)}
        >
          <X className="h-3 w-3" />
          Deny
        </Button>
      </div>
    </div>
  );
}
