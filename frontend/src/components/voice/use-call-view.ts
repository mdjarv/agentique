/**
 * What both docks show about the call.
 *
 * Store reads live here so the rail bar and the phone bubble cannot drift into
 * describing the same call two different ways. Every selector returns a
 * primitive or a value the store already holds — never a fresh array, which
 * would re-render forever (CLAUDE.md).
 */
import { useMemo } from "react";
import { callStatusLine } from "~/lib/voice/copy";
import { useChatStore } from "~/stores/chat-store";
import {
  useVoiceStore,
  type VoiceInterim,
  type VoiceLogEntry,
  type VoiceSpeaker,
  type VoiceStatus,
} from "~/stores/voice-store";

/**
 * The call's second line — one slot, and what claims it.
 *
 * The order is the reader's question, answered as directly as the call can. An
 * ended or failed call says so and nothing else. A working call names the work,
 * because that is the one state where silence used to look like death. Then
 * words being heard right now, then the last thing said, then the status.
 */
export type CallLine =
  | { kind: "activity"; text: string }
  | { kind: "interim"; text: string; source: VoiceSpeaker }
  | { kind: "spoken"; text: string }
  | { kind: "status"; text: string };

export interface CallView {
  status: VoiceStatus;
  detail: string | undefined;
  /** True while the call is on screen at all — connecting, live, broken or over. */
  active: boolean;
  /** True only while audio is actually flowing, which is what the meter needs. */
  live: boolean;
  /** True once the call is over and waiting to be dismissed or replaced. */
  ended: boolean;
  /** Name of the session the call is working with, or null when it has none. */
  focusName: string | null;
  /** What the call is working on right now. Empty means nothing is pending. */
  activityLabel: string;
  /** The line still being recognised, if any. */
  interim: VoiceInterim | null;
  /** The one subordinate line both surfaces show under the heading. */
  line: CallLine;
  log: VoiceLogEntry[];
  stop: () => void;
  dismiss: () => void;
  /** Calls again, on the session this call was last working with. */
  restart: () => void;
}

export function useCallView(): CallView {
  const status = useVoiceStore((s) => s.status);
  const detail = useVoiceStore((s) => s.detail);
  const activityLabel = useVoiceStore((s) => s.activityLabel);
  const interim = useVoiceStore((s) => s.interim);
  const log = useVoiceStore((s) => s.log);
  const stop = useVoiceStore((s) => s.stop);
  const dismiss = useVoiceStore((s) => s.dismiss);
  const start = useVoiceStore((s) => s.start);
  const focusSessionId = useVoiceStore((s) => s.focusSessionId);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const focusName = useChatStore((s) =>
    focusSessionId ? (s.sessions[focusSessionId]?.meta.name ?? null) : null,
  );

  // Derived outside the selector: `.filter()` inside one hands back a new
  // reference every call.
  const lastSpoken = useMemo(() => {
    for (let i = log.length - 1; i >= 0; i--) {
      const entry = log[i];
      if (entry && (entry.source === "you" || entry.source === "agent")) return entry.text;
    }
    return undefined;
  }, [log]);

  const line = useMemo<CallLine>(() => {
    if (status === "ended" || status === "error" || status === "connecting") {
      return { kind: "status", text: callStatusLine(status, detail) };
    }
    if (activityLabel) return { kind: "activity", text: activityLabel };
    if (interim) return { kind: "interim", text: interim.text, source: interim.source };
    if (lastSpoken) return { kind: "spoken", text: lastSpoken };
    return { kind: "status", text: callStatusLine(status, detail) };
  }, [status, detail, activityLabel, interim, lastSpoken]);

  const restart = useMemo(
    () => () => start(focusSessionId ?? activeSessionId ?? undefined),
    [start, focusSessionId, activeSessionId],
  );

  return {
    status,
    detail,
    active: status !== "idle",
    live: status === "live",
    ended: status === "ended",
    focusName,
    activityLabel,
    interim,
    line,
    log,
    stop,
    dismiss,
    restart,
  };
}
