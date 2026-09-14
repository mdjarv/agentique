import type { RefObject } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { type FileEntry, listProjectFiles } from "~/lib/api";
import { dirCompletions, splitDirQuery } from "~/lib/dir-complete";
import {
  type CommandFile,
  type CommandsResult,
  getCommands,
  getTrackedFiles,
  type TrackedFilesResult,
} from "~/lib/project-actions";
import { useIsFolderProject } from "~/lib/project-kind";
import { useWebSocket } from "./useWebSocket";

export interface AutocompleteItem {
  label: string;
  value: string;
  category: "file" | "command";
  source?: "project" | "user";
  description?: string;
  /** A directory: accepting it continues completion inside it. */
  isDir?: boolean;
}

interface AutocompleteState {
  isOpen: boolean;
  items: AutocompleteItem[];
  selectedIndex: number;
  triggerType: "@" | "/" | null;
}

const CLOSED: AutocompleteState = {
  isOpen: false,
  items: [],
  selectedIndex: 0,
  triggerType: null,
};

const CACHE_TTL = 60_000;
const MAX_RESULTS = 20;

interface UseAutocompleteOptions {
  projectId: string;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  text: string;
  onTextChange: (text: string) => void;
}

export function useAutocomplete({
  projectId,
  textareaRef,
  text,
  onTextChange,
}: UseAutocompleteOptions) {
  const ws = useWebSocket();
  // A plain folder has no tracked-file list; it completes one directory at a
  // time from the file browser's listing instead (`lib/dir-complete.ts`).
  const folder = useIsFolderProject(projectId);
  const [state, setState] = useState<AutocompleteState>(CLOSED);
  const [cachedFiles, setCachedFiles] = useState<string[] | null>(null);
  const [dirListings, setDirListings] = useState<Record<string, FileEntry[]>>({});
  const dirFetchRef = useRef<{ inFlight: Set<string>; at: Map<string, number> }>({
    inFlight: new Set(),
    at: new Map(),
  });
  const [cachedCommands, setCachedCommands] = useState<CommandFile[] | null>(null);
  const fetchingRef = useRef({ files: false, commands: false });
  const cacheTimesRef = useRef({ filesAt: 0, commandsAt: 0 });
  const prevProjectRef = useRef(projectId);

  // Reset cache when project changes.
  if (prevProjectRef.current !== projectId) {
    prevProjectRef.current = projectId;
    setCachedFiles(null);
    setCachedCommands(null);
    setDirListings({});
    dirFetchRef.current = { inFlight: new Set(), at: new Map() };
    cacheTimesRef.current = { filesAt: 0, commandsAt: 0 };
  }

  const close = useCallback(() => setState(CLOSED), []);

  const accept = useCallback(
    (item: AutocompleteItem) => {
      const ta = textareaRef.current;
      if (!ta) return;

      const cursor = ta.selectionStart;
      const trigger = detectTrigger(text, cursor);
      if (!trigger) return;

      const before = text.slice(0, trigger.start);
      const after = text.slice(cursor);
      // A directory ends the insertion at its slash, so completion carries on
      // into it rather than closing on a path nobody meant to stop at.
      const insertion =
        trigger.type === "@" ? `@${item.value}${item.isDir ? "" : " "}` : `/${item.value} `;
      const newText = before + insertion + after;
      onTextChange(newText);

      const newCursor = before.length + insertion.length;
      requestAnimationFrame(() => {
        ta.setSelectionRange(newCursor, newCursor);
      });

      close();
    },
    [text, textareaRef, onTextChange, close],
  );

  // Fetch helpers — populate state to re-trigger compute.
  const fetchFiles = useCallback(async () => {
    if (fetchingRef.current.files) return;
    if (Date.now() - cacheTimesRef.current.filesAt < CACHE_TTL) return;
    fetchingRef.current.files = true;
    try {
      const result: TrackedFilesResult = await getTrackedFiles(ws, projectId);
      setCachedFiles(result.files);
      cacheTimesRef.current.filesAt = Date.now();
    } catch {
      // Non-critical.
    } finally {
      fetchingRef.current.files = false;
    }
  }, [ws, projectId]);

  const fetchDir = useCallback(
    async (dir: string) => {
      const f = dirFetchRef.current;
      if (f.inFlight.has(dir)) return;
      if (Date.now() - (f.at.get(dir) ?? 0) < CACHE_TTL) return;
      f.inFlight.add(dir);
      let entries: FileEntry[] = [];
      try {
        entries = (await listProjectFiles(projectId, dir)).entries;
      } catch {
        // A directory that does not exist completes to nothing, and is not
        // asked for again until the cache expires.
      } finally {
        f.inFlight.delete(dir);
        f.at.set(dir, Date.now());
      }
      setDirListings((prev) => ({ ...prev, [dir]: entries }));
    },
    [projectId],
  );

  const fetchCommands = useCallback(async () => {
    if (fetchingRef.current.commands) return;
    if (Date.now() - cacheTimesRef.current.commandsAt < CACHE_TTL) return;
    fetchingRef.current.commands = true;
    try {
      const result: CommandsResult = await getCommands(ws, projectId);
      setCachedCommands(result.commands);
      cacheTimesRef.current.commandsAt = Date.now();
    } catch {
      // Non-critical.
    } finally {
      fetchingRef.current.commands = false;
    }
  }, [ws, projectId]);

  // Recompute autocomplete on text or data changes.
  useEffect(() => {
    const ta = textareaRef.current;
    if (!ta) return;

    const cursor = ta.selectionStart;
    const trigger = detectTrigger(text, cursor);

    if (!trigger) {
      setState((prev) => (prev.isOpen ? CLOSED : prev));
      return;
    }

    const query = trigger.query;

    if (trigger.type === "@" && folder) {
      const dq = splitDirQuery(query);
      const listing = dirListings[dq.dir];
      if (listing === undefined) {
        fetchDir(dq.dir);
        return;
      }
      const items = dirCompletions(listing, dq, MAX_RESULTS)
        .map(
          (c): AutocompleteItem => ({
            label: c.value,
            value: c.value,
            category: "file",
            isDir: c.isDir,
          }),
        )
        .reverse();
      setState({
        isOpen: items.length > 0,
        items,
        selectedIndex: items.length - 1,
        triggerType: "@",
      });
    } else if (trigger.type === "@") {
      if (cachedFiles === null) {
        fetchFiles();
        return;
      }
      const items = filterFiles(cachedFiles, query).reverse();
      setState({
        isOpen: items.length > 0,
        items,
        selectedIndex: items.length - 1,
        triggerType: "@",
      });
    } else {
      if (cachedCommands === null) {
        fetchCommands();
        return;
      }
      const items = filterCommands(cachedCommands, query).reverse();
      setState({
        isOpen: items.length > 0,
        items,
        selectedIndex: items.length - 1,
        triggerType: "/",
      });
    }
  }, [
    text,
    folder,
    dirListings,
    cachedFiles,
    cachedCommands,
    textareaRef,
    fetchDir,
    fetchFiles,
    fetchCommands,
  ]);

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (!state.isOpen || state.items.length === 0) return;

      switch (e.key) {
        case "ArrowDown": {
          e.preventDefault();
          setState((prev) => ({
            ...prev,
            selectedIndex: (prev.selectedIndex + 1) % prev.items.length,
          }));
          break;
        }
        case "ArrowUp": {
          e.preventDefault();
          setState((prev) => ({
            ...prev,
            selectedIndex: (prev.selectedIndex - 1 + prev.items.length) % prev.items.length,
          }));
          break;
        }
        case "Enter":
        case "Tab": {
          e.preventDefault();
          const item = state.items[state.selectedIndex];
          if (item) accept(item);
          break;
        }
        case "Escape": {
          e.preventDefault();
          close();
          break;
        }
      }
    },
    [state.isOpen, state.items, state.selectedIndex, accept, close],
  );

  return { ...state, onKeyDown, accept, close };
}

