import { create } from "zustand";

interface StreamingState {
  texts: Record<string, string>;
  toolInputs: Record<string, Record<string, string>>;

  appendText: (sessionId: string, text: string) => void;
  clearText: (sessionId: string) => void;
  appendToolInput: (sessionId: string, toolId: string, partialJson: string) => void;
  clearToolInput: (sessionId: string, toolId: string) => void;
  clearAllToolInputs: (sessionId: string) => void;
  /** Clear all streaming state for a session (text + tool inputs). Use on reconnect. */
  clearSession: (sessionId: string) => void;
  /** Reset all streaming state. Use on WS reconnect to drop orphaned data. */
  reset: () => void;
}

export const useStreamingStore = create<StreamingState>((set) => ({
  texts: {},
  toolInputs: {},

  appendText: (sessionId, text) =>
    set((s) => ({
      texts: { ...s.texts, [sessionId]: (s.texts[sessionId] ?? "") + text },
    })),

  clearText: (sessionId) =>
    set((s) => {
      if (!(sessionId in s.texts)) return s;
      const { [sessionId]: _, ...rest } = s.texts;
      return { texts: rest };
    }),

  appendToolInput: (sessionId, toolId, partialJson) =>
    set((s) => {
      const sessionInputs = s.toolInputs[sessionId] ?? {};
      return {
        toolInputs: {
          ...s.toolInputs,
          [sessionId]: {
            ...sessionInputs,
            [toolId]: (sessionInputs[toolId] ?? "") + partialJson,
          },
        },
      };
    }),

  clearToolInput: (sessionId, toolId) =>
    set((s) => {
      const sessionInputs = s.toolInputs[sessionId];
      if (!sessionInputs || !(toolId in sessionInputs)) return s;
      const { [toolId]: _, ...rest } = sessionInputs;
      const hasRemaining = Object.keys(rest).length > 0;
      return {
        toolInputs: {
          ...s.toolInputs,
          [sessionId]: hasRemaining ? rest : {},
        },
      };
    }),

  clearAllToolInputs: (sessionId) =>
    set((s) => {
      if (!(sessionId in s.toolInputs)) return s;
      const { [sessionId]: _inputs, ...restInputs } = s.toolInputs;
      return { toolInputs: restInputs };
    }),

  clearSession: (sessionId) =>
    set((s) => {
      const hasText = sessionId in s.texts;
      const hasTools = sessionId in s.toolInputs;
      if (!hasText && !hasTools) return s;
      const { [sessionId]: _t, ...restTexts } = s.texts;
      const { [sessionId]: _i, ...restInputs } = s.toolInputs;
      return {
        texts: hasText ? restTexts : s.texts,
        toolInputs: hasTools ? restInputs : s.toolInputs,
      };
    }),

  reset: () => set({ texts: {}, toolInputs: {} }),
}));
