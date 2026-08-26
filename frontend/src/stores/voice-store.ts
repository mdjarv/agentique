/**
 * The app-wide live call.
 *
 * A call used to belong to the session it was opened from, which made it die on
 * navigation — and a call that cannot survive navigation cannot navigate, which
 * is the whole point of the switchboard. So the call is owned here, once, for as
 * long as the tab lives.
 *
 * The `VoiceCall` itself is a module-level reference rather than store state: it
 * holds a socket, a microphone and a playback queue, and none of that belongs in
 * a value React diffs. The store holds only what a component renders.
 */
import { create } from "zustand";
import { primaryVoiceUrl, VoiceCall, type VoiceCallState } from "~/lib/voice/call";
import type { VoiceWorldSession } from "~/lib/voice/protocol";
import { buildWorldSessions } from "~/lib/voice/world";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

/** One line in the call log, whatever produced it. */
export interface VoiceLogEntry {
  id: number;
  /** Where it came from — decides how it should be read, and how much to trust it. */
  source: "you" | "agent" | "dispatched" | "report" | "notice";
  text: string;
  /** report/notice kind, when there is one. */
  kind?: string;
}

/** What the dock says. `error` covers both a refused call and one that broke. */
export type VoiceStatus = "idle" | "connecting" | "live" | "error";

/**
 * Fallbacks are module constants, never fresh literals: a selector returning a
 * new `[]` re-renders its component forever (see CLAUDE.md).
 */
const EMPTY_LOG: VoiceLogEntry[] = [];

/** A long call is a long log; keep the readable tail rather than all of it. */
const LOG_LIMIT = 200;

/**
 * How long a change settles before it is sent.
 *
 * The chat store ticks on every streamed token, and each world snapshot goes to
 * the speech vendor, so the snapshot is deliberately lazy about keeping up.
 */
const SEND_DEBOUNCE_MS = 2000;

interface VoiceState {
  status: VoiceStatus;
  /** Why the call failed or ended, when the server or browser said. */
  detail: string | undefined;
  /** The session the call is working with, as the server last said. */
  focusSessionId: string | null;
  /**
   * Bumped by every `focus` frame.
   *
   * Focus is an instruction to navigate, not a fact about where the operator
   * is, so it has to survive being asked twice for the same session — the
   * counter is what the navigator watches.
   */
  focusSeq: number;
  log: VoiceLogEntry[];
  /** Opens a call, optionally bound to the session it was started from. */
  start: (initialFocusSessionId?: string) => void;
  stop: () => void;
}

let call: VoiceCall | null = null;
let nextLogId = 0;

/** Teardown for the store subscriptions that feed the live call. */
let unwatch: (() => void) | null = null;
/** Last frames actually sent, so an unchanged snapshot costs nothing. */
let sentWorld = "";
let sentViewing: string | null = null;

function append(source: VoiceLogEntry["source"], text: string, kind?: string): void {
  if (!text) return;
  useVoiceStore.setState((s) => {
    const next = [...s.log, { id: nextLogId++, source, text, kind }];
    return { log: next.length > LOG_LIMIT ? next.slice(-LOG_LIMIT) : next };
  });
}

/** The call's own view of itself, in the two or three words a dock can show. */
function toStatus(state: VoiceCallState): VoiceStatus {
  switch (state) {
    case "connecting":
      return "connecting";
    case "live":
      return "live";
    case "failed":
      return "error";
    default:
      return "idle";
  }
}

function currentWorld(): VoiceWorldSession[] {
  const machines = useMachineStore.getState().machines;
  const machineNames: Record<string, string> = {};
  for (const [id, entry] of Object.entries(machines)) {
    if (entry.label) machineNames[id] = entry.label;
  }
  return buildWorldSessions({
    sessions: useChatStore.getState().sessions,
    projects: useAppStore.getState().projects,
    machineNames,
  });
}

function sendWorld(): void {
  if (!call) return;
  const sessions = currentWorld();
  const encoded = JSON.stringify(sessions);
  if (encoded === sentWorld) return;
  sentWorld = encoded;
  call.sendWorld(sessions);
}

