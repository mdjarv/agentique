import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MachineEntry } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

const entry: MachineEntry = {
  machineId: "m-1",
  label: "zbook",
  baseUrl: "http://127.0.0.1:1",
  token: "bearer-token",
  sessionId: "as-1",
  identityKey: "key",
  addedAt: "2026-09-01T00:00:00Z",
};

describe("removeMachine", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response(null, { status: 204 }))),
    );
    useMachineStore.setState({
      machines: { "m-1": entry },
      statuses: { "m-1": "connected" },
      lastSeenAt: { "m-1": 1756700000000 },
      faults: {},
      versions: { "m-1": "v1.0.0" },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // lastSeenAt is persisted, so an entry a remove misses outlives the tab —
  // an orphaned "last seen" for a machine the catalog no longer knows.
  it("prunes every per-machine record, the persisted lastSeenAt included", async () => {
    await useMachineStore.getState().removeMachine("m-1");

    const s = useMachineStore.getState();
    expect(s.machines["m-1"]).toBeUndefined();
    expect(s.statuses["m-1"]).toBeUndefined();
    expect(s.versions["m-1"]).toBeUndefined();
    expect(s.lastSeenAt["m-1"]).toBeUndefined();
  });
});
