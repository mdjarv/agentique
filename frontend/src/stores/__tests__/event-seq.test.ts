import { beforeEach, describe, expect, it } from "vitest";
import { decideSeq, useEventSeqStore } from "~/stores/event-seq";

const SID = "sess-1";
const E = 7; // an arbitrary epoch

describe("decideSeq — pure decision", () => {
  it("seeds + accepts the first event for a session", () => {
    expect(decideSeq(undefined, E, 5)).toEqual({
      action: "accept",
      next: { epoch: E, lastSeq: 5 },
    });
  });

  it("accepts an in-order event (lastSeq + 1)", () => {
    expect(decideSeq({ epoch: E, lastSeq: 5 }, E, 6)).toEqual({
      action: "accept",
      next: { epoch: E, lastSeq: 6 },
    });
  });

  it("drops a duplicate (seq === lastSeq) without changing state", () => {
    const prev = { epoch: E, lastSeq: 6 };
    expect(decideSeq(prev, E, 6)).toEqual({ action: "drop", next: prev });
  });

  it("drops an out-of-order/late event (seq < lastSeq)", () => {
    const prev = { epoch: E, lastSeq: 6 };
    expect(decideSeq(prev, E, 3)).toEqual({ action: "drop", next: prev });
  });

  it("resyncs on a forward gap but still accepts the event", () => {
    expect(decideSeq({ epoch: E, lastSeq: 6 }, E, 10)).toEqual({
      action: "resync",
      next: { epoch: E, lastSeq: 10 },
    });
  });

  it("resyncs + reseeds when the epoch changes (pipeline rebuild / lazy resume)", () => {
    // seq restarted at 1 in the new pipeline — must NOT be dropped as stale.
    expect(decideSeq({ epoch: E, lastSeq: 500 }, E + 1, 1)).toEqual({
      action: "resync",
      next: { epoch: E + 1, lastSeq: 1 },
    });
  });

  it("adopts a null prior epoch (seeded from history) without forcing a resync", () => {
    // Seeded from history high-water 6, epoch unknown; first live event seq 7.
    expect(decideSeq({ epoch: null, lastSeq: 6 }, E, 7)).toEqual({
      action: "accept",
      next: { epoch: E, lastSeq: 7 },
    });
  });

  it("with a null prior epoch, still drops a seq already covered by the high-water", () => {
    expect(decideSeq({ epoch: null, lastSeq: 6 }, E, 6)).toEqual({
      action: "drop",
      next: { epoch: null, lastSeq: 6 },
    });
  });
});

describe("useEventSeqStore", () => {
  beforeEach(() => useEventSeqStore.getState().reset());

  it("seedFromHistory stores epoch>0 directly and seq as the high-water", () => {
    useEventSeqStore.getState().seedFromHistory(SID, E, 42);
    expect(useEventSeqStore.getState().states[SID]).toEqual({ epoch: E, lastSeq: 42 });
  });

  it("seedFromHistory stores epoch 0 (offline session) as null", () => {
    useEventSeqStore.getState().seedFromHistory(SID, 0, 0);
    expect(useEventSeqStore.getState().states[SID]).toEqual({ epoch: null, lastSeq: 0 });
  });

  it("clearSession + reset drop tracked state", () => {
    useEventSeqStore.getState().record(SID, { epoch: E, lastSeq: 1 });
    useEventSeqStore.getState().clearSession(SID);
    expect(useEventSeqStore.getState().states[SID]).toBeUndefined();

    useEventSeqStore.getState().record(SID, { epoch: E, lastSeq: 1 });
    useEventSeqStore.getState().reset();
    expect(useEventSeqStore.getState().states).toEqual({});
  });

  it("history reseed wins over a live event that advanced lastSeq mid-load", () => {
    // A live event arrives during a force history load and advances lastSeq.
    const live = decideSeq(useEventSeqStore.getState().states[SID], E, 30);
    useEventSeqStore.getState().record(SID, live.next);
    expect(useEventSeqStore.getState().states[SID]).toEqual({ epoch: E, lastSeq: 30 });

    // The history response (authoritative) then resolves with high-water 25.
    useEventSeqStore.getState().seedFromHistory(SID, E, 25);
    expect(useEventSeqStore.getState().states[SID]).toEqual({ epoch: E, lastSeq: 25 });

    // A subsequent live event already in that snapshot (seq <= 25) is dropped.
    const dup = decideSeq(useEventSeqStore.getState().states[SID], E, 24);
    expect(dup.action).toBe("drop");
    // ...while the next genuinely-new event (26) is accepted.
    const fresh = decideSeq(useEventSeqStore.getState().states[SID], E, 26);
    expect(fresh.action).toBe("accept");
  });
});
