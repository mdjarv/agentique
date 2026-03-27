import { useNavigate } from "@tanstack/react-router";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { DraftHeader } from "~/components/chat/DraftHeader";
import { type EffortLevel, MessageComposer } from "~/components/chat/MessageComposer";
import { UserMessage } from "~/components/chat/UserMessage";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type ModelId, createSession, submitQuery } from "~/lib/session-actions";
import { copyToClipboard, getErrorMessage } from "~/lib/utils";
import type { Attachment } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

interface NewChatPanelProps {
  projectId: string;
  projectSlug: string;
}

export function NewChatPanel({ projectId, projectSlug }: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const [defaults] = useState(() => useUIStore.getState().sessionDefaults);
  const [worktree, setWorktree] = useState(defaults.worktree);
  const [planMode, setPlanMode] = useState(defaults.planMode);
  const [autoApprove, setAutoApprove] = useState(defaults.autoApprove);
  const [model, setModel] = useState<ModelId>(defaults.model);
  const [effort, setEffort] = useState<EffortLevel>(defaults.effort);
  const [sending, setSending] = useState(false);
  const [pendingPrompt, setPendingPrompt] = useState<string | null>(null);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);

  const handleSend = async (prompt: string, attachments?: Attachment[]) => {
    if (sending) return;
    setSending(true);
    setPendingPrompt(prompt);
    setPendingAttachments(attachments ?? []);
    try {
      const sessionId = await createSession(ws, projectId, "", worktree, {
        model,
        planMode,
        autoApprove,
        effort: effort || undefined,
      });
      await submitQuery(ws, sessionId, prompt, attachments);
      useUIStore.getState().setSessionDefaults({ worktree, planMode, autoApprove, model, effort });
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId: sessionId.split("-")[0] ?? "" },
        replace: true,
      });
    } catch (err) {
      const msg = getErrorMessage(err, "Failed to create session");
      toast.error(msg, {
        action: { label: "Copy", onClick: () => copyToClipboard(msg) },
      });
      setPendingPrompt(null);
      setPendingAttachments([]);
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      <DraftHeader />
      <div className="flex-1 overflow-y-auto">
        {pendingPrompt && (
          <div className="max-w-3xl mx-auto p-4 space-y-4">
            <UserMessage prompt={pendingPrompt} attachments={pendingAttachments} />
            <div className="flex items-center gap-2 text-muted-foreground text-sm">
              <Loader2 className="h-4 w-4 animate-spin" />
              Creating session...
            </div>
          </div>
        )}
      </div>
      <MessageComposer
        projectId={projectId}
        onSend={handleSend}
        disabled={sending}
        worktree={worktree}
        onWorktreeChange={setWorktree}
        planMode={planMode}
        onPlanModeChange={setPlanMode}
        autoApprove={autoApprove}
        onAutoApproveChange={setAutoApprove}
        model={model}
        onModelChange={setModel}
        effort={effort}
        onEffortChange={setEffort}
      />
    </div>
  );
}
