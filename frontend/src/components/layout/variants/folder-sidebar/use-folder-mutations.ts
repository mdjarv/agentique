/**
 * Folder mutations — project↔folder moves plus the local-only bookkeeping for
 * folders that have no projects yet (created in the UI but not persisted to any
 * project record) and the inline rename/create UI state.
 *
 * Renaming a folder spans three concerns: project records (here), persisted
 * order, and expanded state. This hook owns only the project + local-empty
 * parts; the orchestrator (FolderSidebar) sequences the other two so the hooks
 * stay free of cross-dependencies.
 */
import { useCallback, useState } from "react";
import { updateProject } from "~/lib/api";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useUIStore } from "~/stores/ui-store";

export interface FolderMutations {
  /** Name of the folder currently being inline-renamed, or null. */
  renamingFolder: string | null;
  setRenamingFolder: (name: string | null) => void;
  /** Whether the footer "new folder" inline input is open. */
  creatingFolder: boolean;
  setCreatingFolder: (creating: boolean) => void;
  /** Folders created locally that have no projects yet. */
  localEmptyFolders: string[];
  /** Persist a project's folder assignment (`""` = ungrouped). */
  setProjectFolder: (projectId: string, folder: string) => Promise<void>;
  /** Move every project out of `oldName` into `newName` + rename the local-empty entry. */
  renameFolderProjects: (oldName: string, newName: string) => Promise<void>;
  /** Empty `folderName` (move its projects to ungrouped) and forget it locally. */
  deleteFolder: (folderName: string) => Promise<void>;
  /** Register a new (empty) folder and expand it. */
  createFolder: (name: string) => void;
}

export function useFolderMutations(projects: Project[]): FolderMutations {
  const setFolderExpanded = useUIStore((s) => s.setFolderExpanded);

  const [renamingFolder, setRenamingFolder] = useState<string | null>(null);
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [localEmptyFolders, setLocalEmptyFolders] = useState<string[]>([]);

  const setProjectFolder = useCallback(async (projectId: string, folder: string) => {
    try {
      const updated = await updateProject(projectId, { folder });
      useAppStore.getState().updateProject(updated);
    } catch (e) {
      console.error("Failed to update project folder:", e);
    }
  }, []);

  const renameFolderProjects = useCallback(
    async (oldName: string, newName: string) => {
      const inFolder = projects.filter((p) => p.folder === oldName);
      await Promise.all(inFolder.map((p) => setProjectFolder(p.id, newName)));
      setRenamingFolder(null);
      setLocalEmptyFolders((prev) => prev.map((f) => (f === oldName ? newName : f)));
    },
    [projects, setProjectFolder],
  );

  const deleteFolder = useCallback(
    async (folderName: string) => {
      const inFolder = projects.filter((p) => p.folder === folderName);
      await Promise.all(inFolder.map((p) => setProjectFolder(p.id, "")));
      setLocalEmptyFolders((prev) => prev.filter((f) => f !== folderName));
    },
    [projects, setProjectFolder],
  );

  const createFolder = useCallback(
    (name: string) => {
      setCreatingFolder(false);
      if (!name) return;
      setLocalEmptyFolders((prev) => [...prev, name]);
      setFolderExpanded(name, true);
    },
    [setFolderExpanded],
  );

  return {
    renamingFolder,
    setRenamingFolder,
    creatingFolder,
    setCreatingFolder,
    localEmptyFolders,
    setProjectFolder,
    renameFolderProjects,
    deleteFolder,
    createFolder,
  };
}
