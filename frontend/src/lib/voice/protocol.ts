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

/**
 * The call is doing something slow, and says so while it does it.
 *
 * A non-empty `label` opens the state; an empty or absent one closes it. There
 * is at most one at a time and a new label replaces the old, because the
 * question a reader is asking — "is this thing still alive?" — has one answer.
 *
 * It exists because tool work is invisible otherwise: a call computing a
 * summary looks exactly like a call that has died, and the first field report
 * of the switchboard was someone waiting out a working call in silence.
 */
export interface VoiceActivity {
  type: "activity";
  label?: string;
}

/**
 * The screen copy of a session summary the call is about to speak.
 *
 * Sent before the speech, like a report: an engine with no voice, or a
 * reconnecting one, must still leave the answer on screen. The text distils
 * untrusted transcript content, so it is rendered as content, never followed.
 */
export interface VoiceSummary {
  type: "summary";
  sessionId?: string;
  headline?: string;
}

/**
 * The screen follows the voice: the call has moved its focus, and the client
 * navigates there.
 *
 * It is an instruction, not a status — the operator may have wandered off to
 * another session since, and the point of the frame is to bring them back.
 */
export interface VoiceFocus {
  type: "focus";
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
  | VoiceDispatched
  | VoiceFocus
  | VoiceActivity
  | VoiceSummary;

/** Why a session is waiting on the operator. Absent means it is not. */
export type VoiceAttention = "approval" | "question" | "unread";

/**
 * One session as the call's agent sees it — enough to name it, place it, and
 * know whether it is waiting on someone.
 *
 * This is a snapshot of the client's merged view across every machine, not a
 * server-side query: the browser is the only party that holds all of them.
 */
export interface VoiceWorldSession {
  sessionId: string;
  name: string;
  /** Routing slug — machine-qualified, as the route param wants it. */
  projectSlug: string;
  projectName: string;
  machineId?: string;
  machineName?: string;
  state: string;
  attention?: VoiceAttention;
  branch?: string;
  lastActivityAt?: string;
}

/** The client's view of every session it can see. */
export interface VoiceWorld {
  type: "world";
  sessions: VoiceWorldSession[];
}

/**
 * The operator navigated somewhere themselves.
 *
 * An empty `sessionId` means they left the session view. The server decides
 * what to make of it; the client never retargets the call on its own.
 */
export interface VoiceViewing {
  type: "viewing";
  sessionId: string;
}

/**
 * Rows the world snapshot carries at most, newest first.
 *
 * Everything in it goes to the speech vendor on every change, so the cap is a
 * budget rather than a limit anyone is expected to hit.
 */
export const WORLD_ROW_CAP = 200;

/** Control messages the browser sends. */
export type VoiceClientMessage = { type: "stop" } | VoiceWorld | VoiceViewing;

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
    case "focus":
    case "activity":
    case "summary":
      return parsed as VoiceServerMessage;
    default:
      return null;
  }
}
