import { beforeEach, describe, expect, it } from "vitest";
import { useStreamingStore } from "~/stores/streaming-store";

describe("streaming-store", () => {
  beforeEach(() => {
    useStreamingStore.setState({ texts: {}, toolInputs: {} });
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
});
