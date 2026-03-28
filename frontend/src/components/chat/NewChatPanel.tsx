import { useNavigate } from "@tanstack/react-router";
import { GitBranch, Loader2, Plus } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { type EffortLevel, MessageComposer } from "~/components/chat/MessageComposer";
import { UserMessage } from "~/components/chat/UserMessage";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { BehaviorPresets } from "~/lib/generated-types";
import { type ModelId, createSession, submitQuery } from "~/lib/session-actions";
import { copyToClipboard, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

const DEFAULT_PRESETS: BehaviorPresets = {
  autoCommit: true,
  suggestParallel: true,
  planFirst: false,
  terse: false,
};

function parseProjectPresets(raw: string): BehaviorPresets | undefined {
  if (!raw || raw === "{}") return undefined;
  try {
    return JSON.parse(raw) as BehaviorPresets;
  } catch {
    return undefined;
  }
}

interface NewChatPanelProps {
  projectId: string;
  projectSlug: string;
}

export function NewChatPanel({ projectId, projectSlug }: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const gitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
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
      // Resolve behavior presets: project defaults > hardcoded defaults.
      // Backend falls back to project defaults if presets are zero-value,
      // but we resolve here so the frontend sends explicit values.
      const projectPresets = parseProjectPresets(project?.default_behavior_presets ?? "");
      const behaviorPresets = projectPresets ?? DEFAULT_PRESETS;

      const sessionId = await createSession(ws, projectId, "", worktree, {
        model,
        planMode,
        autoApprove,
        effort: effort || undefined,
        behaviorPresets,
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
      <PageHeader>
        <Plus className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium text-muted-foreground">New session</span>
        {isMobile && (
          <div className="ml-auto">
            <ConnectionIndicator />
          </div>
        )}
      </PageHeader>
      <div className="flex flex-1 overflow-y-auto">
        {pendingPrompt ? (
          <div className="p-4 space-y-4 min-w-0 w-full">
            <UserMessage prompt={pendingPrompt} attachments={pendingAttachments} />
            <div className="flex items-center gap-2 text-muted-foreground text-sm">
              <Loader2 className="h-4 w-4 animate-spin" />
              Creating session...
            </div>
          </div>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <div className="text-center text-muted-foreground/60">
              <div className="text-lg font-medium">{project?.name}</div>
              {gitStatus?.branch && (
                <div className="flex items-center justify-center gap-1.5 text-sm mt-1 font-mono">
                  <GitBranch className="h-3.5 w-3.5" />
                  {gitStatus.branch}
                </div>
              )}
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
