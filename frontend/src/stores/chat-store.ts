import { create } from "zustand";

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

export interface Turn {
  id: string;
  prompt: string;
  events: ChatEvent[];
  complete: boolean;
}

export type SessionState =
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
}

export interface SessionData {
  meta: SessionMetadata;
  turns: Turn[];
  currentAssistantText: string;
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

  // Turn/event management (now takes sessionId)
  startTurn: (sessionId: string, prompt: string) => void;
  appendEvent: (sessionId: string, event: ChatEvent) => void;
  completeTurn: (sessionId: string) => void;

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
          };
        } else {
          sessions[meta.id] = { meta, turns: [], currentAssistantText: "" };
        }
      }
      return { sessions };
    }),

  addSession: (meta) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [meta.id]: { meta, turns: [], currentAssistantText: "" },
      },
    })),

  removeSession: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.sessions;
      const activeSessionId = s.activeSessionId === id ? null : s.activeSessionId;
      return { sessions: rest, activeSessionId };
    }),

  setActiveSessionId: (id) => set({ activeSessionId: id }),

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

  startTurn: (sessionId, prompt) =>
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
                id: crypto.randomUUID(),
                prompt,
                events: [],
                complete: false,
              },
            ],
            currentAssistantText: "",
          },
        },
      };
    }),

  appendEvent: (sessionId, event) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const turns = [...session.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) return s;

      const updatedTurn = {
        ...lastTurn,
        events: [...lastTurn.events, event],
      };
      turns[turns.length - 1] = updatedTurn;

      let currentAssistantText = session.currentAssistantText;
      if (event.type === "text" && event.content) {
        currentAssistantText += event.content;
      }

      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, turns, currentAssistantText },
        },
      };
    }),

  completeTurn: (sessionId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const turns = [...session.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) return s;

      turns[turns.length - 1] = { ...lastTurn, complete: true };
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            turns,
            meta: { ...session.meta, state: "idle" },
          },
        },
      };
    }),

  resetProject: () => set({ sessions: {}, activeSessionId: null }),
}));
