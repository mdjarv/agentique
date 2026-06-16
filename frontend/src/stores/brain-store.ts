import { create } from "zustand";
import {
  consolidate as apiConsolidate,
  type CreateMemoryInput,
  createMemory,
  deleteMemory,
  getStatus,
  listMemories,
  type Memory,
  setLocked,
  setPinned,
  updateMemory,
} from "~/lib/brain-api";

interface BrainState {
  memories: Memory[];
  semantic: boolean;
  loaded: boolean;
  loading: boolean;

  load: () => Promise<void>;
  create: (input: CreateMemoryInput) => Promise<Memory>;
  update: (id: string, input: { text?: string; category?: string }) => Promise<Memory>;
  remove: (id: string) => Promise<void>;
  pin: (id: string, pinned: boolean) => Promise<void>;
  lock: (id: string, locked: boolean) => Promise<void>;
  consolidate: (scope: string) => Promise<void>;
}

// upsert replaces a memory by id or appends it, preserving a stable array
// reference contract (always a fresh array, never a mutated one).
function upsert(list: Memory[], m: Memory): Memory[] {
  const idx = list.findIndex((x) => x.id === m.id);
  if (idx === -1) return [...list, m];
  const next = list.slice();
  next[idx] = m;
  return next;
}

export const useBrainStore = create<BrainState>((set, get) => ({
  memories: [],
  semantic: false,
  loaded: false,
  loading: false,

  load: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const [memories, status] = await Promise.all([listMemories(), getStatus()]);
      set({ memories, semantic: status.semantic, loaded: true, loading: false });
    } catch (err) {
      console.error("Failed to load brain:", err);
      set({ loading: false });
    }
  },

  create: async (input) => {
    const m = await createMemory(input);
    set((s) => ({ memories: upsert(s.memories, m) }));
    return m;
  },

  update: async (id, input) => {
    const m = await updateMemory(id, input);
    set((s) => ({ memories: upsert(s.memories, m) }));
    return m;
  },

  remove: async (id) => {
    await deleteMemory(id);
    set((s) => ({ memories: s.memories.filter((m) => m.id !== id) }));
  },

  pin: async (id, pinned) => {
    const m = await setPinned(id, pinned);
    set((s) => ({ memories: upsert(s.memories, m) }));
  },

  lock: async (id, locked) => {
    const m = await setLocked(id, locked);
    set((s) => ({ memories: upsert(s.memories, m) }));
  },

  consolidate: async (scope) => {
    await apiConsolidate(scope);
    // Reload to reflect promotions/merges/decay.
    const memories = await listMemories();
    set({ memories });
  },
}));
