/**
 * Folder ordering — persisted in localStorage, reconciled against the live
 * folder set. Stale names are dropped, new ones appended, so the stored order
 * survives folder renames/deletions without going stale.
 */
import { useCallback, useMemo, useState } from "react";
import type { FolderGroup } from "./types";

const FOLDER_ORDER_KEY = "agentique-folder-order";

function loadFolderOrder(): string[] {
  try {
    const stored = localStorage.getItem(FOLDER_ORDER_KEY);
    return stored ? JSON.parse(stored) : [];
  } catch {
    return [];
  }
}

function saveFolderOrder(order: string[]): void {
  localStorage.setItem(FOLDER_ORDER_KEY, JSON.stringify(order));
}

/** Reconcile stored order with current folder names — drop stale, append new. */
export function reconcileFolderOrder(stored: string[], current: string[]): string[] {
  const currentSet = new Set(current);
  const reconciled = stored.filter((n) => currentSet.has(n));
  const reconciledSet = new Set(reconciled);
  for (const name of current) {
    if (!reconciledSet.has(name)) reconciled.push(name);
  }
  return reconciled;
}

export interface FolderOrder {
  /** `allFolders` sorted by the persisted order. */
  orderedFolders: FolderGroup[];
  /** Move `activeName` to `overName`'s slot and persist. */
  moveFolder: (activeName: string, overName: string) => void;
  /** Rename a folder in the persisted order, preserving its position. */
  renameInFolderOrder: (oldName: string, newName: string) => void;
}

export function useFolderOrder(allFolders: FolderGroup[]): FolderOrder {
  const [folderOrder, setFolderOrder] = useState<string[]>(loadFolderOrder);

  const orderedFolders = useMemo(() => {
    const currentNames = allFolders.map((f) => f.name);
    const reconciled = reconcileFolderOrder(folderOrder, currentNames);
    const orderMap = new Map(reconciled.map((name, idx) => [name, idx]));
    return [...allFolders].sort(
      (a, b) => (orderMap.get(a.name) ?? Infinity) - (orderMap.get(b.name) ?? Infinity),
    );
  }, [allFolders, folderOrder]);

  const moveFolder = useCallback(
    (activeName: string, overName: string) => {
      setFolderOrder((prev) => {
        const currentNames = orderedFolders.map((f) => f.name);
        const base = reconcileFolderOrder(prev, currentNames);
        const oldIdx = base.indexOf(activeName);
        const newIdx = base.indexOf(overName);
        if (oldIdx < 0 || newIdx < 0) return prev;
        base.splice(oldIdx, 1);
        base.splice(newIdx, 0, activeName);
        saveFolderOrder(base);
        return base;
      });
    },
    [orderedFolders],
  );

  const renameInFolderOrder = useCallback((oldName: string, newName: string) => {
    setFolderOrder((prev) => {
      const next = prev.map((f) => (f === oldName ? newName : f));
      saveFolderOrder(next);
      return next;
    });
  }, []);

  return { orderedFolders, moveFolder, renameInFolderOrder };
}
