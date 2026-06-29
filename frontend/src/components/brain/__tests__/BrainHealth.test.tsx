import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { BrainHealth } from "~/components/brain/BrainHealth";
import type { BrainCounts } from "~/lib/brain-api";
import { useBrainStore } from "~/stores/brain-store";

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

const COUNTS: BrainCounts = {
  total: 10,
  byLifecycle: { active: 6, archived: 3, superseded: 1 },
  bySource: { human: 2, agent: 5, capture: 3 },
  byEvidence: { user_stated: 2, inferred: 5, code_verified: 1, corroborated: 1, observed_once: 1 },
  byVolatility: { evergreen: 1, slow: 8, ephemeral: 1 },
  byConfidenceTier: { extracted: 2, inferred: 6, ambiguous: 2 },
  reviewQueue: 2,
  corroboratedTotal: 4,
};

describe("BrainHealth", () => {
  it("renders the health distribution from the store counts", () => {
    useBrainStore.setState({ counts: COUNTS });
    render(<BrainHealth />);

    fireEvent.click(screen.getByRole("button", { name: /Health/ }));

    expect(screen.getByText("Brain health")).toBeTruthy();
    // Pipeline summary labels + the two unique values.
    expect(screen.getByText("Total facts")).toBeTruthy();
    expect(screen.getByText("10")).toBeTruthy(); // total
    expect(screen.getByText("Captures pending")).toBeTruthy();
    expect(screen.getByText("Review queue")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy(); // corroboratedTotal (unique)
    // Distribution buckets render their short labels.
    expect(screen.getByText("stated")).toBeTruthy(); // evidence user_stated
    expect(screen.getByText("evergreen")).toBeTruthy(); // volatility
    expect(screen.getByText("extracted")).toBeTruthy(); // confidence tier
  });
});
