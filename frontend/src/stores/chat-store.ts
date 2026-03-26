import { create } from "zustand";
import { uuid } from "~/lib/utils";

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
    | "stream";
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
  category?: string;
  errorType?: string;
  retryAfterSecs?: number;
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

export interface SessionMetadata {
  id: string;
  projectId: string;
  name: string;
  state: SessionState;
  connected: boolean;
  model?: string;
  permissionMode?: string;
  autoApprove?: boolean;
  effort?: string;
  maxBudget?: number;
  maxTurns?: number;
  totalCost?: number;
  turnCount?: number;
  worktreePath?: string;
  worktreeBranch?: string;
  hasDirtyWorktree?: boolean;
  worktreeMerged?: boolean;
  completedAt?: string;
  commitsAhead?: number;
  commitsBehind?: number;
  branchMissing?: boolean;
  hasUncommitted?: boolean;
  mergeStatus?: "clean" | "conflicts" | "unknown";
  mergeConflictFiles?: string[];
  gitOperation?: string;
  prUrl?: string;
  createdAt: string;
  updatedAt?: string;
  lastQueryAt?: string;
}

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
  rateLimit: RateLimitInfo | null;
  queuedMessages: QueuedMessage[];
  todos: TodoItem[] | null;
  contextUsage: ContextUsage | null;
  draft: string;
}

const emptySessionData = (meta: SessionMetadata): SessionData => ({
  meta,
  turns: [],
  hasUnseenCompletion: false,
  pendingApproval: null,
  pendingQuestion: null,
  planMode: meta.permissionMode === "plan",
  autoApprove: meta.autoApprove ?? false,
  rateLimit: null,
  queuedMessages: [],
  todos: null,
  contextUsage: null,
  draft: "",
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

  // History
  setHistoryLoading: (sessionId: string, loading: boolean) => void;
  setSessionHistory: (sessionId: string, turns: Turn[]) => void;

  // Drafts
  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;

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
      const patch: Partial<SessionMetadata> = { state };
      if (extras) {
        for (const [k, v] of Object.entries(extras)) {
          if (v !== undefined) (patch as Record<string, unknown>)[k] = v;
        }
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

  setSessionAutoApprove: (sessionId, autoApprove) =>
    set((s) => updateSession(s, sessionId, { autoApprove })),

  setSessionPrUrl: (sessionId, prUrl) => set((s) => updateMeta(s, sessionId, { prUrl })),

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

  setDraft: (sessionId, text) => set((s) => updateSession(s, sessionId, { draft: text })),

  clearDraft: (sessionId) => set((s) => updateSession(s, sessionId, { draft: "" })),

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
        if (status === "allowed") return s;
        return updateSession(s, sessionId, {
          rateLimit: { status, utilization: event.utilization ?? 0 },
        });
      }
      if (event.type === "stream") return s;

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
        patch.rateLimit = null;
        if (event.contextWindow && event.contextWindow > 0) {
          patch.contextUsage = {
            contextWindow: event.contextWindow,
            inputTokens: event.inputTokens ?? 0,
            outputTokens: event.outputTokens ?? 0,
          };
        }
      }

      return updateSession(s, sessionId, patch);
    }),
}));
