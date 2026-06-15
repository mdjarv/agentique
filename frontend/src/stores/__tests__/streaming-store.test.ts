import { beforeEach, describe, expect, it } from "vitest";
import { useStreamingStore } from "~/stores/streaming-store";

describe("streaming-store", () => {
  beforeEach(() => {
    // Full reset (all maps) so cases don't leak state between each other.
    useStreamingStore.getState().reset();
  });

  describe("appendText / clearText", () => {
    it("accumulates text for a session", () => {
      useStreamingStore.getState().appendText("s1", "hello ");
      useStreamingStore.getState().appendText("s1", "world");
      expect(useStreamingStore.getState().texts.s1).toBe("hello world");
    });

    it("creates entry for new session", () => {
      useStreamingStore.getState().appendText("s1", "hi");
      expect(useStreamingStore.getState().texts.s1).toBe("hi");
    });

    it("clears text for a session", () => {
      useStreamingStore.getState().appendText("s1", "hi");
      useStreamingStore.getState().clearText("s1");
      expect(useStreamingStore.getState().texts.s1).toBeUndefined();
    });

    it("no-op when clearing non-existent session", () => {
      const before = useStreamingStore.getState();
      useStreamingStore.getState().clearText("missing");
      expect(useStreamingStore.getState()).toBe(before);
    });
  });

  describe("appendToolInput / clearToolInput / clearAllToolInputs", () => {
    it("accumulates tool input per session and toolId", () => {
      useStreamingStore.getState().appendToolInput("s1", "t1", '{"a":');
      useStreamingStore.getState().appendToolInput("s1", "t1", "1}");
      expect(useStreamingStore.getState().toolInputs.s1?.t1).toBe('{"a":1}');
    });

    it("clears specific toolId, keeps others", () => {
      useStreamingStore.getState().appendToolInput("s1", "t1", "a");
      useStreamingStore.getState().appendToolInput("s1", "t2", "b");
      useStreamingStore.getState().clearToolInput("s1", "t1");
      expect(useStreamingStore.getState().toolInputs.s1?.t1).toBeUndefined();
      expect(useStreamingStore.getState().toolInputs.s1?.t2).toBe("b");
    });

    it("clears all tool inputs for a session", () => {
      useStreamingStore.getState().appendToolInput("s1", "t1", "a");
      useStreamingStore.getState().appendToolInput("s1", "t2", "b");
      useStreamingStore.getState().clearAllToolInputs("s1");
      expect(useStreamingStore.getState().toolInputs.s1).toBeUndefined();
    });

    it("no-op when clearing non-existent session", () => {
      const before = useStreamingStore.getState();
      useStreamingStore.getState().clearAllToolInputs("missing");
      expect(useStreamingStore.getState()).toBe(before);
    });
  });

  describe("toolBlockIndex", () => {
    it("maps content-block index → tool id per session", () => {
      useStreamingStore.getState().setToolBlockId("s1", 0, "toolA");
      useStreamingStore.getState().setToolBlockId("s1", 1, "toolB");
      expect(useStreamingStore.getState().toolBlockIndex.s1?.[0]).toBe("toolA");
      expect(useStreamingStore.getState().toolBlockIndex.s1?.[1]).toBe("toolB");
    });

    it("clears the index for a session", () => {
      useStreamingStore.getState().setToolBlockId("s1", 0, "toolA");
      useStreamingStore.getState().clearToolBlockIndex("s1");
      expect(useStreamingStore.getState().toolBlockIndex.s1).toBeUndefined();
    });

    it("no-op when clearing a session with no index", () => {
      const before = useStreamingStore.getState();
      useStreamingStore.getState().clearToolBlockIndex("missing");
      expect(useStreamingStore.getState()).toBe(before);
    });
  });

  describe("clearSession", () => {
    it("reclaims every map for a session — including the tool block index", () => {
      const s = useStreamingStore.getState();
      s.appendText("s1", "hi");
      s.appendToolInput("s1", "t1", "{");
      s.appendToolOutput("s1", "t1", "out");
      s.setToolProgress("s1", "t1", { elapsedMs: 5 });
      s.appendReasoning("s1", "r1", "think");
      s.setToolBlockId("s1", 0, "t1");

      useStreamingStore.getState().clearSession("s1");

      const after = useStreamingStore.getState();
      expect(after.texts.s1).toBeUndefined();
      expect(after.toolInputs.s1).toBeUndefined();
      expect(after.toolOutputs.s1).toBeUndefined();
      expect(after.toolProgress.s1).toBeUndefined();
      expect(after.reasoningDeltas.s1).toBeUndefined();
      expect(after.toolBlockIndex.s1).toBeUndefined();
    });

    it("no-op when the session has no streaming state", () => {
      const before = useStreamingStore.getState();
      useStreamingStore.getState().clearSession("missing");
      expect(useStreamingStore.getState()).toBe(before);
    });
  });
});
