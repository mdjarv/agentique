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
import {
  primaryVoiceUrl,
  VoiceCall,
  type VoiceCallHandlers,
  type VoiceCallState,
} from "~/lib/voice/call";
import type { VoiceWorldSession } from "~/lib/voice/protocol";
import { buildWorldSessions } from "~/lib/voice/world";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

/** One line in the call log, whatever produced it. */
export interface VoiceLogEntry {
  id: number;
  /** Where it came from — decides how it should be read, and how much to trust it. */
  source: "you" | "agent" | "dispatched" | "report" | "notice" | "summary";
  text: string;
  /** report/notice kind, when there is one. */
  kind?: string;
  /** The session a summary is about, when it named one. */
  sessionId?: string;
}

/** The half of a conversation a transcript line came from. */
export type VoiceSpeaker = "you" | "agent";

/**
 * The line still being recognised.
 *
 * It is held apart from the log rather than appended to it: interim text
 * rewrites itself word by word, so a log of it would be a column of
 * half-sentences. On screen it is one provisional line that the final form
 * replaces.
 */
export interface VoiceInterim {
  source: VoiceSpeaker;
  text: string;
}

/**
 * What the dock says.
 *
 * `error` covers a refused call and one that broke; `ended` is a call that
 * finished — the server hung up, or the operator did. An ended call is still
 * on screen, because a call that vanishes is indistinguishable from one that
 * was never there, and the answer it just delivered is in its log.
 */
export type VoiceStatus = "idle" | "connecting" | "live" | "error" | "ended";

/** True while the call is over but still on screen, waiting to be dismissed. */
function isTerminal(status: VoiceStatus): boolean {
  return status === "error" || status === "ended";
}

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
  /**
   * What the call is working on, in the server's words. Empty means nothing.
   *
   * There is only ever one: the reader's question is whether the call is still
   * alive, and two answers to that is one too many.
   */
  activityLabel: string;
  /** The line still being recognised, replaced by its final form when it lands. */
  interim: VoiceInterim | null;
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
  /** Hangs up. The call stays on screen as `ended` until it is dismissed. */
  stop: () => void;
  /** Clears an ended or failed call off the surfaces, log and all. */
  dismiss: () => void;
}

let call: VoiceCall | null = null;
let nextLogId = 0;

/** Teardown for the store subscriptions that feed the live call. */
let unwatch: (() => void) | null = null;
/** Last frames actually sent, so an unchanged snapshot costs nothing. */
let sentWorld = "";
let sentViewing: string | null = null;

function append(
  source: VoiceLogEntry["source"],
  text: string,
  extra?: { kind?: string; sessionId?: string },
): void {
  if (!text) return;
  useVoiceStore.setState((s) => {
    const entry: VoiceLogEntry = { id: nextLogId++, source, text, ...extra };
    const next = [...s.log, entry];
    return { log: next.length > LOG_LIMIT ? next.slice(-LOG_LIMIT) : next };
  });
}

/**
 * The call's own view of itself, in the two or three words a dock can show.
 *
 * `idle` maps to nothing on purpose. It is the call object saying it holds no
 * socket, which happens *after* a hangup the store has already described — and
 * the description is the part worth keeping. Whether the surfaces go quiet is
 * the store's decision (`stop`, `dismiss`), never a late callback's.
 */
function toStatus(state: VoiceCallState): VoiceStatus | null {
  switch (state) {
    case "connecting":
      return "connecting";
    case "live":
      return "live";
    case "failed":
      return "error";
    case "closed":
      return "ended";
    default:
      return null;
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

/**
 * Every control frame, as a change to what the surfaces show.
 *
 * Exported because this is the store's reducer: driving it directly is how the
 * frame handling is tested, without a socket, a microphone or an engine.
 */
export const voiceCallHandlers: VoiceCallHandlers = {
  onState: (next, why) => {
    const status = toStatus(next);
    if (!status) return;
    useVoiceStore.setState((s) => ({
      status,
      // A hangup arrives twice — the server's `closed` frame carrying the
      // reason, then the socket closing carrying none. Keep the one that said
      // why, but only between endings: a fresh connection clears the last
      // call's epitaph rather than inheriting it.
      detail: why ?? (isTerminal(status) && isTerminal(s.status) ? s.detail : undefined),
      // Off the line, nothing is being worked on and nothing is half-heard.
      ...(status === "live" ? {} : { activityLabel: "", interim: null }),
    }));
    if (status === "live") watch();
    else if (status !== "connecting") stopWatching();
  },

  // Only settled transcripts are logged. Interim text rewrites itself, so it is
  // held as one provisional line instead — the fastest evidence the call is
  // hearing you, and the first thing a stalled call stops producing.
  onTranscript: (t) => {
    const source: VoiceSpeaker = t.source === "caller" ? "you" : "agent";
    const text = t.text ?? "";
    if (!t.final) {
      useVoiceStore.setState({ interim: text ? { source, text } : null });
      return;
    }
    useVoiceStore.setState({ interim: null });
    append(source, text);
  },

  onActivity: (a) => {
    useVoiceStore.setState({ activityLabel: (a.label ?? "").trim() });
  },

  // The screen copy of a summary, which arrives before it is spoken. It is
  // logged as its own source because it is an answer, not a status line.
  onSummary: (s) => append("summary", s.headline ?? "", { sessionId: s.sessionId }),

  onDispatched: (d) => append("dispatched", d.headline ?? ""),
  onReport: (r) => append("report", r.headline ?? "", { kind: r.kind }),
  onNotice: (n) => append("notice", n.headline ?? "", { kind: n.kind }),

  onFocus: (f) => {
    const sessionId = f.sessionId ?? "";
    if (!sessionId) return;
    useVoiceStore.setState((s) => ({ focusSessionId: sessionId, focusSeq: s.focusSeq + 1 }));
  },
};

function ensureCall(): VoiceCall {
  if (call) return call;
  call = new VoiceCall(voiceCallHandlers);
  return call;
}

export const useVoiceStore = create<VoiceState>((set, get) => ({
  status: "idle",
  detail: undefined,
  activityLabel: "",
  interim: null,
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
      activityLabel: "",
      interim: null,
      focusSessionId: initialFocusSessionId ?? null,
    });
    void ensureCall().start(primaryVoiceUrl(initialFocusSessionId));
  },

  stop: () => {
    if (get().status === "idle") return;
    stopWatching();
    void call?.stop();
    // Ended, not gone. The log holds whatever the call just delivered — a
    // summary above all — and a surface that disappears on hangup takes the
    // answer with it. The focus stays too, so the ended line still names it.
    set({ status: "ended", detail: "you ended the call", activityLabel: "", interim: null });
  },

  dismiss: () => {
    stopWatching();
    // A failed call can still be holding an open socket; dismissing it is also
    // the last chance to let go of one.
    void call?.stop();
    set({
      status: "idle",
      detail: undefined,
      activityLabel: "",
      interim: null,
      focusSessionId: null,
      log: EMPTY_LOG,
    });
  },
}));
