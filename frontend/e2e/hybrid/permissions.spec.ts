import { test, expect, type Page, type Locator, type APIRequestContext } from "@playwright/test";
import {
  type SeedRequest,
  type SeedSession,
  type Scenario,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  BASIC_SCENARIO,
  text,
  thinking,
  toolUse,
  toolResult,
  result,
  withDelay,
  immediate,
  seedFixture,
  resetFixture,
  getTestState,
} from "./fixtures";

// --- Constants ---

const PERM_SESSION_ID = "eee00005-0000-4000-8000-000000000005";
const SESSION_NAME = "Permission Test";

// Tool IDs
const TOOL_BASH = "perm-bash-001";
const TOOL_READ = "perm-read-001";
const TOOL_EXIT_PLAN = "perm-exit-plan-001";

// --- Scenarios ---

const SCENARIO_BASH_ONLY: Scenario = {
  events: [
    immediate(thinking("I need to run the test suite.")),
    withDelay(20, text("Let me run the tests now.")),
    withDelay(30, toolUse(TOOL_BASH, "Bash", { command: "npm test" })),
    withDelay(20, toolResult(TOOL_BASH, "All tests passed")),
    withDelay(20, text("Tests completed successfully.")),
    withDelay(10, result()),
  ],
};

const SCENARIO_READ_THEN_BASH: Scenario = {
  events: [
    immediate(thinking("I need to read a file and then run a command.")),
    withDelay(20, text("Let me read the configuration first.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/config.ts" })),
    withDelay(20, toolResult(TOOL_READ, "export default { port: 3000 };")),
    withDelay(20, text("Found the config. Now running the build.")),
    withDelay(30, toolUse(TOOL_BASH, "Bash", { command: "npm run build" })),
    withDelay(20, toolResult(TOOL_BASH, "Build completed")),
    withDelay(20, text("Build finished successfully.")),
    withDelay(10, result()),
  ],
};

const PLAN_MARKDOWN =
  "## Login Page Plan\n\n1. Create LoginPage component\n2. Add form with email and password\n3. Add routing";

const SCENARIO_PLAN_EXIT: Scenario = {
  events: [
    immediate(thinking("Let me create a plan for this feature.")),
    withDelay(20, text("Reading the project structure first.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/src/App.tsx" })),
    withDelay(20, toolResult(TOOL_READ, "function App() { return <div>Hello</div> }")),
    withDelay(20, text("Here is my plan for the login page.")),
    withDelay(30, toolUse(TOOL_EXIT_PLAN, "ExitPlanMode", { plan: PLAN_MARKDOWN })),
    withDelay(20, toolResult(TOOL_EXIT_PLAN, "Plan approved")),
    withDelay(20, text("Plan approved. Starting implementation now.")),
    withDelay(10, result()),
  ],
};

const SCENARIO_PLAN_READ_THEN_BASH: Scenario = {
  events: [
    immediate(thinking("I need to read files and run a command in plan mode.")),
    withDelay(20, text("Reading the source file first.")),
    withDelay(30, toolUse(TOOL_READ, "Read", { file_path: "/tmp/fixture-project/main.ts" })),
    withDelay(20, toolResult(TOOL_READ, "const main = () => console.log('hello');")),
    withDelay(20, text("Read complete. Now running a check.")),
    withDelay(30, toolUse(TOOL_BASH, "Bash", { command: "npm run lint" })),
    withDelay(20, toolResult(TOOL_BASH, "No lint errors found")),
    withDelay(20, text("Lint check passed.")),
    withDelay(10, result()),
  ],
};

// --- Helpers ---

function permSeed(overrides: Partial<SeedSession> & { behavior: Scenario[] }): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: PERM_SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        ...overrides,
      },
    ],
  };
}

async function waitForIdle(request: APIRequestContext) {
  await expect(async () => {
    const states = await getTestState(request);
    const session = states.find((s) => s.id === PERM_SESSION_ID);
    expect(session).toBeDefined();
    expect(session!.state).toBe("idle");
  }).toPass({ timeout: 10_000 });
}

async function navigateToSession(page: Page): Promise<Locator> {
  await page.goto(`/project/${TEST_PROJECT.slug}`);
  const sessionLink = page.getByText(SESSION_NAME);
  await expect(sessionLink).toBeVisible({ timeout: 10_000 });
  await sessionLink.click();
  const composer = page.getByPlaceholder("Send a message...");
  await expect(composer).toBeVisible({ timeout: 5_000 });
  return composer;
}

