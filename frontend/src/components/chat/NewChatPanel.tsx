import { useNavigate } from "@tanstack/react-router";
import { GitBranch, Loader2, Plus, Users2 } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { type EffortLevel, MessageComposer } from "~/components/chat/MessageComposer";
import { SwarmComposer } from "~/components/chat/SwarmComposer";
import { UserMessage } from "~/components/chat/UserMessage";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { BehaviorPresets } from "~/lib/generated-types";
import { type ModelId, createSession, submitQuery } from "~/lib/session-actions";
import { cn, copyToClipboard, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, AutoApproveMode } from "~/stores/chat-store";
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

type PanelMode = "session" | "team";

export function NewChatPanel({ projectId, projectSlug }: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const gitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
  const [defaults] = useState(() => useUIStore.getState().sessionDefaults);
  const [panelMode, setPanelMode] = useState<PanelMode>("session");
  const [worktree, setWorktree] = useState(defaults.worktree);
  const [planMode, setPlanMode] = useState(defaults.planMode);
  const [autoApproveMode, setAutoApproveMode] = useState<AutoApproveMode>(defaults.autoApproveMode);
  const [model, setModel] = useState<ModelId>(defaults.model);
  const [effort, setEffort] = useState<EffortLevel>(defaults.effort);
  const [sending, setSending] = useState(false);
  const [pendingPrompt, setPendingPrompt] = useState<string | null>(null);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const projectPresets = parseProjectPresets(project?.default_behavior_presets ?? "");

  const handleSwarmCreated = useCallback(
    (_teamId: string, firstSessionId: string) => {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId: firstSessionId.split("-")[0] ?? "" },
        replace: true,
      });
    },
    [navigate, projectSlug],
  );

  const handleSend = async (prompt: string, attachments?: Attachment[]) => {
    if (sending) return;
    setSending(true);
    setPendingPrompt(prompt);
    setPendingAttachments((attachments ?? []).map(({ previewUrl: _, ...rest }) => rest));
    try {
      const behaviorPresets = projectPresets ?? DEFAULT_PRESETS;

      const sessionId = await createSession(ws, projectId, "", worktree, {
        model,
        planMode,
        autoApproveMode,
        effort: effort || undefined,
        behaviorPresets,
      });
      await submitQuery(ws, sessionId, prompt, attachments);
      useUIStore
        .getState()
        .setSessionDefaults({ worktree, planMode, autoApproveMode, model, effort });
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
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setPanelMode("session")}
            className={cn(
              "flex items-center gap-1.5 px-2.5 py-1 rounded-md text-sm font-medium transition-colors",
              panelMode === "session"
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Plus className="h-3.5 w-3.5" />
            Session
          </button>
          <button
            type="button"
            onClick={() => setPanelMode("team")}
            className={cn(
              "flex items-center gap-1.5 px-2.5 py-1 rounded-md text-sm font-medium transition-colors",
              panelMode === "team"
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Users2 className="h-3.5 w-3.5" />
            Team
          </button>
        </div>
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
            <div className="text-center text-muted-foreground/40 space-y-2">
              <div className="text-base font-medium text-muted-foreground/60">{project?.name}</div>
              {gitStatus?.branch && (
                <div className="flex items-center justify-center gap-1.5 text-sm font-mono">
                  <GitBranch className="h-3.5 w-3.5" />
                  {gitStatus.branch}
                </div>
              )}
              <p className="text-xs pt-2">Describe what you want to work on below</p>
            </div>
          </div>
        )}
      </div>
      {panelMode === "session" ? (
        <MessageComposer
          projectId={projectId}
          onSend={handleSend}
          disabled={sending}
          worktree={worktree}
          onWorktreeChange={setWorktree}
          planMode={planMode}
          onPlanModeChange={setPlanMode}
          autoApproveMode={autoApproveMode}
          onAutoApproveModeChange={setAutoApproveMode}
          model={model}
          onModelChange={setModel}
          effort={effort}
          onEffortChange={setEffort}
        />
      ) : (
        <SwarmComposer
          projectId={projectId}
          model={model}
          onModelChange={setModel}
          behaviorPresets={projectPresets ?? DEFAULT_PRESETS}
          onCreated={handleSwarmCreated}
        />
      )}
    </div>
  );
}
