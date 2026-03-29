import { test, expect } from "@playwright/test";
import {
  BASIC_SCENARIO,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  seedFixture,
  resetFixture,
  TEST_BASE,
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

const HIST_SESSION_ID = "eee00040-0000-4000-8000-000000000040";
const SESSION_NAME = "History Test";

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Session history", () => {
  test("navigating away and back loads previous messages from DB", async ({ page, request }) => {
    await seedFixture(request, {
      projects: [TEST_PROJECT],
      sessions: [
        {
          id: HIST_SESSION_ID,
          projectId: TEST_PROJECT_ID,
          name: SESSION_NAME,
          workDir: "/tmp/fixture-project",
          live: true,
          behavior: [BASIC_SCENARIO],
          autoApproveMode: "auto",
        },
      ],
    });

    // Navigate to session and complete a turn.
    const composer = await navigateToSession(page, SESSION_NAME);
    await sendQuery(page, composer, "Show me the config");
    await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, HIST_SESSION_ID, "idle");

    // Stop the session so it's no longer live (simulates leaving and coming back).
    await request.post(`${TEST_BASE}/api/sessions/${HIST_SESSION_ID}/stop`);

    // Navigate away to the project root.
    await page.goto(`/project/${TEST_PROJECT.slug}`);
    await expect(page.getByText(SESSION_NAME)).toBeVisible({ timeout: 10_000 });

    // Navigate back to the session.
    await page.getByText(SESSION_NAME).click();

    // Previous messages should load from DB history.
    await expect(page.getByText("Show me the config")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("1 tool call")).toBeVisible({ timeout: 10_000 });

    // Resume banner should be visible since session is stopped.
    await expect(page.getByText("Session interrupted")).toBeVisible({ timeout: 5_000 });
  });
});
