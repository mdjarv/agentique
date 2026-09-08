import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";
import {
  installFileDropGuard,
  isDropTargetActive,
  registerFileSink,
  subscribeFileDrag,
} from "~/lib/file-drop";

/**
 * Installs the app-wide guard so no file drop can navigate the page away. Call
 * it once, from the shell — see `lib/file-drop.ts` for why it is separate from
 * having somewhere to put the files.
 */
export function useFileDropGuard(): void {
  useEffect(() => installFileDropGuard(), []);
}

interface FileDropTargetOptions {
  /** Registers the sink. False still swallows the drop; it just goes nowhere. */
  enabled: boolean;
  onFiles: (files: File[]) => void;
}

/**
 * Claims window-wide file drops for this component, and reports whether a drag
 * is currently over the window heading here — which is what the overlay draws.
 */
export function useFileDropTarget({ enabled, onFiles }: FileDropTargetOptions): boolean {
  const onFilesRef = useRef(onFiles);
  onFilesRef.current = onFiles;

  // The sink *is* this component's slot in the stack, so its identity must
  // survive a re-render: re-registering on every new `onFiles` would move the
  // slot to the top of the stack for no reason.
  const sinkRef = useRef<((files: File[]) => void) | null>(null);
  if (!sinkRef.current) sinkRef.current = (files: File[]) => onFilesRef.current(files);
  const sink = sinkRef.current;

  // A target works on its own, in a test or a surface mounted outside the shell.
  useEffect(() => installFileDropGuard(), []);

  useEffect(() => {
    if (!enabled) return;
    return registerFileSink(sink);
  }, [enabled, sink]);

  const getSnapshot = useCallback(() => isDropTargetActive(sink), [sink]);
  return useSyncExternalStore(subscribeFileDrag, getSnapshot, () => false);
}
