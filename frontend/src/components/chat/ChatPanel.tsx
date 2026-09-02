import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { BrowserPanel } from "~/components/browser/BrowserPanel";
import { AgentFlightStrip } from "~/components/chat/AgentFlightStrip";
import { ApprovalBanner } from "~/components/chat/banners/ApprovalBanner";
import { PlanReviewBanner } from "~/components/chat/banners/PlanReviewBanner";
import { QuestionBanner } from "~/components/chat/banners/QuestionBanner";
import { ResumeBanner } from "~/components/chat/banners/ResumeBanner";
import { SpawnWorkerApprovalBanner } from "~/components/chat/banners/SpawnWorkerApprovalBanner";
import { ContextBar } from "~/components/chat/ContextBar";
import { CrewStrip } from "~/components/chat/CrewStrip";
import { ChangesView } from "~/components/chat/changes/ChangesView";
import { CommitDialog } from "~/components/chat/dialogs/CommitDialog";
import { CreatePRDialog } from "~/components/chat/dialogs/CreatePRDialog";
import { DockResizeHandle } from "~/components/chat/dock/DockResizeHandle";
import type { DockTabMark } from "~/components/chat/dock/DockTabBar";
import { DockToggle } from "~/components/chat/dock/DockToggle";
import { SessionDock } from "~/components/chat/dock/SessionDock";
import { WorkView } from "~/components/chat/dock/WorkView";
import { type ComposerHandle, MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { finishActionKind, SessionFinishAction } from "~/components/chat/SessionFinishAction";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { SessionMachineContext } from "~/components/chat/SessionMachineContext";
import { StatusPage } from "~/components/layout/PageHeader";
import { LoopsPanel } from "~/components/schedules/LoopsPanel";
import { ScheduleApprovalBanner } from "~/components/schedules/ScheduleApprovalBanner";
import { TemplatePicker } from "~/components/templates/TemplatePicker";
import { VariableDialog } from "~/components/templates/VariableDialog";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { useGitActions } from "~/hooks/git/useGitActions";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useSessionAttention } from "~/hooks/session/useSessionAttention";
import { useSessionState } from "~/hooks/session/useSessionState";
import { useAgentRuns } from "~/hooks/useAgentRuns";
import { useAutoOpenDock } from "~/hooks/useAutoOpenDock";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useProjectPresentation } from "~/hooks/useProjectPresentation";
import { useWebSocket } from "~/hooks/useWebSocket";
import { agentBadgeState, partitionAgentRuns } from "~/lib/agent-runs";
import type { EffortLevel } from "~/lib/composer-constants";
import { appendQuote } from "~/lib/diff-quote";
import type { PromptTemplate } from "~/lib/generated-types";
import { loopBadgeState } from "~/lib/loop-attention";
import { sessionModelLabel } from "~/lib/model-catalog";
import { useNavigationGuard } from "~/lib/navigation";
import { markScheduleViewed } from "~/lib/schedule-actions";
import {
  createSession,
  enqueueMessage,
  interruptSession,
  isGitFresh,
  type ModelId,
  type ProviderId,
  refreshGitStatus,
  resumeSession,
  setAutoApproveMode,
  setPermissionMode,
  setSessionModel,
  stopSession,
} from "~/lib/session/actions";
import { deriveCrew } from "~/lib/session/crew";
import {
  availableDockViews,
  type DockAvailability,
  type DockView,
  dockAlertState,
  resolveDockView,
} from "~/lib/session/dock";
import { loadSessionHistory } from "~/lib/session/history";
import { extractVariables, parseSettings } from "~/lib/template-utils";
import { copyToClipboard, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, AutoApproveMode, PendingApproval } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";
import { useScheduleStore } from "~/stores/schedule-store";
import { useVoiceStore } from "~/stores/voice-store";

function ApprovalBannerSwitch({
  sessionId,
  approval,
  onStartFresh,
  projectPath,
  worktreePath,
}: {
  sessionId: string;
  approval: PendingApproval;
  onStartFresh: (plan: string) => void;
  projectPath?: string;
  worktreePath?: string;
}) {
  if (approval.toolName === "ExitPlanMode") {
    return (
      <PlanReviewBanner sessionId={sessionId} approval={approval} onStartFresh={onStartFresh} />
    );
  }
  if (approval.toolName === "SpawnWorkers") {
    return <SpawnWorkerApprovalBanner sessionId={sessionId} approval={approval} />;
  }
  return (
    <ApprovalBanner
      sessionId={sessionId}
      approval={approval}
      projectPath={projectPath}
      worktreePath={worktreePath}
    />
  );
}

