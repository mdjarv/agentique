import { beforeEach, describe, expect, it } from "vitest";
import { sessionDock, useUIStore } from "~/stores/ui-store";

function reset() {
  useUIStore.setState({ dock: {}, dockWidth: 500, dockMaximized: false });
}

describe("session dock state", () => {
  beforeEach(reset);

  it("defaults to closed on Work for a session that never opened one", () => {
    expect(sessionDock(useUIStore.getState(), "sess-1")).toEqual({ open: false, view: "work" });
  });

  it("returns a stable reference for the default, so selectors do not loop", () => {
    const a = sessionDock(useUIStore.getState(), "sess-1");
    const b = sessionDock(useUIStore.getState(), "sess-2");
    expect(a).toBe(b);
    expect(sessionDock(useUIStore.getState(), null)).toBe(a);
  });

  it("keeps one session's dock out of another's", () => {
    useUIStore.getState().openDock("sess-1", "changes");
    useUIStore.getState().openDock("sess-2", "loops");

    expect(sessionDock(useUIStore.getState(), "sess-1")).toEqual({ open: true, view: "changes" });
    expect(sessionDock(useUIStore.getState(), "sess-2")).toEqual({ open: true, view: "loops" });
  });

  it("remembers the view across a close, so reopening lands where you left", () => {
    useUIStore.getState().openDock("sess-1", "loops");
    useUIStore.getState().setDockOpen("sess-1", false);

    expect(sessionDock(useUIStore.getState(), "sess-1")).toEqual({ open: false, view: "loops" });
  });

  it("no-ops rather than churning state when the dock is already in that state", () => {
    useUIStore.getState().openDock("sess-1", "work");
    const before = useUIStore.getState().dock;

    useUIStore.getState().setDockOpen("sess-1", true);
    useUIStore.getState().setDockView("sess-1", "work");

    expect(useUIStore.getState().dock).toBe(before);
  });

  it("selecting a view opens the dock as well as switching it", () => {
    useUIStore.getState().setDockView("sess-1", "browser");
    expect(sessionDock(useUIStore.getState(), "sess-1")).toEqual({ open: true, view: "browser" });
  });

  it("clamps width to the resizable range", () => {
    useUIStore.getState().setDockWidth(50);
    expect(useUIStore.getState().dockWidth).toBe(300);
    useUIStore.getState().setDockWidth(5000);
    expect(useUIStore.getState().dockWidth).toBe(900);
  });

  it("prunes oldest-first so the map cannot grow without bound", () => {
    for (let i = 0; i < 130; i++) useUIStore.getState().openDock(`sess-${i}`, "work");

    const dock = useUIStore.getState().dock;
    expect(Object.keys(dock)).toHaveLength(120);
    // The ten oldest lost their memory; the newest kept theirs.
    expect(dock["sess-0"]).toBeUndefined();
    expect(dock["sess-9"]).toBeUndefined();
    expect(dock["sess-10"]).toBeDefined();
    expect(dock["sess-129"]).toBeDefined();
  });

  it("re-opening a pruned-adjacent session does not evict a fresher one", () => {
    for (let i = 0; i < 120; i++) useUIStore.getState().openDock(`sess-${i}`, "work");
    // Touching an existing key must not count as an insertion.
    useUIStore.getState().openDock("sess-0", "changes");

    const dock = useUIStore.getState().dock;
    expect(Object.keys(dock)).toHaveLength(120);
    expect(dock["sess-0"]).toEqual({ open: true, view: "changes" });
    expect(dock["sess-119"]).toBeDefined();
  });
});
