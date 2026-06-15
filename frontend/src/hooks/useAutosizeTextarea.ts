import { type RefObject, useLayoutEffect } from "react";

/**
 * Single source of truth for textarea auto-grow. Runs synchronously after every
 * value change (before paint, so there is no flicker) and resizes the element to
 * fit its content up to `maxHeight`, after which the textarea scrolls.
 *
 * Keying on `value` — rather than resizing imperatively from each mutation site —
 * eliminates the dual-source-of-truth bug where state-driven updates (speech,
 * stash restore, programmatic setText) and DOM-driven updates (typing) resized
 * via different code paths and could disagree.
 */
export function useAutosizeTextarea(
  ref: RefObject<HTMLTextAreaElement | null>,
  value: string,
  maxHeight = 200,
) {
  // biome-ignore lint/correctness/useExhaustiveDependencies: `value` is a trigger-only dep — the new content lands in the DOM before this runs, and we measure scrollHeight rather than read `value`, but the resize must re-run whenever it changes.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`;
  }, [ref, value, maxHeight]);
}
