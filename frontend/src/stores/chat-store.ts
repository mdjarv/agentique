import { create } from "zustand";
import { extractContextUsageFromTurns, extractTodosFromTurns } from "~/lib/event-extractors";
import { uuid } from "~/lib/utils";
import { type ApplyResult, applyServerEvent } from "~/stores/apply-event";
import type {
  AutoApproveMode,
  ChatEvent,
  PendingApproval,
  PendingQuestion,
  SessionData,
  SessionMetadata,
  SessionState,
  Turn,
} from "~/stores/chat-types";
import { useRateLimitStore } from "~/stores/rate-limit-store";

// Re-export all types from chat-types so existing consumers don't break.
export type {
  AgentMessageEvent,
  Attachment,
  AutoApproveMode,
  ChatEvent,
  ChatEventType,
  CompactBoundaryEvent,
  CompactStatusEvent,
  ContextManagementEvent,
  ContextUsage,
  ErrorEvent,
  MessageDeliveryEvent,
  PendingApproval,
  PendingQuestion,
  Question,
  QuestionOption,
  RateLimitEvent,
  ResultEvent,
  SessionData,
  SessionMetadata,
  SessionState,
  StreamEvent,
  TaskEvent,
  // Discriminated union variants
  TextEvent,
  ThinkingEvent,
  TodoItem,
  ToolContentBlock,
  ToolResultEvent,
  ToolUseEvent,
  Turn,
  UserMessageEvent,
} from "~/stores/chat-types";

// --- Pending state buffer ---
// Buffers session.state updates that arrive before session.created.
type StateExtras = Partial<
  Pick<
    SessionMetadata,
    | "connected"
    | "hasDirtyWorktree"
    | "worktreeMerged"
    | "archivedAt"
    | "hasUncommitted"
    | "commitsAhead"
    | "commitsBehind"
    | "branchMissing"
    | "mergeStatus"
    | "mergeConflictFiles"
    | "gitOperation"
    | "gitVersion"
    | "worktreeBranch"
    | "worktreePath"
  >
>;
interface PendingStateEntry {
  state: SessionState;
  extras?: StateExtras;
}
const pendingStateUpdates = new Map<string, PendingStateEntry>();

// Exported for tests.
export function _clearPendingStateUpdates(): void {
  pendingStateUpdates.clear();
}

/** When evicting turns from an inactive session, keep this many recent turns
 *  so switching back renders instantly while the full history backfills. */
const TAIL_TURN_COUNT = 20;

const emptySessionData = (meta: SessionMetadata): SessionData => ({
  meta,
  turns: [],
  streamingEvents: [],
  historyComplete: false,
  hasUnseenCompletion: false,
  hasUnreadChannelMessage: false,
  pendingApproval: null,
  pendingQuestion: null,
  planMode: meta.permissionMode === "plan",
  autoApproveMode: (meta.autoApproveMode as AutoApproveMode) ?? "manual",
  todos: null,
  contextUsage: null,
  compacting: false,
});

/**
 * True when `prompt` is the turn a queued message became. The backend coalesces
 * a whole queued batch into one prompt joined by blank lines (coalescePending),
 * so an exact match only covers the single-message case; the boundary checks
 * cover a batch without letting an arbitrary substring count as a match.
 */
function promptCarriesMessage(prompt: string, content: string): boolean {
  if (!content) return false;
  return (
    prompt === content ||
    prompt.startsWith(`${content}\n\n`) ||
    prompt.endsWith(`\n\n${content}`) ||
    prompt.includes(`\n\n${content}\n\n`)
  );
}

function evictTurns(session: SessionData): Partial<SessionData> {
  if (session.turns.length <= TAIL_TURN_COUNT) return {};
  return {
    turns: session.turns.slice(-TAIL_TURN_COUNT),
    streamingEvents: [],
    historyComplete: false,
  };
}

// --- Immutable update helpers ---

function updateSession(
  s: ChatState,
  sessionId: string,
  patch: Partial<SessionData>,
): Partial<ChatState> {
  const session = s.sessions[sessionId];
  if (!session) return s;
  return {
    sessions: {
      ...s.sessions,
      [sessionId]: { ...session, ...patch },
    },
  };
}

