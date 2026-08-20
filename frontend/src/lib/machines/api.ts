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