async function sendQuery(page: Page, composer: Locator, prompt: string) {
  await composer.fill(prompt);
  await page.keyboard.press("Enter");
}

// --- Tests ---

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Permission selector", () => {
  test("manual mode: Bash requires approval", async ({ page, request }) => {
    const seed = permSeed({ behavior: [SCENARIO_BASH_ONLY], autoApproveMode: "manual" });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Verify mode indicator.
    await expect(page.getByRole("button", { name: "Manual" })).toBeVisible();

    await sendQuery(page, composer, "Run the tests");

    // ApprovalBanner should appear for Bash.
    const allowBtn = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Bash").first()).toBeVisible();

    await allowBtn.click();

    // Scenario completes after approval.
    await expect(page.getByText("Tests completed successfully.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });

  test("auto mode: Read auto-approved, Bash requires approval", async ({ page, request }) => {
    const seed = permSeed({ behavior: [SCENARIO_READ_THEN_BASH], autoApproveMode: "auto" });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await expect(page.getByRole("button", { name: "Auto" })).toBeVisible();

    await sendQuery(page, composer, "Read config and build");

    // Read auto-approves — its result text appears without user interaction.
    await expect(page.getByText("Found the config.")).toBeVisible({ timeout: 10_000 });

    // Bash triggers approval banner.
    const allowBtn = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });

    await allowBtn.click();

    await expect(page.getByText("Build finished successfully.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });

  test("change manual to auto mid-session", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_BASH_ONLY, SCENARIO_READ_THEN_BASH],
      autoApproveMode: "manual",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Turn 1: manual mode, Bash needs approval.
    await sendQuery(page, composer, "Run tests");
    const allowBtn = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await allowBtn.click();
    await waitForIdle(request);

    // Switch to auto.
    await page.getByRole("button", { name: "Manual" }).click();
    await expect(page.getByRole("button", { name: "Auto" })).toBeVisible();

    // Turn 2: auto mode. Read auto-approves, Bash needs approval.
    await sendQuery(page, composer, "Read and build");
    await expect(page.getByText("Found the config.")).toBeVisible({ timeout: 10_000 });

    const allowBtn2 = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn2).toBeVisible({ timeout: 10_000 });
    await allowBtn2.click();

    await expect(page.getByText("Build finished successfully.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });

  test("change auto to fullAuto mid-session", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_BASH_ONLY, SCENARIO_BASH_ONLY],
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Turn 1: auto mode, Bash needs approval.
    await sendQuery(page, composer, "Run tests");
    const allowBtn = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await allowBtn.click();
    await waitForIdle(request);

    // Switch auto -> fullAuto.
    await page.getByRole("button", { name: "Auto" }).click();
    await expect(page.getByRole("button", { name: "Full Auto" })).toBeVisible();

    // Turn 2: fullAuto, Bash auto-approves — no banner.
    await sendQuery(page, composer, "Run tests again");
    await expect(page.getByText("Tests completed successfully.").nth(1)).toBeVisible({
      timeout: 10_000,
    });

    // Verify no approval banner appeared.
    await expect(page.getByRole("button", { name: "Allow" })).not.toBeVisible();
    await waitForIdle(request);
  });

  test("Allow all sets auto mode", async ({ page, request }) => {
    const seed = permSeed({ behavior: [SCENARIO_BASH_ONLY], autoApproveMode: "manual" });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await expect(page.getByRole("button", { name: "Manual" })).toBeVisible();

    await sendQuery(page, composer, "Run tests");

    // Click "Allow all" instead of "Allow".
    const allowAllBtn = page.getByRole("button", { name: "Allow all" });
    await expect(allowAllBtn).toBeVisible({ timeout: 10_000 });
    await allowAllBtn.click();

    // Mode should change to auto.
    await expect(page.getByRole("button", { name: "Auto" })).toBeVisible({ timeout: 5_000 });

    await expect(page.getByText("Tests completed successfully.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });
});

test.describe("Chat/Plan toggle", () => {
  test("toggle to plan mode before first query", async ({ page, request }) => {
    const seed = permSeed({ behavior: [SCENARIO_PLAN_EXIT], autoApproveMode: "auto" });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Toggle from Chat to Plan.
    await page.getByRole("button", { name: "Chat" }).click();
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible();

    await sendQuery(page, composer, "Create a login page");

    // Read auto-approves. ExitPlanMode triggers PlanReviewBanner.
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Continue with plan" }).click();

    await expect(page.getByText("Plan approved. Starting implementation now.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForIdle(request);
  });

  test("toggle to plan mode between queries", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [BASIC_SCENARIO, SCENARIO_PLAN_EXIT],
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Turn 1: default mode, auto-approves.
    await sendQuery(page, composer, "Check the project");
    await expect(page.getByText("The configuration looks good.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);

    // Toggle to plan mode.
    await page.getByRole("button", { name: "Chat" }).click();
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible();

    // Turn 2: plan mode.
    await sendQuery(page, composer, "Create a login page");
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Continue with plan" }).click();

    await expect(page.getByText("Plan approved. Starting implementation now.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForIdle(request);
  });

  test("toggle disabled while session running", async ({ page, request }) => {
    const seed = permSeed({ behavior: [SCENARIO_BASH_ONLY], autoApproveMode: "manual" });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await sendQuery(page, composer, "Run tests");

    // Wait for approval banner (session is running).
    await expect(page.getByRole("button", { name: "Allow" })).toBeVisible({ timeout: 10_000 });

    // Plan toggle should be disabled.
    await expect(page.getByRole("button", { name: "Chat" })).toBeDisabled();

    // Resolve and verify toggle re-enables.
    await page.getByRole("button", { name: "Allow" }).click();
    await waitForIdle(request);
    await expect(page.getByRole("button", { name: "Chat" })).toBeEnabled();
  });

  test("plan mode: Read auto-approved, Bash blocked", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_PLAN_READ_THEN_BASH],
      planMode: true,
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    // Session starts in plan mode.
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible();

    await sendQuery(page, composer, "Read and lint");

    // Read auto-approves (plan-safe).
    await expect(page.getByText("Read complete.")).toBeVisible({ timeout: 10_000 });

    // Bash blocked (not plan-safe in plan mode).
    const allowBtn = page.getByRole("button", { name: "Allow" });
    await expect(allowBtn).toBeVisible({ timeout: 10_000 });
    await allowBtn.click();

    await expect(page.getByText("Lint check passed.")).toBeVisible({ timeout: 10_000 });
    await waitForIdle(request);
  });
});

test.describe("Plan approval", () => {
  test("ExitPlanMode shows PlanReviewBanner with correct buttons", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_PLAN_EXIT],
      planMode: true,
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await sendQuery(page, composer, "Create a login page");

    // PlanReviewBanner appears.
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });

    // Correct buttons present.
    await expect(page.getByRole("button", { name: "Continue with plan" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Keep chatting" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Start fresh" })).toBeVisible();

    // ApprovalBanner's "Allow" button should NOT be visible (this is plan review, not tool approval).
    await expect(page.getByRole("button", { name: "Allow" })).not.toBeVisible();

    // Approve and complete.
    await page.getByRole("button", { name: "Continue with plan" }).click();
    await expect(page.getByText("Plan approved. Starting implementation now.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForIdle(request);
  });

  test("Keep chatting denies and stays in plan mode", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_PLAN_EXIT],
      planMode: true,
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await sendQuery(page, composer, "Create a login page");

    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });

    // Click "Keep chatting" to deny.
    await page.getByRole("button", { name: "Keep chatting" }).click();

    // Banner should disappear.
    await expect(page.getByText("Plan ready for review")).not.toBeVisible({ timeout: 5_000 });

    // Session should reach idle eventually (scenario events continue after deny).
    await waitForIdle(request);

    // Plan toggle should still show "Plan" (session stayed in plan mode).
    await expect(page.getByRole("button", { name: "Plan" })).toBeVisible();
  });

  test("fullAuto auto-approves ExitPlanMode without banner", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_PLAN_EXIT],
      planMode: true,
      autoApproveMode: "fullAuto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await sendQuery(page, composer, "Create a login page");

    // ExitPlanMode should auto-approve — no PlanReviewBanner.
    await expect(page.getByText("Plan approved. Starting implementation now.")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText("Plan ready for review")).not.toBeVisible();

    await waitForIdle(request);
  });

  test("PlanReviewBanner renders plan markdown content", async ({ page, request }) => {
    const seed = permSeed({
      behavior: [SCENARIO_PLAN_EXIT],
      planMode: true,
      autoApproveMode: "auto",
    });
    await seedFixture(request, seed);
    const composer = await navigateToSession(page);

    await sendQuery(page, composer, "Create a login page");

    // Banner shows the plan markdown content.
    await expect(page.getByText("Plan ready for review")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Login Page Plan")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByText("Create LoginPage component")).toBeVisible();

    // Clean up.
    await page.getByRole("button", { name: "Continue with plan" }).click();
    await waitForIdle(request);
  });
});
