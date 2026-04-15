import { test, expect, type Page } from "@playwright/test";
import {
  type Scenario,
  type SeedRequest,
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
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

// --- Constants ---

const SESSION_1_ID = "eee00070-0000-4000-8000-000000000070";
const SESSION_2_ID = "eee00071-0000-4000-8000-000000000071";
const SESSION_1_NAME = "Lead Agent";
const SESSION_2_NAME = "Worker Agent";
const TEAM_NAME = "Alpha Squad";

// --- Scenarios ---

/** Session 1 reads a file, then sends a message to Worker Agent. */
const SCENARIO_SEND_MESSAGE: Scenario = {
  events: [
    immediate(thinking("I should delegate the test work to Worker Agent.")),
    withDelay(20, text("Let me check the code first, then delegate.")),
    withDelay(
      30,
      toolUse("team-read-001", "Read", { file_path: "/tmp/fixture-project/src/app.ts" }),
    ),
    withDelay(20, toolResult("team-read-001", "export function main() { return 42; }")),
    withDelay(20, text("Delegating test writing to Worker Agent.")),
    withDelay(
      30,
      toolUse("team-send-001", "SendMessage", {
        to: SESSION_2_NAME,
        content: "Please write tests for the main function in src/app.ts",
      }),
    ),
    withDelay(20, toolResult("team-send-001", "Message sent to Worker Agent")),
    withDelay(20, text("Delegated to the worker. Waiting for their response.")),
    withDelay(10, result()),
  ],
};

/** Simple read-only scenario for the worker session. */
const SCENARIO_WORKER_IDLE: Scenario = {
  events: [
    immediate(thinking("Ready to receive work.")),
    withDelay(20, text("Standing by for instructions.")),
    withDelay(10, result()),
  ],
};

// --- Seed helpers ---

function teamSeed(): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: SESSION_1_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_1_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [SCENARIO_SEND_MESSAGE],
        autoApproveMode: "auto",
      },
      {
        id: SESSION_2_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_2_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [SCENARIO_WORKER_IDLE],
        autoApproveMode: "auto",
      },
    ],
  };
}

// --- UI helpers ---

/** Open the session header overflow menu (MoreHorizontal button). */
async function openOverflowMenu(page: Page) {
  // The overflow trigger is the last button in the header actions (no accessible name, just an icon).
  const header = page.locator("header");
  // Wait for header to render.
  await expect(header).toBeVisible({ timeout: 5_000 });
  // The overflow button is in the right-side group, after "Mark done".
  // Use a CSS class-based selector since the button has no accessible name.
  const trigger = header.locator("button").last();
  await trigger.click();
}

/** Create a team from the currently active session. */
async function createTeamFromUI(page: Page, name: string, role = "") {
  await openOverflowMenu(page);
  await page.getByText("Create team...").click();
  await page.getByPlaceholder("Team name").fill(name);
  if (role) {
    await page.getByPlaceholder("Your role (optional)").fill(role);
  }
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByText("Team created")).toBeVisible({ timeout: 5_000 });
}

/** Join an existing team from the currently active session. */
async function joinTeamFromUI(page: Page, role = "") {
  await openOverflowMenu(page);
  await page.getByText("Join team...").click();
  if (role) {
    await page.getByPlaceholder("Your role (optional)").fill(role);
  }
  await page.getByRole("button", { name: "Join" }).click();
  await expect(page.getByText("Joined team")).toBeVisible({ timeout: 5_000 });
}

