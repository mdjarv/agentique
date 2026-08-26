/**
 * What both docks show about the call.
 *
 * Store reads live here so the rail bar and the phone bubble cannot drift into
 * describing the same call two different ways. Every selector returns a
 * primitive or a value the store already holds — never a fresh array, which
 * would re-render forever (CLAUDE.md).
 */
import { useMemo } from "react";
import { useChatStore } from "~/stores/chat-store";
import { useVoiceStore, type VoiceLogEntry, type VoiceStatus } from "~/stores/voice-store";

export interface CallView {
  status: VoiceStatus;
  detail: string | undefined;
  /** True while the call holds a socket — connecting, live, or just broken. */
  active: boolean;
  /** Name of the session the call is working with, or null when it has none. */
  focusName: string | null;
  /** The last thing said, either side of the conversation. */
  lastSpoken: string | undefined;
  log: VoiceLogEntry[];
  stop: () => void;
}

export function useCallView(): CallView {
  const status = useVoiceStore((s) => s.status);
  const detail = useVoiceStore((s) => s.detail);
  const log = useVoiceStore((s) => s.log);
  const stop = useVoiceStore((s) => s.stop);
  const focusSessionId = useVoiceStore((s) => s.focusSessionId);
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

  return {
    status,
    detail,
    active: status !== "idle",
    focusName,
    lastSpoken,
    log,
    stop,
  };
}
