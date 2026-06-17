import { create } from "zustand";
import {
  applyConsolidate,
  applyGlobalConsolidate,
  type ConsolidateReport,
  type ConsolidationJob,
  type ConsolidationPlan,
  type CreateMemoryInput,
  createMemory,
  deleteMemory,
  type GlobalConsolidationPlan,
  getConsolidationJob,
  getStatus,
  listMemories,
  type Memory,
  setLocked,
  setPinned,
  startGlobalPreview,
  startScopePreview,
  updateMemory,
} from "~/lib/brain-api";

interface ScopePreview {
  report: ConsolidateReport;
  plan: ConsolidationPlan;
}
interface GlobalPreview {
  report: ConsolidateReport;
  plan: GlobalConsolidationPlan;
}

interface BrainState {
  memories: Memory[];
  semantic: boolean;
  loaded: boolean;
  loading: boolean;

  // Consolidation runs as a background job on the server; progress and the final
  // result arrive over the WebSocket (via setJob), so these mirror that job and
  // are shared across tabs. preview/globalPreview hold the model's proposal once a
  // job finishes; the matching plan is posted back to apply.
  preview: ScopePreview | null;
  previewScope: string | null;
  previewing: boolean;
  applying: boolean;
  globalPreview: GlobalPreview | null;
  globalPreviewing: boolean;
  globalApplying: boolean;
  progress: { current: number; total: number } | null;

  // Bumped on any memory change (local or from another tab) to drive the nav
  // "flare". A counter, not a flag, so repeated changes each re-trigger it.
  flareSeq: number;

  load: () => Promise<void>;
  create: (input: CreateMemoryInput) => Promise<Memory>;
  update: (id: string, input: { text?: string; category?: string }) => Promise<Memory>;
  remove: (id: string) => Promise<void>;
  pin: (id: string, pinned: boolean) => Promise<void>;
  lock: (id: string, locked: boolean) => Promise<void>;

  startPreview: (scope: string, model: string) => Promise<void>;
  startGlobalConsolidate: (model: string) => Promise<void>;
  applyPreview: () => Promise<number>;
  applyGlobalPreview: () => Promise<number>;
  dismissPreview: () => void;
  dismissGlobalPreview: () => void;

  // Driven by WebSocket pushes (and an initial fetch).
  setJob: (job: ConsolidationJob | null) => void;
  hydrateJob: () => Promise<void>;
  onBrainUpdated: () => void;
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
  progress: null,
  flareSeq: 0,

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
    // Kick off the job; progress + result arrive via setJob over the WS.
    set({ previewScope: scope, previewing: true, preview: null, progress: null });
    const job = await startScopePreview(scope, model);
    get().setJob(job);
  },

  startGlobalConsolidate: async (model) => {
    set({ globalPreviewing: true, globalPreview: null, progress: null });
    const job = await startGlobalPreview(model);
    get().setJob(job);
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
      const memories = await listMemories();
      set({ memories, applying: false, preview: null, previewScope: null, progress: null });
      return changes;
    } catch (err) {
      set({ applying: false, preview: null, previewScope: null, progress: null });
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
      set({ memories, globalApplying: false, globalPreview: null, progress: null });
      return changes;
    } catch (err) {
      set({ globalApplying: false, globalPreview: null, progress: null });
      throw err;
    }
  },

  dismissPreview: () =>
    set({ preview: null, previewScope: null, previewing: false, progress: null }),
  dismissGlobalPreview: () => set({ globalPreview: null, globalPreviewing: false, progress: null }),

  setJob: (job) => {
    if (!job) return;
    const progress =
      job.phase === "running" && job.total > 0 ? { current: job.current, total: job.total } : null;
    if (job.kind === "scope") {
      set({
        previewScope: job.phase === "error" ? null : (job.scope ?? null),
        previewing: job.phase === "running",
        preview:
          job.phase === "done" && job.report && job.plan
            ? { report: job.report, plan: job.plan as ConsolidationPlan }
            : null,
        progress,
      });
    } else {
      set({
        globalPreviewing: job.phase === "running",
        globalPreview:
          job.phase === "done" && job.report && job.plan
            ? { report: job.report, plan: job.plan as GlobalConsolidationPlan }
            : null,
        progress,
      });
    }
  },

  hydrateJob: async () => {
    try {
      get().setJob(await getConsolidationJob());
    } catch (err) {
      console.error("Failed to hydrate consolidation job:", err);
    }
  },

  onBrainUpdated: () => {
    set((s) => ({ flareSeq: s.flareSeq + 1 }));
    // Keep the list fresh across tabs (a change may have come from elsewhere).
    listMemories()
      .then((memories) => set({ memories }))
      .catch((err) => console.error("Failed to refresh memories:", err));
  },
}));
