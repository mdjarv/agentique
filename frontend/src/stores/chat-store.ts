import { create } from "zustand";
import { extractContextUsageFromTurns, extractTodosFromTurns } from "~/lib/event-extractors";
import { uuid } from "~/lib/utils";
import { type ApplyResult, applyServerEvent } from "~/stores/apply-event";
import type {
  AutoApproveMode,
  ChatEvent,
  PendingApproval,
  PendingQuestion,
  SessionData,
  SessionMetadata,
  SessionState,
  Turn,
} from "~/stores/chat-types";
import { useRateLimitStore } from "~/stores/rate-limit-store";

// Re-export all types from chat-types so existing consumers don't break.
export type {
  AgentMessageEvent,
  Attachment,
  AutoApproveMode,
  ChatEvent,
  ChatEventType,
  CompactBoundaryEvent,
  CompactStatusEvent,
  ContextManagementEvent,
  ContextUsage,
  ErrorEvent,
  MessageDeliveryEvent,
  PendingApproval,
  PendingQuestion,
  Question,
  QuestionOption,
  RateLimitEvent,
  ResultEvent,
  SessionData,
  SessionMetadata,
  SessionState,
  StreamEvent,
  TaskEvent,
  // Discriminated union variants
  TextEvent,
  ThinkingEvent,
  TodoItem,
  ToolContentBlock,
  ToolResultEvent,
  ToolUseEvent,
  Turn,
  UserMessageEvent,
} from "~/stores/chat-types";

// --- Pending state buffer ---
// Buffers session.state updates that arrive before session.created.
type StateExtras = Partial<
  Pick<
    SessionMetadata,
    | "connected"
    | "hasDirtyWorktree"
    | "worktreeMerged"
    | "completedAt"
    | "hasUncommitted"
    | "commitsAhead"
    | "commitsBehind"
    | "branchMissing"
    | "mergeStatus"
    | "mergeConflictFiles"
    | "gitOperation"
    | "gitVersion"
    | "worktreeBranch"
    | "worktreePath"
  >
>;
interface PendingStateEntry {
  state: SessionState;
  extras?: StateExtras;
}
const pendingStateUpdates = new Map<string, PendingStateEntry>();

// Exported for tests.
export function _clearPendingStateUpdates(): void {
  pendingStateUpdates.clear();
}

/** When evicting turns from an inactive session, keep this many recent turns
 *  so switching back renders instantly while the full history backfills. */
const TAIL_TURN_COUNT = 20;

const emptySessionData = (meta: SessionMetadata): SessionData => ({
  meta,
  turns: [],
  streamingEvents: [],
  historyComplete: false,
  hasUnseenCompletion: false,
  hasUnreadChannelMessage: false,
  pendingApproval: null,
  pendingQuestion: null,
  planMode: meta.permissionMode === "plan",
  autoApproveMode: (meta.autoApproveMode as AutoApproveMode) ?? "manual",
  todos: null,
  contextUsage: null,
  compacting: false,
});

function evictTurns(session: SessionData): Partial<SessionData> {
  if (session.turns.length <= TAIL_TURN_COUNT) return {};
  return {
    turns: session.turns.slice(-TAIL_TURN_COUNT),
    streamingEvents: [],
    historyComplete: false,
  };
}

// --- Immutable update helpers ---

function updateSession(
  s: ChatState,
  sessionId: string,
  patch: Partial<SessionData>,
): Partial<ChatState> {
  const session = s.sessions[sessionId];
  if (!session) return s;
  return {
    sessions: {
      ...s.sessions,
      [sessionId]: { ...session, ...patch },
    },
  };
}

function updateMeta(
  s: ChatState,
  sessionId: string,
  metaPatch: Partial<SessionMetadata>,
): Partial<ChatState> {
  const session = s.sessions[sessionId];
  if (!session) return s;
  return {
    sessions: {
      ...s.sessions,
      [sessionId]: { ...session, meta: { ...session.meta, ...metaPatch } },
    },
  };
}

// --- Store ---

export interface ChatState {
  sessions: Record<string, SessionData>;
  activeSessionId: string | null;
  loadedProjects: Set<string>;
  historyLoading: Set<string>;

