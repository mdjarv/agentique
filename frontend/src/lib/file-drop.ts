/**
 * Window-level file drag-and-drop.
 *
 * A file dragged into the app has exactly one destination — the next message —
 * so the drop target is the window rather than the composer's own box. That box
 * is 48px on a screen of hundreds, which made it something to aim at, and a
 * near miss was not a no-op: an unclaimed drop is the browser's, and the
 * browser navigates away to the file, taking the session view with it.
 *
 * Two jobs, deliberately separable:
 *
 * - The **guard** swallows every file drop, on every route, so none can
 *   navigate. It is installed once from the app shell.
 * - A **sink** is somewhere a drop can actually go. The composer registers one
 *   while it is mounted and attachments are supported. With no sink the drop is
 *   still swallowed, and silently: a page that never offered a paperclip is not
 *   refusing anything.
 *
 * Sinks form a stack and the most recently registered one wins, so a composer
 * opened over another receives the drop rather than fighting it — the same
 * serial-channel argument the speech singleton makes.
 */

type FileSink = (files: File[]) => void;

const sinks: FileSink[] = [];
const listeners = new Set<() => void>();

/**
 * Nesting depth of the drag over the document. `dragenter` and `dragleave` fire
 * for every element the pointer crosses, and enter on the new element arrives
 * *before* leave on the old one, so a counter is the only reading that does not
 * flicker off between two adjacent children. Zero means the drag left the
 * window.
 */
let depth = 0;
let dragging = false;
let installs = 0;

function emit() {
  for (const listener of listeners) listener();
}

function setDragging(next: boolean) {
  if (dragging === next) return;
  dragging = next;
  emit();
}

function reset() {
  depth = 0;
  setDragging(false);
}

/**
 * A file drag, as opposed to dragged text or an element the app itself is
 * moving. Everything here is gated on it, so a rail reorder never draws the
 * drop overlay and never has its drop swallowed.
 */
function carriesFiles(dt: DataTransfer | null): boolean {
  if (!dt) return false;
  for (const type of Array.from(dt.types)) {
    if (type === "Files") return true;
  }
  return false;
}

function activeSink(): FileSink | undefined {
  return sinks[sinks.length - 1];
}

function onDragEnter(e: DragEvent) {
  if (!carriesFiles(e.dataTransfer)) return;
  depth += 1;
  setDragging(true);
}

function onDragOver(e: DragEvent) {
  if (!carriesFiles(e.dataTransfer)) return;
  // The whole guard is this line: without a preventDefault on dragover the drop
  // belongs to the browser, whatever the drop handler does afterwards.
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = activeSink() ? "copy" : "none";
  // A drag that begins inside the window (dragging a file out of the page's own
  // content) can reach us without a dragenter. Adopt it rather than showing an
  // overlay-less accept.
  if (!dragging) {
    depth = 1;
    setDragging(true);
  }
}

function onDragLeave(e: DragEvent) {
  if (!carriesFiles(e.dataTransfer)) return;
  depth = Math.max(0, depth - 1);
  if (depth === 0) setDragging(false);
}

function onDrop(e: DragEvent) {
  if (!carriesFiles(e.dataTransfer)) return;
  e.preventDefault();
  reset();
  const sink = activeSink();
  if (!sink) return;
  const files = Array.from(e.dataTransfer?.files ?? []);
  if (files.length > 0) sink(files);
}

const GUARD_EVENTS = [
  ["dragenter", onDragEnter],
  ["dragover", onDragOver],
  ["dragleave", onDragLeave],
  ["drop", onDrop],
  ["dragend", reset],
] as const;

/** Installs the window listeners. Ref-counted, so callers need not coordinate. */
export function installFileDropGuard(): () => void {
  installs += 1;
  if (installs === 1) {
    for (const [name, handler] of GUARD_EVENTS) {
      window.addEventListener(name, handler as EventListener);
    }
  }
  return () => {
    installs -= 1;
    if (installs > 0) return;
    for (const [name, handler] of GUARD_EVENTS) {
      window.removeEventListener(name, handler as EventListener);
    }
    reset();
  };
}

/** Makes `sink` the destination for dropped files until the returned function runs. */
export function registerFileSink(sink: FileSink): () => void {
  sinks.push(sink);
  emit();
  return () => {
    const at = sinks.lastIndexOf(sink);
    if (at >= 0) sinks.splice(at, 1);
    emit();
  };
}

export function subscribeFileDrag(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** True while a file drag is over the window *and* `sink` is what would receive it. */
export function isDropTargetActive(sink: FileSink): boolean {
  return dragging && activeSink() === sink;
}
