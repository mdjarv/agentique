import type {
  BehaviorPresets,
  DiskStats,
  PresetDefinition,
  ReclaimRequest,
  ReclaimResponse,
  StorageUsage,
} from "~/lib/generated-types";
import { throwIfNotOk } from "~/lib/http";
import { apiFetch, machineIdForProject } from "~/lib/machines/api";
import type { Project } from "~/lib/types";

const BASE = "/api";

async function fetchWithRetry(
  input: RequestInfo,
  init?: RequestInit,
  maxRetries = 2,
): Promise<Response> {
  return retrying(() => fetch(input, init), maxRetries);
}

/** fetchWithRetry against the machine owning the target (multi-machine);
 *  machineId undefined = the primary, same-origin. */
async function machineFetchWithRetry(
  machineId: string | undefined,
  path: string,
  init?: RequestInit,
  maxRetries = 2,
): Promise<Response> {
  return retrying(() => apiFetch(machineId, path, init), maxRetries);
}

async function retrying(doFetch: () => Promise<Response>, maxRetries: number): Promise<Response> {
  let lastError: Error | undefined;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const res = await doFetch();
      if (res.ok || res.status < 500) return res;
      lastError = new Error(`Server error: ${res.status}`);
    } catch (err) {
      lastError = err instanceof Error ? err : new Error(String(err));
    }
    if (attempt < maxRetries) {
      await new Promise((r) => setTimeout(r, 500 * 2 ** attempt));
    }
  }
  throw lastError;
}

export async function listProjects(): Promise<Project[]> {
  const res = await fetchWithRetry(`${BASE}/projects`);
  await throwIfNotOk(res, "Failed to list projects");
  return res.json();
}

export async function createProject(
  name: string,
  path: string,
  machineId?: string,
): Promise<Project> {
  const res = await apiFetch(machineId, `${BASE}/projects`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, path }),
  });
  await throwIfNotOk(res, "Failed to create project");
  return res.json();
}

export async function updateProject(
  id: string,
  updates: {
    name?: string;
    slug?: string;
    behaviorPresets?: BehaviorPresets;
    color?: string;
    icon?: string;
    folder?: string;
    maxSessions?: number;
  },
): Promise<Project> {
  const res = await apiFetch(machineIdForProject(id), `${BASE}/projects/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(updates),
  });
  await throwIfNotOk(res, "Failed to update project");
  return res.json();
}

export async function deleteProject(id: string): Promise<void> {
  const res = await apiFetch(machineIdForProject(id), `${BASE}/projects/${id}`, {
    method: "DELETE",
  });
  await throwIfNotOk(res, "Failed to delete project");
}

export async function listPresetDefinitions(): Promise<PresetDefinition[]> {
  const res = await fetchWithRetry(`${BASE}/preset-definitions`);
  await throwIfNotOk(res, "Failed to list preset definitions");
  return res.json();
}

export async function healthCheck(): Promise<{ status: string }> {
  const res = await fetchWithRetry(`${BASE}/health`);
  await throwIfNotOk(res, "Health check failed");
  return res.json();
}

export async function getDiskStats(): Promise<DiskStats> {
  const res = await fetchWithRetry(`${BASE}/storage/disk`);
  await throwIfNotOk(res, "Failed to fetch disk stats");
  return res.json();
}

export async function getStorageUsage(refresh = false): Promise<StorageUsage> {
  const res = await fetchWithRetry(`${BASE}/storage/usage${refresh ? "?refresh=1" : ""}`);
  await throwIfNotOk(res, "Failed to fetch storage usage");
  return res.json();
}

export async function deleteOrphanedWorktree(path: string): Promise<void> {
  const res = await fetch(`${BASE}/storage/worktrees?path=${encodeURIComponent(path)}`, {
    method: "DELETE",
  });
  await throwIfNotOk(res, "Failed to delete worktree");
}

/**
 * Reclaim the on-disk artifacts of these sessions — worktree, browser profile,
 * scratchpad — keeping each session row and git branch, so the session stays
 * resumable. The server re-plans from its own snapshot, so a session that woke
 * up since the page last refreshed comes back in `skipped` rather than being
 * removed; the caller reports what actually happened, never what it asked for.
 */
export async function reclaimSessions(sessionIds: string[]): Promise<ReclaimResponse> {
  const res = await fetch(`${BASE}/storage/reclaim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sessionIds } satisfies ReclaimRequest),
  });
  await throwIfNotOk(res, "Failed to reclaim sessions");
  return res.json();
}

export interface DirectoryEntry {
  name: string;
  path: string;
  isGitRepo: boolean;
}

export interface BrowseResult {
  path: string;
  parent: string;
  entries: DirectoryEntry[];
}

export interface PathValidation {
  exists: boolean;
  isDirectory: boolean;
  parentExists: boolean;
}

export async function validatePath(path: string, machineId?: string): Promise<PathValidation> {
  const res = await machineFetchWithRetry(
    machineId,
    `${BASE}/filesystem/validate?path=${encodeURIComponent(path)}`,
  );
  await throwIfNotOk(res, "Failed to validate path");
  return res.json();
}

export async function browseDirectory(path?: string, machineId?: string): Promise<BrowseResult> {
  const params = path ? `?path=${encodeURIComponent(path)}` : "";
  const res = await machineFetchWithRetry(machineId, `${BASE}/filesystem/browse${params}`);
  await throwIfNotOk(res, "Failed to browse directory");
  return res.json();
}

// --- Project file browser ---

export interface FileEntry {
  name: string;
  isDir: boolean;
  size: number;
  modTime: string;
}

export interface FileListResult {
  path: string;
  entries: FileEntry[];
}

export async function listProjectFiles(projectId: string, path = ""): Promise<FileListResult> {
  const params = path ? `?path=${encodeURIComponent(path)}` : "";
  const res = await machineFetchWithRetry(
    machineIdForProject(projectId),
    `${BASE}/projects/${projectId}/files${params}`,
  );
  await throwIfNotOk(res, "Failed to list files");
  return res.json();
}

export async function getFileContent(projectId: string, path: string): Promise<string> {
  const res = await machineFetchWithRetry(
    machineIdForProject(projectId),
    `${BASE}/projects/${projectId}/files/content?path=${encodeURIComponent(path)}`,
  );
  await throwIfNotOk(res, "Failed to fetch file content");
  return res.text();
}

/** Fetches a project file as a Blob — used for <img> previews, which cannot
 *  carry the Authorization header a remote machine needs. Callers own the
 *  object URL lifecycle. */
export async function getFileBlob(projectId: string, path: string): Promise<Blob> {
  const res = await machineFetchWithRetry(
    machineIdForProject(projectId),
    `${BASE}/projects/${projectId}/files/content?path=${encodeURIComponent(path)}`,
  );
  await throwIfNotOk(res, "Failed to fetch file");
  return res.blob();
}
