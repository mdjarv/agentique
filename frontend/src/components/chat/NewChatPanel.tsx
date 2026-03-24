import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { MessageComposer } from "~/components/chat/MessageComposer";
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
  const [autoApprove, setAutoApprove] = useState(false);
  const [sending, setSending] = useState(false);

  const handleSend = async (prompt: string, attachments?: Attachment[]) => {
    if (sending) return;
    setSending(true);
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
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1" />
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
