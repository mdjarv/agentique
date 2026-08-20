import { machineFetch } from "~/lib/machines/registry";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Machine-aware REST (multi-machine). WS traffic routes through the
 * facade in router.ts; plain REST resolves its target here — by project or
 * session id — and reaches remote machines via machineFetch (bearer +
 * absolute base URL). undefined machineId = the primary, same-origin.
 */

export function machineIdForProject(projectId: string): string | undefined {
  const project = useAppStore.getState().projects.find((p) => p.id === projectId);
  const id = project?.machineId;
  return id && useMachineStore.getState().machines[id] ? id : undefined;
}

export function machineIdForSession(sessionId: string): string | undefined {
  const meta = useChatStore.getState().sessions[sessionId]?.meta;
  return meta ? machineIdForProject(meta.projectId) : undefined;
}

export function apiFetch(
  machineId: string | undefined,
  path: string,
  init?: RequestInit,
): Promise<Response> {
  if (!machineId) return fetch(path, init);
  return machineFetch(machineId, path, init);
}

/**
 * Rewrites a localhost URL printed by a remote machine's agent to that
 * machine's host (same port/path/scheme) — "open http://localhost:9210"
 * means the MACHINE's localhost, not the reader's device. Reachability still
 * depends on the target binding a non-loopback interface; a wrong-host link
 * never works, a rewritten one works whenever it can.
 */
export function rewriteRemoteLocalhost(href: string, machineId: string | null): string {
  if (!machineId) return href;
  try {
    const url = new URL(href);
    if (url.hostname !== "localhost" && url.hostname !== "127.0.0.1") return href;
    const entry = useMachineStore.getState().machines[machineId];
    if (!entry) return href;
    url.hostname = new URL(entry.baseUrl).hostname;
    return url.toString();
  } catch {
    return href; // relative or non-URL href
  }
}

const SESSION_FILE_PATH = /^\/api\/sessions\/([0-9a-fA-F-]+)\/files\//;

/** For a session-file URL (agents embed their session files as
 *  `/api/sessions/{id}/files/…` per the preamble), the owning machine's id —
 *  undefined for anything else, including the primary's own sessions. Besides
 *  the instructed relative form, absolute variants agents sometimes write are
 *  tolerated when the session id resolves: viewer-origin and
 *  localhost/127.0.0.1 (the agent's idea of "this server"). */
export function sessionFileMachineId(href: string): string | undefined {
  const parsed = parseSessionFileHref(href);
  return parsed ? machineIdForSession(parsed.sessionId) : undefined;
}

/** The origin-relative path of a recognized session-file URL, or undefined.
 *  Lets renderers normalize an absolute-localhost variant to a relative URL
 *  that works from any device (cookie-authenticated against the primary). */
export function sessionFilePath(href: string): string | undefined {
  return parseSessionFileHref(href)?.path;
}

function parseSessionFileHref(href: string): { sessionId: string; path: string } | undefined {
  try {
    const url = new URL(href, window.location.origin);
    const recognizedHost =
      url.origin === window.location.origin ||
      url.hostname === "localhost" ||
      url.hostname === "127.0.0.1";
    if (!recognizedHost) return undefined;
    const match = SESSION_FILE_PATH.exec(url.pathname);
    if (!match?.[1]) return undefined;
    return { sessionId: match[1], path: url.pathname + url.search };
  } catch {
    return undefined;
  }
}
