import { create } from "zustand";
import {
  applyConsolidate,
  applyGlobalConsolidate,
  type BrainCounts,
  type ConsolidateMode,
  type ConsolidateReport,
  type ConsolidationJob,
  type ConsolidationPlan,
  type CreateMemoryInput,
  confirmMemory,
  createMemory,
  deleteMemory,
  type GlobalConsolidationPlan,
  type GraphData,
  getConsolidationJob,
  getGraph,
  getStatus,
  listMemories,
  type Memory,
  restoreMemory,
  setLocked,
  setPinned,
  startConsolidateAll,
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
  // Brain-health distribution from the status endpoint (F6); null until first load. Refreshed
  // on brain.updated so the health strip tracks the live corpus.
  counts: BrainCounts | null;
  loaded: boolean;
  loading: boolean;

  // The force-graph payload (centrality + insight report). Loaded on demand when the
  // graph view is shown; null until then. A rebuildable, request-time index.
  graph: GraphData | null;
  graphLoading: boolean;

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
  // Bulk "Consolidate all": one job consolidates every scope and auto-applies each.
  consolidatingAll: boolean;
  progress: { current: number; total: number } | null;

  // Bumped on any memory change (local or from another tab) to drive the nav
  // "flare". A counter, not a flag, so repeated changes each re-trigger it.
  flareSeq: number;

  load: () => Promise<void>;
  loadGraph: () => Promise<void>;
  create: (input: CreateMemoryInput) => Promise<Memory>;
  update: (id: string, input: { text?: string; category?: string }) => Promise<Memory>;
  remove: (id: string) => Promise<void>;
  pin: (id: string, pinned: boolean) => Promise<void>;
  lock: (id: string, locked: boolean) => Promise<void>;
  confirm: (id: string) => Promise<void>;
  // restore un-archives a cold fact back into the live set (F3).
  restore: (id: string) => Promise<void>;

  startPreview: (
    scope: string,
    model: string,
    mode?: ConsolidateMode,
    force?: boolean,
  ) => Promise<void>;
  startGlobalConsolidate: (model: string) => Promise<void>;
  startConsolidateAll: (model: string) => Promise<void>;
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
  counts: null,
  loaded: false,
  loading: false,
  graph: null,
  graphLoading: false,
  preview: null,
  previewScope: null,
  previewing: false,
  applying: false,
  globalPreview: null,
  globalPreviewing: false,
  globalApplying: false,
  consolidatingAll: false,
  progress: null,
  flareSeq: 0,

  load: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const [memories, status] = await Promise.all([listMemories(), getStatus()]);
      set({
        memories,
        semantic: status.semantic,
        counts: status.counts ?? null,
        loaded: true,
        loading: false,
      });
    } catch (err) {
      console.error("Failed to load brain:", err);
      set({ loading: false });
    }
  },

  loadGraph: async () => {
    if (get().graphLoading) return;
    set({ graphLoading: true });
    try {
      set({ graph: await getGraph(), graphLoading: false });
    } catch (err) {
      console.error("Failed to load brain graph:", err);
      set({ graphLoading: false });
    }
  },

  create: async (input) => {
    const m = await createMemory(input);
    set((s) => ({ memories: upsert(s.memories, m) }));
    return m;
  },

  confirm: async (id) => {
    const m = await confirmMemory(id);
    set((s) => ({ memories: upsert(s.memories, m) }));
    // The confirm queue/report just changed; refresh the graph if it's open.
    if (get().graph) void get().loadGraph();
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

  restore: async (id) => {
    const m = await restoreMemory(id);
    set((s) => ({ memories: upsert(s.memories, m) }));
  },

  startPreview: async (scope, model, mode = "conservative", force = false) => {
    // Kick off the job; progress + result arrive via setJob over the WS.
    set({ previewScope: scope, previewing: true, preview: null, progress: null });
    const job = await startScopePreview(scope, model, mode, force);
    get().setJob(job);
  },

  startGlobalConsolidate: async (model) => {
    set({ globalPreviewing: true, globalPreview: null, progress: null });
    const job = await startGlobalPreview(model);
    get().setJob(job);
  },

  startConsolidateAll: async (model) => {
    set({ consolidatingAll: true, progress: null });
    const job = await startConsolidateAll(model);
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
    if (!job) {
      // The server has no active/recent job (e.g. it restarted mid-preview). Clear
      // any local "running" spinner so it doesn't hang forever; a finished preview
      // the client still holds stays applyable (apply re-checks staleness).
      set((s) => ({
        previewing: false,
        globalPreviewing: false,
        consolidatingAll: false,
        progress: null,
        previewScope: s.preview ? s.previewScope : null,
      }));
      return;
    }
    const progress =
      job.phase === "running" && job.total > 0 ? { current: job.current, total: job.total } : null;
    if (job.kind === "all") {
      set({ consolidatingAll: job.phase === "running", progress });
      return;
    }
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
    // Coalesce bursts (e.g. scheduled consolidation broadcasts once per scope) into a single
    // list refresh so we don't fire N fetches back-to-back.
    scheduleRefresh();
  },
}));

let refreshTimer: ReturnType<typeof setTimeout> | null = null;
function scheduleRefresh() {
  if (refreshTimer) return;
  refreshTimer = setTimeout(() => {
    refreshTimer = null;
    // Refresh the memory list AND the health counts together so the strip stays in step with
    // the corpus (both are cheap, request-time aggregations).
    Promise.all([listMemories(), getStatus()])
      .then(([memories, status]) =>
        useBrainStore.setState({
          memories,
          semantic: status.semantic,
          counts: status.counts ?? null,
        }),
      )
      .catch((err) => console.error("Failed to refresh brain:", err));
  }, 600);
}
