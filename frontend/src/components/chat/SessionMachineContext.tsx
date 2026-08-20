import { createContext, useContext } from "react";

/**
 * The machineId of the remote machine whose session the surrounding chat
 * content belongs to; null when the content is the primary's (or has no
 * session context, e.g. channel/discussion views). Lets markdown renderers
 * fix machine-relative content — most importantly rewriting `localhost` URLs
 * an agent prints, which would otherwise point at whatever device the reader
 * happens to hold.
 */
export const SessionMachineContext = createContext<string | null>(null);

export function useSessionMachineId(): string | null {
  return useContext(SessionMachineContext);
}
