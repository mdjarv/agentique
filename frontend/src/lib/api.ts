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

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`${BASE}/projects/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error("Failed to delete project");
}

export async function healthCheck(): Promise<{ status: string }> {
  const res = await fetch(`${BASE}/health`);
  if (!res.ok) throw new Error("Health check failed");
  return res.json();
}
