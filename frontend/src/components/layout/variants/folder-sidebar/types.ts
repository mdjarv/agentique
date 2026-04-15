import type { ProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import type { SessionData } from "~/stores/chat-store";
import type { BadgeState } from "../../session/SessionBadge";

export interface FolderGroup {
  name: string;
  projects: ProjectEntry[];
}

export interface ProjectEntry {
  project: Project;
  color: ProjectColor;
  active: SessionItem[];
  completed: SessionItem[];
  worstState: BadgeState | null;
}

export interface SessionItem {
  id: string;
  data: SessionData;
}

export interface TodoProgress {
  done: number;
  total: number;
}

/**
 * 5-unit (20px) column = matches indicator width (size-5).
 * Classes are listed explicitly so Tailwind can detect them at build time.
 * Dynamic template strings like `pl-${n}` are invisible to the scanner.
 */
const INDENT_CLASSES = ["pl-0", "pl-5", "pl-10", "pl-15", "pl-20", "pl-25"] as const;

export function indentClass(level: number): string {
  return INDENT_CLASSES[level] ?? INDENT_CLASSES[INDENT_CLASSES.length - 1]!;
}

/** Named levels for readability. */
export const LEVEL = {
  folder: 0,
  project: 0,
  session: 2,
  worker: 3,
} as const;

export const UNGROUPED = "";

/** Pixels per indent level — matches Tailwind's 5-unit (1.25rem = 20px) step. */
export const INDENT_PX = 20;
