/**
 * "Am I inside a modal layer?" — the one fact a portalled child needs in order
 * to scroll under a finger.
 *
 * A modal Dialog (and the Sheet built on it) locks scrolling with
 * react-remove-scroll, which cancels every touchmove whose target is outside
 * the locked element and its shards. A Popover portals to `document.body`, so
 * a palette opened from inside the mobile sidebar lands outside that set and
 * its list stops scrolling — silently, and only on touch: a desktop sidebar is
 * not in a Sheet, so a wheel over the same list never meets the lock.
 *
 * Radix's answer is `modal`: the inner layer mounts its own lock, which becomes
 * the innermost one and allows its own content. DropdownMenu already defaults
 * to modal for this reason; Popover does not, so it reads the answer from here
 * rather than every call site inside a Sheet having to remember.
 *
 * Portalling into the dialog instead would work for the Sheet and break the
 * centred Dialog, whose `translate-x-[-50%]` makes it a containing block for
 * the fixed-position popper.
 */
import { createContext, useContext } from "react";

const ModalLayerContext = createContext(false);

/** Marks its subtree as living inside a modal, scroll-locked layer. */
export function ModalLayerProvider({ children }: { children: React.ReactNode }) {
  return <ModalLayerContext.Provider value={true}>{children}</ModalLayerContext.Provider>;
}

/** True when the caller renders inside a modal Dialog or Sheet. */
export function useInModalLayer(): boolean {
  return useContext(ModalLayerContext);
}
