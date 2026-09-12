import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyAssistantDelta,
  applyAssistantJournal,
  applyAssistantMessage,
  applyAssistantProposal,
} from "~/lib/assistant/apply-push";
import { useAssistantStore } from "~/stores/assistant-store";

describe("assistant push applier", () => {
  beforeEach(() => {
    useAssistantStore.getState().reset();
    vi.restoreAllMocks();
  });

  it("stores a pushed message", () => {
    applyAssistantMessage({
      id: "m1",
      role: "assistant",
      text: "the branch is behind",
      createdAt: "2026-01-01T00:00:00.000000000Z",
    });
    expect(useAssistantStore.getState().messages.map((m) => m.id)).toEqual(["m1"]);
  });

  it("accumulates deltas and lets the stored message replace them", () => {
    applyAssistantDelta({ text: "one " });
    applyAssistantDelta({ text: "two" });
    expect(useAssistantStore.getState().streaming).toBe("one two");

    applyAssistantMessage({ id: "m2", role: "assistant", text: "one two" });
    expect(useAssistantStore.getState().streaming).toBeNull();
  });

  it("treats a delta with no text as a reply still owed", () => {
    applyAssistantDelta({});
    expect(useAssistantStore.getState().streaming).toBe("");
  });

  it("stores a pushed journal entry once, however often it arrives", () => {
    const row = { id: 4, at: "2026-01-01T00:00:00Z", kind: "report", summary: "tests were red" };
    applyAssistantJournal(row);
    applyAssistantJournal(row);
    expect(useAssistantStore.getState().journal).toHaveLength(1);
    expect(useAssistantStore.getState().journal[0]?.kind).toBe("report");
  });

  it("survives a message from a peer that speaks none of the optional fields", () => {
    applyAssistantMessage({});
    expect(useAssistantStore.getState().messages).toHaveLength(1);
  });

  it("stores a pushed proposal, open", () => {
    applyAssistantProposal({
      id: "p1",
      verb: "merge_session",
      sessionId: "sess-1",
      status: "open",
      createdAt: "2026-01-01T00:00:00Z",
    });
    const after = useAssistantStore.getState();
    expect(after.proposals.map((p) => p.id)).toEqual(["p1"]);
    expect(after.openProposals.map((p) => p.id)).toEqual(["p1"]);
  });

  it("lets the decision push replace the open row it is about", () => {
    applyAssistantProposal({ id: "p1", verb: "merge_session", status: "open" });
    applyAssistantProposal({
      id: "p1",
      verb: "merge_session",
      status: "stale",
      outcome: "the branch is 2 commits behind",
    });
    const after = useAssistantStore.getState();
    // One row, decided, and out of the open list — but still held, because the
    // card somebody pressed has to be able to say what happened.
    expect(after.proposals).toHaveLength(1);
    expect(after.proposals[0]?.status).toBe("stale");
    expect(after.openProposals).toHaveLength(0);
  });

  it("counts nothing as unseen for a proposal", () => {
    applyAssistantProposal({ id: "p1", verb: "archive_session", status: "open" });
    // A proposal's claim on attention is the card and the deck row; the
    // `proposal_made` journal entry rides the journal push beside it.
    expect(useAssistantStore.getState().unseen).toBe(0);
  });

  it("drops a payload it cannot read rather than half-applying it", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    applyAssistantMessage({ text: 42 });
    applyAssistantDelta("not an object");
    applyAssistantJournal({ id: "not a number" });
    applyAssistantProposal({ verb: 7 });
    expect(useAssistantStore.getState().messages).toHaveLength(0);
    expect(useAssistantStore.getState().streaming).toBeNull();
    expect(useAssistantStore.getState().journal).toHaveLength(0);
    expect(useAssistantStore.getState().proposals).toHaveLength(0);
    expect(warn).toHaveBeenCalledTimes(4);
  });
});
