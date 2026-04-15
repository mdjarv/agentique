import { useMemo, useRef } from "react";
import { useShallow } from "zustand/shallow";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import { getWorstSessionState } from "~/lib/session/priority";
import { useAppStore } from "~/stores/app-store";
import type { SessionData, SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import type { FolderGroup, ProjectEntry } from "./types";

/** Lightweight snapshot of session metadata used to gate sidebar recomputation. */
interface SessionSummary {
  meta: SessionMetadata;
  hasPending: boolean;
}

function getSessionPriority(data: SessionData): number {
  if (data.pendingApproval || data.pendingQuestion) return 0;
  if (data.meta.state === "running") return 1;
  if (data.meta.state === "idle" && !data.meta.completedAt) return 2;
  if (data.meta.state === "failed") return 3;
  return 10;
}

/** Compare two summary maps by meta reference + pending flag.
 *  Returns true when nothing relevant to folder grouping changed. */
function summariesEqual(
  a: Record<string, SessionSummary>,
  b: Record<string, SessionSummary>,
): boolean {
  const aKeys = Object.keys(a);
  if (aKeys.length !== Object.keys(b).length) return false;
  for (const key of aKeys) {
    const av = a[key];
    const bv = b[key];
    if (!av || !bv || av.meta !== bv.meta || av.hasPending !== bv.hasPending) return false;
  }
  return true;
}

/** Selector that extracts session metadata summaries with stable references.
 *  Returns the same object reference when only turns/events/streaming change,
 *  preventing useFolderGroups from recomputing the entire sidebar tree. */
function useSessionSummaries(): Record<string, SessionSummary> {
  const prevRef = useRef<Record<string, SessionSummary>>({});
  return useChatStore((s) => {
    const next: Record<string, SessionSummary> = {};
    for (const [id, data] of Object.entries(s.sessions) as [string, SessionData][]) {
      next[id] = { meta: data.meta, hasPending: !!(data.pendingApproval || data.pendingQuestion) };
    }
    if (summariesEqual(prevRef.current, next)) return prevRef.current;
    prevRef.current = next;
    return next;
  });
}

export function useFolderGroups(): {
  folders: FolderGroup[];
  ungrouped: ProjectEntry[];
  emptyFolders: string[];
} {
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const summaries = useSessionSummaries();
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    // Read full SessionData from the store for downstream consumers that need it.
    // The memo only recomputes when summaries change (metadata + pending flags),
    // not on turn/event/streaming updates.
    const sessions = useChatStore.getState().sessions;
    const entries: ProjectEntry[] = [];

    for (const project of projects) {
      const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
      const active: Array<{ id: string; data: SessionData }> = [];
      const completed: Array<{ id: string; data: SessionData }> = [];

      for (const id of Object.keys(summaries)) {
        const data = sessions[id];
        if (!data || data.meta.projectId !== project.id) continue;
        if (data.meta.completedAt) completed.push({ id, data });
        else active.push({ id, data });
      }

      active.sort((a, b) => {
        const aPri = getSessionPriority(a.data);
        const bPri = getSessionPriority(b.data);
        if (aPri !== bPri) return aPri - bPri;
        return (
          new Date(b.data.meta.updatedAt ?? b.data.meta.createdAt).getTime() -
          new Date(a.data.meta.updatedAt ?? a.data.meta.createdAt).getTime()
        );
      });

      const worstState = getWorstSessionState(active);
      entries.push({ project, color, active, completed, worstState });
    }

    const folderMap = new Map<string, ProjectEntry[]>();
    const ungrouped: ProjectEntry[] = [];

    for (const entry of entries) {
      const folder = entry.project.folder;
      if (!folder) {
        ungrouped.push(entry);
        continue;
      }
      if (!folderMap.has(folder)) folderMap.set(folder, []);
      folderMap.get(folder)?.push(entry);
    }

    const folders = Array.from(folderMap.entries())
      .map(([name, projs]) => ({ name, projects: projs }))
      .sort((a, b) => {
        const aMin = Math.min(...a.projects.map((p) => p.project.sort_order || 9999));
        const bMin = Math.min(...b.projects.map((p) => p.project.sort_order || 9999));
        return aMin - bMin;
      });

    return { folders, ungrouped, emptyFolders: [] };
  }, [projects, projectIds, summaries, resolvedTheme]);
}