  // Session management
  setSessions: (sessions: SessionMetadata[], projectId: string) => void;
  addSession: (meta: SessionMetadata) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string | null) => void;
  setSessionState: (sessionId: string, state: SessionState, extras?: StateExtras) => void;
  flushPendingState: (sessionId: string) => void;
  setSessionName: (sessionId: string, name: string) => void;
  setSessionModel: (sessionId: string, model: string) => void;
  setPendingApproval: (sessionId: string, approval: PendingApproval) => void;
  clearPendingApproval: (sessionId: string) => void;
  setPendingQuestion: (sessionId: string, question: PendingQuestion) => void;
  clearPendingQuestion: (sessionId: string) => void;
  setSessionPlanMode: (sessionId: string, planMode: boolean) => void;
  setSessionAutoApproveMode: (sessionId: string, mode: AutoApproveMode) => void;
  setSessionPrUrl: (sessionId: string, prUrl: string) => void;
  setSessionIcon: (sessionId: string, icon: string | undefined) => void;
  addSessionChannel: (sessionId: string, channelId: string, role?: string) => void;
  removeSessionChannel: (sessionId: string, channelId: string) => void;
  setUnreadChannelMessage: (sessionId: string, value: boolean) => void;
  updateStreamingContextUsage: (
    sessionId: string,
    patch: { inputTokens?: number; outputTokens?: number },
  ) => void;

  // History
  setHistoryLoading: (sessionId: string, loading: boolean) => void;
  setSessionHistory: (sessionId: string, turns: Turn[], complete?: boolean) => void;

  // Turn/event management
  submitQuery: (
    sessionId: string,
    prompt: string,
    attachments?: import("~/stores/chat-types").Attachment[],
  ) => void;
  rollbackOptimisticTurn: (sessionId: string, prompt: string) => void;
  handleServerEvent: (sessionId: string, event: ChatEvent) => void;
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: {},
  activeSessionId: null,
  loadedProjects: new Set<string>(),
  historyLoading: new Set<string>(),

  setSessions: (metas, projectId) =>
    set((s) => {
      // Keep sessions from other projects, replace sessions for this project
      const sessions: Record<string, SessionData> = {};
      for (const [id, data] of Object.entries(s.sessions)) {
        if (data.meta.projectId !== projectId) {
          sessions[id] = data;
        }
      }
      for (const meta of metas) {
        const tagged = { ...meta, projectId };
        const existing = s.sessions[meta.id];
        if (existing) {
          // Preserve live state/gitVersion when the frontend has a newer version
          // than the session.list response (which reads state from DB and may lag).
          const existingV = existing.meta.gitVersion ?? 0;
          const incomingV = tagged.gitVersion ?? 0;
          const keepState = existingV > 0 && existingV >= incomingV;
          const mergedMeta = keepState
            ? {
                ...tagged,
                state: existing.meta.state,
                connected: existing.meta.connected,
                gitVersion: existingV,
              }
            : tagged;
          sessions[meta.id] = {
            ...existing,
            meta: mergedMeta,
            planMode: mergedMeta.permissionMode === "plan",
            autoApproveMode: (mergedMeta.autoApproveMode as AutoApproveMode) ?? "manual",
            pendingApproval: tagged.pendingApproval ?? existing.pendingApproval,
            pendingQuestion: tagged.pendingQuestion ?? existing.pendingQuestion,
          };
        } else {
          const data = emptySessionData(tagged);
          if (tagged.pendingApproval) data.pendingApproval = tagged.pendingApproval;
          if (tagged.pendingQuestion) data.pendingQuestion = tagged.pendingQuestion;
          sessions[meta.id] = data;
        }
      }
      const loadedProjects = new Set(s.loadedProjects);
      loadedProjects.add(projectId);
      return { sessions, loadedProjects };
    }),

  addSession: (meta) =>
    set((s) => ({
      sessions: { ...s.sessions, [meta.id]: emptySessionData(meta) },
    })),

  flushPendingState: (sessionId) => {
    const pending = pendingStateUpdates.get(sessionId);
    if (!pending) return;
    pendingStateUpdates.delete(sessionId);
    useChatStore.getState().setSessionState(sessionId, pending.state, pending.extras);
  },

  removeSession: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.sessions;
      const activeSessionId = s.activeSessionId === id ? null : s.activeSessionId;
      const historyLoading = new Set(s.historyLoading);
      historyLoading.delete(id);
      return { sessions: rest, activeSessionId, historyLoading };
    }),

  setActiveSessionId: (id) =>
    set((s) => {
      const mark = `session:switch ${id?.slice(0, 8) ?? "null"}`;
      performance.mark(`${mark}:start`);

      // Evict turns from the previous completed session, keeping a tail cache.
      // Only creates a new sessions reference when eviction actually changes data.
      let sessions = s.sessions;
      const prevId = s.activeSessionId;
      if (prevId && prevId !== id) {
        const prev = sessions[prevId];
        if (prev?.meta.completedAt && prev.turns.length > 0) {
          const eviction = evictTurns(prev);
          if (Object.keys(eviction).length > 0) {
            sessions = { ...sessions, [prevId]: { ...prev, ...eviction } };
          }
        }
      }

      // Only spread sessions when unseen flags need clearing — avoids creating a
      // new sessions reference on every switch, which would trigger expensive
      // sidebar re-renders from useFolderGroups.
      if (id) {
        const next = sessions[id];
        if (next && (next.hasUnseenCompletion || next.hasUnreadChannelMessage)) {
          sessions = {
            ...sessions,
            [id]: { ...next, hasUnseenCompletion: false, hasUnreadChannelMessage: false },
          };
        }
      }

      performance.mark(`${mark}:end`);
      performance.measure(mark, `${mark}:start`, `${mark}:end`);
      return { activeSessionId: id, sessions };
    }),

  setSessionState: (sessionId, state, extras) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        // Buffer for when addSession creates this session (race: state arrives before created).
        const existing = pendingStateUpdates.get(sessionId);
        const incomingV = extras?.gitVersion ?? 0;
        const existingV = existing?.extras?.gitVersion ?? 0;
        if (!existing || incomingV >= existingV) {
          pendingStateUpdates.set(sessionId, { state, extras });
        }
        return s;
      }

      // Reject stale updates via monotonic version.
      const incoming = extras?.gitVersion ?? 0;
      const current = session.meta.gitVersion ?? 0;
      if (incoming > 0 && current > 0 && incoming < current) return s;

      // Transient states (running, merging) don't compute git fields on the
      // backend — preserve the frontend's cached values instead of zeroing them.
      const transient = state === "running" || state === "merging";
      const staleTransient = transient && incoming <= current;
      const m = session.meta;
      const patch: Partial<SessionMetadata> = {
        state,
        connected: extras?.connected ?? m.connected,
        gitOperation: extras?.gitOperation ?? "",
        gitVersion: incoming || current,
        gitRefreshedAt: incoming > current ? Date.now() : m.gitRefreshedAt,
        completedAt: extras?.completedAt,
        hasDirtyWorktree: staleTransient ? m.hasDirtyWorktree : (extras?.hasDirtyWorktree ?? false),
        hasUncommitted: staleTransient ? m.hasUncommitted : (extras?.hasUncommitted ?? false),
        worktreeMerged: extras?.worktreeMerged ?? false,
        commitsAhead: transient ? m.commitsAhead : (extras?.commitsAhead ?? 0),
        commitsBehind: transient ? m.commitsBehind : (extras?.commitsBehind ?? 0),
        branchMissing: transient ? m.branchMissing : (extras?.branchMissing ?? false),
        mergeStatus: transient ? m.mergeStatus : extras?.mergeStatus,
        mergeConflictFiles: transient ? m.mergeConflictFiles : extras?.mergeConflictFiles,
        worktreeBranch: extras?.worktreeBranch ?? m.worktreeBranch,
        worktreePath: extras?.worktreePath ?? m.worktreePath,
      };
      // Evict turns when a session becomes completed and isn't being viewed.
      const becameCompleted = !transient && extras?.completedAt && !m.completedAt;
      if (becameCompleted && s.activeSessionId !== sessionId && session.turns.length > 0) {
        return updateSession(s, sessionId, {
          meta: { ...m, ...patch },
          ...evictTurns(session),
        });
      }
      return updateMeta(s, sessionId, patch);
    }),

  setSessionName: (sessionId, name) => set((s) => updateMeta(s, sessionId, { name })),
  setSessionModel: (sessionId, model) => set((s) => updateMeta(s, sessionId, { model })),

  setPendingApproval: (sessionId, approval) =>
    set((s) => updateSession(s, sessionId, { pendingApproval: approval })),

  clearPendingApproval: (sessionId) =>
    set((s) => updateSession(s, sessionId, { pendingApproval: null })),

  setPendingQuestion: (sessionId, question) =>
    set((s) => updateSession(s, sessionId, { pendingQuestion: question })),

  clearPendingQuestion: (sessionId) =>
    set((s) => updateSession(s, sessionId, { pendingQuestion: null })),

  setSessionPlanMode: (sessionId, planMode) =>
    set((s) => updateSession(s, sessionId, { planMode })),

  setSessionAutoApproveMode: (sessionId, autoApproveMode) =>
    set((s) => updateSession(s, sessionId, { autoApproveMode })),

  setSessionPrUrl: (sessionId, prUrl) => set((s) => updateMeta(s, sessionId, { prUrl })),

  setSessionIcon: (sessionId, icon) => set((s) => updateMeta(s, sessionId, { icon })),
  addSessionChannel: (sessionId, channelId, role?) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const existing = session.meta.channelIds ?? [];
      const patch: Partial<SessionMetadata> = {};
      if (!existing.includes(channelId)) {
        patch.channelIds = [...existing, channelId];
      }
      if (role) {
        patch.channelRoles = { ...session.meta.channelRoles, [channelId]: role };
      }
      if (!Object.keys(patch).length) return s;
      return updateMeta(s, sessionId, patch);
    }),
  removeSessionChannel: (sessionId, channelId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const existing = session.meta.channelIds ?? [];
      const filtered = existing.filter((id) => id !== channelId);
      const { [channelId]: _, ...remainingRoles } = session.meta.channelRoles ?? {};
      return updateMeta(s, sessionId, {
        channelIds: filtered.length > 0 ? filtered : undefined,
        channelRoles: Object.keys(remainingRoles).length > 0 ? remainingRoles : undefined,
      });
    }),

  setUnreadChannelMessage: (sessionId, value) =>
    set((s) => updateSession(s, sessionId, { hasUnreadChannelMessage: value })),

  updateStreamingContextUsage: (sessionId, patch) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const prev = session.contextUsage;
      const contextWindow =
        prev?.contextWindow ?? (session.meta.model?.endsWith("[1m]") ? 1_000_000 : 200_000);
      return updateSession(s, sessionId, {
        contextUsage: {
          contextWindow,
          inputTokens: patch.inputTokens ?? prev?.inputTokens ?? 0,
          outputTokens: patch.outputTokens ?? prev?.outputTokens ?? 0,
        },
      });
    }),

  setHistoryLoading: (sessionId, loading) =>
    set((s) => {
      const next = new Set(s.historyLoading);
      if (loading) next.add(sessionId);
      else next.delete(sessionId);
      return { historyLoading: next };
    }),

  setSessionHistory: (sessionId, turns, complete = true) =>
    set((s) => {
      const sid = sessionId.slice(0, 8);
      const mark = `history:${sid} set-state`;
      performance.mark(`${mark}:start`);

      const nextLoading = new Set(s.historyLoading);
      if (complete) nextLoading.delete(sessionId);
      const session = s.sessions[sessionId];
      if (!session) return { historyLoading: nextLoading };

      // During backfill (tail cache → full history), preserve existing turn
      // objects whose IDs match so React can skip re-rendering them.
      let merged = turns;
      let preserved = 0;
      if (!session.historyComplete && session.turns.length > 0) {
        const cached = new Map(session.turns.map((t) => [t.id, t]));
        if (cached.size > 0) {
          merged = turns.map((t) => {
            const existing = cached.get(t.id);
            if (existing) preserved++;
            return existing ?? t;
          });
        }
      }

      const todos = extractTodosFromTurns(merged);
      const contextUsage = extractContextUsageFromTurns(merged);
      const result = {
        historyLoading: nextLoading,
        ...updateSession(s, sessionId, {
          turns: merged,
          streamingEvents: [],
          historyComplete: complete,
          todos,
          contextUsage,
        }),
      };

      performance.mark(`${mark}:end`);
      performance.measure(
        `${mark} (${turns.length} turns, ${preserved} cached)`,
        `${mark}:start`,
        `${mark}:end`,
      );
      return result;
    }),

  submitQuery: (sessionId, prompt, attachments) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return updateSession(s, sessionId, {
        turns: [
          ...session.turns,
          {
            id: uuid(),
            prompt,
            attachments: (attachments ?? []).map(({ previewUrl: _, ...rest }) => rest),
            events: [],
            complete: false,
          },
        ],
        streamingEvents: [],
      });
    }),

  rollbackOptimisticTurn: (sessionId, prompt) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const turns = session.turns;
      const last = turns[turns.length - 1];
      if (
        last &&
        !last.complete &&
        last.events.length === 0 &&
        session.streamingEvents.length === 0 &&
        last.prompt === prompt
      ) {
        return updateSession(s, sessionId, { turns: turns.slice(0, -1) });
      }
      return s;
    }),

  handleServerEvent: (sessionId, event) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        console.warn("handleServerEvent: unknown session", sessionId);
        return s;
      }

      const isViewing = s.activeSessionId === sessionId;
      const result: ApplyResult = applyServerEvent(session, event, isViewing);
      if (!result) return s;

      // Execute side effects that can't live in the pure function.
      if (result.sideEffect?.type === "rate_limit") {
        const se = result.sideEffect;
        useRateLimitStore
          .getState()
          .updateEntry(se.rateLimitType, se.status, se.utilization, se.resetsAt);
      }

      // rate_limit returns an empty patch — skip the updateSession call.
      if (Object.keys(result.patch).length === 0) return s;

      return updateSession(s, sessionId, result.patch);
    }),
}));
