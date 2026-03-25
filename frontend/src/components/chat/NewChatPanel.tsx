import { useNavigate } from "@tanstack/react-router";
import { Loader2, User } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { MessageComposer } from "~/components/chat/MessageComposer";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createSession, submitQuery } from "~/lib/session-actions";
import { copyToClipboard } from "~/lib/utils";
import type { Attachment } from "~/stores/chat-store";

interface NewChatPanelProps {
  projectId: string;
}

export function NewChatPanel({ projectId }: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const [worktree, setWorktree] = useState(true);
  const [planMode, setPlanMode] = useState(false);
  const [autoApprove, setAutoApprove] = useState(true);
  const [sending, setSending] = useState(false);
  const [pendingPrompt, setPendingPrompt] = useState<string | null>(null);

  const handleSend = async (prompt: string, attachments?: Attachment[]) => {
    if (sending) return;
    setSending(true);
    setPendingPrompt(prompt);
    try {
      const sessionId = await createSession(ws, projectId, "", worktree, { planMode, autoApprove });
      await submitQuery(ws, sessionId, prompt, attachments);
      navigate({
        to: "/project/$projectId/session/$sessionId",
        params: { projectId, sessionId },
        replace: true,
      });
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      toast.error(msg, {
        action: { label: "Copy", onClick: () => copyToClipboard(msg) },
      });
      setPendingPrompt(null);
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-y-auto">
        {pendingPrompt && (
          <div className="max-w-3xl mx-auto p-4 space-y-4">
            <div className="flex gap-3 flex-row-reverse">
              <Avatar className="h-8 w-8 shrink-0">
                <AvatarFallback className="bg-primary text-primary-foreground">
                  <User className="h-4 w-4" />
                </AvatarFallback>
              </Avatar>
              <div className="max-w-[75%] rounded-lg px-4 py-2 bg-primary text-primary-foreground">
                <p className="text-sm whitespace-pre-wrap">{pendingPrompt}</p>
              </div>
            </div>
            <div className="flex items-center gap-2 text-muted-foreground text-sm">
              <Loader2 className="h-4 w-4 animate-spin" />
              Creating session...
            </div>
          </div>
        )}
      </div>
      <MessageComposer
        onSend={handleSend}
        disabled={sending}
        isDraft
        planMode={planMode}
        onPlanModeChange={setPlanMode}
        autoApprove={autoApprove}
        onAutoApproveChange={setAutoApprove}
        worktree={worktree}
        onWorktreeChange={setWorktree}
      />
    </div>
  );
}