/**
 * Reports where the operator went, when that is somewhere other than where the
 * call already is.
 *
 * Only a report: the client never retargets the call from here. What the server
 * makes of it — follow along, ask, ignore — is the server's decision.
 */
function sendViewing(): void {
  if (!call) return;
  const active = useChatStore.getState().activeSessionId;
  const focus = useVoiceStore.getState().focusSessionId;
  // The call navigating there itself is not the operator navigating there.
  if (active && active === focus) return;
  const sessionId = active ?? "";
  if (sessionId === sentViewing) return;
  sentViewing = sessionId;
  call.sendViewing(sessionId);
}

/**
 * Coalesces a burst of changes into one send, without ever starving.
 *
 * A restarting debounce would starve: the chat store ticks on every streamed
 * token, so a busy session could push the next snapshot back indefinitely. The
 * first change opens a window instead, and everything inside it rides along.
 */
function coalesced(fn: () => void) {
  let timer: number | undefined;
  return {
    schedule() {
      if (timer !== undefined) return;
      timer = window.setTimeout(() => {
        timer = undefined;
        fn();
      }, SEND_DEBOUNCE_MS);
    },
    cancel() {
      if (timer !== undefined) window.clearTimeout(timer);
      timer = undefined;
    },
  };
}

const worldSender = coalesced(sendWorld);
const viewingSender = coalesced(sendViewing);

/**
 * Starts feeding the live call: the world it can act on, and where the operator
 * is looking. Both are coalesced; the first world goes out immediately, because
 * a call that opens knowing nothing has nothing to answer with.
 */
function watch(): void {
  if (unwatch) return;
  sentWorld = "";
  sentViewing = null;
  sendWorld();

  let lastActive = useChatStore.getState().activeSessionId;
  const unsubChat = useChatStore.subscribe((s) => {
    worldSender.schedule();
    // Where the operator is looking changes on navigation, not on every token.
    if (s.activeSessionId === lastActive) return;
    lastActive = s.activeSessionId;
    viewingSender.schedule();
  });
  const unsubProjects = useAppStore.subscribe(() => worldSender.schedule());
  const unsubMachines = useMachineStore.subscribe(() => worldSender.schedule());

  unwatch = () => {
    unsubChat();
    unsubProjects();
    unsubMachines();
    worldSender.cancel();
    viewingSender.cancel();
  };
}

function stopWatching(): void {
  unwatch?.();
  unwatch = null;
}

function ensureCall(): VoiceCall {
  if (call) return call;
  call = new VoiceCall({
    onState: (next, why) => {
      const status = toStatus(next);
      useVoiceStore.setState({ status, detail: why });
      if (status === "live") watch();
      else if (status !== "connecting") stopWatching();
    },
    // Only settled transcripts are logged; interim text rewrites itself and
    // would fill the log with half-sentences.
    onTranscript: (t) => {
      if (!t.final) return;
      append(t.source === "caller" ? "you" : "agent", t.text ?? "");
    },
    onDispatched: (d) => append("dispatched", d.headline ?? "", undefined),
    onReport: (r) => append("report", r.headline ?? "", r.kind),
    onNotice: (n) => append("notice", n.headline ?? "", n.kind),
    onFocus: (f) => {
      const sessionId = f.sessionId ?? "";
      if (!sessionId) return;
      useVoiceStore.setState((s) => ({ focusSessionId: sessionId, focusSeq: s.focusSeq + 1 }));
    },
  });
  return call;
}

export const useVoiceStore = create<VoiceState>((set, get) => ({
  status: "idle",
  detail: undefined,
  focusSessionId: null,
  focusSeq: 0,
  log: EMPTY_LOG,

  start: (initialFocusSessionId) => {
    // One call at a time: the engine is per-call server-side, and a second
    // socket would leave the first one billing with nobody listening. The dock
    // is already showing the running one.
    const status = get().status;
    if (status === "connecting" || status === "live") return;
    set({
      log: EMPTY_LOG,
      detail: undefined,
      focusSessionId: initialFocusSessionId ?? null,
    });
    void ensureCall().start(primaryVoiceUrl(initialFocusSessionId));
  },

  stop: () => {
    stopWatching();
    void call?.stop();
    set({ status: "idle", focusSessionId: null });
  },
}));
