import { create } from "zustand";
import { uuid } from "~/lib/utils";

export interface ChatEvent {
  id: string;
  type: "text" | "thinking" | "tool_use" | "tool_result" | "result" | "error";
  content?: string;
  toolId?: string;
  toolName?: string;
  toolInput?: unknown;
  cost?: number;
  duration?: number;
  usage?: { inputTokens: number; outputTokens: number };
  stopReason?: string;
  fatal?: boolean;
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
  name: string;
  state: SessionState;
  worktreePath?: string;
  worktreeBranch?: string;
  createdAt: string;
  worktree?: boolean; // draft-only: user's worktree toggle preference
}

export interface SessionData {
  meta: SessionMetadata;
  turns: Turn[];
  currentAssistantText: string;
  hasUnseenCompletion: boolean;
}

interface ChatState {
  sessions: Record<string, SessionData>;
  activeSessionId: string | null;

  // Session management
  setSessions: (sessions: SessionMetadata[]) => void;
  addSession: (meta: SessionMetadata) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string | null) => void;
  setSessionState: (sessionId: string, state: SessionState) => void;
  setSessionName: (sessionId: string, name: string) => void;

  // Draft session management
  createDraft: (projectId: string) => void;
  setDraftWorktree: (sessionId: string, worktree: boolean) => void;

  // History
  setSessionHistory: (sessionId: string, turns: Turn[]) => void;

  // Turn/event management
  submitQuery: (sessionId: string, prompt: string, attachments?: Attachment[]) => void;
  handleServerEvent: (sessionId: string, event: ChatEvent) => void;

  // Draft → real session promotion (atomic)
  promoteDraft: (draftId: string, realSession: SessionMetadata) => void;

  // Project-level reset
  resetProject: () => void;
}

// Use these with useChatStore((s) => ...) for direct access.
// Avoid derived selectors that create new references.

export const useChatStore = create<ChatState>((set) => ({
  sessions: {},
  activeSessionId: null,

  setSessions: (metas) =>
    set((s) => {
      const sessions = { ...s.sessions };
      for (const meta of metas) {
        if (sessions[meta.id]) {
          const existing = sessions[meta.id] as SessionData;
          sessions[meta.id] = {
            meta,
            turns: existing.turns,
            currentAssistantText: existing.currentAssistantText,
            hasUnseenCompletion: existing.hasUnseenCompletion,
          };
        } else {
          sessions[meta.id] = {
            meta,
            turns: [],
            currentAssistantText: "",
            hasUnseenCompletion: false,
          };
        }
      }
      return { sessions };
    }),

  addSession: (meta) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [meta.id]: { meta, turns: [], currentAssistantText: "", hasUnseenCompletion: false },
      },
    })),

  removeSession: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.sessions;
      const activeSessionId = s.activeSessionId === id ? null : s.activeSessionId;
      return { sessions: rest, activeSessionId };
    }),

  setActiveSessionId: (id) =>
    set((s) => {
      if (id && s.sessions[id]) {
        return {
          activeSessionId: id,
          sessions: {
            ...s.sessions,
            [id]: { ...(s.sessions[id] as SessionData), hasUnseenCompletion: false },
          },
        };
      }
      return { activeSessionId: id };
    }),

  setSessionState: (sessionId, state) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            meta: { ...session.meta, state },
          },
        },
      };
    }),

  setSessionName: (sessionId, name) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            meta: { ...session.meta, name },
          },
        },
      };
    }),

  setSessionHistory: (sessionId, turns) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            turns,
            currentAssistantText: "",
          },
        },
      };
    }),

  createDraft: () =>
    set((s) => {
      const draftId = `draft-${uuid()}`;
      const meta: SessionMetadata = {
        id: draftId,
        name: "New session",
        state: "draft",
        createdAt: new Date().toISOString(),
        worktree: false,
      };
      return {
        sessions: {
          ...s.sessions,
          [draftId]: { meta, turns: [], currentAssistantText: "", hasUnseenCompletion: false },
        },
        activeSessionId: draftId,
      };
    }),

  setDraftWorktree: (sessionId, worktree) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session || session.meta.state !== "draft") return s;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            meta: { ...session.meta, worktree },
          },
        },
      };
    }),

  submitQuery: (sessionId, prompt, attachments) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            turns: [
              ...session.turns,
              {
                id: uuid(),
                prompt,
                attachments: attachments ?? [],
                events: [],
                complete: false,
              },
            ],
            currentAssistantText: "",
          },
        },
      };
    }),

  handleServerEvent: (sessionId, event) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        console.warn("handleServerEvent: unknown session", sessionId);
        return s;
      }
      const turns = [...session.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) {
        console.warn("handleServerEvent: no turns for session", sessionId);
        return s;
      }

      const updatedTurn = {
        ...lastTurn,
        events: [...lastTurn.events, event],
        complete: lastTurn.complete || event.type === "result",
      };
      turns[turns.length - 1] = updatedTurn;

      let currentAssistantText = session.currentAssistantText;
      if (event.type === "text" && event.content) {
        currentAssistantText += event.content;
      }

      const isResult = event.type === "result";
      const isViewing = s.activeSessionId === sessionId;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            turns,
            currentAssistantText,
            meta: isResult ? { ...session.meta, state: "idle" } : session.meta,
            hasUnseenCompletion: isResult ? !isViewing : session.hasUnseenCompletion,
          },
        },
      };
    }),

  promoteDraft: (draftId, realSession) =>
    set((s) => {
      const { [draftId]: _, ...rest } = s.sessions;
      return {
        sessions: {
          ...rest,
          [realSession.id]: {
            meta: realSession,
            turns: [],
            currentAssistantText: "",
            hasUnseenCompletion: false,
          },
        },
        activeSessionId: realSession.id,
      };
    }),

  resetProject: () => set({ sessions: {}, activeSessionId: null }),
}));
