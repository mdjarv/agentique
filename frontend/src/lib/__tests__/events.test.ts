import { describe, expect, it } from "vitest";
import { parseServerEvent } from "~/lib/events";

describe("parseServerEvent", () => {
  it("parses text event", () => {
    const event = parseServerEvent({ type: "text", content: "Hello world" });
    expect(event.type).toBe("text");
    expect(event.content).toBe("Hello world");
    expect(event.id).toBeDefined();
  });

  it("parses thinking event", () => {
    const event = parseServerEvent({ type: "thinking", content: "Let me think..." });
    expect(event.type).toBe("thinking");
    expect(event.content).toBe("Let me think...");
  });

  it("parses tool_use event", () => {
    const event = parseServerEvent({
      type: "tool_use",
      toolId: "t1",
      toolName: "Read",
      toolInput: { path: "/test.go" },
      category: "file_read",
    });
    expect(event.type).toBe("tool_use");
    expect(event.toolId).toBe("t1");
    expect(event.toolName).toBe("Read");
    expect(event.toolInput).toEqual({ path: "/test.go" });
    expect(event.category).toBe("file_read");
  });

  it("parses tool_result with content blocks", () => {
    const content = [
      { type: "text", text: "file contents" },
      { type: "image", mediaType: "image/png", url: "data:image/png;base64,abc" },
    ];
    const event = parseServerEvent({ type: "tool_result", toolId: "t1", content });
    expect(event.type).toBe("tool_result");
    expect(event.toolId).toBe("t1");
    expect(event.contentBlocks).toEqual(content);
    // text-type content should NOT be put in the content field for tool_result
    expect(event.content).toBeUndefined();
  });

  it("parses result event", () => {
    const event = parseServerEvent({
      type: "result",
      cost: 0.05,
      duration: 1500,
      usage: { inputTokens: 5000, outputTokens: 1000 },
      stopReason: "end_turn",
      contextWindow: 200000,
      inputTokens: 5000,
      outputTokens: 1000,
    });
    expect(event.type).toBe("result");
    expect(event.cost).toBe(0.05);
    expect(event.duration).toBe(1500);
    expect(event.stopReason).toBe("end_turn");
    expect(event.contextWindow).toBe(200000);
  });

  it("parses error event", () => {
    const event = parseServerEvent({
      type: "error",
      content: "Something went wrong",
      fatal: true,
      errorType: "rate_limit",
      retryAfterSecs: 30,
    });
    expect(event.type).toBe("error");
    expect(event.content).toBe("Something went wrong");
    expect(event.fatal).toBe(true);
    expect(event.errorType).toBe("rate_limit");
    expect(event.retryAfterSecs).toBe(30);
  });

  it("parses rate_limit event", () => {
    const event = parseServerEvent({
      type: "rate_limit",
      status: "warning",
      utilization: 0.85,
    });
    expect(event.type).toBe("rate_limit");
    expect(event.status).toBe("warning");
    expect(event.utilization).toBe(0.85);
  });

  it("parses compact_boundary event", () => {
    const event = parseServerEvent({
      type: "compact_boundary",
      trigger: "auto",
      preTokens: 50000,
    });
    expect(event.type).toBe("compact_boundary");
    expect(event.trigger).toBe("auto");
    expect(event.preTokens).toBe(50000);
  });

  it("generates unique IDs per call", () => {
    const e1 = parseServerEvent({ type: "text", content: "a" });
    const e2 = parseServerEvent({ type: "text", content: "b" });
    expect(e1.id).not.toBe(e2.id);
  });
});