// --- Helpers ---

interface Trigger {
  type: "@" | "/";
  start: number;
  query: string;
}

function detectTrigger(text: string, cursor: number): Trigger | null {
  if (cursor === 0) return null;

  for (let i = cursor - 1; i >= 0; i--) {
    const ch = text[i];

    if (ch === " " || ch === "\n" || ch === "\t") return null;

    if (ch === "@") {
      if (i > 0 && text[i - 1] !== " " && text[i - 1] !== "\n" && text[i - 1] !== "\t") {
        return null;
      }
      return { type: "@", start: i, query: text.slice(i + 1, cursor) };
    }

    if (ch === "/") {
      if (i !== 0) return null;
      return { type: "/", start: 0, query: text.slice(1, cursor) };
    }
  }

  return null;
}

function filterFiles(files: string[], query: string): AutocompleteItem[] {
  if (query === "") {
    return files.slice(0, MAX_RESULTS).map(fileToItem);
  }

  const q = query.toLowerCase();
  const prefixMatches: AutocompleteItem[] = [];
  const containsMatches: AutocompleteItem[] = [];

  for (const f of files) {
    const lower = f.toLowerCase();
    if (!lower.includes(q)) continue;

    const basename = f.slice(f.lastIndexOf("/") + 1).toLowerCase();
    if (basename.startsWith(q)) {
      prefixMatches.push(fileToItem(f));
    } else {
      containsMatches.push(fileToItem(f));
    }

    if (prefixMatches.length + containsMatches.length >= MAX_RESULTS) break;
  }

  return [...prefixMatches, ...containsMatches].slice(0, MAX_RESULTS);
}

function fileToItem(f: string): AutocompleteItem {
  return { label: f, value: f, category: "file" };
}

function filterCommands(commands: CommandFile[], query: string): AutocompleteItem[] {
  const q = query.toLowerCase();
  const items: AutocompleteItem[] = [];
  for (const cmd of commands) {
    if (q && !cmd.name.toLowerCase().startsWith(q)) continue;
    items.push({
      label: cmd.name,
      value: cmd.name,
      category: "command",
      source: cmd.source,
      description: cmd.description,
    });
    if (items.length >= MAX_RESULTS) break;
  }
  return items;
}
