/**
 * Dismissing the mobile sidebar is the router's job, not each link's.
 *
 * On mobile the sidebar is a Sheet over the whole viewport, so it IS the
 * navigation surface: arriving somewhere finishes its job, and leaving it
 * standing means the page you just asked for is behind an opaque panel. That
 * read as "the link does nothing", because the only visible evidence of the
 * navigation — the page — was covered.
 *
 * The rule lives at the router because the sidebar's destinations are not all
 * in its own tree. The ⋯ menu and the footer popover render into portals, so a
 * delegated click handler on the sheet never sees them, and a link added to
 * either would silently miss a per-control dismiss. One subscription covers
 * every destination the sidebar has and every one it grows.
 *
 * What it cannot see is a link to the page you are already on — no location
 * change, no callback. Those few controls call `dismissSidebar` directly.
 *
 * Desktop is untouched: `sidebarOpen` drives nothing there.
 */
import { useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";
import { useAppStore } from "~/stores/app-store";

/** Close the mobile sidebar, if it is open. A no-op on desktop. */
export function dismissSidebar(): void {
  const { sidebarOpen, setSidebarOpen } = useAppStore.getState();
  if (sidebarOpen) setSidebarOpen(false);
}

/** Dismiss the mobile sidebar whenever the location changes. */
export function useSidebarDismissOnNavigate(): void {
  const href = useRouterState({ select: (s) => s.location.href });
  // Deliberately keyed on the location alone: reading the store through
  // getState is what keeps opening the sheet from re-running this and closing
  // it again on the same frame.
  // biome-ignore lint/correctness/useExhaustiveDependencies: href is the trigger
  useEffect(() => {
    dismissSidebar();
  }, [href]);
}
