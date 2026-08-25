/**
 * Control messages on the voice socket.
 *
 * Audio never appears here — it rides binary frames, and the WebSocket frame
 * type is the only discriminator. These are the text frames.
 *
 * Every field past `type` is optional, mirroring the Go tags. A peer running an
 * older or newer build may omit anything, and an absent field means "not set"
 * rather than a reason to reject the payload.
 */

export interface VoiceReady {
  type: "ready";
  inputSampleRate?: number;
  outputSampleRate?: number;
}

export interface VoiceTurnComplete {
  type: "turn_complete";
  interrupted?: boolean;
}

export interface VoiceTranscript {
  type: "transcript";
  text?: string;
  final?: boolean;
  source?: string;
}

export interface VoiceError {
  type: "error";
  message?: string;
}

export interface VoiceClosed {
  type: "closed";
  reason?: string;
}

/** An agent-written progress report from the session being followed. */
export interface VoiceReportMessage {
  type: "report";
  kind?: string;
  headline?: string;
  sessionId?: string;
}

/** A runtime fact about the followed session: finished, failed, or blocked. */
export interface VoiceNotice {
  type: "notice";
  kind?: string;
  headline?: string;
  sessionId?: string;
}

/** The prompt the voice agent just handed to the session. */
export interface VoiceDispatched {
  type: "dispatched";
  headline?: string;
  sessionId?: string;
}

export type VoiceServerMessage =
  | VoiceReady
  | VoiceTurnComplete
  | VoiceTranscript
  | VoiceError
  | VoiceClosed
  | VoiceReportMessage
  | VoiceNotice
  | VoiceDispatched;

/** Control messages the browser sends. */
export type VoiceClientMessage = { type: "stop" };

/**
 * Parses a text frame, returning null for anything unrecognised.
 *
 * Unknown types are dropped rather than thrown: a newer server may send a
 * control type this build predates, and that is not an error the user needs to
 * hear about.
 */
export function parseServerMessage(raw: string): VoiceServerMessage | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;

  const type = (parsed as { type?: unknown }).type;
  switch (type) {
    case "ready":
    case "turn_complete":
    case "transcript":
    case "error":
    case "closed":
    case "report":
    case "notice":
    case "dispatched":
      return parsed as VoiceServerMessage;
    default:
      return null;
  }
}
