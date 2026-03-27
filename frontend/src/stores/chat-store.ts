import { create } from "zustand";
import type { SessionInfo } from "~/lib/generated-types";
import { uuid } from "~/lib/utils";

// Debounce timer for rate-limit "allowed" → clear transition.
// Prevents flickering when multiple sessions alternate between warning and allowed.
let rateLimitClearTimer: ReturnType<typeof setTimeout> | null = null;

export interface ToolContentBlock {
  type: "text" | "image";
  text?: string;
  mediaType?: string;
  url?: string;
}

export interface ChatEvent {
  id: string;
  type:
    | "text"
    | "thinking"
    | "tool_use"
    | "tool_result"
    | "result"
    | "error"
    | "rate_limit"
    | "stream"
    | "compact_status"
    | "compact_boundary";
  content?: string;
  contentBlocks?: ToolContentBlock[];
  toolId?: string;
  toolName?: string;
  toolInput?: unknown;
  cost?: number;
  duration?: number;
  usage?: { inputTokens: number; outputTokens: number };
  stopReason?: string;
  contextWindow?: number;
  inputTokens?: number;
  outputTokens?: number;
  fatal?: boolean;
  status?: string;
  utilization?: number;
  resetsAt?: number;
  category?: string;
  errorType?: string;
  retryAfterSecs?: number;
  trigger?: string;
  preTokens?: number;
}

export interface Attachment {
  id: string;
  name: string;
  mimeType: string;
  dataUrl: string; // data:...;base64,... for sending/history
  previewUrl?: string; // blob: URL for local preview (not persisted)
}

export interface Turn {
  id: string;
  prompt: string;
  attachments: Attachment[];
  events: ChatEvent[];
  complete: boolean;
}

export type SessionState = "idle" | "running" | "done" | "failed" | "stopped" | "merging";

export type SessionMetadata = Omit<SessionInfo, "state" | "mergeStatus"> & {
  state: SessionState;
  mergeStatus?: "clean" | "conflicts" | "unknown";
};

export interface PendingApproval {
  approvalId: string;
  toolName: string;
  input: unknown;
}

export interface QuestionOption {
  label: string;
  description?: string;
}

export interface Question {
  question: string;
  header?: string;
  options?: QuestionOption[];
  multiSelect?: boolean;
}

export interface PendingQuestion {
  questionId: string;
  questions: Question[];
}

export interface RateLimitInfo {
  status: string;
  utilization: number;
  resetsAt: number | null;
}

export interface QueuedMessage {
  id: string;
  prompt: string;
  attachments?: Attachment[];
}

export interface TodoItem {
  content: string;
  activeForm?: string;
  status: "completed" | "in_progress" | "pending";
}

export interface ContextUsage {
  contextWindow: number;
  inputTokens: number;
  outputTokens: number;
}

export interface SessionData {
  meta: SessionMetadata;
  turns: Turn[];
  hasUnseenCompletion: boolean;
  pendingApproval: PendingApproval | null;
  pendingQuestion: PendingQuestion | null;
  planMode: boolean;
  autoApprove: boolean;
  queuedMessages: QueuedMessage[];
  todos: TodoItem[] | null;
  contextUsage: ContextUsage | null;
  compacting: boolean;
}

const emptySessionData = (meta: SessionMetadata): SessionData => ({
  meta,
  turns: [],
  hasUnseenCompletion: false,
  pendingApproval: null,
  pendingQuestion: null,
  planMode: meta.permissionMode === "plan",
  autoApprove: meta.autoApprove ?? false,
  queuedMessages: [],
  todos: null,
  contextUsage: null,
  compacting: false,
});

// --- Todo extraction helpers ---

function parseTodoItems(input: unknown): TodoItem[] | null {
  if (!input || typeof input !== "object") return null;
  const obj = input as Record<string, unknown>;
  if (!Array.isArray(obj.todos)) return null;
  const items: TodoItem[] = [];
  for (const item of obj.todos) {
    if (!item || typeof item !== "object") continue;
    const t = item as Record<string, unknown>;
    if (typeof t.content !== "string" || typeof t.status !== "string") continue;
    items.push({
      content: t.content,
      activeForm: typeof t.activeForm === "string" ? t.activeForm : undefined,
      status: t.status as TodoItem["status"],
    });
  }
  return items.length > 0 ? items : null;
}

function extractTodosFromEvent(event: ChatEvent): TodoItem[] | null {
  if (event.type !== "tool_use" || event.toolName !== "TodoWrite") return null;
  return parseTodoItems(event.toolInput);
}

