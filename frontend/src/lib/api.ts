import type { BehaviorPresets, PresetDefinition } from "~/lib/generated-types";
import type { Project } from "~/lib/types";

const BASE = "/api";

export async function listProjects(): Promise<Project[]> {
  const res = await fetch(`${BASE}/projects`);
  if (!res.ok) throw new Error("Failed to list projects");
  return res.json();
}

export async function createProject(name: string, path: string): Promise<Project> {
  const res = await fetch(`${BASE}/projects`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, path }),
  });
  if (!res.ok) throw new Error("Failed to create project");
  return res.json();
}

export async function updateProject(
  id: string,
  updates: { name?: string; slug?: string; behaviorPresets?: BehaviorPresets },
): Promise<Project> {
  const res = await fetch(`${BASE}/projects/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(updates),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error ?? "Failed to update project");
  }
  return res.json();
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`${BASE}/projects/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error("Failed to delete project");
}

export async function listPresetDefinitions(): Promise<PresetDefinition[]> {
  const res = await fetch(`${BASE}/preset-definitions`);
  if (!res.ok) throw new Error("Failed to list preset definitions");
  return res.json();
}

export async function healthCheck(): Promise<{ status: string }> {
  const res = await fetch(`${BASE}/health`);
  if (!res.ok) throw new Error("Health check failed");
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

export async function validatePath(path: string): Promise<PathValidation> {
  const res = await fetch(`${BASE}/filesystem/validate?path=${encodeURIComponent(path)}`);
  if (!res.ok) throw new Error("Failed to validate path");
  return res.json();
}

export async function browseDirectory(path?: string): Promise<BrowseResult> {
  const params = path ? `?path=${encodeURIComponent(path)}` : "";
  const res = await fetch(`${BASE}/filesystem/browse${params}`);
  if (!res.ok) throw new Error("Failed to browse directory");
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
  const res = await fetch(`${BASE}/projects/${projectId}/files${params}`);
  if (!res.ok) throw new Error("Failed to list files");
  return res.json();
}

export async function getFileContent(projectId: string, path: string): Promise<string> {
  const res = await fetch(
    `${BASE}/projects/${projectId}/files/content?path=${encodeURIComponent(path)}`,
  );
  if (!res.ok) throw new Error("Failed to fetch file content");
  return res.text();
}

export function fileContentUrl(projectId: string, path: string): string {
  return `${BASE}/projects/${projectId}/files/content?path=${encodeURIComponent(path)}`;
}