function updateMeta(
  s: ChatState,
  sessionId: string,
  metaPatch: Partial<SessionMetadata>,
): Partial<ChatState> {
  const session = s.sessions[sessionId];
  if (!session) return s;
  return {
    sessions: {
      ...s.sessions,
      [sessionId]: { ...session, meta: { ...session.meta, ...metaPatch } },
    },
  };
}

// --- Store ---

export interface ChatState {
  sessions: Record<string, SessionData>;
  activeSessionId: string | null;
  loadedProjects: Set<string>;
  historyLoading: Set<string>;

  // Session management
  setSessions: (sessions: SessionMetadata[], projectId: string, authoritative?: boolean) => void;
  addSession: (meta: SessionMetadata) => void;
  removeSession: (id: string) => void;
  /** Freeze sessions whose machine went away — see `markSessionsAway`. */
  markSessionsAway: (sessionIds: string[]) => void;
  setActiveSessionId: (id: string | null) => void;
  setSessionState: (sessionId: string, state: SessionState, extras?: StateExtras) => void;
  flushPendingState: (sessionId: string) => void;
  setSessionName: (sessionId: string, name: string) => void;
  setSessionPinned: (sessionId: string, pinned: boolean, pinOrder: number) => void;
  setSessionModel: (sessionId: string, model: string) => void;
  setSessionResolvedModel: (sessionId: string, resolvedModel: string) => void;
  setPendingApproval: (sessionId: string, approval: PendingApproval) => void;
  clearPendingApproval: (sessionId: string) => void;
  setPendingQuestion: (sessionId: string, question: PendingQuestion) => void;
  clearPendingQuestion: (sessionId: string) => void;
  setSessionPlanMode: (sessionId: string, planMode: boolean) => void;
  setSessionAutoApproveMode: (sessionId: string, mode: AutoApproveMode) => void;
  setSessionPrUrl: (sessionId: string, prUrl: string) => void;
  setSessionIcon: (sessionId: string, icon: string | undefined) => void;
  addSessionChannel: (sessionId: string, channelId: string, role?: string) => void;
  removeSessionChannel: (sessionId: string, channelId: string) => void;
  setUnreadChannelMessage: (sessionId: string, value: boolean) => void;
  updateStreamingContextUsage: (
    sessionId: string,
    patch: { inputTokens?: number; outputTokens?: number },
  ) => void;

  // History
  setHistoryLoading: (sessionId: string, loading: boolean) => void;
  setSessionHistory: (sessionId: string, turns: Turn[], complete?: boolean) => void;

  // Turn/event management
  /** Appends the turn and returns its id, which rollbackOptimisticTurn takes. */
  submitQuery: (
    sessionId: string,
    prompt: string,
    attachments?: import("~/stores/chat-types").Attachment[],
    meta?: {
      turnIndex?: number;
      origin?: import("~/lib/generated-types").QueryOrigin;
    },
  ) => string;
  rollbackOptimisticTurn: (sessionId: string, turnId: string) => void;
  adoptTurnPrompt: (
    sessionId: string,
    prompt: string,
    meta?: {
      turnIndex?: number;
      origin?: import("~/lib/generated-types").QueryOrigin;
    },
  ) => void;
  handleServerEvent: (sessionId: string, event: ChatEvent) => void;
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: {},
  activeSessionId: null,
  loadedProjects: new Set<string>(),
  historyLoading: new Set<string>(),

