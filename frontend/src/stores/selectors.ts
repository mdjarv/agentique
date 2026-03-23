import type { ChatState, SessionData, SessionMetadata } from "~/stores/chat-store";

export const selectActiveSession = (s: ChatState): SessionData | undefined =>
  s.activeSessionId ? s.sessions[s.activeSessionId] : undefined;

export const selectActiveSessionMeta = (s: ChatState): SessionMetadata | undefined =>
  s.activeSessionId ? s.sessions[s.activeSessionId]?.meta : undefined;
