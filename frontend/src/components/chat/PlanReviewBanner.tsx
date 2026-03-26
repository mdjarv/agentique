import { ClipboardList, MessageSquare, Play, Sparkles } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Markdown } from "~/components/chat/Markdown";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval } from "~/lib/session-actions";
import type { PendingApproval } from "~/stores/chat-store";

interface PlanReviewBannerProps {
  sessionId: string;
  approval: PendingApproval;
  onStartFresh: (plan: string) => void;
}

function extractPlan(input: unknown): string {
  if (!input || typeof input !== "object") return "";
  return String((input as Record<string, unknown>).plan ?? "");
}

export function PlanReviewBanner({ sessionId, approval, onStartFresh }: PlanReviewBannerProps) {
  const ws = useWebSocket();
  const [submitting, setSubmitting] = useState(false);
  const plan = extractPlan(approval.input);

  const handleContinue = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, true).catch((err) => {
      setSubmitting(false);
      toast.error(err instanceof Error ? err.message : "Failed to approve");
    });
  }, [ws, sessionId, approval.approvalId]);

  const handleStartFresh = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, false, "User chose to start fresh")
      .then(() => onStartFresh(plan))
      .catch((err) => {
        setSubmitting(false);
        toast.error(err instanceof Error ? err.message : "Failed to start fresh session");
      });
  }, [ws, sessionId, approval.approvalId, onStartFresh, plan]);

  const handleKeepChatting = useCallback(() => {
    setSubmitting(true);
    resolveApproval(
      ws,
      sessionId,
      approval.approvalId,
      false,
      "User wants to continue discussing the plan",
    ).catch((err) => {
      setSubmitting(false);
      toast.error(err instanceof Error ? err.message : "Failed to deny");
    });
  }, [ws, sessionId, approval.approvalId]);

  return (
    <div className="mx-4 mb-2 rounded-md border border-blue-500/40 bg-blue-500/5 overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 text-sm border-b border-blue-500/20">
        <ClipboardList className="h-4 w-4 shrink-0 text-blue-500" />
        <span className="font-medium">Plan ready for review</span>
      </div>

      {plan && (
        <div className="max-h-80 overflow-y-auto px-3 py-2 text-sm">
          <Markdown content={plan} />
        </div>
      )}

      <div className="flex items-center gap-2 px-3 py-2 border-t border-blue-500/20 bg-blue-500/5">
        <Button
          size="sm"
          variant="ghost"
          className="h-8 px-3 text-muted-foreground hover:text-foreground"
          onClick={handleKeepChatting}
          disabled={submitting}
        >
          <MessageSquare className="h-3.5 w-3.5 mr-1.5" />
          Keep chatting
        </Button>
        <div className="ml-auto flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-8 px-3"
            onClick={handleStartFresh}
            disabled={submitting}
          >
            <Sparkles className="h-3.5 w-3.5 mr-1.5" />
            Start fresh
          </Button>
          <Button
            size="sm"
            className="h-8 px-3 bg-green-600 hover:bg-green-700 text-white"
            onClick={handleContinue}
            disabled={submitting}
          >
            <Play className="h-3.5 w-3.5 mr-1.5" />
            Continue with plan
          </Button>
        </div>
      </div>
    </div>
  );
}
