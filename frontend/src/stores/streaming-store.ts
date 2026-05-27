import { create } from "zustand";

interface ToolProgress {
  elapsedMs: number;
  toolName?: string;
}

interface StreamingState {
  texts: Record<string, string>;
  toolInputs: Record<string, Record<string, string>>;
  toolOutputs: Record<string, Record<string, string>>;
  toolProgress: Record<string, Record<string, ToolProgress>>;
  reasoningDeltas: Record<string, Record<string, string>>;

  appendText: (sessionId: string, text: string) => void;
  clearText: (sessionId: string) => void;
  appendToolInput: (sessionId: string, toolId: string, partialJson: string) => void;
  clearToolInput: (sessionId: string, toolId: string) => void;
  clearAllToolInputs: (sessionId: string) => void;
  appendToolOutput: (sessionId: string, toolId: string, delta: string) => void;
  clearToolOutput: (sessionId: string, toolId: string) => void;
  setToolProgress: (sessionId: string, toolId: string, progress: ToolProgress) => void;
  clearToolProgress: (sessionId: string, toolId: string) => void;
  appendReasoning: (sessionId: string, itemId: string, delta: string) => void;
  clearReasoning: (sessionId: string, itemId: string) => void;
  clearAllReasoning: (sessionId: string) => void;
  /** Clear all streaming state for a session (text + tool inputs). Use on reconnect. */
  clearSession: (sessionId: string) => void;
  /** Reset all streaming state. Use on WS reconnect to drop orphaned data. */
  reset: () => void;
}

export const useStreamingStore = create<StreamingState>((set) => ({
  texts: {},
  toolInputs: {},
  toolOutputs: {},
  toolProgress: {},
  reasoningDeltas: {},

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

  appendToolOutput: (sessionId, toolId, delta) =>
    set((s) => {
      const sessionOutputs = s.toolOutputs[sessionId] ?? {};
      return {
        toolOutputs: {
          ...s.toolOutputs,
          [sessionId]: {
            ...sessionOutputs,
            [toolId]: (sessionOutputs[toolId] ?? "") + delta,
          },
        },
      };
    }),

  clearToolOutput: (sessionId, toolId) =>
    set((s) => {
      const sessionOutputs = s.toolOutputs[sessionId];
      if (!sessionOutputs || !(toolId in sessionOutputs)) return s;
      const { [toolId]: _, ...rest } = sessionOutputs;
      return {
        toolOutputs: {
          ...s.toolOutputs,
          [sessionId]: Object.keys(rest).length > 0 ? rest : {},
        },
      };
    }),

  setToolProgress: (sessionId, toolId, progress) =>
    set((s) => {
      const sessionProgress = s.toolProgress[sessionId] ?? {};
      return {
        toolProgress: {
          ...s.toolProgress,
          [sessionId]: { ...sessionProgress, [toolId]: progress },
        },
      };
    }),

  clearToolProgress: (sessionId, toolId) =>
    set((s) => {
      const sessionProgress = s.toolProgress[sessionId];
      if (!sessionProgress || !(toolId in sessionProgress)) return s;
      const { [toolId]: _, ...rest } = sessionProgress;
      return {
        toolProgress: {
          ...s.toolProgress,
          [sessionId]: Object.keys(rest).length > 0 ? rest : {},
        },
      };
    }),

  appendReasoning: (sessionId, itemId, delta) =>
    set((s) => {
      const sessionReasoning = s.reasoningDeltas[sessionId] ?? {};
      return {
        reasoningDeltas: {
          ...s.reasoningDeltas,
          [sessionId]: {
            ...sessionReasoning,
            [itemId]: (sessionReasoning[itemId] ?? "") + delta,
          },
        },
      };
    }),

  clearReasoning: (sessionId, itemId) =>
    set((s) => {
      const sessionReasoning = s.reasoningDeltas[sessionId];
      if (!sessionReasoning || !(itemId in sessionReasoning)) return s;
      const { [itemId]: _, ...rest } = sessionReasoning;
      return {
        reasoningDeltas: {
          ...s.reasoningDeltas,
          [sessionId]: Object.keys(rest).length > 0 ? rest : {},
        },
      };
    }),

  clearAllReasoning: (sessionId) =>
    set((s) => {
      if (!(sessionId in s.reasoningDeltas)) return s;
      const { [sessionId]: _, ...rest } = s.reasoningDeltas;
      return { reasoningDeltas: rest };
    }),

  clearSession: (sessionId) =>
    set((s) => {
      const hasText = sessionId in s.texts;
      const hasInputs = sessionId in s.toolInputs;
      const hasOutputs = sessionId in s.toolOutputs;
      const hasProgress = sessionId in s.toolProgress;
      const hasReasoning = sessionId in s.reasoningDeltas;
      if (!hasText && !hasInputs && !hasOutputs && !hasProgress && !hasReasoning) return s;
      const { [sessionId]: _t, ...restTexts } = s.texts;
      const { [sessionId]: _i, ...restInputs } = s.toolInputs;
      const { [sessionId]: _o, ...restOutputs } = s.toolOutputs;
      const { [sessionId]: _p, ...restProgress } = s.toolProgress;
      const { [sessionId]: _r, ...restReasoning } = s.reasoningDeltas;
      return {
        texts: hasText ? restTexts : s.texts,
        toolInputs: hasInputs ? restInputs : s.toolInputs,
        toolOutputs: hasOutputs ? restOutputs : s.toolOutputs,
        toolProgress: hasProgress ? restProgress : s.toolProgress,
        reasoningDeltas: hasReasoning ? restReasoning : s.reasoningDeltas,
      };
    }),

  reset: () =>
    set({ texts: {}, toolInputs: {}, toolOutputs: {}, toolProgress: {}, reasoningDeltas: {} }),
}));