  setSessions: (metas, projectId, authoritative = false) =>
    set((s) => {
      // Keep sessions from other projects, replace sessions for this project
      const sessions: Record<string, SessionData> = {};
      for (const [id, data] of Object.entries(s.sessions)) {
        if (data.meta.projectId !== projectId) {
          sessions[id] = data;
        }
      }
      for (const meta of metas) {
        const tagged = { ...meta, projectId };
        const existing = s.sessions[meta.id];
        if (existing) {
          // Preserve live state/gitVersion when the frontend has a newer version
          // than the session.list response (which reads state from DB and may lag).
          const existingV = existing.meta.gitVersion ?? 0;
          const incomingV = tagged.gitVersion ?? 0;
          const keepState = existingV > 0 && existingV >= incomingV;
          const mergedMeta = keepState
            ? {
                ...tagged,
                state: existing.meta.state,
                connected: existing.meta.connected,
                gitVersion: existingV,
              }
            : tagged;
          sessions[meta.id] = {
            ...existing,
            meta: mergedMeta,
            planMode: mergedMeta.permissionMode === "plan",
            autoApproveMode: (mergedMeta.autoApproveMode as AutoApproveMode) ?? "manual",
            // On the reconnect (authoritative) path the fetched list is the
            // source of truth: an absent pending field CLEARS a stale one that
            // was resolved while disconnected. On the normal path keep the
            // existing value to avoid a live-push-vs-stale-list race.
            // Narrow known race: an approval raised between project.subscribe
            // completing and session.list resolving could be cleared by the
            // older-snapshot list. Net improvement over the persistent-stale
            // bug; the proper fix is server-side replay/sequencing (out of scope).
            pendingApproval: authoritative
              ? (tagged.pendingApproval ?? null)
              : (tagged.pendingApproval ?? existing.pendingApproval),
            pendingQuestion: authoritative
              ? (tagged.pendingQuestion ?? null)
              : (tagged.pendingQuestion ?? existing.pendingQuestion),
          };
        } else {
          const data = emptySessionData(tagged);
          if (tagged.pendingApproval) data.pendingApproval = tagged.pendingApproval;
          if (tagged.pendingQuestion) data.pendingQuestion = tagged.pendingQuestion;
          sessions[meta.id] = data;
        }
      }
      const loadedProjects = new Set(s.loadedProjects);
      loadedProjects.add(projectId);
      return { sessions, loadedProjects };
    }),

  addSession: (meta) =>
    set((s) => ({
      sessions: { ...s.sessions, [meta.id]: emptySessionData(meta) },
    })),

  flushPendingState: (sessionId) => {
    const pending = pendingStateUpdates.get(sessionId);
    if (!pending) return;
    pendingStateUpdates.delete(sessionId);
    useChatStore.getState().setSessionState(sessionId, pending.state, pending.extras);
  },

