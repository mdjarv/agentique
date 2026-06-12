import { describe, expect, it } from "vitest";
import { type ActivityItem, buildSegments, type Segment } from "~/lib/segments";
import type { ChatEvent } from "~/stores/chat-types";

function toolUse(toolId: string, toolName: string, toolInput: unknown, id = toolId): ChatEvent {
  return { id, type: "tool_use", toolId, toolName, toolInput };
}

function toolResult(toolId: string, text: string): ChatEvent {
  return {
    id: `${toolId}-r`,
    type: "tool_result",
    toolId,
    contentBlocks: [{ type: "text", text }],
  };
}

const isTool = (i: ActivityItem): i is Extract<ActivityItem, { kind: "tool" }> => i.kind === "tool";

function toolItems(segments: Segment[]) {
  return segments
    .filter((s): s is Extract<Segment, { kind: "activity" }> => s.kind === "activity")
    .flatMap((s) => s.items)
    .filter(isTool);
}

describe("buildSegments tool dedupe", () => {
  it("merges a pending-approval tool_use and the started tool_use sharing an item ID", () => {
    // Codex emits the approval-derived tool_use and the started item with the
    // same ID; they must collapse to one element with the result attached.
    const events: ChatEvent[] = [
      toolUse("call_1", "Bash", {}), // pending approval — sparse input
      toolUse("call_1", "Bash", { command: "git status" }), // started — full input
      toolResult("call_1", "clean"),
    ];

    const items = toolItems(buildSegments(events, true).segments);

    expect(items).toHaveLength(1);
    expect(items[0]?.use.toolInput).toEqual({ command: "git status" }); // later event wins
    expect(items[0]?.result?.contentBlocks?.[0]?.text).toBe("clean");
  });

  it("keeps fanned-out fileChange files (#N) as separate, individually correlated items", () => {
    const events: ChatEvent[] = [
      toolUse("item_9#0", "Edit", { file_path: "a.ts", diff: "@@ a" }),
      toolUse("item_9#1", "Write", { file_path: "b.ts", diff: "@@ b" }),
      toolResult("item_9#0", "diff-a"),
      toolResult("item_9#1", "diff-b"),
    ];

    const items = toolItems(buildSegments(events, true).segments);

    expect(items).toHaveLength(2);
    expect(items[0]?.result?.contentBlocks?.[0]?.text).toBe("diff-a");
    expect(items[1]?.result?.contentBlocks?.[0]?.text).toBe("diff-b");
  });
});
