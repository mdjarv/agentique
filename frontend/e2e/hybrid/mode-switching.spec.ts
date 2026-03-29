import { test, expect } from "@playwright/test";
import {
  type Scenario,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  text,
  thinking,
  toolUse,
  toolResult,
  result,
  withDelay,
  immediate,
  seedFixture,
  resetFixture,
  TEST_BASE,
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

// --- Constants ---

const SESSION_ID = "eee00060-0000-4000-8000-000000000060";
const SESSION_NAME = "Mode Switch Test";

// Tool IDs
const TOOL_ENTER_PLAN = "mode-enter-plan-001";
const TOOL_EXIT_PLAN = "mode-exit-plan-001";
const TOOL_READ = "mode-read-001";
const TOOL_BASH = "mode-bash-001";

const PLAN_MD = "## Refactor Plan\n\n1. Extract helper\n2. Add tests\n3. Update imports";

// --- Scenarios ---

/** Agent enters plan mode on its own (no user toggle). */
const SCENARIO_ENTER_PLAN: Scenario = {
  events: [
    immediate(thinking("I should plan this out first.")),
    withDelay(20, text("Let me create a plan for this refactoring.")),
    withDelay(30, toolUse(TOOL_ENTER_PLAN, "EnterPlanMode", {})),
    withDelay(20, toolResult(TOOL_ENTER_PLAN, "Entered plan mode")),
    withDelay(20, text("Now in plan mode. I'll outline the approach.")),
    withDelay(10, result()),
  ],
};

/** Agent enters plan mode, does work, then exits with a plan. */
const SCENARIO_FULL_PLAN_CYCLE: Scenario = {
  events: [
    immediate(thinking("Planning the feature implementation.")),
    withDelay(20, text("Let me analyze the codebase first.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/src/app.ts" })),
    withDelay(20, toolResult(TOOL_READ, "export function main() { return 'hello'; }")),
    withDelay(20, text("Here's my plan for the refactoring.")),
    withDelay(30, toolUse(TOOL_EXIT_PLAN, "ExitPlanMode", { plan: PLAN_MD })),
    withDelay(20, toolResult(TOOL_EXIT_PLAN, "Plan approved")),
    withDelay(20, text("Great, plan approved. Starting implementation.")),
    withDelay(10, result()),
  ],
};

/** Post-plan implementation work (consumed after plan approval auto-query). */
const SCENARIO_POST_PLAN: Scenario = {
  events: [
    immediate(thinking("Implementing the approved plan.")),
    withDelay(20, text("Extracting the helper function now.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/src/helper.ts" })),
    withDelay(20, toolResult(TOOL_READ, "export function helper() { return 42; }")),
    withDelay(20, text("Helper extracted successfully.")),
    withDelay(10, result()),
  ],
};

/** Bash-only scenario for permission toggling tests. */
const SCENARIO_BASH: Scenario = {
  events: [
    immediate(thinking("Running the command.")),
    withDelay(20, text("Executing the build.")),
    withDelay(30, toolUse(TOOL_BASH, "Bash", { command: "npm run build" })),
    withDelay(20, toolResult(TOOL_BASH, "Build succeeded")),
    withDelay(20, text("Build complete.")),
    withDelay(10, result()),
  ],
};

/** Read-only scenario (plan-safe). */
const SCENARIO_READ_ONLY: Scenario = {
  events: [
    immediate(thinking("Reading the file.")),
    withDelay(20, text("Let me check the configuration.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/config.ts" })),
    withDelay(20, toolResult(TOOL_READ, "export default { port: 3000 };")),
    withDelay(20, text("Configuration looks correct.")),
    withDelay(10, result()),
  ],
};

/** Agent enters plan mode by itself, reads (plan-safe), then tries Bash (not plan-safe). */
const SCENARIO_ENTER_PLAN_THEN_BASH: Scenario = {
  events: [
    immediate(thinking("Let me plan this.")),
    withDelay(20, toolUse(TOOL_ENTER_PLAN, "EnterPlanMode", {})),
    withDelay(20, toolResult(TOOL_ENTER_PLAN, "Entered plan mode")),
    withDelay(20, text("Reading the source first.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/main.ts" })),
    withDelay(20, toolResult(TOOL_READ, "const x = 1;")),
    withDelay(20, text("Now running a check.")),
    withDelay(30, toolUse(TOOL_BASH, "Bash", { command: "npm run lint" })),
    withDelay(20, toolResult(TOOL_BASH, "No errors")),
    withDelay(20, text("Lint passed.")),
    withDelay(10, result()),
  ],
};

// --- Seed helper ---

