import { test, expect } from "@playwright/test";
import {
  type Scenario,
  type SeedRequest,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  BASIC_SCENARIO,
  text,
  thinking,
  result,
  withDelay,
  immediate,
  seedFixture,
  resetFixture,
  TEST_BASE,
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

const STOP_SESSION_ID = "eee00030-0000-4000-8000-000000000030";
const SESSION_NAME = "Stop Resume Test";

function stopSeed(behavior: Scenario[]): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: STOP_SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior,
        autoApproveMode: "auto",
      },
    ],
  };
}

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Session stop and resume", () => {
  test("stopped session shows resume banner after navigation", async ({ page, request }) => {
    const resumeScenario: Scenario = {
      events: [
        immediate(thinking("Picking up where I left off.")),
        withDelay(20, text("Resumed successfully.")),
        withDelay(10, result()),
      ],
    };
    await seedFixture(request, stopSeed([BASIC_SCENARIO, resumeScenario]));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Turn 1: run the basic scenario to completion.
    await sendQuery(page, composer, "Check the project");
    await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, STOP_SESSION_ID, "idle");

    // Stop the session via REST API.
    const stopResp = await request.post(`${TEST_BASE}/api/sessions/${STOP_SESSION_ID}/stop`);
    expect(stopResp.ok()).toBeTruthy();

    // Navigate away and back to pick up the stopped state from DB.
    await page.goto(`/project/${TEST_PROJECT.slug}`);
    await expect(page.getByText(SESSION_NAME)).toBeVisible({ timeout: 10_000 });
    await page.getByText(SESSION_NAME).click();

    // Resume banner should appear.
    await expect(page.getByText("Session interrupted")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeVisible();
  });

  test("mark-done shows Session complete banner", async ({ page, request }) => {
    await seedFixture(request, stopSeed([BASIC_SCENARIO]));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Complete a turn first.
    await sendQuery(page, composer, "Check config");
    await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, STOP_SESSION_ID, "idle");

    // Click mark-done button in the session header.
    const markDoneBtn = page.getByTitle("Mark done");
    await expect(markDoneBtn).toBeVisible();
    await markDoneBtn.click();

    // Resume banner should show "Session complete" with "Continue" button.
    await expect(page.getByText("Session complete")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: "Continue" })).toBeVisible();
  });
});