// --- Tests ---

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Team lifecycle", () => {
  test("create team and join session via UI", async ({ page, request }) => {
    await seedFixture(request, teamSeed());
    await navigateToSession(page, SESSION_1_NAME);

    await createTeamFromUI(page, TEAM_NAME, "lead");

    // Team tab should now be visible.
    await expect(page.getByRole("button", { name: "Team", exact: true })).toBeVisible({
      timeout: 5_000,
    });
  });

  test("second session can join existing team", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    // Session 1 creates the team.
    await navigateToSession(page, SESSION_1_NAME);
    await createTeamFromUI(page, TEAM_NAME, "lead");

    // Navigate to session 2 and join.
    await navigateToSession(page, SESSION_2_NAME);
    await joinTeamFromUI(page, "worker");

    // Team tab should appear.
    await expect(page.getByRole("button", { name: "Team", exact: true })).toBeVisible({
      timeout: 5_000,
    });
  });

  test("team view shows all members with correct state", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    // Create team and join both sessions.
    await navigateToSession(page, SESSION_1_NAME);
    await createTeamFromUI(page, TEAM_NAME, "lead");
    await navigateToSession(page, SESSION_2_NAME);
    await joinTeamFromUI(page, "worker");

    // Open Team tab.
    await page.getByRole("button", { name: "Team", exact: true }).click();

    // Both members should be visible with roles (scope to main to avoid sidebar/select matches).
    const main = page.getByRole("main");
    await expect(main.getByText(SESSION_1_NAME, { exact: true })).toBeVisible({ timeout: 5_000 });
    await expect(main.getByText(`${SESSION_2_NAME} (you)`)).toBeVisible();
    await expect(main.getByText("lead", { exact: true })).toBeVisible();
    await expect(main.getByText("worker", { exact: true })).toBeVisible();
  });
});

test.describe("Team message routing", () => {
  test("SendMessage tool routes message to teammate", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    // Create team and join both sessions.
    await navigateToSession(page, SESSION_1_NAME);
    await createTeamFromUI(page, TEAM_NAME, "lead");
    await navigateToSession(page, SESSION_2_NAME);
    await joinTeamFromUI(page, "worker");

    // Go to session 1 and trigger the scenario with SendMessage.
    await navigateToSession(page, SESSION_1_NAME);
    const composer = page.getByPlaceholder("Send a message...");
    await sendQuery(page, composer, "Check app.ts and delegate tests");

    // SendMessage tool_use should render in session 1's chat.
    await expect(page.getByText("Delegated to the worker")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, SESSION_1_ID, "idle");

    // Navigate to session 2 and check the Team tab for the received message.
    await navigateToSession(page, SESSION_2_NAME);
    await page.getByRole("button", { name: "Team", exact: true }).click();

    // The routed message should appear in the team timeline.
    await expect(
      page.getByText("Please write tests for the main function"),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("user can send message to teammate via Team view", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    // Set up team with both sessions.
    await navigateToSession(page, SESSION_1_NAME);
    await createTeamFromUI(page, TEAM_NAME);
    await navigateToSession(page, SESSION_2_NAME);
    await joinTeamFromUI(page);

    // Open Team tab from session 2.
    await page.getByRole("button", { name: "Team", exact: true }).click();

    // Select session 1 as target from dropdown.
    await page.locator("select").selectOption({ label: SESSION_1_NAME });

    // Type and send a message.
    await page.getByPlaceholder("Message...").fill("Status update: tests are passing");
    await page.getByPlaceholder("Message...").press("Enter");

    // Message should appear in the timeline.
    await expect(page.getByText("Status update: tests are passing")).toBeVisible({ timeout: 5_000 });
  });

  test("leave team removes team tab", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    await navigateToSession(page, SESSION_1_NAME);
    await createTeamFromUI(page, TEAM_NAME);
    await expect(page.getByRole("button", { name: "Team", exact: true })).toBeVisible({
      timeout: 5_000,
    });

    // Leave team via overflow menu.
    await openOverflowMenu(page);
    await page.getByText("Leave team").click();
    await expect(page.getByText("Left team")).toBeVisible({ timeout: 5_000 });

    // Team tab should disappear.
    await expect(page.getByRole("button", { name: "Team", exact: true })).not.toBeVisible({
      timeout: 5_000,
    });
  });
});
