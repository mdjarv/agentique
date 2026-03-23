import { create } from "zustand";

interface StreamingState {
  texts: Record<string, string>;
  appendText: (sessionId: string, text: string) => void;
  clearText: (sessionId: string) => void;
}

export const useStreamingStore = create<StreamingState>((set) => ({
  texts: {},

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
}));
