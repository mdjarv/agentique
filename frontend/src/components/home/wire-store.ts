/**
 * The wire — the landing page's event river.
 *
 * The durable memory is the backend (`wire.list` backfill + the
 * `project.activity-item` push); this store is a per-load in-memory merge of
 * that feed with locally derived entries (schedule runs, brain flares,
 * commits, fine-resolution pulses for the active session). Backend-sourced
 * entries carry stable ids so backfill and live pushes converge.
 */
import { create } from "zustand";

export type WireKind = "commit" | "tool" | "sched" | "brain" | "state" | "attn" | "fail";

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

const MAX_ENTRIES = 300;
/** Identical (session, kind, rest) within this window is a duplicate. */
const DEDUP_MS = 90_000;

interface WireState {
  entries: WireEntry[];
  /** Append one entry; pass a stable id for backend-sourced items. */
  add: (e: Omit<WireEntry, "id"> & { id?: string }) => void;
  /** Merge a backfill page by id, keeping newest-first order. */
  backfill: (entries: WireEntry[]) => void;
}

export const useWireStore = create<WireState>()((set, get) => ({
  entries: [],

  add: (e) => {
    const existing = get().entries;
    if (e.id && existing.some((x) => x.id === e.id)) return;
    if (!e.id) {
      const dup = existing.find(
        (x) =>
          x.sessionId === e.sessionId &&
          x.kind === e.kind &&
          x.rest === e.rest &&
          Math.abs(x.at - e.at) < DEDUP_MS,
      );
      if (dup) return;
    }
    const entry: WireEntry = { ...e, id: e.id ?? crypto.randomUUID() };
    set((s) => ({ entries: [entry, ...s.entries].slice(0, MAX_ENTRIES) }));
  },

  backfill: (entries) =>
    set((s) => {
      const seen = new Set(s.entries.map((e) => e.id));
      const fresh = entries.filter((e) => !seen.has(e.id));
      if (fresh.length === 0) return s;
      return {
        entries: [...s.entries, ...fresh].sort((a, b) => b.at - a.at).slice(0, MAX_ENTRIES),
      };
    }),
}));
