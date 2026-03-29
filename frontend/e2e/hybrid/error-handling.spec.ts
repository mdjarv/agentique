import { test, expect } from "@playwright/test";
import {
  type Scenario,
  type SeedRequest,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  text,
  thinking,
  errorEvent,
  result,
  withDelay,
  immediate,
  seedFixture,
  resetFixture,
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

const ERR_SESSION_ID = "eee00020-0000-4000-8000-000000000020";
const SESSION_NAME = "Error Test";

function errSeed(behavior: Scenario[]): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: ERR_SESSION_ID,
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

test.describe("Error events", () => {
  test("non-fatal error renders warning and session continues", async ({ page, request }) => {
    const scenario: Scenario = {
      events: [
        immediate(thinking("Working on it.")),
        withDelay(20, text("Starting analysis.")),
        withDelay(30, errorEvent("Temporary API failure", false)),
        withDelay(30, text("Recovered. Continuing analysis.")),
        withDelay(10, result()),
      ],
    };
    await seedFixture(request, errSeed([scenario]));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Analyze the code");

    // Non-fatal error should render.
    await expect(page.getByText("Temporary API failure")).toBeVisible({ timeout: 10_000 });

    // Session should continue past the error.
    await expect(page.getByText("Recovered. Continuing analysis.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForState(request, ERR_SESSION_ID, "idle");
  });

  test("fatal error renders destructive style", async ({ page, request }) => {
    const scenario: Scenario = {
      events: [
        immediate(thinking("Starting work.")),
        withDelay(20, text("Reading files.")),
        withDelay(30, errorEvent("Session crashed unexpectedly", true)),
        withDelay(10, result("end_turn")),
      ],
    };
    await seedFixture(request, errSeed([scenario]));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Do the thing");

    // Fatal error should render with error text.
    await expect(page.getByText("Session crashed unexpectedly")).toBeVisible({ timeout: 10_000 });
    // Error title should be "API error" (generic error type from mock).
    await expect(page.getByText("API error")).toBeVisible({ timeout: 5_000 });
  });

  test("multiple errors in one turn all render", async ({ page, request }) => {
    const scenario: Scenario = {
      events: [
        immediate(thinking("Processing.")),
        withDelay(20, errorEvent("First error", false)),
        withDelay(20, errorEvent("Second error", false)),
        withDelay(20, text("Finally succeeded.")),
        withDelay(10, result()),
      ],
    };
    await seedFixture(request, errSeed([scenario]));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Try this");

    await expect(page.getByText("First error")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Second error")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Finally succeeded.")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, ERR_SESSION_ID, "idle");
  });
});
