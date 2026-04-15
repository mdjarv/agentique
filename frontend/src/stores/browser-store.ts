import { create } from "zustand";

export interface ScreencastMetadata {
  offsetTop: number;
  pageScaleFactor: number;
  deviceWidth: number;
  deviceHeight: number;
  scrollOffsetX: number;
  scrollOffsetY: number;
}

export interface BrowserSession {
  launched: boolean;
  launching: boolean;
  url: string;
  zoom: number;
  /** Mutable ref — mutated directly by updateFrame to avoid React re-renders per frame. */
  frameRef: { current: string | null };
  metadata: ScreencastMetadata | null;
}

function createBrowserSession(): BrowserSession {
  return {
    launched: false,
    launching: false,
    url: "",
    zoom: 1.0,
    frameRef: { current: null },
    metadata: null,
  };
}

interface BrowserState {
  sessions: Record<string, BrowserSession>;

  setLaunching: (sessionId: string) => void;
  setLaunched: (sessionId: string) => void;
  setStopped: (sessionId: string) => void;
  updateFrame: (sessionId: string, data: string, metadata: ScreencastMetadata) => void;
  setUrl: (sessionId: string, url: string) => void;
  setZoom: (sessionId: string, zoom: number) => void;
}

export const useBrowserStore = create<BrowserState>((set, get) => ({
  sessions: {},

  setLaunching: (sessionId) =>
    set((s) => {
      const existing = s.sessions[sessionId];
      const base = existing ?? createBrowserSession();
      return {
        sessions: { ...s.sessions, [sessionId]: { ...base, launching: true } },
      };
    }),

  setLaunched: (sessionId) =>
    set((s) => {
      const existing = s.sessions[sessionId] ?? createBrowserSession();
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...existing, launched: true, launching: false },
        },
      };
    }),

  setStopped: (sessionId) =>
    set((s) => {
      const existing = s.sessions[sessionId];
      if (!existing) return s;
      existing.frameRef.current = null;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...existing,
            launched: false,
            launching: false,
            metadata: null,
            url: "",
          },
        },
      };
    }),

  updateFrame: (sessionId, data, metadata) => {
    const session = get().sessions[sessionId];
    if (!session) return;
    // Mutate ref directly — no re-render per frame.
    session.frameRef.current = data;
    // Only trigger state update when viewport dimensions change.
    const prev = session.metadata;
    if (
      !prev ||
      prev.deviceWidth !== metadata.deviceWidth ||
      prev.deviceHeight !== metadata.deviceHeight ||
      prev.pageScaleFactor !== metadata.pageScaleFactor
    ) {
      set((s) => {
        const existing = s.sessions[sessionId];
        if (!existing) return s;
        return {
          sessions: { ...s.sessions, [sessionId]: { ...existing, metadata } },
        };
      });
    }
  },

  setUrl: (sessionId, url) =>
    set((s) => {
      const existing = s.sessions[sessionId];
      if (!existing) return s;
      return {
        sessions: { ...s.sessions, [sessionId]: { ...existing, url } },
      };
    }),

  setZoom: (sessionId, zoom) =>
    set((s) => {
      const existing = s.sessions[sessionId];
      if (!existing) return s;
      const clamped = Math.round(Math.max(0.25, Math.min(3.0, zoom)) * 100) / 100;
      return {
        sessions: { ...s.sessions, [sessionId]: { ...existing, zoom: clamped } },
      };
    }),
}));
