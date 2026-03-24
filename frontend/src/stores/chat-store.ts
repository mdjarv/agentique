import { create } from "zustand";
import { uuid } from "~/lib/utils";

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
  toolId?: string;
  toolName?: string;
  toolInput?: unknown;
  cost?: number;
  duration?: number;
  usage?: { inputTokens: number; outputTokens: number };
  stopReason?: string;
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

export type SessionState =
  | "draft"
  | "disconnected"
  | "starting"
  | "idle"
  | "running"
  | "done"
  | "failed"
  | "stopped";

export interface SessionMetadata {
  id: string;
  projectId: string;
  name: string;
  state: SessionState;
  model?: string;
  permissionMode?: string;
  autoApprove?: boolean;
  worktreePath?: string;
  worktreeBranch?: string;
  hasDirtyWorktree?: boolean;
  worktreeMerged?: boolean;
  commitsAhead?: number;
  branchMissing?: boolean;
  hasUncommitted?: boolean;
  createdAt: string;
  updatedAt?: string;
  worktree?: boolean; // draft-only: user's worktree toggle preference
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

export interface SessionData {
  meta: SessionMetadata;
  turns: Turn[];
  hasUnseenCompletion: boolean;
  pendingApproval: PendingApproval | null;
  pendingQuestion: PendingQuestion | null;
  planMode: boolean;
  autoApprove: boolean;
  rateLimit: RateLimitInfo | null;
  draftText: string;
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
  draftText: "",
});

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
  historyLoading: Set<string>;

  // Session management
  setSessions: (sessions: SessionMetadata[], projectId: string) => void;
  addSession: (meta: SessionMetadata) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string | null) => void;
  setSessionState: (
    sessionId: string,
    state: SessionState,
    git?: Partial<
      Pick<
        SessionMetadata,
        "hasDirtyWorktree" | "worktreeMerged" | "hasUncommitted" | "commitsAhead" | "branchMissing"
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

  // Draft session management
  createDraft: (projectId: string) => void;
  setDraftWorktree: (sessionId: string, worktree: boolean) => void;
  setDraftText: (sessionId: string, text: string) => void;

  // History
  setHistoryLoading: (sessionId: string, loading: boolean) => void;
  setSessionHistory: (sessionId: string, turns: Turn[]) => void;

  // Turn/event management
  submitQuery: (sessionId: string, prompt: string, attachments?: Attachment[]) => void;
  handleServerEvent: (sessionId: string, event: ChatEvent) => void;

  // Project-level reset
  resetProject: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: {},
  activeSessionId: null,
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
      return { sessions };
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

  setSessionState: (sessionId, state, git) =>
    set((s) => {
      const patch: Partial<SessionMetadata> = { state };
      if (git) {
        for (const [k, v] of Object.entries(git)) {
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
      return {
        historyLoading: nextLoading,
        ...updateSession(s, sessionId, { turns }),
      };
    }),

  createDraft: (projectId) =>
    set((s) => {
      const existing = Object.keys(s.sessions).find((id) => s.sessions[id]?.meta.state === "draft");
      if (existing) {
        return { activeSessionId: existing };
      }
      const draftId = `draft-${uuid()}`;
      const meta: SessionMetadata = {
        id: draftId,
        projectId,
        name: "New session",
        state: "draft",
        createdAt: new Date().toISOString(),
        worktree: true,
      };
      return {
        sessions: { ...s.sessions, [draftId]: emptySessionData(meta) },
        activeSessionId: draftId,
      };
    }),

  setDraftWorktree: (sessionId, worktree) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session || session.meta.state !== "draft") return s;
      return updateMeta(s, sessionId, { worktree });
    }),

  setDraftText: (sessionId, text) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session || session.meta.state !== "draft") return s;
      return updateSession(s, sessionId, { draftText: text });
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

      const isResult = event.type === "result";
      const isViewing = s.activeSessionId === sessionId;
      const patch: Partial<SessionData> = { turns };
      if (isResult) {
        patch.meta = { ...session.meta, state: "idle" };
        patch.hasUnseenCompletion = !isViewing;
        patch.rateLimit = null;
      }

      return updateSession(s, sessionId, patch);
    }),

  resetProject: () => set({ activeSessionId: null, historyLoading: new Set() }),
}));
