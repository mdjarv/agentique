import { describe, expect, it } from "vitest";
import type { ChatEvent, Turn } from "~/stores/chat-types";
import { type AgentRun, collectAgentRuns } from "./agent-runs";

const SPAWN = "toolu_spawn_1";
const AGENT_ID = "a1b2c3";

/** Fold one turn's events and return the single run they describe. */
function onlyRun(events: ChatEvent[]): AgentRun {
  const runs = collectAgentRuns([{ turnIndex: 1, events } as Turn], undefined);
  const run = runs[0];
  if (runs.length !== 1 || !run) throw new Error(`expected 1 run, got ${runs.length}`);
  return run;
}

const spawnEvents: ChatEvent[] = [
  {
    id: "e1",
    type: "tool_use",
    toolId: SPAWN,
    toolName: "Agent",
    toolInput: { description: "Check the thing", subagent_type: "Explore" },
    category: "agent",
  } as ChatEvent,
  {
    id: "e2",
    type: "task",
    taskSubtype: "task_started",
    taskId: AGENT_ID,
    toolUseId: SPAWN,
    taskDescription: "Check the thing",
    taskType: "local_agent",
  } as ChatEvent,
  {
    id: "e3",
    type: "task",
    taskSubtype: "task_notification",
    taskId: AGENT_ID,
    toolUseId: SPAWN,
    taskStatus: "completed",
  } as ChatEvent,
];

function agentResult(text: string, agentId = AGENT_ID): ChatEvent {
  return {
    id: "ar",
    type: "agent_result",
    status: "completed",
    agentId,
    agentType: "Explore",
    contentBlocks: [{ type: "text", text }],
    // The server sends this empty for a top-level agent result — the whole
    // reason the roster needs the agentId → taskId → toolUseId join.
    parentToolUseId: "",
  } as ChatEvent;
}

function toolResult(text: string): ChatEvent {
  return {
    id: "tr",
    type: "tool_result",
    toolId: SPAWN,
    contentBlocks: [{ type: "text", text }],
  } as ChatEvent;
}

describe("collectAgentRuns — agent_result join", () => {
  it("prefers the agent_result report over the truncated tool_result", () => {
    const run = onlyRun([
      ...spawnEvents,
      toolResult("head\n...[truncated]...\ntail"),
      agentResult("head\nmiddle\ntail"),
    ]);

    expect(run.report).toBe("head\nmiddle\ntail");
    expect(run.state).toBe("done");
  });

  it("falls back to the tool_result when no agent_result joins", () => {
    expect(onlyRun([...spawnEvents, toolResult("only source")]).report).toBe("only source");
  });

  it("ignores an agent_result whose agentId matches no task", () => {
    const run = onlyRun([
      ...spawnEvents,
      toolResult("from the tool result"),
      agentResult("stray", "unrelated"),
    ]);

    expect(run.report).toBe("from the tool result");
  });

  it("keeps agent_result out of the narration steps", () => {
    const run = onlyRun([
      ...spawnEvents,
      { id: "s1", type: "text", content: "working", parentToolUseId: SPAWN } as ChatEvent,
      agentResult("report"),
    ]);

    expect(run.steps).toHaveLength(1);
    expect(run.steps.map((s) => s.type)).toEqual(["text"]);
  });

  it("does not report an empty agent_result as the return value", () => {
    // A background launch: real event, real agentId, no content yet.
    const launched = { ...agentResult(""), status: "async_launched" } as ChatEvent;
    const run = onlyRun([...spawnEvents, toolResult("launched in the background"), launched]);

    expect(run.report).toBe("launched in the background");
  });
});
