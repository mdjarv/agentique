/**
 * The wire — the landing page's event river.
 *
 * Entries are derived client-side from store transitions (use-wire-capture)
 * and persisted locally so a reload keeps recent history. This is a lossy,
 * per-browser feed by design; a server-side event aggregate can replace the
 * capture layer later without touching the render side.
 */
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

export type WireKind = "commit" | "tool" | "sched" | "brain" | "state" | "attn";

export interface WireEntry {
  id: string;
  /** Epoch ms. */
  at: number;
  kind: WireKind;
  sessionId?: string;
  /** The bolded subject (session or schedule name). */
  strong: string;
  /** The rest of the sentence. */
  rest: string;
  /** Optional trailing mono fragment (path, command, commit subject). */
  mono?: string;
}

const MAX_ENTRIES = 200;
/** Identical (session, kind, rest) within this window is a duplicate. */
const DEDUP_MS = 90_000;

interface WireState {
  entries: WireEntry[];
  add: (e: Omit<WireEntry, "id">) => void;
  /** Bulk insert for first-load seeding; skips ids already present. */
  seed: (entries: Omit<WireEntry, "id">[]) => void;
}

export const useWireStore = create<WireState>()(
  persist(
    (set, get) => ({
      entries: [],

      add: (e) => {
        const dup = get().entries.find(
          (x) =>
            x.sessionId === e.sessionId &&
            x.kind === e.kind &&
            x.rest === e.rest &&
            Math.abs(x.at - e.at) < DEDUP_MS,
        );
        if (dup) return;
        set((s) => ({
          entries: [{ ...e, id: crypto.randomUUID() }, ...s.entries].slice(0, MAX_ENTRIES),
        }));
      },

      seed: (entries) =>
        set((s) => {
          if (s.entries.length > 0) return s;
          return {
            entries: entries
              .map((e) => ({ ...e, id: crypto.randomUUID() }))
              .sort((a, b) => b.at - a.at)
              .slice(0, MAX_ENTRIES),
          };
        }),
    }),
    {
      name: "agentique:wire",
      version: 1,
      storage: createJSONStorage(() => localStorage),
    },
  ),
);
