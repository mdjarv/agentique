import { cleanup, render } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { Memory } from "~/lib/brain-api";

// Capture every graphData object react-force-graph receives so we can assert on its
// reference identity (stable across data-only refetches) and the positions stamped on
// its node objects (carried forward across a topology change). vi.hoisted survives the
// vi.mock factory hoist.
const captured = vi.hoisted(() => ({ graphData: [] as { nodes: GNode[]; links: unknown[] }[] }));

interface GNode {
  id: string;
  val: number;
  uses: number;
  x?: number;
  y?: number;
}

vi.mock("react-force-graph-2d", async () => {
  const React = await import("react");
  // The component talks to the instance only through optional-chained imperative calls
  // (d3Force/zoomToFit/…), so an inert ref is enough; we just need the graphData prop.
  const Mock = React.forwardRef(function MockForceGraph(
    props: { graphData: { nodes: GNode[]; links: unknown[] } },
    _ref: unknown,
  ) {
    captured.graphData.push(props.graphData);
    return null;
  });
  return { default: Mock };
});

import { BrainGraph } from "~/components/brain/BrainGraph";

const labelForScope = (s: string) => s;
const onConfirm = () => {};

// Distinct, non-overlapping text per memory so no incidental Jaccard-similarity edges
// form — the topology is exactly the explicit derivedFrom links we set.
const TEXT: Record<string, string> = {
  m1: "alpha apple",
  m2: "bravo banana",
  m3: "charlie cherry",
  m4: "delta dragon",
  m5: "echo eggplant",
};

function mem(id: string, over: Partial<Memory> = {}): Memory {
  return {
    id,
    scope: "global",
    text: TEXT[id] ?? id,
    category: "fact",
    source: "test",
    pinned: false,
    locked: false,
    uses: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...over,
  };
}

function lastGraphData() {
  const gd = captured.graphData.at(-1);
  if (!gd) throw new Error("ForceGraph2D never rendered — size/guard not satisfied");
  return gd;
}

describe("BrainGraph layout stability", () => {
  beforeAll(() => {
    // jsdom reports 0 size and lacks ResizeObserver; the graph only renders when the
    // measured container has a non-zero size.
    (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, value: 800 });
    Object.defineProperty(HTMLElement.prototype, "clientHeight", {
      configurable: true,
      value: 600,
    });
  });

  afterAll(() => {
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, value: 0 });
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, value: 0 });
  });

  afterEach(() => {
    cleanup();
    captured.graphData.length = 0;
  });

  it("preserves node positions and graphData identity across a data-only refetch, and rebuilds only on a topology change", () => {
    // m2 derives from m1 → one structural edge, so the topology signature is non-trivial.
    const initial = [mem("m1"), mem("m2", { derivedFrom: ["m1"] }), mem("m3")];
    const { rerender } = render(
      <BrainGraph
        memories={initial}
        report={null}
        labelForScope={labelForScope}
        onConfirm={onConfirm}
      />,
    );

    const gd0 = lastGraphData();
    expect(gd0.nodes).toHaveLength(3);

    // Simulate the force engine settling: it stamps x/y onto the very node objects we
    // passed (which the component also holds in its carry-forward ref).
    for (const [i, n] of gd0.nodes.entries()) {
      n.x = (i + 1) * 100;
      n.y = (i + 1) * 200;
    }
    const m1Node0 = gd0.nodes.find((n) => n.id === "m1");
    expect(m1Node0?.val).toBe(4); // 3 + min(uses=1) + 0 + 0

    // --- Data-only refetch: same ids + same links, only `uses` bumped. ---
    const bumped = [mem("m1", { uses: 5 }), mem("m2", { derivedFrom: ["m1"] }), mem("m3")];
    rerender(
      <BrainGraph
        memories={bumped}
        report={null}
        labelForScope={labelForScope}
        onConfirm={onConfirm}
      />,
    );

    const gd1 = lastGraphData();
    // Reference is reused → react-force-graph skips its digest → no reheat, no jump.
    expect(gd1).toBe(gd0);
    const m1Node1 = gd1.nodes.find((n) => n.id === "m1");
    expect(m1Node1).toBe(m1Node0); // same live object
    expect(m1Node1?.val).toBe(8); // display field refreshed in place: 3 + min(uses=5)
    expect(m1Node1?.x).toBe(100); // position untouched
    expect(m1Node1?.y).toBe(200);

    // --- Topology change: m4 (linked to m1) and m5 (isolated) are added. ---
    const added = [...bumped, mem("m4", { derivedFrom: ["m1"] }), mem("m5")];
    rerender(
      <BrainGraph
        memories={added}
        report={null}
        labelForScope={labelForScope}
        onConfirm={onConfirm}
      />,
    );

    const gd2 = lastGraphData();
    expect(gd2).not.toBe(gd0); // fresh reference → the (single) reheat that places m4/m5
    expect(gd2.nodes).toHaveLength(5);
    // Surviving nodes carry their settled positions forward — no re-randomization.
    const m1Node2 = gd2.nodes.find((n) => n.id === "m1");
    expect(m1Node2?.x).toBe(100);
    expect(m1Node2?.y).toBe(200);
    // A new node with a placed neighbour is seeded near it (m1 at x=100) rather than
    // flying in from the origin.
    const m4Node = gd2.nodes.find((n) => n.id === "m4");
    expect(m4Node?.x).toBeGreaterThanOrEqual(98);
    expect(m4Node?.x).toBeLessThanOrEqual(102);
    // A new node with no placed neighbour is left for the engine to position.
    const m5Node = gd2.nodes.find((n) => n.id === "m5");
    expect(m5Node).toBeDefined();
    expect(m5Node?.x).toBeUndefined();
  });
});
