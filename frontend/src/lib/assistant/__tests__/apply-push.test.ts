import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyAssistantDelta,
  applyAssistantJournal,
  applyAssistantMessage,
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

  it("drops a payload it cannot read rather than half-applying it", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    applyAssistantMessage({ text: 42 });
    applyAssistantDelta("not an object");
    applyAssistantJournal({ id: "not a number" });
    expect(useAssistantStore.getState().messages).toHaveLength(0);
    expect(useAssistantStore.getState().streaming).toBeNull();
    expect(useAssistantStore.getState().journal).toHaveLength(0);
    expect(warn).toHaveBeenCalledTimes(3);
  });
});
