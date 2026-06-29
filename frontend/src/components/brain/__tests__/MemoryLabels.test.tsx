import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryLabels } from "~/components/brain/MemoryLabels";
import type { Memory } from "~/lib/brain-api";

afterEach(cleanup);

function mem(over: Partial<Memory> = {}): Memory {
  return {
    id: "m1",
    scope: "global",
    text: "x",
    category: "fact",
    source: "agent",
    pinned: false,
    locked: false,
    uses: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    lifecycle: "active",
    evidence: "inferred",
    volatility: "slow",
    ...over,
  };
}

describe("MemoryLabels", () => {
  it("renders nothing for an ordinary active, default-labelled, non-capture fact", () => {
    const { container } = render(<MemoryLabels memory={mem()} />);
    expect(container.firstChild).toBeNull();
  });

  it("shows the capture badge for a raw capture", () => {
    render(<MemoryLabels memory={mem({ source: "capture", evidence: "observed_once" })} />);
    expect(screen.getByText("capture")).toBeTruthy();
  });

  it("shows the archived badge for a cold fact", () => {
    render(<MemoryLabels memory={mem({ lifecycle: "archived" })} />);
    expect(screen.getByText("archived")).toBeTruthy();
  });

  it("shows the superseded badge for a replaced fact", () => {
    render(<MemoryLabels memory={mem({ lifecycle: "superseded" })} />);
    expect(screen.getByText("superseded")).toBeTruthy();
  });

  it("renders evidence + volatility chip labels for non-default labels", () => {
    render(<MemoryLabels memory={mem({ evidence: "code_verified", volatility: "evergreen" })} />);
    expect(screen.getByText("✓ code")).toBeTruthy();
    expect(screen.getByText("evergreen")).toBeTruthy();
  });

  it("shows a corroboration count when > 0", () => {
    render(<MemoryLabels memory={mem({ corroborations: 3 })} />);
    expect(screen.getByText("×3")).toBeTruthy();
  });
});