import { sessionDock, useUIStore } from "~/stores/ui-store";

interface ChatPanelProps {
  projectId: string;
  sessionId: string;
  /** Which dock view the URL asks for, if any. */
  dock?: DockView;
  /** Deep-link target: persisted turn index to scroll to (?turn= search param). */
  targetTurn?: number;
  /** Reflect the dock's view back into the URL — null closes it. */
  onDockChange?: (view: DockView | null) => void;
}

const resumePlaceholders: Record<string, string> = {
  stopped: "Send a message or press Enter to resume...",
  done: "Send a message or press Enter to continue...",
  failed: "Send a message or press Enter to retry...",
};

const resumableStates = new Set(["stopped", "failed", "done"]);

export function ChatPanel({
  projectId,
  sessionId,
  dock,
  targetTurn,
  onDockChange,
}: ChatPanelProps) {
  const navigate = useNavigate();
  const navGuard = useNavigationGuard();
  const ws = useWebSocket();
  // Pop the dock open on Work when this session launches a live workflow.
  useAutoOpenDock(sessionId);
  // Keep the open session out of the idle-eviction sweep: server-side idleness
  // is measured from the last turn, which says nothing about a user reading or
  // typing.
  useSessionAttention(sessionId);
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  // A session on a machine that is currently away: everything about it is
  // still readable — history, diffs, todos — but it cannot be driven, and
  // saying so up front beats a send that spins for ten seconds and fails.
  const machineEntry = useMachineStore((s) =>
    project?.machineId ? (s.machines[project.machineId] ?? null) : null,
  );
  const machineStatus = useMachineStore((s) =>
    project?.machineId ? (s.statuses[project.machineId] ?? "disconnected") : "connected",
  );
  const machineFault = useMachineStore((s) =>
    project?.machineId ? (s.faults[project.machineId] ?? null) : null,
  );
  const machineAway = !!project?.machineId && machineStatus !== "connected";
  const machineName = machineEntry?.label ?? "That machine";
  const projectSlug = project?.slug ?? "";
  const mainBranch = useAppStore((s) => s.projectGitStatus[projectId]?.branch);
  const projectGitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
  const projectGitActions = useProjectGitActions(projectId);

  const {
    turns,
    meta,
    pendingApproval,
    pendingQuestion,
    autoApproveMode,
    todos,
    contextUsage,
    compacting,
  } = useSessionState(sessionId);
  const agentRuns = useAgentRuns(sessionId);
  const [flightExpanded, setFlightExpanded] = useState(false);
  const [crewExpanded, setCrewExpanded] = useState(false);
  // The whole map, because membership is a property of the *other* sessions:
  // a worker appearing is a new key, which a narrower selector cannot see.
  const allSessions = useChatStore((s) => s.sessions);
  const allProjects = useAppStore((s) => s.projects);
  const sessionListLoaded = useChatStore((s) => s.loadedProjects.has(projectId));
  // Schedules targeting this session (loops). Element refs are stable in the
  // store, so useShallow keeps the selector reference-stable across renders.
  const sessionSchedules = useScheduleStore(
    useShallow((s) => Object.values(s.schedules).filter((sc) => sc.sessionId === sessionId)),
  );
  const isLoadingHistory = useChatStore((s) => s.historyLoading.has(sessionId));
  const historyComplete = useChatStore((s) => s.sessions[sessionId]?.historyComplete ?? false);

  // Through the representative, never this checkout's own row: a session on a
  // remote machine must wear the repo's colour, not that machine's opinion of it.
  const agentColor = useProjectPresentation(projectId).color?.fg;

  const composerRef = useRef<ComposerHandle>(null);
  const sessionState = meta?.state ?? "idle";
  const draft = useUIStore((s) => s.drafts[sessionId] ?? "");
  const stashStack = useUIStore((s) => s.stashes[sessionId]);
  const stashedText = stashStack?.[stashStack.length - 1] ?? "";
  const stashDepth = stashStack?.length ?? 0;
  const hasTodos = todos !== null && todos.length > 0;
  const isWorktree = !!meta?.worktreeBranch;
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const hasRemoteChanges =
    (projectGitStatus?.aheadRemote ?? 0) > 0 || (projectGitStatus?.behindRemote ?? 0) > 0;

  const [activeDialog, setActiveDialog] = useState<"none" | "pr" | "commit">("none");
  const [pendingTemplate, setPendingTemplate] = useState<{
    template: PromptTemplate;
    variables: string[];
  } | null>(null);
  const dockState = useUIStore((s) => sessionDock(s, sessionId));
  const openDock = useUIStore((s) => s.openDock);
  const setDockOpen = useUIStore((s) => s.setDockOpen);
  const dockWidth = useUIStore((s) => s.dockWidth);
  const dockMaximized = useUIStore((s) => s.dockMaximized);
  const setDockMaximized = useUIStore((s) => s.setDockMaximized);

  const latestTurnIndex = turns[turns.length - 1]?.turnIndex;
  const agentFlight = useMemo(() => partitionAgentRuns(agentRuns), [agentRuns]);
  const agentBadge = useMemo(() => agentBadgeState(agentRuns), [agentRuns]);
  const crew = useMemo(() => deriveCrew(allSessions, sessionId), [allSessions, sessionId]);
  // A spawn can land in another checkout, so a chip resolves its own project
  // rather than borrowing the lead's route params.
  const crewProjectSlug = useCallback(
    (id: string) => allProjects.find((p) => p.id === id)?.slug,
    [allProjects],
  );
  // No local seen-state: the server owns when a loop's attention clears
  // (`schedule.mark-viewed`, sent by LoopsPanel), and a failed loop
  // deliberately survives being looked at.
  const loopsAttention = useMemo(() => loopBadgeState(sessionSchedules), [sessionSchedules]);
  const [resuming, setResuming] = useState(false);
  const [expandFile, setExpandFile] = useState<string | null>(null);
  const [followRequest, setFollowRequest] = useState(0);
  const voiceEnabled = useFeatureStore((s) => s.features.voice);
  const browserEnabled = useFeatureStore((s) => s.features.browser);
  // A call is physical, not logical: the voice socket opens against the origin
  // serving this page, and dispatch goes through *that* server's session
  // service. A session on a paired machine is not there, so the handoff would
  // fail with "that could not be sent" after a whole conversation. Offer Live
  // only where it can actually deliver.
  const liveAvailable = voiceEnabled && !project?.machineId;

  const handleExpandFileConsumed = useCallback(() => {
    setExpandFile(null);
  }, []);
  /**
   * A diff selection becomes text in the composer and stops there — the same
   * contract the live call honours. One path into the session pipeline, and it
   * is the send button the reader can see.
   */
  const handleQuoteToComposer = useCallback((quote: string) => {
    const composer = composerRef.current;
    if (!composer) return;
    composer.setText(appendQuote(composer.getText(), quote));
  }, []);
  const handleFollowRequestConsumed = useCallback(() => {
    setFollowRequest(0);
  }, []);

  const git = useGitActions(sessionId);

  const hasChanges =
    (git.diffResult?.files.length ?? 0) + (git.uncommittedDiffResult?.files.length ?? 0) > 0;
  const totalAdd = (git.diffTotals?.add ?? 0) + (git.uncommittedDiffTotals?.add ?? 0);
  const totalDel = (git.diffTotals?.del ?? 0) + (git.uncommittedDiffTotals?.del ?? 0);

  // Reset transient UI state on session switch
  const prevSessionIdRef = useRef(sessionId);
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId;
      setActiveDialog("none");
      setResuming(false);
      setFollowRequest(0);
    }
  }, [sessionId]);

  // Set this session as active for unseen-completion tracking
  useEffect(() => {
    useChatStore.getState().setActiveSessionId(sessionId);
    return () => {
      const s = useChatStore.getState();
      if (s.activeSessionId === sessionId) {
        s.setActiveSessionId(null);
      }
    };
  }, [sessionId]);

  // Refresh git status on session navigation (skip if already fresh)
  useEffect(() => {
    if (!isGitFresh(sessionId)) {
      refreshGitStatus(ws, sessionId).catch((err) => console.error("refreshGitStatus failed", err));
    }
  }, [ws, sessionId]);

  // Design contract (docs/scheduled-loops.md): action_needed attention clears
  // on *viewing* the session — this panel being mounted is the view. failed
  // attention is never cleared here (explicit act only; the backend guards
  // this too). The push flips attention to "" so the effect settles; the ref
  // set (keyed by id + updatedAt) prevents duplicate RPCs while the ack is in
  // flight.
  const clearedAttentionRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    for (const sc of sessionSchedules) {
      if (sc.attention !== "action_needed") continue;
      const key = `${sc.id}:${sc.updatedAt}`;
      if (clearedAttentionRef.current.has(key)) continue;
      clearedAttentionRef.current.add(key);
      markScheduleViewed(ws, { id: sc.id }).catch((err) =>
        console.error("markScheduleViewed failed", err),
      );
    }
  }, [ws, sessionSchedules]);

  // Load history on mount or session switch
  const sessionExists = !!meta;
  const hasTurns = turns.length > 0;
  const needsArchivedBackfill = !!meta?.archivedAt && hasTurns && !historyComplete;
  useEffect(() => {
    if (sessionExists && (!hasTurns || needsArchivedBackfill)) {
      loadSessionHistory(ws, sessionId);
    }
  }, [ws, sessionId, sessionExists, hasTurns, needsArchivedBackfill]);

  // Redirect if session was deleted or doesn't exist
  useEffect(() => {
    if (sessionListLoaded && !meta) {
      navigate({
        to: "/project/$projectSlug",
        params: { projectSlug },
        replace: true,
      });
    }
  }, [sessionListLoaded, meta, navigate, projectSlug]);

  const handlePlanModeChange = useCallback(
    (enabled: boolean) => {
      useChatStore.getState().setSessionPlanMode(sessionId, enabled);
      const mode = enabled ? "plan" : "default";
      setPermissionMode(ws, sessionId, mode).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set plan mode"));
      });
    },
    [ws, sessionId],
  );

  const handleAutoApproveModeChange = useCallback(
    (mode: AutoApproveMode) => {
      useChatStore.getState().setSessionAutoApproveMode(sessionId, mode);
      setAutoApproveMode(ws, sessionId, mode).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set auto-approve mode"));
      });
    },
    [ws, sessionId],
  );

  const handleModelChange = useCallback(
    (model: ModelId) => {
      setSessionModel(ws, sessionId, model).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set model"));
      });
    },
    [ws, sessionId],
  );

  const handleTextPersist = useCallback(
    (text: string) => {
      useUIStore.getState().setDraft(sessionId, text);
    },
    [sessionId],
  );

  const handleStash = useCallback(
    (text: string) => {
      useUIStore.getState().pushStash(sessionId, text);
      useUIStore.getState().clearDraft(sessionId);
      toast("Prompt stashed", { description: "Ctrl+S on empty input to restore" });
    },
    [sessionId],
  );

  const handleUnstash = useCallback((): string | undefined => {
    return useUIStore.getState().popStash(sessionId);
  }, [sessionId]);

  const handleSend = useCallback(
    async (prompt: string, attachments?: Attachment[]): Promise<boolean> => {
      setFollowRequest((n) => n + 1);
      try {
        await enqueueMessage(ws, sessionId, prompt, attachments);
        useUIStore.getState().clearDraft(sessionId);
        const popped = useUIStore.getState().popStash(sessionId);
        if (popped) {
          composerRef.current?.setText(popped);
        }
        return true;
      } catch (err) {
        const msg = getErrorMessage(err, "Failed to send message");
        toast.error(msg, {
          action: { label: "Copy", onClick: () => copyToClipboard(msg) },
        });
        return false;
      }
    },
    [ws, sessionId],
  );

  const handleStartFresh = useCallback(
    async (plan: string) => {
      const stillHere = navGuard();
      try {
        const newId = await createSession(ws, projectId, "", !!meta?.worktreeBranch, {
          model: meta?.model,
          autoApproveMode: meta?.autoApproveMode,
        });
        await stopSession(ws, sessionId);
        await enqueueMessage(ws, newId, plan);
        if (!stillHere()) return;
        navigate({
          to: "/project/$projectSlug/session/$sessionShortId",
          params: { projectSlug, sessionShortId: sessionShortId(newId) },
        });
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to start fresh session"));
      }
    },
    [ws, projectId, sessionId, meta, navigate, navGuard, projectSlug],
  );

  const handleInterrupt = useCallback(async () => {
    interruptSession(ws, sessionId).catch(console.error);
  }, [ws, sessionId]);

  // Settings application mutates session state on the backend (set-model,
  // set-permission, set-auto-approve), so it must wait until the user has
  // committed to the template — i.e. either no variables to fill, or the
  // variable dialog was submitted. A canceled variable dialog leaves the
  // session untouched.
  const applyTemplateSettings = useCallback(
    (tmpl: PromptTemplate) => {
      const settings = parseSettings(tmpl.settings);
      // Mutable settings only — worktree and effort can't change on a running session.
      if (settings.model) handleModelChange(settings.model);
      if (settings.autoApproveMode) handleAutoApproveModeChange(settings.autoApproveMode);
      if (settings.planMode !== undefined) handlePlanModeChange(settings.planMode);
    },
    [handleModelChange, handleAutoApproveModeChange, handlePlanModeChange],
  );

  const handleTemplateSelect = useCallback(
    (tmpl: PromptTemplate) => {
      const vars = extractVariables(tmpl.content);
      if (vars.length > 0) {
        setPendingTemplate({ template: tmpl, variables: vars });
      } else {
        applyTemplateSettings(tmpl);
        composerRef.current?.setText(tmpl.content);
      }
    },
    [applyTemplateSettings],
  );

  const handleVariableSubmit = useCallback(
    (substituted: string) => {
      if (pendingTemplate) applyTemplateSettings(pendingTemplate.template);
      setPendingTemplate(null);
      composerRef.current?.setText(substituted);
    },
    [pendingTemplate, applyTemplateSettings],
  );

  const handleVariableCancel = useCallback(() => {
    setPendingTemplate(null);
  }, []);

  const handleResume = useCallback(async () => {
    if (resuming) return;
    setResuming(true);
    try {
      await resumeSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to resume session"));
    } finally {
      setResuming(false);
    }
  }, [ws, sessionId, resuming]);

  // Resuming reaches into the machine that owns the session, so an away
  // machine has nothing to resume — offering the button would only produce a
  // timeout. The banner disappears with it; the composer's placeholder is
  // where the session says what it is waiting for.
  const isResumable = resumableStates.has(sessionState) && !machineAway;
  const caps = meta?.capabilities;
  // `caps.planMode` and `handlePlanModeChange` stay: a template can still start
  // a session in plan mode, and the CLI can enter it on its own — what went is
  // the per-message toggle, not the capability.
  const attachmentsSupported = caps?.attachments !== false;
  const midTurnSendSupported = caps?.midTurnSendMessage !== false;
  const resumeSupported = caps?.resume !== false;
  const modelSwitchSupported = caps?.modelSwitch !== false;
  const blockedMidTurn = sessionState === "running" && !midTurnSendSupported;
  const isMobile = useIsMobile();

  if (!meta) {
    return <StatusPage message="Loading session..." />;
  }
  const uncommittedCount = git.uncommittedFiles?.length ?? 0;
  const hasGitContent = isWorktree || isDirty || hasRemoteChanges || hasChanges;
  const hasLoops = sessionSchedules.length > 0;
  const hasAgents = agentRuns.length > 0;
  const pendingApprovalSchedules = sessionSchedules.filter(
    (sc) => sc.pauseReason === "pending-approval",
  );
  // A stopped session with an enabled schedule is parked, not dead — the
  // scheduler resumes it on the next fire, so don't offer manual resume.
  const isParkedLoop =
    sessionState === "stopped" && sessionSchedules.some((sc) => sc.enabled && sc.nextRunAt !== "");
  // Neither is one agentique evicted, and for the stronger reason: nothing
  // happened. The idle sweep reclaimed a CLI that was sitting idle, and typing
  // resumes it — the same gesture the banner's button performs, minus the
  // banner. Announcing "Session interrupted" for that reported a fault where
  // there was none, and did it to every session left alone over a break.
  //
  // The composer stays live either way (`disabled` never covers stopped), so
  // suppressing this costs nothing and is not a hidden state: the session's
  // pill still reads Stopped, and the placeholder still says Enter resumes it.
  const wasEvicted = sessionState === "stopped" && !!meta.evictedAt;
  // The mobile finish action shares the strip below the header; compute it here
  // so the strip renders even when there is nothing else in it.
  const finishKind = isMobile ? finishActionKind(meta, git) : null;
  const ahead = isWorktree ? (meta?.commitsAhead ?? 0) : (projectGitStatus?.aheadRemote ?? 0);
  const behind = isWorktree ? (meta?.commitsBehind ?? 0) : (projectGitStatus?.behindRemote ?? 0);

  // Derived, never curated: a view exists because the session has the thing.
  const availability: DockAvailability = {
    work: hasTodos || hasAgents,
    changes: hasGitContent || hasChanges,
    loops: hasLoops,
    browser: browserEnabled,
  };
  const dockViews = availableDockViews(availability);
  // The reconciler: a stored view whose subject has since gone falls back
  // rather than collapsing the dock, which would read as the user's own gesture.
  const activeDockView = resolveDockView(dock ?? dockState.view, availability);
  const dockOpen = !!activeDockView && (dockState.open || dock !== undefined);
  const dockAlert = dockAlertState(agentBadge, loopsAttention);

  const changesMark: DockTabMark =
    ahead > 0 || behind > 0 || uncommittedCount > 0
      ? {
          kind: "count",
          label: [
            ahead > 0 ? `↑${ahead}` : null,
            behind > 0 ? `↓${behind}` : null,
            uncommittedCount > 0 ? `●${uncommittedCount}` : null,
          ]
            .filter(Boolean)
            .join(" "),
        }
      : totalAdd > 0 || totalDel > 0
        ? { kind: "count", label: `+${totalAdd}/-${totalDel}` }
        : null;

  const dockMarks: Partial<Record<DockView, DockTabMark>> = {
    work:
      agentBadge.running > 0
        ? { kind: "live", count: agentBadge.running }
        : hasTodos && todos
          ? {
              kind: "count",
              label: `${todos.filter((t) => t.status === "completed").length}/${todos.length}`,
            }
          : null,
    changes: changesMark,
    loops: loopsAttention
      ? loopsAttention.kind === "blocked"
        ? { kind: "blocked", count: loopsAttention.count }
        : { kind: "failed", count: loopsAttention.count }
      : null,
  };

  const selectDockView = (view: DockView) => {
    openDock(sessionId, view);
    onDockChange?.(view);
  };
  const closeDock = () => {
    setDockOpen(sessionId, false);
    onDockChange?.(null);
  };
  const toggleDock = () => {
    if (dockOpen) closeDock();
    else if (activeDockView) selectDockView(activeDockView);
  };

  const dockToggle = (
    <DockToggle
      open={dockOpen}
      onToggle={toggleDock}
      available={dockViews.length > 0}
      alert={dockAlert}
    />
  );

  // Suppressed while the dock is showing Work, where the board says it louder.
  const showFlightRail =
    agentFlight.inFlight.length > 0 && !(dockOpen && activeDockView === "work");
  const flightRail = showFlightRail ? (
    <AgentFlightStrip
      inFlight={agentFlight.inFlight}
      density={isMobile ? "line" : "rail"}
      expanded={flightExpanded}
      onExpandedChange={setFlightExpanded}
    />
  ) : null;

  // Never gated on the lead being busy: workers outlive the turn that spawned
  // them, so the moment this is most worth reading — idle, waiting on three
  // check-ins — is exactly when a busy-gated strip would be gone. It sits
  // below the flight rail because it is the more persistent of the two, and a
  // strip that comes and goes should not shove the steady one around.
  const crewStrip = (
    <CrewStrip
      crew={crew}
      density={isMobile ? "line" : "rail"}
      projectSlugFor={crewProjectSlug}
      expanded={crewExpanded}
      onExpandedChange={setCrewExpanded}
    />
  );

  const dockBody = !activeDockView ? null : activeDockView === "work" ? (
    <WorkView sessionId={sessionId} todos={todos} latestTurnIndex={latestTurnIndex} />
  ) : activeDockView === "changes" ? (
    <ChangesView
      meta={meta}
      git={git}
      mainBranch={mainBranch}
      projectGitStatus={projectGitStatus}
      projectGitActions={projectGitActions}
      committedDiff={git.diffResult}
      uncommittedDiff={git.uncommittedDiffResult}
      sessionState={sessionState}
      onSendMessage={handleSend}
      onOpenDialog={(d: "pr" | "commit") => setActiveDialog(d)}
      onQuoteToComposer={handleQuoteToComposer}
      expandFile={expandFile}
      onExpandFileConsumed={handleExpandFileConsumed}
    />
  ) : activeDockView === "loops" ? (
    <LoopsPanel sessionId={sessionId} />
  ) : (
    <BrowserPanel sessionId={sessionId} />
  );

  // Maximized is a desktop mode: on mobile the dock is a sheet over the chat,
  // so the chat never gives up its pane.
  const chatHidden = !isMobile && dockMaximized && dockOpen && !!activeDockView;

  const dockElement =
    dockOpen && activeDockView ? (
      <SessionDock
        views={dockViews}
        active={activeDockView}
        marks={dockMarks}
        onSelect={selectDockView}
        onClose={closeDock}
        maximized={dockMaximized}
        // No maximize on mobile: the sheet is already the whole screen.
        {...(isMobile ? {} : { onMaximizedChange: setDockMaximized })}
        accentColor={agentColor}
      >
        {dockBody}
      </SessionDock>
    ) : null;

  return (
    <SessionMachineContext.Provider value={project?.machineId ?? null}>
      <div
        className="flex flex-col h-full chat-frost"
        data-project-id={projectId}
        // Only the hue crosses over; .chat-frost derives the pane's ground and
        // its blobs from it, so the wash lives in one place.
        style={agentColor ? ({ "--agent": agentColor } as React.CSSProperties) : undefined}
      >
        <SessionHeader
          meta={meta}
          hasPendingInput={!!pendingApproval || !!pendingQuestion}
          dockToggle={dockToggle}
          agentsInFlight={agentFlight.inFlight.length}
          accentColor={agentColor}
          git={git}
          projectGitStatus={projectGitStatus}
          mainBranch={mainBranch}
          onSendMessage={handleSend}
        />

        {/* Mobile-only strip for branch completion. Pin, archive, and the
            dock's own control live in the header on every layout. */}
        {isMobile && finishKind && (
          <div className="shrink-0 flex items-center justify-end gap-2 px-2 py-1 border-b text-xs">
            <SessionFinishAction
              meta={meta}
              git={git}
              projectGitStatus={projectGitStatus}
              mainBranch={mainBranch}
              onSendMessage={handleSend}
            />
          </div>
        )}

        {/* The chat is the page; the dock sits beside it. */}
        <div className="flex-1 flex min-h-0 min-w-0">
          {!chatHidden && (
            <div className="flex-1 flex flex-col min-h-0 min-w-0">
              <MessageList
                turns={turns}
                sessionId={sessionId}
                projectId={projectId}
                sessionState={sessionState}
                projectPath={project?.path}
                worktreePath={meta.worktreePath}
                isLoadingHistory={isLoadingHistory}
                isBackfilling={isLoadingHistory && hasTurns && !historyComplete}
                targetTurnIndex={targetTurn}
                followRequest={followRequest}
                onFollowRequestConsumed={handleFollowRequestConsumed}
              />
              {pendingApproval && (
                <ApprovalBannerSwitch
                  sessionId={sessionId}
                  approval={pendingApproval}
                  onStartFresh={handleStartFresh}
                  projectPath={project?.path}
                  worktreePath={meta.worktreePath}
                />
              )}
              {pendingQuestion && (
                <QuestionBanner sessionId={sessionId} pending={pendingQuestion} />
              )}
              {pendingApprovalSchedules.map((sc) => (
                <ScheduleApprovalBanner key={sc.id} schedule={sc} />
              ))}

              {(contextUsage || compacting) && (
                <ContextBar usage={contextUsage} compacting={compacting} compact={isMobile} />
              )}
              {/* Suppressed while parked: the schedule will resume this session.
                  Suppressed after an eviction: nothing interrupted it. */}
              {isResumable && !isParkedLoop && !wasEvicted && (
                <ResumeBanner
                  state={sessionState as "stopped" | "failed" | "done"}
                  onResume={handleResume}
                  resuming={resuming}
                  branchMissing={meta?.branchMissing}
                  resumeUnsupported={!resumeSupported}
                />
              )}
              {flightRail}
              {crewStrip}
              <MessageComposer
                key={sessionId}
                projectId={projectId}
                ref={composerRef}
                onSend={handleSend}
                initialText={draft}
                onTextPersist={handleTextPersist}
                disabled={machineAway || sessionState === "merging" || compacting || blockedMidTurn}
                isRunning={sessionState === "running"}
                onInterrupt={handleInterrupt}
                // The call belongs to the app, not to this panel — the button
                // only names the session it should start on.
                onStartLive={
                  liveAvailable ? () => useVoiceStore.getState().start(sessionId) : undefined
                }
                attachmentsSupported={attachmentsSupported}
                focusMode
                placeholder={
                  machineAway
                    ? machineFault
                      ? `${machineName}: ${machineFault.detail}`
                      : `${machineName} is offline — this session picks up when it's back`
                    : compacting
                      ? "Compacting context..."
                      : sessionState === "merging"
                        ? "Git operation in progress..."
                        : blockedMidTurn
                          ? "Provider can't accept mid-turn messages — wait for the turn to finish"
                          : resumePlaceholders[sessionState]
                }
                autoApproveMode={autoApproveMode}
                onAutoApproveModeChange={handleAutoApproveModeChange}
                provider={(meta.provider as ProviderId) || undefined}
                model={(meta.model as ModelId) ?? undefined}
                modelDisplayName={sessionModelLabel(meta.model, meta.resolvedModel)}
                onModelChange={modelSwitchSupported ? handleModelChange : undefined}
                effort={(meta.effort as EffortLevel) ?? ""}
                onEmptySubmit={isResumable ? handleResume : undefined}
                stashedText={stashedText || undefined}
                stashDepth={stashDepth}
                onStash={handleStash}
                onUnstash={handleUnstash}
                templatePicker={
                  <TemplatePicker
                    onSelect={handleTemplateSelect}
                    disabled={sessionState === "merging" || compacting}
                  />
                }
              />
            </div>
          )}

          {/* Maximized, the dock takes the pane instead of splitting it — the
              same control serves a diff, a long report and a browser. */}
          {!isMobile && dockElement && (
            <div
              className="relative flex shrink-0 flex-col border-l"
              style={dockMaximized ? { flex: "1 1 auto" } : { width: dockWidth }}
            >
              {!dockMaximized && <DockResizeHandle />}
              {dockElement}
            </div>
          )}
        </div>

        {/* Mobile: the same dock, as a sheet. One navigation model, two
            presentations — a second model on mobile is how the chrome
            fragmented in the first place. */}
        {isMobile && (
          <Sheet open={dockOpen && !!dockElement} onOpenChange={(open) => !open && closeDock()}>
            <SheetContent side="right" className="w-[92vw] p-0" showCloseButton={false}>
              <SheetTitle className="sr-only">Session dock</SheetTitle>
              <SheetDescription className="sr-only">
                Todos, agents, changes and loops for this session
              </SheetDescription>
              {dockElement}
            </SheetContent>
          </Sheet>
        )}

        {/* Dialogs */}
        <CreatePRDialog
          open={activeDialog === "pr"}
          onOpenChange={(open) => setActiveDialog(open ? "pr" : "none")}
          sessionId={sessionId}
          defaultTitle={meta.name}
          onSubmit={async (title, body) => {
            const ok = await git.handlePRSubmit(title, body);
            if (ok) setActiveDialog("none");
          }}
          loading={git.creatingPR}
        />
        <CommitDialog
          open={activeDialog === "commit"}
          onOpenChange={(open) => setActiveDialog(open ? "commit" : "none")}
          sessionId={sessionId}
          defaultTitle={meta.name}
          onSubmit={async (message) => {
            const ok = await git.handleCommit(message);
            if (ok) setActiveDialog("none");
          }}
          loading={git.committing}
        />
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
    </SessionMachineContext.Provider>
  );
}
