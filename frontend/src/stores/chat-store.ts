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

type SessionState = "disconnected" | "starting" | "idle" | "running" | "done" | "failed";

interface ChatState {
  sessionId: string | null;
  sessionState: SessionState;
  turns: Turn[];
  currentAssistantText: string;

  setSessionId: (id: string) => void;
  setSessionState: (state: SessionState) => void;
  startTurn: (prompt: string) => void;
  appendEvent: (event: ChatEvent) => void;
  completeTurn: () => void;
  reset: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  sessionId: null,
  sessionState: "disconnected",
  turns: [],
  currentAssistantText: "",

  setSessionId: (id) => set({ sessionId: id }),

  setSessionState: (state) => set({ sessionState: state }),

  startTurn: (prompt) =>
    set((s) => ({
      turns: [
        ...s.turns,
        {
          id: crypto.randomUUID(),
          prompt,
          events: [],
          complete: false,
        },
      ],
      currentAssistantText: "",
    })),

  appendEvent: (event) =>
    set((s) => {
      const turns = [...s.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) return s;

      const updatedTurn = {
        ...lastTurn,
        events: [...lastTurn.events, event],
      };
      turns[turns.length - 1] = updatedTurn;

      let currentAssistantText = s.currentAssistantText;
      if (event.type === "text" && event.content) {
        currentAssistantText += event.content;
      }

      return { turns, currentAssistantText };
    }),

  completeTurn: () =>
    set((s) => {
      const turns = [...s.turns];
      const lastTurn = turns[turns.length - 1];
      if (!lastTurn) return s;

      turns[turns.length - 1] = { ...lastTurn, complete: true };
      return { turns, sessionState: "idle" };
    }),

  reset: () =>
    set({
      sessionId: null,
      sessionState: "disconnected",
      turns: [],
      currentAssistantText: "",
    }),
}));