function modeSeed(overrides: {
  behavior: Scenario[];
  autoApproveMode?: string;
  planMode?: boolean;
}) {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: overrides.behavior,
        autoApproveMode: overrides.autoApproveMode ?? "auto",
        planMode: overrides.planMode,
      },
    ],
  };
}

async function waitForIdle(request: import("@playwright/test").APIRequestContext) {
  await waitForState(request, SESSION_ID, "idle");
}

// --- Tests ---

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Permission level toggling", () => {
  test("fullAuto to manual: next tool requires approval", async ({ page, request }) => {
    // Start in fullAuto with two Bash turns. First auto-approves, switch to manual, second needs approval.
    await seedFixture(request, modeSeed({ behavior: [SCENARIO_BASH, SCENARIO_BASH], autoApproveMode: "fullAuto" }));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Turn 1: fullAuto, Bash auto-approves.
    await sendQuery(page, composer, "Build it");
    await expect(page.getByText("Build complete.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);

    // Downgrade: fullAuto → manual (two clicks: fullAuto → manual cycles).
    await page.getByRole("button", { name: "Full Auto" }).click();
    await expect(page.getByRole("button", { name: "Manual" })).toBeVisible();

    // Turn 2: manual, Bash needs approval.
    await sendQuery(page, composer, "Build again");
    const allowBtn = page.getByRole("button", { name: "Allow", exact: true });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await allowBtn.click();

    await expect(page.getByText("Build complete.").nth(1)).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });

  test("switching to fullAuto auto-resolves pending approval", async ({ page, request }) => {
    // Start in manual. Tool triggers approval banner. Switch to fullAuto — banner should dismiss automatically.
    await seedFixture(request, modeSeed({ behavior: [SCENARIO_BASH], autoApproveMode: "manual" }));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Run the build");

    // Approval banner appears for Bash.
    const allowBtn = page.getByRole("button", { name: "Allow", exact: true });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });

    // Switch manual → auto → fullAuto (two clicks).
    await page.getByRole("button", { name: "Manual" }).click();
    await page.getByRole("button", { name: "Auto" }).click();
    await expect(page.getByRole("button", { name: "Full Auto" })).toBeVisible();

    // Approval should auto-resolve — banner should disappear and scenario should complete.
    await expect(allowBtn).not.toBeVisible({ timeout: 5_000 });
    await expect(page.getByText("Build complete.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });

  test("permission mode persists after stop and reopen", async ({ page, request }) => {
    await seedFixture(request, modeSeed({ behavior: [SCENARIO_READ_ONLY], autoApproveMode: "manual" }));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Switch to fullAuto.
    await page.getByRole("button", { name: "Manual" }).click();
    await page.getByRole("button", { name: "Auto" }).click();
    await expect(page.getByRole("button", { name: "Full Auto" })).toBeVisible();

    // Complete a turn so there's state to persist.
    await sendQuery(page, composer, "Check config");
    await expect(page.getByText("Configuration looks correct.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);

    // Stop session.
    const stopResp = await request.post(`${TEST_BASE}/api/sessions/${SESSION_ID}/stop`);
    expect(stopResp.ok()).toBeTruthy();

    // Navigate away and back.
    await page.goto(`/project/${TEST_PROJECT.slug}`);
    await expect(page.getByText(SESSION_NAME)).toBeVisible({ timeout: 10_000 });
    await page.getByText(SESSION_NAME).click();

    // Mode should still be fullAuto.
    await expect(page.getByRole("button", { name: "Full Auto" })).toBeVisible({ timeout: 5_000 });
  });
});

test.describe("Plan acceptance", () => {
  test("accept plan and agent continues working in same session", async ({ page, request }) => {
    // Seed: turn 1 = plan cycle (ExitPlanMode), turn 2 = post-plan work (consumed by auto-query).
    await seedFixture(
      request,
      modeSeed({
        behavior: [SCENARIO_FULL_PLAN_CYCLE, SCENARIO_POST_PLAN],
        planMode: true,
        autoApproveMode: "auto",
      }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Refactor the module");

    // PlanReviewBanner should appear with the plan.
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Refactor Plan")).toBeVisible({ timeout: 5_000 });

    // Approve the plan.
    await page.getByRole("button", { name: "Continue with plan" }).click();

    // After approval, mode should switch to Chat and agent should continue.
    await expect(page.getByRole("button", { name: "Chat", exact: true })).toBeVisible({
      timeout: 10_000,
    });

    // Post-plan work should render (the second scenario).
    await expect(page.getByText("Helper extracted successfully.")).toBeVisible({ timeout: 15_000 });
    await waitForIdle(request);
  });

  test("Keep chatting then approve revised plan", async ({ page, request }) => {
    // Two plan cycles: first rejected (Keep chatting), second approved.
    await seedFixture(
      request,
      modeSeed({
        behavior: [SCENARIO_FULL_PLAN_CYCLE, SCENARIO_FULL_PLAN_CYCLE, SCENARIO_POST_PLAN],
        planMode: true,
        autoApproveMode: "auto",
      }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    // Turn 1: plan exit → reject.
    await sendQuery(page, composer, "Refactor the module");
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Keep chatting" }).click();

    // Banner should dismiss, session stays in plan mode.
    await expect(page.getByText("Plan ready for review")).not.toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole("button", { name: "Plan", exact: true })).toBeVisible();
    await waitForIdle(request);

    // Turn 2: plan exit → approve.
    await sendQuery(page, composer, "Revise the plan and try again");
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Continue with plan" }).click();

    // Agent continues with post-plan work.
    await expect(page.getByText("Helper extracted successfully.")).toBeVisible({ timeout: 15_000 });
    await waitForIdle(request);
  });

  test("Start fresh creates new session with plan context", async ({ page, request }) => {
    await seedFixture(
      request,
      modeSeed({
        behavior: [SCENARIO_FULL_PLAN_CYCLE],
        planMode: true,
        autoApproveMode: "auto",
      }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Plan the refactoring");

    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });

    // Click Start fresh.
    await page.getByRole("button", { name: "Start fresh" }).click();

    // Should navigate to a new session (different URL).
    await expect(page).not.toHaveURL(new RegExp(SESSION_ID.slice(0, 8)), { timeout: 10_000 });

    // New session should have a composer (running with enqueued plan).
    await expect(page.getByPlaceholder("Queue a follow-up...")).toBeVisible({ timeout: 5_000 });
  });
});

test.describe("Agent-initiated mode switching", () => {
  test("EnterPlanMode tool switches UI to Plan indicator", async ({ page, request }) => {
    // Session starts in default (Chat) mode. Agent calls EnterPlanMode.
    await seedFixture(
      request,
      modeSeed({ behavior: [SCENARIO_ENTER_PLAN], autoApproveMode: "auto" }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    // Verify starting in Chat mode.
    await expect(page.getByRole("button", { name: "Chat", exact: true })).toBeVisible();

    await sendQuery(page, composer, "Plan the refactoring");

    // After EnterPlanMode, UI should switch to Plan indicator.
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible({ timeout: 10_000 });

    // Session should complete.
    await expect(page.getByText("outline the approach")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);

    // Plan mode should persist after turn completes.
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible();
  });

  test("full plan cycle: agent enters plan, exits, user approves, back to Chat", async ({
    page,
    request,
  }) => {
    // Agent starts in Chat mode, enters plan, reads files, exits plan. User approves. Back to Chat.
    await seedFixture(
      request,
      modeSeed({
        behavior: [SCENARIO_FULL_PLAN_CYCLE, SCENARIO_POST_PLAN],
        autoApproveMode: "auto",
      }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    // Starts in Chat mode.
    await expect(page.getByRole("button", { name: "Chat", exact: true })).toBeVisible();

    await sendQuery(page, composer, "Refactor this module");

    // Agent enters plan mode — UI should switch to Plan.
    // Note: EnterPlanMode is not in SCENARIO_FULL_PLAN_CYCLE — the session starts with planMode: false
    // but the agent calls ExitPlanMode with a plan. The event pipeline detects ExitPlanMode and triggers
    // the plan review flow. Since the session wasn't in plan mode, it enters plan review directly.

    // PlanReviewBanner appears.
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Refactor Plan")).toBeVisible();

    // Approve.
    await page.getByRole("button", { name: "Continue with plan" }).click();

    // Mode should return to Chat.
    await expect(page.getByRole("button", { name: "Chat", exact: true })).toBeVisible({
      timeout: 10_000,
    });

    // Post-plan work renders.
    await expect(page.getByText("Helper extracted successfully.")).toBeVisible({ timeout: 15_000 });
    await waitForIdle(request);
  });

  test("EnterPlanMode activates plan-safe permission filtering", async ({ page, request }) => {
    // Agent enters plan mode by itself. Read auto-approves (plan-safe), Bash needs approval (not plan-safe).
    await seedFixture(
      request,
      modeSeed({ behavior: [SCENARIO_ENTER_PLAN_THEN_BASH], autoApproveMode: "auto" }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Plan and check");

    // Read auto-approves (plan-safe after EnterPlanMode).
    await expect(page.getByText("Now running a check.")).toBeVisible({ timeout: 10_000 });

    // Bash blocked (not plan-safe in plan mode) — approval banner appears.
    const allowBtn = page.getByRole("button", { name: "Allow", exact: true });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await allowBtn.click();

    await expect(page.getByText("Lint passed.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });
});
