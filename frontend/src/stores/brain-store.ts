import { create } from "zustand";
import {
  applyConsolidate,
  applyGlobalConsolidate,
  type CreateMemoryInput,
  createMemory,
  deleteMemory,
  type GlobalPreviewResult,
  getStatus,
  listMemories,
  type Memory,
  type PreviewResult,
  previewConsolidate,
  previewGlobalConsolidate,
  setLocked,
  setPinned,
  updateMemory,
} from "~/lib/brain-api";

interface BrainState {
  memories: Memory[];
  semantic: boolean;
  loaded: boolean;
  loading: boolean;

  // A single in-flight consolidation preview (one scope at a time): the model's
  // proposal, awaiting Apply or Dismiss. The model runs at preview; apply replays
  // the held plan with no further model call.
  preview: PreviewResult | null;
  previewScope: string | null;
  previewing: boolean;
  applying: boolean;

  load: () => Promise<void>;
  create: (input: CreateMemoryInput) => Promise<Memory>;
  update: (id: string, input: { text?: string; category?: string }) => Promise<Memory>;
  remove: (id: string) => Promise<void>;
  pin: (id: string, pinned: boolean) => Promise<void>;
  lock: (id: string, locked: boolean) => Promise<void>;

  startPreview: (scope: string, model: string) => Promise<void>;
  applyPreview: () => Promise<number>;
  dismissPreview: () => void;

  // Cross-scope "Consolidate Global": scans all projects and promotes cross-cutting
  // facts to global. Separate from the per-scope preview above.
  globalPreview: GlobalPreviewResult | null;
  globalPreviewing: boolean;
  globalApplying: boolean;
  startGlobalPreview: (model: string) => Promise<void>;
  applyGlobalPreview: () => Promise<number>;
  dismissGlobalPreview: () => void;
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
  preview: null,
  previewScope: null,
  previewing: false,
  applying: false,
  globalPreview: null,
  globalPreviewing: false,
  globalApplying: false,

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

  startPreview: async (scope, model) => {
    set({ previewing: true, preview: null, previewScope: scope });
    try {
      const result = await previewConsolidate(scope, model);
      set({ preview: result, previewScope: scope, previewing: false });
    } catch (err) {
      set({ previewing: false, preview: null, previewScope: null });
      throw err;
    }
  },

  applyPreview: async () => {
    const { preview } = get();
    if (!preview) return 0;
    const r = preview.report;
    const changes =
      (r.promoted?.length ?? 0) +
      (r.rewritten?.length ?? 0) +
      (r.abstracted?.length ?? 0) +
      (r.deleted?.length ?? 0) +
      (r.decayed?.length ?? 0);
    set({ applying: true });
    try {
      await applyConsolidate(preview.plan);
      // Reload to reflect promotions/merges/decay.
      const memories = await listMemories();
      set({ memories, applying: false, preview: null, previewScope: null });
      return changes;
    } catch (err) {
      // Clear the (now likely stale) preview so the user re-previews.
      set({ applying: false, preview: null, previewScope: null });
      throw err;
    }
  },

  dismissPreview: () => set({ preview: null, previewScope: null }),

  startGlobalPreview: async (model) => {
    set({ globalPreviewing: true, globalPreview: null });
    try {
      const result = await previewGlobalConsolidate(model);
      set({ globalPreview: result, globalPreviewing: false });
    } catch (err) {
      set({ globalPreviewing: false, globalPreview: null });
      throw err;
    }
  },

  applyGlobalPreview: async () => {
    const { globalPreview } = get();
    if (!globalPreview) return 0;
    const r = globalPreview.report;
    const changes = (r.promoted?.length ?? 0) + (r.deleted?.length ?? 0);
    set({ globalApplying: true });
    try {
      await applyGlobalConsolidate(globalPreview.plan);
      const memories = await listMemories();
      set({ memories, globalApplying: false, globalPreview: null });
      return changes;
    } catch (err) {
      set({ globalApplying: false, globalPreview: null });
      throw err;
    }
  },

  dismissGlobalPreview: () => set({ globalPreview: null }),
}));
