import { useNavigate } from "@tanstack/react-router";
import { GitBranch, Loader2, Monitor, Plus, Users2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { type ComposerHandle, MessageComposer } from "~/components/chat/MessageComposer";
import { SwarmComposer } from "~/components/chat/SwarmComposer";
import { UserMessage } from "~/components/chat/UserMessage";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { PageHeader } from "~/components/layout/PageHeader";
import { TemplatePicker } from "~/components/templates/TemplatePicker";
import { VariableDialog } from "~/components/templates/VariableDialog";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useLogicalProjectOf } from "~/hooks/useLogicalProjects";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useProjectPresentation } from "~/hooks/useProjectPresentation";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { EffortLevel } from "~/lib/composer-constants";
import type { BehaviorPresets, PromptTemplate } from "~/lib/generated-types";
import { groupProjects } from "~/lib/machines/grouping";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { preferredMember } from "~/lib/machines/launch-targets";
import { useNavigationGuard } from "~/lib/navigation";
import { createSession, type ModelId, type ProviderId, submitQuery } from "~/lib/session/actions";
import { newSessionDraftKey } from "~/lib/session/new-session-draft";
import { extractVariables, parseSettings } from "~/lib/template-utils";
import type { Project } from "~/lib/types";
import { cn, copyToClipboard, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, AutoApproveMode } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";
import { DEFAULT_SESSION_DEFAULTS, useUIStore } from "~/stores/ui-store";

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
  initialPrompt?: string;
  initialWorktree?: boolean;
}

type PanelMode = "session" | "channel";

interface PendingTemplate {
  template: PromptTemplate;
  variables: string[];
}

