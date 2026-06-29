import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import * as brainApi from "~/lib/brain-api";

vi.mock("~/lib/brain-api", () => ({
  listSnapshots: vi.fn(),
  createSnapshot: vi.fn(),
  restoreSnapshot: vi.fn(),
}));

import { BrainSnapshots } from "~/components/brain/BrainSnapshots";

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

const SNAP = {
  id: "20260601T120000Z",
  createdAt: "2026-06-01T12:00:00Z",
  files: 3,
  bytes: 2048,
};

beforeEach(() => {
  vi.mocked(brainApi.listSnapshots).mockResolvedValue([SNAP]);
  vi.mocked(brainApi.createSnapshot).mockResolvedValue({ ...SNAP, id: "new" });
  vi.mocked(brainApi.restoreSnapshot).mockResolvedValue(undefined);
});

describe("BrainSnapshots", () => {
  it("lists snapshots and guards restore behind a confirm", async () => {
    render(<BrainSnapshots onClose={vi.fn()} jobActive={false} />);

    // The list renders the snapshot.
    expect(await screen.findByText(SNAP.id, { exact: false })).toBeTruthy();

    // First Restore click reveals the confirm — it does NOT call the api yet.
    fireEvent.click(screen.getByRole("button", { name: /Restore/ }));
    expect(screen.getByText(/rolls the/i)).toBeTruthy();
    expect(brainApi.restoreSnapshot).not.toHaveBeenCalled();

    // Confirming calls the api with the snapshot id.
    fireEvent.click(screen.getByRole("button", { name: /^Restore$/ }));
    await waitFor(() => expect(brainApi.restoreSnapshot).toHaveBeenCalledWith(SNAP.id));
  });

  it("takes a snapshot on demand", async () => {
    render(<BrainSnapshots onClose={vi.fn()} jobActive={false} />);
    await screen.findByText(SNAP.id, { exact: false });
    fireEvent.click(screen.getByRole("button", { name: /Take snapshot/ }));
    await waitFor(() => expect(brainApi.createSnapshot).toHaveBeenCalled());
  });

  it("disables restore and warns while a consolidation is running", async () => {
    render(<BrainSnapshots onClose={vi.fn()} jobActive={true} />);
    await screen.findByText(SNAP.id, { exact: false });
    expect(screen.getByText(/consolidation is running/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Restore/ })).toHaveProperty("disabled", true);
  });
});
