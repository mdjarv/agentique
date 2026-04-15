import { useNavigate } from "@tanstack/react-router";
import { GitBranch, Loader2, Plus, Users2 } from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { type ComposerHandle, MessageComposer } from "~/components/chat/MessageComposer";
import { SwarmComposer } from "~/components/chat/SwarmComposer";
import { UserMessage } from "~/components/chat/UserMessage";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { TemplatePicker } from "~/components/templates/TemplatePicker";
import { VariableDialog } from "~/components/templates/VariableDialog";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { EffortLevel } from "~/lib/composer-constants";
import type { BehaviorPresets, PromptTemplate } from "~/lib/generated-types";
import { getProjectColor } from "~/lib/project-colors";
import { createSession, type ModelId, submitQuery } from "~/lib/session/actions";
import { extractVariables, parseSettings } from "~/lib/template-utils";
import { cn, copyToClipboard, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, AutoApproveMode } from "~/stores/chat-store";
import { DEFAULT_SESSION_DEFAULTS } from "~/stores/ui-store";

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

type PanelMode = "session" | "channel";

interface PendingTemplate {
  template: PromptTemplate;
  variables: string[];
}

export function NewChatPanel({ projectId, projectSlug }: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const gitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
  const { resolvedTheme } = useTheme();
  const Icon = useProjectIcon(project?.icon ?? "");
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const color = useMemo(
    () =>
      project ? getProjectColor(project.color, project.id, projectIds, resolvedTheme) : undefined,
    [project, projectIds, resolvedTheme],
  );
  const initials =
    project?.slug
      .split("-")
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2) ?? "";
  const composerRef = useRef<ComposerHandle>(null);
  const [panelMode, setPanelMode] = useState<PanelMode>("session");
  const [worktree, setWorktree] = useState(DEFAULT_SESSION_DEFAULTS.worktree);
  const [planMode, setPlanMode] = useState(DEFAULT_SESSION_DEFAULTS.planMode);
  const [autoApproveMode, setAutoApproveMode] = useState<AutoApproveMode>(
    DEFAULT_SESSION_DEFAULTS.autoApproveMode,
  );
  const [model, setModel] = useState<ModelId>(DEFAULT_SESSION_DEFAULTS.model);
  const [effort, setEffort] = useState<EffortLevel>(DEFAULT_SESSION_DEFAULTS.effort);
  const [sending, setSending] = useState(false);
  const [pendingPrompt, setPendingPrompt] = useState<string | null>(null);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const [pendingTemplate, setPendingTemplate] = useState<PendingTemplate | null>(null);
  const projectPresets = parseProjectPresets(project?.default_behavior_presets ?? "");

  const handleSwarmCreated = useCallback(
    (_channelId: string, firstSessionId: string) => {
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

  const handleTemplateSelect = useCallback((tmpl: PromptTemplate) => {
    const settings = parseSettings(tmpl.settings);

    // Apply template session settings
    if (settings.model) setModel(settings.model);
    if (settings.effort) setEffort(settings.effort);
    if (settings.autoApproveMode) setAutoApproveMode(settings.autoApproveMode);
    if (settings.worktree !== undefined) setWorktree(settings.worktree);
    if (settings.planMode !== undefined) setPlanMode(settings.planMode);

    // Check for variables
    const vars = extractVariables(tmpl.content);
    if (vars.length > 0) {
      setPendingTemplate({ template: tmpl, variables: vars });
    } else {
      composerRef.current?.setText(tmpl.content);
    }
  }, []);

  const handleVariableSubmit = useCallback((substituted: string) => {
    setPendingTemplate(null);
    composerRef.current?.setText(substituted);
  }, []);

  const handleVariableCancel = useCallback(() => {
    setPendingTemplate(null);
  }, []);

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
            onClick={() => setPanelMode("channel")}
            className={cn(
              "flex items-center gap-1.5 px-2.5 py-1 rounded-md text-sm font-medium transition-colors",
              panelMode === "channel"
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Users2 className="h-3.5 w-3.5" />
            Channel
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
            <div className="flex flex-col items-center gap-5 text-center">
              <div
                className="size-16 rounded-2xl flex items-center justify-center"
                style={{
                  backgroundColor: color ? `${color.bg}20` : undefined,
                  color: color?.fg,
                  boxShadow: color
                    ? `inset 0 1px 0 0 ${color.bg}18, 0 2px 8px 0 rgba(0,0,0,0.06)`
                    : undefined,
                }}
              >
                {Icon ? (
                  <Icon className="size-7" strokeWidth={1.75} />
                ) : (
                  <span className="text-lg font-bold">{initials}</span>
                )}
              </div>
              <div className="text-lg font-semibold" style={{ color: color?.fg }}>
                {project?.name}
              </div>
              {gitStatus?.branch && (
                <div className="flex items-center justify-center gap-1.5 text-sm font-mono text-muted-foreground">
                  <GitBranch className="h-3.5 w-3.5" />
                  {gitStatus.branch}
                </div>
              )}
              <p className="text-xs text-muted-foreground-faint pt-1">
                Describe what you want to work on below
              </p>
            </div>
          </div>
        )}
      </div>
      {panelMode === "session" ? (
        <MessageComposer
          ref={composerRef}
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
          templatePicker={<TemplatePicker onSelect={handleTemplateSelect} disabled={sending} />}
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

      {pendingTemplate && (
        <VariableDialog
          open
          templateName={pendingTemplate.template.name}
          variables={pendingTemplate.variables}
          content={pendingTemplate.template.content}
          onSubmit={handleVariableSubmit}
          onCancel={handleVariableCancel}
        />
      )}
    </div>
  );
}
