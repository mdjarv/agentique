/**
 * The rule under test is "arriving somewhere dismisses the mobile sidebar" —
 * and, just as load-bearing, "opening it does not". Reading the store through
 * `getState` rather than a selector is what keeps the second true; a selector
 * would make the sheet's own open a dependency change and close it again.
 */
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "~/stores/app-store";
import { dismissSidebar, useSidebarDismissOnNavigate } from "./sidebar-nav";

const href = vi.hoisted(() => ({ value: "/" }));

vi.mock("@tanstack/react-router", () => ({
  useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
    select({ location: { href: href.value } }),
}));

describe("dismissSidebar", () => {
  beforeEach(() => {
    href.value = "/";
    useAppStore.setState({ sidebarOpen: false });
  });

  it("closes an open sidebar", () => {
    useAppStore.setState({ sidebarOpen: true });
    dismissSidebar();
    expect(useAppStore.getState().sidebarOpen).toBe(false);
  });

  it("is a no-op when the sidebar is already closed", () => {
    const setSidebarOpen = vi.fn();
    useAppStore.setState({ sidebarOpen: false, setSidebarOpen });
    dismissSidebar();
    expect(setSidebarOpen).not.toHaveBeenCalled();
  });
});

describe("useSidebarDismissOnNavigate", () => {
  beforeEach(() => {
    href.value = "/";
    useAppStore.setState({
      sidebarOpen: false,
      setSidebarOpen: (open) => useAppStore.setState({ sidebarOpen: open }),
    });
  });

  it("dismisses the sidebar when the location changes", () => {
    const { rerender } = renderHook(() => useSidebarDismissOnNavigate());
    useAppStore.setState({ sidebarOpen: true });

    href.value = "/storage";
    rerender();

    expect(useAppStore.getState().sidebarOpen).toBe(false);
  });

  // Opening the sheet must not re-run the effect: the sidebar is opened from a
  // page, so an effect keyed on the store would close it on the same frame.
  it("leaves the sidebar open while the location holds still", () => {
    const { rerender } = renderHook(() => useSidebarDismissOnNavigate());
    useAppStore.setState({ sidebarOpen: true });

    rerender();

    expect(useAppStore.getState().sidebarOpen).toBe(true);
  });
});