function extractTodosFromTurns(turns: Turn[]): TodoItem[] | null {
  for (let i = turns.length - 1; i >= 0; i--) {
    const events = turns[i]?.events;
    if (!events) continue;
    for (let j = events.length - 1; j >= 0; j--) {
      const event = events[j];
      if (!event) continue;
      const todos = extractTodosFromEvent(event);
      if (todos) return todos;
    }
  }
  return null;
}

function extractContextUsageFromTurns(turns: Turn[]): ContextUsage | null {
  for (let i = turns.length - 1; i >= 0; i--) {
    const events = turns[i]?.events;
    if (!events) continue;
    for (let j = events.length - 1; j >= 0; j--) {
      const event = events[j];
      if (event?.type === "compact_boundary") return null;
      if (event?.type === "result" && event.contextWindow && event.contextWindow > 0) {
        return {
          contextWindow: event.contextWindow,
          inputTokens: event.inputTokens ?? 0,
          outputTokens: event.outputTokens ?? 0,
        };
      }
    }
  }
  return null;
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
  rateLimit: RateLimitInfo | null;

  // Session management
  setSessions: (sessions: SessionMetadata[], projectId: string) => void;
  addSession: (meta: SessionMetadata) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string | null) => void;
  setSessionState: (
    sessionId: string,
    state: SessionState,
    extras?: Partial<
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
      >
    >,
  ) => void;
  setSessionName: (sessionId: string, name: string) => void;
  setSessionModel: (sessionId: string, model: string) => void;
  setPendingApproval: (sessionId: string, approval: PendingApproval) => void;
  clearPendingApproval: (sessionId: string) => void;
  setPendingQuestion: (sessionId: string, question: PendingQuestion) => void;
  clearPendingQuestion: (sessionId: string) => void;
  setSessionPlanMode: (sessionId: string, planMode: boolean) => void;
  setSessionAutoApprove: (sessionId: string, autoApprove: boolean) => void;
  setSessionPrUrl: (sessionId: string, prUrl: string) => void;
  updateStreamingContextUsage: (
    sessionId: string,
    patch: { inputTokens?: number; outputTokens?: number },
  ) => void;

  // History
  setHistoryLoading: (sessionId: string, loading: boolean) => void;
  setSessionHistory: (sessionId: string, turns: Turn[]) => void;

  // Message queue
  enqueueMessage: (sessionId: string, prompt: string, attachments?: Attachment[]) => void;
  dequeueMessage: (sessionId: string) => void;
  cancelQueuedMessage: (sessionId: string, messageId: string) => void;
  clearQueue: (sessionId: string) => void;

  // Turn/event management
  submitQuery: (sessionId: string, prompt: string, attachments?: Attachment[]) => void;
  handleServerEvent: (sessionId: string, event: ChatEvent) => void;
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: {},
  activeSessionId: null,
  loadedProjects: new Set<string>(),
  historyLoading: new Set<string>(),
  rateLimit: null,

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
          sessions[meta.id] = {
            ...existing,
            meta: tagged,
            planMode: tagged.permissionMode === "plan",
            autoApprove: tagged.autoApprove ?? false,
          };
        } else {
          sessions[meta.id] = emptySessionData(tagged);
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
      if (id && s.sessions[id]) {
        return {
          activeSessionId: id,
          ...updateSession(s, id, { hasUnseenCompletion: false }),
        };
      }
      return { activeSessionId: id };
    }),

  setSessionState: (sessionId, state, extras) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;

      // Reject stale updates via monotonic version.
      const incoming = extras?.gitVersion ?? 0;
      const current = session.meta.gitVersion ?? 0;
      if (incoming > 0 && current > 0 && incoming < current) return s;

      // Transient states (running, merging) don't compute git fields on the
      // backend — preserve the frontend's cached values instead of zeroing them.
      // Exception: mid-turn git refreshes send real dirty/uncommitted values
      // during running state with a newer version — accept those.
      const transient = state === "running" || state === "merging";
      const staleTransient = transient && incoming <= current;
      const m = session.meta;
      const patch: Partial<SessionMetadata> = {
        state,
        connected: extras?.connected ?? m.connected,
        gitOperation: extras?.gitOperation ?? "",
        gitVersion: incoming || current,
        completedAt: transient ? m.completedAt : extras?.completedAt,
        hasDirtyWorktree: staleTransient ? m.hasDirtyWorktree : (extras?.hasDirtyWorktree ?? false),
        hasUncommitted: staleTransient ? m.hasUncommitted : (extras?.hasUncommitted ?? false),
        worktreeMerged: transient ? m.worktreeMerged : (extras?.worktreeMerged ?? false),
        commitsAhead: transient ? m.commitsAhead : (extras?.commitsAhead ?? 0),
        commitsBehind: transient ? m.commitsBehind : (extras?.commitsBehind ?? 0),
        branchMissing: transient ? m.branchMissing : (extras?.branchMissing ?? false),
        mergeStatus: transient ? m.mergeStatus : extras?.mergeStatus,
        mergeConflictFiles: transient ? m.mergeConflictFiles : extras?.mergeConflictFiles,
      };
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

  setSessionAutoApprove: (sessionId, autoApprove) =>
    set((s) => updateSession(s, sessionId, { autoApprove })),

  setSessionPrUrl: (sessionId, prUrl) => set((s) => updateMeta(s, sessionId, { prUrl })),

  updateStreamingContextUsage: (sessionId, patch) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const prev = session.contextUsage;
      // All current Claude models: 200k context window
      const contextWindow = prev?.contextWindow ?? 200_000;
      return updateSession(s, sessionId, {
        contextUsage: {
          contextWindow,
          inputTokens: patch.inputTokens ?? prev?.inputTokens ?? 0,
          outputTokens:
            patch.outputTokens !== undefined
              ? Math.max(patch.outputTokens, prev?.outputTokens ?? 0)
              : (prev?.outputTokens ?? 0),
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

  setSessionHistory: (sessionId, turns) =>
    set((s) => {
      const nextLoading = new Set(s.historyLoading);
      nextLoading.delete(sessionId);
      const session = s.sessions[sessionId];
      if (!session) return { historyLoading: nextLoading };
      const todos = extractTodosFromTurns(turns);
      const contextUsage = extractContextUsageFromTurns(turns);
      return {
        historyLoading: nextLoading,
        ...updateSession(s, sessionId, { turns, todos, contextUsage }),
      };
    }),

  enqueueMessage: (sessionId, prompt, attachments) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return updateSession(s, sessionId, {
        queuedMessages: [...session.queuedMessages, { id: uuid(), prompt, attachments }],
      });
    }),

  dequeueMessage: (sessionId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session || session.queuedMessages.length === 0) return s;
      return updateSession(s, sessionId, {
        queuedMessages: session.queuedMessages.slice(1),
      });
    }),

  cancelQueuedMessage: (sessionId, messageId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return updateSession(s, sessionId, {
        queuedMessages: session.queuedMessages.filter((m) => m.id !== messageId),
      });
    }),

  clearQueue: (sessionId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session || session.queuedMessages.length === 0) return s;
      return updateSession(s, sessionId, { queuedMessages: [] });
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
      });
    }),

  handleServerEvent: (sessionId, event) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        console.warn("handleServerEvent: unknown session", sessionId);
        return s;
      }

      // Transient events: update session state without appending to turns
      if (event.type === "rate_limit") {
        const status = event.status ?? "";
        if (rateLimitClearTimer) {
          clearTimeout(rateLimitClearTimer);
          rateLimitClearTimer = null;
        }
        if (status === "allowed") {
          rateLimitClearTimer = setTimeout(() => {
            rateLimitClearTimer = null;
            useChatStore.setState({ rateLimit: null });
          }, 5_000);
          return s;
        }
        return {
          rateLimit: {
            status,
            utilization: event.utilization ?? 0,
            resetsAt: event.resetsAt ?? null,
          },
        };
      }
      if (event.type === "stream") return s;
      if (event.type === "compact_status") {
        return updateSession(s, sessionId, {
          compacting: event.status === "compacting",
        });
      }

      const turns = [...session.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) {
        console.warn("handleServerEvent: no turns for session", sessionId);
        return s;
      }

      turns[turns.length - 1] = {
        ...lastTurn,
        events: [...lastTurn.events, event],
        complete: lastTurn.complete || event.type === "result",
      };

      const todos = extractTodosFromEvent(event);
      const isResult = event.type === "result";
      const isViewing = s.activeSessionId === sessionId;
      const patch: Partial<SessionData> = { turns };
      if (todos) patch.todos = todos;
      if (isResult) {
        patch.meta = { ...session.meta, state: "idle" };
        patch.hasUnseenCompletion = !isViewing;
        if (event.contextWindow && event.contextWindow > 0) {
          patch.contextUsage = {
            contextWindow: event.contextWindow,
            inputTokens: event.inputTokens ?? 0,
            outputTokens: event.outputTokens ?? 0,
          };
        }
      }
      if (event.type === "compact_boundary") {
        patch.contextUsage = null;
        patch.compacting = false;
      }

      return updateSession(s, sessionId, patch);
    }),
}));
