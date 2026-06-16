import { throwIfNotOk } from "~/lib/http";

const BASE = "/api/brain";

export interface Memory {
  id: string;
  scope: string;
  text: string;
  category: string;
  source: string;
  pinned: boolean;
  locked: boolean;
  uses: number;
  createdAt: string;
  updatedAt: string;
  derivedFrom?: string[];
  related?: string[];
}

export interface BrainStatus {
  semantic: boolean;
}

export interface SearchResult {
  pinned: Memory[];
  recalled: Memory[];
}

export interface ConsolidateReport {
  scope: string;
  promoted: Memory[] | null;
  rewritten: { before: Memory; after: Memory }[] | null;
  abstracted: Memory[] | null;
  deleted: Memory[] | null;
  decayed: Memory[] | null;
  capturesConsumed: string[] | null;
  skipped: boolean;
  reorgRefused: boolean;
}

export async function getStatus(): Promise<BrainStatus> {
  const res = await fetch(`${BASE}/status`);
  await throwIfNotOk(res, "Failed to load brain status");
  return res.json();
}

export async function listMemories(scope?: string): Promise<Memory[]> {
  const q = scope ? `?scope=${encodeURIComponent(scope)}` : "";
  const res = await fetch(`${BASE}/memories${q}`);
  await throwIfNotOk(res, "Failed to list memories");
  return res.json();
}

export interface CreateMemoryInput {
  scope?: string;
  text: string;
  category?: string;
}

export async function createMemory(input: CreateMemoryInput): Promise<Memory> {
  const res = await fetch(`${BASE}/memories`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  await throwIfNotOk(res, "Failed to create memory");
  return res.json();
}

export async function updateMemory(
  id: string,
  input: { text?: string; category?: string },
): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  await throwIfNotOk(res, "Failed to update memory");
  return res.json();
}

export async function deleteMemory(id: string): Promise<void> {
  const res = await fetch(`${BASE}/memories/${id}`, { method: "DELETE" });
  await throwIfNotOk(res, "Failed to delete memory");
}

export async function setPinned(id: string, pinned: boolean): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/pin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pinned }),
  });
  await throwIfNotOk(res, "Failed to update pin");
  return res.json();
}

export async function setLocked(id: string, locked: boolean): Promise<Memory> {
  const res = await fetch(`${BASE}/memories/${id}/lock`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ locked }),
  });
  await throwIfNotOk(res, "Failed to update lock");
  return res.json();
}

export async function searchMemories(q: string, scope?: string): Promise<SearchResult> {
  const params = new URLSearchParams({ q });
  if (scope) params.set("scope", scope);
  const res = await fetch(`${BASE}/search?${params.toString()}`);
  await throwIfNotOk(res, "Failed to search memories");
  return res.json();
}

export async function consolidate(scope: string): Promise<ConsolidateReport> {
  const res = await fetch(`${BASE}/consolidate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scope }),
  });
  await throwIfNotOk(res, "Failed to consolidate");
  return res.json();
}
