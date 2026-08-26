/**
 * The words a call is described in.
 *
 * One module, because the rail dock, the phone bubble and the sheet all report
 * the same call: a call that says "hung up after silence" in one place and
 * "closed: idle" in another is two calls to the reader.
 */
import type { VoiceStatus } from "~/stores/voice-store";

/**
 * Close reasons the server sends as tokens, in words.
 *
 * Anything not listed is shown as it arrived — the server's own sentence is
 * better than a generic fallback, and an unknown token is still a clue.
 */
const REASON_WORDS: Record<string, string> = {
  idle: "Hung up after silence",
  timeout: "Hung up after silence",
  "engine-closed": "The voice engine closed the call",
  server_shutdown: "The server restarted",
};

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Why the call is over, in a sentence rather than a token. */
export function callEndedLine(detail?: string): string {
  const reason = detail?.trim();
  if (!reason) return "The connection closed";
  return REASON_WORDS[reason] ?? capitalize(reason);
}

/** One line of status, in the words the reader needs. */
export function callStatusLine(status: VoiceStatus, detail?: string): string {
  switch (status) {
    case "live":
      return "Listening";
    case "connecting":
      return "Connecting";
    case "error":
      return detail ? capitalize(detail) : "The call hit a problem";
    case "ended":
      return callEndedLine(detail);
    default:
      return "Not connected";
  }
}

/** The heading a call surface shows when there is no session to name. */
export function callHeadline(status: VoiceStatus, focusName: string | null): string {
  if (status === "ended") return "Call ended";
  return focusName || "No focus";
}
