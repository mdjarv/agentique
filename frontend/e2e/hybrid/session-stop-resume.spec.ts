import { expect, test } from "@playwright/test";
import {
  BASIC_SCENARIO,
  immediate,
  resetFixture,
  result,
  type Scenario,
  type SeedRequest,
  seedFixture,
  TEST_BASE,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  text,
  thinking,
  withDelay,
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

  test("archive releases the CLI without claiming the session is done", async ({
    page,
    request,
  }) => {
    await seedFixture(request, stopSeed([BASIC_SCENARIO]));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Complete a turn first.
    await sendQuery(page, composer, "Check config");
    await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, STOP_SESSION_ID, "idle");

    // Click the archive button in the session header.
    const archiveBtn = page.getByTitle("Archive session");
    await expect(archiveBtn).toBeVisible();
    await archiveBtn.click();

    // Archiving files the session away and drops you on the new-session panel,
    // so the banner is only observable on the way back in. Navigate by URL: an
    // archived session is no longer in the sidebar's open list.
    //
    // The state is "stopped", not "done": archive released the idle CLI through
    // the normal stop path rather than fabricating a lifecycle state of its own.
    await waitForState(request, STOP_SESSION_ID, "stopped");
    await page.goto(`/project/${TEST_PROJECT.slug}/session/${STOP_SESSION_ID.slice(0, 8)}`);

    await expect(page.getByText("Session interrupted")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeVisible();

    // And it is offered back: the action inverts on an archived session.
    await expect(page.getByTitle("Unarchive session")).toBeVisible();
  });
});
