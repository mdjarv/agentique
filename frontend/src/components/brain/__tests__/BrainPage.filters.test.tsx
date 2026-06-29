import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { Memory } from "~/lib/brain-api";
import { useAppStore } from "~/stores/app-store";
import { useBrainStore } from "~/stores/brain-store";

// The 2D graph renders a real <canvas> via react-force-graph (default view = graph), which
// jsdom can't drive; stub it so this test stays about the list filtering. The 3D view is
// lazy-loaded only when selected, so it never mounts here.
vi.mock("~/components/brain/BrainGraph", () => ({ BrainGraph: () => null }));

import { BrainPage } from "~/components/brain/BrainPage";

beforeAll(() => {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  if (!window.matchMedia) {
    window.matchMedia = (q: string) =>
      ({
        matches: false,
        media: q,
        addEventListener() {},
        removeEventListener() {},
      }) as unknown as MediaQueryList;
  }
  // The store effects fire network calls on mount; stub fetch so they reject quietly
  // (the store actions already swallow the error) instead of throwing on undefined fetch.
  globalThis.fetch = vi.fn(() => Promise.reject(new Error("no network"))) as typeof fetch;
});

afterEach(cleanup);

function mem(over: Partial<Memory> & { id: string; text: string }): Memory {
  return {
    scope: "global",
    category: "fact",
    source: "agent",
    pinned: false,
    locked: false,
    uses: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    lifecycle: "active",
    ...over,
  };
}

beforeEach(() => {
  useAppStore.setState({ projects: [] });
  useBrainStore.setState({
    memories: [
      mem({ id: "n1", text: "NORMAL FACT" }),
      mem({ id: "c1", text: "CAPTURE FACT", source: "capture", evidence: "observed_once" }),
      mem({ id: "a1", text: "ARCHIVED ONE", lifecycle: "archived" }),
      mem({ id: "a2", text: "ARCHIVED TWO", lifecycle: "archived" }),
    ],
    loaded: true,
    loading: false,
    graph: null,
    graphLoading: false,
  });
});

describe("BrainPage tier filters", () => {
  it("hides captures + archived by default, shows their counts, and reveals on toggle", () => {
    render(<BrainPage />);

    // Default list = live injectable facts only.
    expect(screen.getByText("NORMAL FACT")).toBeTruthy();
    expect(screen.queryByText("CAPTURE FACT")).toBeNull();
    expect(screen.queryByText("ARCHIVED ONE")).toBeNull();
    expect(screen.queryByText("ARCHIVED TWO")).toBeNull();

    // Toolbar advertises what's hidden, with counts.
    const capturesBtn = screen.getByRole("button", { name: /Captures \(1\)/ });
    const archivedBtn = screen.getByRole("button", { name: /Archived \(2\)/ });

    // Reveal captures.
    fireEvent.click(capturesBtn);
    expect(screen.getByText("CAPTURE FACT")).toBeTruthy();
    expect(screen.queryByText("ARCHIVED ONE")).toBeNull(); // independent toggle

    // Reveal archived too.
    fireEvent.click(archivedBtn);
    expect(screen.getByText("ARCHIVED ONE")).toBeTruthy();
    expect(screen.getByText("ARCHIVED TWO")).toBeTruthy();
  });

  it("does not show a toggle for a tier with nothing hidden", () => {
    useBrainStore.setState({ memories: [mem({ id: "n1", text: "NORMAL FACT" })] });
    render(<BrainPage />);
    expect(screen.queryByRole("button", { name: /Captures/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Archived/ })).toBeNull();
  });
});
