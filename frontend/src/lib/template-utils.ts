import type { EffortLevel } from "~/lib/composer-constants";
import type { BehaviorPresets, PromptTemplate } from "~/lib/generated-types";
import type { ModelId } from "~/lib/session/actions";
import type { AutoApproveMode } from "~/stores/chat-store";

export interface TemplateSettings {
  model?: ModelId;
  effort?: EffortLevel;
  worktree?: boolean;
  planMode?: boolean;
  autoApproveMode?: AutoApproveMode;
  behaviorPresets?: Partial<BehaviorPresets>;
}

const VARIABLE_RE = /\{\{(\w+)\}\}/g;

export function parseSettings(raw: string): TemplateSettings {
  if (!raw || raw === "{}") return {};
  try {
    return JSON.parse(raw) as TemplateSettings;
  } catch {
    return {};
  }
}

export function stringifySettings(s: TemplateSettings): string {
  return JSON.stringify(s);
}

export function parseTags(raw: string): string[] {
  if (!raw || raw === "[]") return [];
  try {
    return JSON.parse(raw) as string[];
  } catch {
    return [];
  }
}

export function stringifyTags(tags: string[]): string {
  return JSON.stringify(tags);
}

export function extractVariables(content: string): string[] {
  const seen = new Set<string>();
  const vars: string[] = [];
  for (const match of content.matchAll(VARIABLE_RE)) {
    const name = match[1] ?? "";
    if (name && !seen.has(name)) {
      seen.add(name);
      vars.push(name);
    }
  }
  return vars;
}

export function substituteVariables(content: string, values: Record<string, string>): string {
  return content.replace(VARIABLE_RE, (_, name: string) => values[name] ?? `{{${name}}}`);
}

export function formatVariableName(name: string): string {
  return name
    .replace(/_/g, " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

export function templateHasVariables(tmpl: PromptTemplate): boolean {
  return VARIABLE_RE.test(tmpl.content);
}
