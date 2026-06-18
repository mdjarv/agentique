import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { MemoryReview } from "~/components/brain/MemoryReview";
import type { Memory } from "~/lib/brain-api";

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
});
afterEach(cleanup);

// No related/derivedFrom → the fact "stands alone", so the subgraph branch renders a
// message instead of the graph; this keeps the test about the review workflow itself.
function mem(id: string, text: string): Memory {
  return {
    id,
    scope: "global",
    text,
    category: "preference",
    source: "consolidated",
    pinned: false,
    locked: false,
    uses: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    confidence: "inferred",
    confidenceScore: 0.65,
  };
}

const LONG =
  "Run Go tests with the race detector for all concurrency-sensitive packages before merging";

describe("MemoryReview", () => {
  it("shows full (untruncated) text, confirms, advances, and ends", async () => {
    const queue = [mem("a", LONG), mem("b", "Prefer table-driven Go tests with subtests")];
    const onConfirm = vi.fn().mockResolvedValue(undefined);

    render(
      <MemoryReview
        queue={queue}
        allMemories={queue}
        labelForScope={(s) => s}
        onConfirm={onConfirm}
        onDelete={vi.fn()}
        onUpdate={vi.fn()}
        onClose={vi.fn()}
      />,
    );

    // Full text is shown verbatim — the whole point of the surface (no 40-char clip).
    expect(screen.getByText(LONG)).toBeTruthy();
    expect(screen.getByText("1 of 2")).toBeTruthy();
    // It explains why the fact is queued.
    expect(screen.getByText(/cross-project generalization/i)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Confirm/i }));
    expect(onConfirm).toHaveBeenCalledWith("a");

    // Advances to the second fact, then to the cleared state.
    expect(await screen.findByText("2 of 2")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Confirm/i }));
    expect(await screen.findByText(/Nothing left to review/i)).toBeTruthy();
  });

  it("frames a promotion as inputs → output with the merge question", () => {
    const promoted: Memory = {
      ...mem("g", "Uses `just` as the task runner across projects"),
      derivedFrom: ["x", "y"],
      subsumed: [
        { scope: "project:a", text: "uses just as the task runner" },
        { scope: "project:b", text: "task runner is just" },
      ],
    };
    render(
      <MemoryReview
        queue={[promoted]}
        allMemories={[promoted]}
        labelForScope={(s) => s}
        onConfirm={vi.fn()}
        onDelete={vi.fn()}
        onUpdate={vi.fn()}
        onClose={vi.fn()}
      />,
    );

    // Inputs are shown with their real source text, plus the output and the question.
    expect(screen.getByText(/Input memories \(2\)/i)).toBeTruthy();
    expect(screen.getByText("uses just as the task runner")).toBeTruthy();
    expect(screen.getByText("task runner is just")).toBeTruthy();
    expect(screen.getByText("Uses `just` as the task runner across projects")).toBeTruthy();
    expect(screen.getByText(/agree with this merge/i)).toBeTruthy();
  });
});