export function NewChatPanel({
  projectId,
  projectSlug,
  initialPrompt,
  initialWorktree,
}: NewChatPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const navGuard = useNavigationGuard();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const gitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
  const presentation = useProjectPresentation(projectId);
  const Icon = useProjectIcon(presentation.icon);
  const color = presentation.color;
  const initials =
    project?.slug
      .split("-")
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2) ?? "";
  const draftKey = newSessionDraftKey(projectId);
  const persistedDraft = useUIStore((s) => s.drafts[draftKey] ?? "");
  const composerInitialText = persistedDraft || initialPrompt;
  const composerRef = useRef<ComposerHandle>(null);

  // The draft can also be discarded — and that discard undone — from outside
  // this panel (the sidebar's Drafts section). The composer owns its own text
  // and writes it back on the next keystroke and on unmount, so both gestures
  // have to reach it or the last write wins and undoes the user.
  const lastPersisted = useRef(persistedDraft);
  useEffect(() => {
    const previous = lastPersisted.current;
    lastPersisted.current = persistedDraft;
    const composer = composerRef.current;
    if (persistedDraft === previous || !composer) return;
    if (persistedDraft === "") {
      composer.setText("");
      return;
    }
    // Restored from outside. Only while the composer holds nothing of its own:
    // every keystroke also lands here (the composer persists what it typed),
    // and overwriting then would fight the person typing.
    if (previous === "" && !composer.getText()) composer.setText(persistedDraft);
  }, [persistedDraft]);
  const [panelMode, setPanelMode] = useState<PanelMode>("session");
  const [worktree, setWorktree] = useState(initialWorktree ?? DEFAULT_SESSION_DEFAULTS.worktree);
  const [planMode, setPlanMode] = useState(DEFAULT_SESSION_DEFAULTS.planMode);
  const [autoApproveMode, setAutoApproveMode] = useState<AutoApproveMode>(
    DEFAULT_SESSION_DEFAULTS.autoApproveMode,
  );
  const [provider, setProvider] = useState<ProviderId>("claude");
  // Model and effort carry over from the last session created (see
  // `LastUsedSettings`). Read once, as the initial value: this is a starting
  // point, not a binding — a session created in another tab must not move the
  // dropdowns under someone mid-compose.
  const lastUsed = useRef(useUIStore.getState().lastUsed).current;
  const [model, setModel] = useState<ModelId>(lastUsed.model);
  const [effort, setEffort] = useState<EffortLevel>(lastUsed.effort);
  const [sending, setSending] = useState(false);
  const [pendingPrompt, setPendingPrompt] = useState<string | null>(null);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const [pendingTemplate, setPendingTemplate] = useState<PendingTemplate | null>(null);
  const projectPresets = parseProjectPresets(project?.default_behavior_presets ?? "");

  // "Run on": when this logical project spans machines, the session targets
  // one physical member — defaulting to the checkout this panel was opened
  // for. Choice locks at send; the created session belongs to the member, so
  // all follow-up traffic routes to its machine.
  //
  // That default steps aside for one case: the opened checkout's machine is
  // away while a sibling is up. The panel is reached through a LOGICAL row
  // (one row per repo), so its project id is whichever copy represents the
  // repo, which is chosen for presentation and not for being reachable —
  // blocking the composer then would refuse a session the repo can perfectly
  // well take. An explicit pick always wins, so this only ever fills in.
  const allProjects = useAppStore((s) => s.projects);
  const members = useMemo(
    () =>
      groupProjects(allProjects).find((g) => g.members.some((m) => m.id === projectId))?.members ??
      [],
    [allProjects, projectId],
  );
  const logicalRow = useLogicalProjectOf(projectId);
  const [pickedProjectId, setTargetProjectId] = useState<string | null>(null);
  const openedIsReachable =
    logicalRow?.members.find((m) => m.projectId === projectId)?.offline !== true;
  // Derived rather than stored: a machine that connects while the panel sits
  // open should move the default, and a stored one would keep pointing at the
  // sleeper.
  const targetProjectId =
    pickedProjectId ??
    (openedIsReachable ? projectId : (preferredMember(logicalRow)?.projectId ?? projectId));
  const target = members.find((m) => m.id === targetProjectId) ?? project;

  // A repo that lives ONLY on a machine that is away has no picker to grey
  // out — the panel itself has to say so, or it accepts a prompt and fails on
  // submit. (Where the repo spans machines, the picker handles it and the
  // reachable member stays selectable.)
  const machineStatuses = useMachineStore((s) => s.statuses);
  const machines = useMachineStore((s) => s.machines);
  const targetMachineId = target?.machineId;
  const targetAway = !!targetMachineId && machineStatuses[targetMachineId] !== "connected";
  const targetMachineName = targetMachineId
    ? (machines[targetMachineId]?.label ?? "That machine")
    : "";

  const handleSwarmCreated = useCallback(
    (_channelId: string, firstSessionId: string) => {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId: sessionShortId(firstSessionId) },
        replace: true,
      });
    },
    [navigate, projectSlug],
  );

  const handleTextPersist = useCallback(
    (text: string) => {
      useUIStore.getState().setDraft(draftKey, text);
    },
    [draftKey],
  );

  const handleSend = async (prompt: string, attachments?: Attachment[]): Promise<boolean> => {
    if (sending) return false;
    setSending(true);
    setPendingPrompt(prompt);
    setPendingAttachments((attachments ?? []).map(({ previewUrl: _, ...rest }) => rest));
    // Creating the session is a round trip; if the user picks another session
    // meanwhile, the session is still created but we must not yank them here.
    const stillHere = navGuard();
    try {
      const behaviorPresets = projectPresets ?? DEFAULT_PRESETS;

      const sessionId = await createSession(ws, target?.id ?? projectId, "", worktree, {
        provider,
        model,
        planMode,
        autoApproveMode,
        effort: effort || undefined,
        behaviorPresets,
      });
      // A session exists with these, so they are now what "last used" means.
      // Recorded here rather than on selection, and before the query, because
      // the choice is spent at creation whatever the first prompt does.
      useUIStore.getState().recordLastUsed({ model, effort });
      await submitQuery(ws, sessionId, prompt, attachments);
      useUIStore.getState().clearDraft(draftKey);
      if (!stillHere()) return true;
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        // The session lives in the chosen member's project — its slug, not
        // necessarily the representative's, resolves the route.
        params: {
          projectSlug: target?.slug ?? projectSlug,
          sessionShortId: sessionShortId(sessionId),
        },
        replace: true,
      });
      return true;
    } catch (err) {
      const msg = getErrorMessage(err, "Failed to create session");
      toast.error(msg, {
        action: { label: "Copy", onClick: () => copyToClipboard(msg) },
      });
      setPendingPrompt(null);
      setPendingAttachments([]);
      setSending(false);
      return false;
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
              {members.length > 1 && (
                <RunOnPicker
                  members={members}
                  value={targetProjectId}
                  onChange={setTargetProjectId}
                  disabled={sending}
                />
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
          key={draftKey}
          ref={composerRef}
          projectId={projectId}
          onSend={handleSend}
          disabled={sending || targetAway}
          placeholder={
            targetAway
              ? `${targetMachineName} is offline — sessions start when it's back`
              : undefined
          }
          initialText={composerInitialText}
          onTextPersist={handleTextPersist}
          worktree={worktree}
          onWorktreeChange={setWorktree}
          autoApproveMode={autoApproveMode}
          onAutoApproveModeChange={setAutoApproveMode}
          onProviderChange={setProvider}
          model={model}
          onModelChange={setModel}
          effort={effort}
          onEffortChange={setEffort}
          lastUsed={lastUsed}
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

/** Segmented "Run on" control for logical projects spanning several
 *  machines — one option per physical member, primary first. */
function RunOnPicker({
  members,
  value,
  onChange,
  disabled,
}: {
  members: Project[];
  value: string;
  onChange: (projectId: string) => void;
  disabled?: boolean;
}) {
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const primaryLabel = useFeatureStore((s) => s.machineLabel);

  return (
    <div className="flex flex-col items-center gap-1.5">
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground-faint">
        Run on
      </span>
      <div className="flex items-center gap-0.5 rounded-lg border border-border/60 bg-muted/30 p-0.5">
        {members.map((m) => {
          const entry = m.machineId ? machines[m.machineId] : undefined;
          const label = m.machineId ? (entry?.label ?? "remote") : primaryLabel || "This machine";
          // A machine that is merely asleep still belongs in the list — it is
          // where the repo lives, and knowing that is worth more than a
          // shorter row. It just can't be picked until it wakes.
          const offline = !!m.machineId && statuses[m.machineId] !== "connected";
          const Icon = m.machineId
            ? (getMachineIcon(entry?.icon ?? "") ?? DEFAULT_MACHINE_ICON)
            : Monitor;
          const selected = m.id === value;
          return (
            <button
              key={m.id}
              type="button"
              disabled={disabled || offline}
              onClick={() => onChange(m.id)}
              className={cn(
                "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition-colors border",
                selected
                  ? "border-primary/50 bg-primary/15 text-primary font-medium shadow-sm"
                  : "border-transparent text-muted-foreground hover:text-foreground",
                offline
                  ? "cursor-not-allowed opacity-45 hover:text-muted-foreground"
                  : "cursor-pointer",
              )}
              title={offline ? `${label} is offline — ${m.path}` : m.path}
            >
              <Icon className="size-3" />
              {label}
              {offline && <span className="text-[10px] text-muted-foreground-faint">offline</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
