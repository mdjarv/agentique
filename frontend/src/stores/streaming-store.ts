import { create } from "zustand";

interface StreamingState {
  texts: Record<string, string>;
  // content_block index -> toolId mapping per session
  activeToolBlocks: Record<string, Record<number, string>>;
  // Accumulated partial JSON per tool per session
  toolInputs: Record<string, Record<string, string>>;

  appendText: (sessionId: string, text: string) => void;
  clearText: (sessionId: string) => void;
  startToolBlock: (sessionId: string, index: number, toolId: string) => void;
  appendToolInput: (sessionId: string, index: number, partialJson: string) => void;
  clearToolInput: (sessionId: string, toolId: string) => void;
  clearAllToolInputs: (sessionId: string) => void;
}

export const useStreamingStore = create<StreamingState>((set) => ({
  texts: {},
  activeToolBlocks: {},
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

  startToolBlock: (sessionId, index, toolId) =>
    set((s) => ({
      activeToolBlocks: {
        ...s.activeToolBlocks,
        [sessionId]: { ...s.activeToolBlocks[sessionId], [index]: toolId },
      },
    })),

  appendToolInput: (sessionId, index, partialJson) =>
    set((s) => {
      const toolId = s.activeToolBlocks[sessionId]?.[index];
      if (!toolId) return s;
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
      if (!(sessionId in s.toolInputs) && !(sessionId in s.activeToolBlocks)) return s;
      const { [sessionId]: _inputs, ...restInputs } = s.toolInputs;
      const { [sessionId]: _blocks, ...restBlocks } = s.activeToolBlocks;
      return {
        toolInputs: restInputs,
        activeToolBlocks: restBlocks,
      };
    }),
}));
