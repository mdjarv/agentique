import type { APIRequestContext } from "@playwright/test";

// --- Types matching backend SeedRequest shape ---

export interface WireEvent {
  type: string;
  [key: string]: unknown;
}

export interface ScriptedEvent {
  delay?: number;
  event: WireEvent;
}

export interface Scenario {
  events: ScriptedEvent[];
}

export interface SeedProject {
  id: string;
  name: string;
  path: string;
  slug: string;
}

export interface SeedSession {
  id: string;
  projectId: string;
  name: string;
  workDir: string;
  live: boolean;
  behavior?: Scenario[];
  planMode?: boolean;
  autoApproveMode?: string;
}

export interface SeedRequest {
  projects: SeedProject[];
  sessions: SeedSession[];
}

// --- Constants ---

export const TEST_BASE = process.env.BASE_URL ?? "http://localhost:8090";
export const TEST_API = `${TEST_BASE}/api/test`;

const TEST_PROJECT_ID = "eee00001-0000-4000-8000-000000000001";
const TEST_SESSION_ID = "eee00002-0000-4000-8000-000000000002";
const COMPACT_SESSION_ID = "eee00004-0000-4000-8000-000000000004";

export const TEST_PROJECT: SeedProject = {
  id: TEST_PROJECT_ID,
  name: "Fixture Project",
  path: "/tmp/fixture-project",
  slug: "fixture-project",
};

export { TEST_PROJECT_ID, TEST_SESSION_ID, COMPACT_SESSION_ID };

// --- Event builders ---

export function text(content: string): WireEvent {
  return { type: "text", content };
}

export function thinking(content: string): WireEvent {
  return { type: "thinking", content };
}

export function toolUse(
  toolId: string,
  toolName: string,
  toolInput: Record<string, unknown>,
): WireEvent {
  return { type: "tool_use", toolId, toolName, toolInput };
}

export function toolResult(toolId: string, content: string): WireEvent {
  return {
    type: "tool_result",
    toolId,
    content: [{ type: "text", text: content }],
  };
}

export function result(stopReason = "end_turn", cost = 0.01, duration = 1500): WireEvent {
  return {
    type: "result",
    stopReason,
    cost,
    duration,
    usage: { inputTokens: 1200, outputTokens: 350 },
  };
}

export function compactBoundary(trigger: "manual" | "auto", preTokens: number): WireEvent {
  return { type: "compact_boundary", trigger, preTokens };
}

export function errorEvent(message: string, fatal = false): WireEvent {
  return { type: "error", message, fatal };
}

// --- Delay wrappers ---

export function withDelay(delay: number, event: WireEvent): ScriptedEvent {
  return { delay, event };
}

export function immediate(event: WireEvent): ScriptedEvent {
  return { event };
}

// --- Scenarios ---

const TOOL_ID_READ = "tool-read-001";

/** Basic: thinking -> text -> Read tool_use -> tool_result -> text -> result */
export const BASIC_SCENARIO: Scenario = {
  events: [
    immediate(thinking("Let me analyze the project structure first.")),
    withDelay(20, text("I'll start by reading the main configuration file.")),
    withDelay(30, toolUse(TOOL_ID_READ, "Read", { file_path: "/tmp/fixture-project/config.ts" })),
    withDelay(40, toolResult(TOOL_ID_READ, "export default { port: 3000, env: 'test' };")),
    withDelay(20, text("The configuration looks good. The project is set up correctly with port 3000.")),
    withDelay(10, result()),
  ],
};

const TOOL_ID_GREP = "tool-grep-001";
const TOOL_ID_EDIT = "tool-edit-001";

/** Multi-turn compaction, turn 1: thinking -> text -> Grep -> result */
export const COMPACT_TURN_1: Scenario = {
  events: [
    immediate(thinking("Analyzing the codebase for the refactoring target.")),
    withDelay(20, text("I found several files that need updating.")),
    withDelay(30, toolUse(TOOL_ID_GREP, "Grep", { pattern: "TODO", path: "/tmp/fixture-project" })),
    withDelay(40, toolResult(TOOL_ID_GREP, "Found 3 matches in 2 files")),
    withDelay(20, text("I'll update these files now.")),
    withDelay(10, result("end_turn", 0.02, 2500)),
  ],
};

/** Multi-turn compaction, turn 2: compact_boundary -> thinking -> text -> Edit -> result */
export const COMPACT_TURN_2: Scenario = {
  events: [
    immediate(compactBoundary("auto", 95000)),
    withDelay(20, thinking("Continuing with the refactoring after context compaction.")),
    withDelay(30, text("Now applying the changes to the remaining files.")),
    withDelay(
      30,
      toolUse(TOOL_ID_EDIT, "Edit", {
        file_path: "/tmp/fixture-project/src/main.ts",
        old_string: "// TODO: fix",
        new_string: "// Fixed",
      }),
    ),
    withDelay(40, toolResult(TOOL_ID_EDIT, "Successfully edited file")),
    withDelay(20, text("All TODO items have been resolved.")),
    withDelay(10, result("end_turn", 0.03, 3000)),
  ],
};

// --- Seed factories ---

export function basicChatSeed(): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: TEST_SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: "Basic Chat Test",
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [BASIC_SCENARIO],
        autoApproveMode: "auto",
      },
    ],
  };
}

export function compactChatSeed(): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: COMPACT_SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: "Compaction Test",
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [COMPACT_TURN_1, COMPACT_TURN_2],
        autoApproveMode: "fullAuto",
      },
    ],
  };
}

// --- API helpers ---

export async function seedFixture(request: APIRequestContext, seed: SeedRequest): Promise<void> {
  const resp = await request.post(`${TEST_API}/seed`, { data: seed });
  if (!resp.ok()) {
    throw new Error(`Seed failed: ${resp.status()} ${await resp.text()}`);
  }
}

export async function resetFixture(request: APIRequestContext): Promise<void> {
  const resp = await request.post(`${TEST_API}/reset`);
  if (!resp.ok()) {
    throw new Error(`Reset failed: ${resp.status()} ${await resp.text()}`);
  }
}

export async function getTestState(
  request: APIRequestContext,
): Promise<Array<{ id: string; state: string; live: boolean }>> {
  const resp = await request.get(`${TEST_API}/state`);
  return resp.json();
}

export async function injectEvent(
  request: APIRequestContext,
  sessionId: string,
  event: WireEvent,
): Promise<void> {
  const resp = await request.post(`${TEST_API}/inject-event`, {
    data: { sessionId, event },
  });
  if (!resp.ok()) {
    throw new Error(`Inject-event failed: ${resp.status()} ${await resp.text()}`);
  }
}
