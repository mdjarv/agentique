import type { PromptTemplate } from "~/lib/generated-types";

const BASE = "/api";

export async function listTemplates(): Promise<PromptTemplate[]> {
  const res = await fetch(`${BASE}/templates`);
  if (!res.ok) throw new Error("Failed to list templates");
  return res.json();
}

export async function getTemplate(id: string): Promise<PromptTemplate> {
  const res = await fetch(`${BASE}/templates/${id}`);
  if (!res.ok) throw new Error("Failed to get template");
  return res.json();
}

export interface CreateTemplateInput {
  name: string;
  description?: string;
  content: string;
  settings?: string;
  tags?: string;
}

export async function createTemplate(input: CreateTemplateInput): Promise<PromptTemplate> {
  const res = await fetch(`${BASE}/templates`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error("Failed to create template");
  return res.json();
}

export interface UpdateTemplateInput {
  name?: string;
  description?: string;
  content?: string;
  settings?: string;
  tags?: string;
}

export async function updateTemplate(
  id: string,
  input: UpdateTemplateInput,
): Promise<PromptTemplate> {
  const res = await fetch(`${BASE}/templates/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error("Failed to update template");
  return res.json();
}

export async function deleteTemplate(id: string): Promise<void> {
  const res = await fetch(`${BASE}/templates/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error("Failed to delete template");
}