  removeSession: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.sessions;
      const activeSessionId = s.activeSessionId === id ? null : s.activeSessionId;
      const historyLoading = new Set(s.historyLoading);
      historyLoading.delete(id);
      return { sessions: rest, activeSessionId, historyLoading };
    }),

  /**
   * A machine went away, so its sessions cannot still be live: drop the
   * connected flag, settle anything mid-turn, and clear requests nothing can
   * answer. Same sanitize the offline snapshot applies — applied to the live
   * store too, or the UI keeps pulsing for a laptop that closed its lid an
   * hour ago and offers Allow/Deny buttons that can only time out.
   */
  markSessionsAway: (sessionIds) =>
    set((s) => {
      const sessions = { ...s.sessions };
      let changed = false;
      for (const id of sessionIds) {
        const data = sessions[id];
        if (!data) continue;
        const wasLive =
          data.meta.connected ||
          data.meta.state === "running" ||
          !!data.pendingApproval ||
          !!data.pendingQuestion;
        if (!wasLive) continue;
        changed = true;
        sessions[id] = {
          ...data,
          meta: {
            ...data.meta,
            connected: false,
            state: data.meta.state === "running" ? "idle" : data.meta.state,
          },
          pendingApproval: null,
          pendingQuestion: null,
        };
      }
      return changed ? { sessions } : s;
    }),

  setActiveSessionId: (id) =>
    set((s) => {
      const mark = `session:switch ${id?.slice(0, 8) ?? "null"}`;
      performance.mark(`${mark}:start`);

      // Evict turns from the previous completed session, keeping a tail cache.
      // Only creates a new sessions reference when eviction actually changes data.
      let sessions = s.sessions;
      const prevId = s.activeSessionId;
      if (prevId && prevId !== id) {
        const prev = sessions[prevId];
        if (prev?.meta.archivedAt && prev.turns.length > 0) {
          const eviction = evictTurns(prev);
          if (Object.keys(eviction).length > 0) {
            sessions = { ...sessions, [prevId]: { ...prev, ...eviction } };
          }
        }
      }

      // Only spread sessions when unseen flags need clearing — avoids creating a
      // new sessions reference on every switch, which would trigger expensive
      // sidebar re-renders from useFolderGroups.
      if (id) {
        const next = sessions[id];
        if (next && (next.hasUnseenCompletion || next.hasUnreadChannelMessage)) {
          sessions = {
            ...sessions,
            [id]: { ...next, hasUnseenCompletion: false, hasUnreadChannelMessage: false },
          };
        }
      }

      performance.mark(`${mark}:end`);
      performance.measure(mark, `${mark}:start`, `${mark}:end`);
      return { activeSessionId: id, sessions };
    }),

  setSessionState: (sessionId, state, extras) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        // Buffer for when addSession creates this session (race: state arrives before created).
        const existing = pendingStateUpdates.get(sessionId);
        const incomingV = extras?.gitVersion ?? 0;
        const existingV = existing?.extras?.gitVersion ?? 0;
        if (!existing || incomingV >= existingV) {
          pendingStateUpdates.set(sessionId, { state, extras });
        }
        return s;
      }

      // Reject stale updates via monotonic version.
      const incoming = extras?.gitVersion ?? 0;
      const current = session.meta.gitVersion ?? 0;
      if (incoming > 0 && current > 0 && incoming < current) return s;

      // Transient states (running, merging) don't compute git fields on the
      // backend — preserve the frontend's cached values instead of zeroing them.
      const transient = state === "running" || state === "merging";
      const staleTransient = transient && incoming <= current;
      const m = session.meta;
      const patch: Partial<SessionMetadata> = {
        state,
        connected: extras?.connected ?? m.connected,
        gitOperation: extras?.gitOperation ?? "",
        gitVersion: incoming || current,
        gitRefreshedAt: incoming > current ? Date.now() : m.gitRefreshedAt,
        // Transient states carry no computed git fields — preserve cached values
        // instead of wiping them (a clear re-triggers becameArchived below,
        // causing badge flicker + spurious tail eviction / auto-navigate).
        //
        // archivedAt is the exception, and deliberately so: it is read from
        // session state rather than computed from git, so every snapshot states
        // it — including the running one. Starting a turn un-archives a session
        // server-side, and honouring that here is what moves the row back out of
        // Archived the moment you send, rather than when the turn ends.
        archivedAt: extras?.archivedAt,
        hasDirtyWorktree: staleTransient ? m.hasDirtyWorktree : (extras?.hasDirtyWorktree ?? false),
        hasUncommitted: staleTransient ? m.hasUncommitted : (extras?.hasUncommitted ?? false),
        worktreeMerged: transient ? m.worktreeMerged : (extras?.worktreeMerged ?? false),
        commitsAhead: transient ? m.commitsAhead : (extras?.commitsAhead ?? 0),
        commitsBehind: transient ? m.commitsBehind : (extras?.commitsBehind ?? 0),
        branchMissing: transient ? m.branchMissing : (extras?.branchMissing ?? false),
        mergeStatus: transient ? m.mergeStatus : extras?.mergeStatus,
        mergeConflictFiles: transient ? m.mergeConflictFiles : extras?.mergeConflictFiles,
        worktreeBranch: extras?.worktreeBranch ?? m.worktreeBranch,
        worktreePath: extras?.worktreePath ?? m.worktreePath,
      };
      // Evict turns when a session is archived and isn't being viewed. Still
      // gated on !transient: a session cannot become archived by starting a
      // turn, and dropping the tail out from under a live turn would be wrong.
      const becameArchived = !transient && extras?.archivedAt && !m.archivedAt;
      if (becameArchived && s.activeSessionId !== sessionId && session.turns.length > 0) {
        return updateSession(s, sessionId, {
          meta: { ...m, ...patch },
          ...evictTurns(session),
        });
      }
      return updateMeta(s, sessionId, patch);
    }),

  setSessionName: (sessionId, name) => set((s) => updateMeta(s, sessionId, { name })),
  setSessionPinned: (sessionId, pinned, pinOrder) =>
    set((s) => updateMeta(s, sessionId, { pinned, pinOrder })),
  setSessionModel: (sessionId, model) =>
    set((s) => updateMeta(s, sessionId, { model, resolvedModel: undefined })),
  setSessionResolvedModel: (sessionId, resolvedModel) =>
    set((s) => updateMeta(s, sessionId, { resolvedModel })),

  setPendingApproval: (sessionId, approval) =>
    set((s) => updateSession(s, sessionId, { pendingApproval: approval })),

  clearPendingApproval: (sessionId) =>
    set((s) => updateSession(s, sessionId, { pendingApproval: null })),

  setPendingQuestion: (sessionId, question) =>
    set((s) => updateSession(s, sessionId, { pendingQuestion: question })),

  clearPendingQuestion: (sessionId) =>
    set((s) => updateSession(s, sessionId, { pendingQuestion: null })),

  setSessionPlanMode: (sessionId, planMode) =>
    set((s) => updateSession(s, sessionId, { planMode })),

  setSessionAutoApproveMode: (sessionId, autoApproveMode) =>
    set((s) => updateSession(s, sessionId, { autoApproveMode })),

  setSessionPrUrl: (sessionId, prUrl) => set((s) => updateMeta(s, sessionId, { prUrl })),

  setSessionIcon: (sessionId, icon) => set((s) => updateMeta(s, sessionId, { icon })),
  addSessionChannel: (sessionId, channelId, role?) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const existing = session.meta.channelIds ?? [];
      const patch: Partial<SessionMetadata> = {};
      if (!existing.includes(channelId)) {
        patch.channelIds = [...existing, channelId];
      }
      if (role) {
        patch.channelRoles = { ...session.meta.channelRoles, [channelId]: role };
      }
      if (!Object.keys(patch).length) return s;
      return updateMeta(s, sessionId, patch);
    }),
  removeSessionChannel: (sessionId, channelId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const existing = session.meta.channelIds ?? [];
      const filtered = existing.filter((id) => id !== channelId);
      const { [channelId]: _, ...remainingRoles } = session.meta.channelRoles ?? {};
      return updateMeta(s, sessionId, {
        channelIds: filtered.length > 0 ? filtered : undefined,
        channelRoles: Object.keys(remainingRoles).length > 0 ? remainingRoles : undefined,
      });
    }),

  setUnreadChannelMessage: (sessionId, value) =>
    set((s) => updateSession(s, sessionId, { hasUnreadChannelMessage: value })),

  updateStreamingContextUsage: (sessionId, patch) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const prev = session.contextUsage;
      const contextWindow =
        prev?.contextWindow ?? (session.meta.model?.endsWith("[1m]") ? 1_000_000 : 200_000);
      const inputTokens = patch.inputTokens ?? prev?.inputTokens ?? 0;
      const outputTokens = patch.outputTokens ?? prev?.outputTokens ?? 0;
      // message_start reports the tokens the API call actually carried, so
      // after a compaction it is already the compacted number — it may claim
      // usedTokens from an earlier live measurement without going stale.
      return updateSession(s, sessionId, {
        contextUsage: {
          contextWindow,
          inputTokens,
          outputTokens,
          usedTokens: inputTokens + outputTokens,
        },
      });
    }),

  setHistoryLoading: (sessionId, loading) =>
    set((s) => {
      const next = new Set(s.historyLoading);
      if (loading) next.add(sessionId);
      else next.delete(sessionId);
      return { historyLoading: next };
    }),

  setSessionHistory: (sessionId, turns, complete = true) =>
    set((s) => {
      const sid = sessionId.slice(0, 8);
      const mark = `history:${sid} set-state`;
      performance.mark(`${mark}:start`);

      const nextLoading = new Set(s.historyLoading);
      if (complete) nextLoading.delete(sessionId);
      const session = s.sessions[sessionId];
      if (!session) return { historyLoading: nextLoading };

      // During backfill (tail cache → full history), preserve existing turn
      // objects whose IDs match so React can skip re-rendering them.
      let merged = turns;
      let preserved = 0;
      if (!session.historyComplete && session.turns.length > 0) {
        const cached = new Map(session.turns.map((t) => [t.id, t]));
        if (cached.size > 0) {
          merged = turns.map((t) => {
            const existing = cached.get(t.id);
            if (existing) preserved++;
            return existing ?? t;
          });
        }
      }

      const todos = extractTodosFromTurns(merged);
      const contextUsage = extractContextUsageFromTurns(merged);
      // Preserve any carried-over queued message (a provider-without-native-
      // mid-turn echo targeting the NEXT turn). History only contains durable
      // turns, so a mid-window resync would otherwise drop the "queued" bubble
      // until its replayed turn lands. Mirrors the carry-over in apply-event.ts.
      // A message whose replayed turn is already IN this history is done being
      // queued — normally submitQuery clears it when turn-started arrives, but a
      // reconnect can land the history without ever delivering that push, and
      // the stale bubble then duplicates the turn it became.
      const carryOver = session.streamingEvents.filter(
        (e) =>
          e.type === "user_message" &&
          e.deliveryStatus === "queued" &&
          !merged.some((t) => promptCarriesMessage(t.prompt, e.content ?? "")),
      );
      const result = {
        historyLoading: nextLoading,
        ...updateSession(s, sessionId, {
          turns: merged,
          streamingEvents: carryOver,
          historyComplete: complete,
          todos,
          contextUsage,
        }),
      };

      performance.mark(`${mark}:end`);
      performance.measure(
        `${mark} (${turns.length} turns, ${preserved} cached)`,
        `${mark}:start`,
        `${mark}:end`,
      );
      return result;
    }),

  submitQuery: (sessionId, prompt, attachments, meta) => {
    const turnId = uuid();
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      return updateSession(s, sessionId, {
        turns: [
          ...session.turns,
          {
            id: turnId,
            prompt,
            attachments: (attachments ?? []).map(({ previewUrl: _, ...rest }) => rest),
            events: [],
            complete: false,
            turnIndex: meta?.turnIndex,
            origin: meta?.origin,
          },
        ],
        streamingEvents: [],
      });
    });
    return turnId;
  },

  // Removes the turn `submitQuery` drew, by id — the send it stood for either
  // failed or turned out not to be a turn at all. Identity, not prompt text:
  // two turns can carry the same words, and taking the last one that matches
  // could delete a turn that is genuinely running.
  rollbackOptimisticTurn: (sessionId, turnId) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const turns = session.turns;
      const last = turns[turns.length - 1];
      // A queued/pending user_message in the buffer is not output — it belongs
      // to a message that is still waiting, and it must not veto the rollback of
      // a turn whose send failed (that leaves a phantom prompt on screen).
      const turnProduced = session.streamingEvents.some((e) => e.type !== "user_message");
      if (
        last &&
        !last.complete &&
        last.events.length === 0 &&
        !turnProduced &&
        last.id === turnId
      ) {
        return updateSession(s, sessionId, { turns: turns.slice(0, -1) });
      }
      return s;
    }),

  // Replace the optimistic turn's prompt with the authoritative one from the
  // server's turn-started broadcast, and adopt its turn identity (turnIndex/
  // origin) too — the optimistic turn built from raw composer input has
  // neither. The broadcast prompt may carry a system-injected <brain> recall
  // envelope the optimistic turn lacks; adopting it lets the recalled-memory
  // card render live instead of only after a history reload. No-op unless the
  // last turn is still an unstarted optimistic turn (incomplete, no events)
  // with something to adopt.
  adoptTurnPrompt: (sessionId, prompt, meta) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) return s;
      const turns = session.turns;
      const last = turns[turns.length - 1];
      if (!last || last.complete || last.events.length !== 0) {
        return s;
      }
      const adopted = {
        ...last,
        prompt,
        turnIndex: meta?.turnIndex ?? last.turnIndex,
        origin: meta?.origin ?? last.origin,
      };
      if (
        adopted.prompt === last.prompt &&
        adopted.turnIndex === last.turnIndex &&
        adopted.origin === last.origin
      ) {
        return s;
      }
      const next = [...turns];
      next[next.length - 1] = adopted;
      return updateSession(s, sessionId, { turns: next });
    }),

  handleServerEvent: (sessionId, event) =>
    set((s) => {
      const session = s.sessions[sessionId];
      if (!session) {
        console.warn("handleServerEvent: unknown session", sessionId);
        return s;
      }

      const isViewing = s.activeSessionId === sessionId;
      const result: ApplyResult = applyServerEvent(session, event, isViewing);
      if (!result) return s;

      // Execute side effects that can't live in the pure function.
      if (result.sideEffect?.type === "rate_limit") {
        const se = result.sideEffect;
        useRateLimitStore
          .getState()
          .updateEntry(se.rateLimitType, se.status, se.utilization, se.resetsAt);
      }

      // rate_limit returns an empty patch — skip the updateSession call.
      if (Object.keys(result.patch).length === 0) return s;

      return updateSession(s, sessionId, result.patch);
    }),
}));
