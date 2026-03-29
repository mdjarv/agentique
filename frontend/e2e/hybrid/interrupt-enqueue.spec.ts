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
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

// --- Constants ---

const SESSION_ID = "eee00080-0000-4000-8000-000000000080";
const SESSION_NAME = "Interrupt Test";

// --- Scenarios ---

/** Slow scenario with long delays between events — gives time to interrupt. */
const SCENARIO_SLOW: Scenario = {
  events: [
    immediate(thinking("Starting a long operation.")),
    withDelay(50, text("Step 1: reading files.")),
    withDelay(100, toolUse("slow-read-001", "Read", { file_path: "/tmp/fixture-project/a.ts" })),
    withDelay(100, toolResult("slow-read-001", "const a = 1;")),
    withDelay(100, text("Step 2: processing.")),
    withDelay(200, toolUse("slow-read-002", "Read", { file_path: "/tmp/fixture-project/b.ts" })),
    withDelay(200, toolResult("slow-read-002", "const b = 2;")),
    withDelay(200, text("Step 3: finalizing.")),
    withDelay(100, text("All steps complete.")),
    withDelay(50, result()),
  ],
};

/** Quick follow-up scenario (consumed after interrupt + resume or enqueue). */
const SCENARIO_FOLLOWUP: Scenario = {
  events: [
    immediate(thinking("Handling the follow-up.")),
    withDelay(20, text("Got your follow-up message.")),
    withDelay(10, result()),
  ],
};

/** Quick scenario for enqueue test. */
const SCENARIO_QUICK: Scenario = {
  events: [
    immediate(thinking("Processing.")),
    withDelay(50, text("Working on the initial request.")),
    withDelay(300, text("Still processing...")),
    withDelay(200, text("Done with the first task.")),
    withDelay(50, result()),
  ],
};

// --- Seed helper ---

function interruptSeed(overrides: { behavior: Scenario[]; autoApproveMode?: string }) {
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
      },
    ],
  };
}

// --- Tests ---

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Interrupt", () => {
  test("stop button appears while session is running", async ({ page, request }) => {
    await seedFixture(request, interruptSeed({ behavior: [SCENARIO_SLOW] }));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Run the slow task");

    // Stop button should appear while running.
    const stopBtn = page.getByRole("button", { name: "Stop" });
    await expect(stopBtn).toBeVisible({ timeout: 5_000 });

    // Wait for scenario to complete naturally.
    await expect(page.getByText("All steps complete.")).toBeVisible({ timeout: 15_000 });
    await waitForState(request, SESSION_ID, "idle");

    // Stop button should disappear when idle.
    await expect(stopBtn).not.toBeVisible({ timeout: 5_000 });
  });

  test("clicking stop interrupts generation and session goes idle", async ({ page, request }) => {
    await seedFixture(request, interruptSeed({ behavior: [SCENARIO_SLOW, SCENARIO_FOLLOWUP] }));
    const composer = await navigateToSession(page, SESSION_NAME);

    await sendQuery(page, composer, "Run the slow task");

    // Wait for first text to confirm session is running.
    await expect(page.getByText("Step 1: reading files.")).toBeVisible({ timeout: 5_000 });

    // Click the stop button.
    const stopBtn = page.getByRole("button", { name: "Stop" });
    await expect(stopBtn).toBeVisible({ timeout: 5_000 });
    await stopBtn.click();

    // Session should transition to idle (the interrupt should stop the replay).
    await waitForState(request, SESSION_ID, "idle", 10_000);

    // "All steps complete." should NOT appear (we interrupted before it finished).
    await expect(page.getByText("All steps complete.")).not.toBeVisible();

    // Should be able to send a new query.
    await sendQuery(page, composer, "What happened?");
    await expect(page.getByText("Got your follow-up message.")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, SESSION_ID, "idle");
  });
});

test.describe("Enqueue", () => {
  test("composer shows queue placeholder while running", async ({ page, request }) => {
    await seedFixture(request, interruptSeed({ behavior: [SCENARIO_SLOW] }));
    const composer = await navigateToSession(page, SESSION_NAME);

    // Before sending — normal placeholder.
    await expect(composer).toHaveAttribute("placeholder", "Send a message...");

    await sendQuery(page, composer, "Run the slow task");

    // While running — placeholder should change to queue mode.
    await expect(page.getByText("Step 1: reading files.")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByPlaceholder(/[Qq]ueue/)).toBeVisible({ timeout: 5_000 });

    // Let it finish.
    await expect(page.getByText("All steps complete.")).toBeVisible({ timeout: 15_000 });
    await waitForState(request, SESSION_ID, "idle");

    // Back to normal placeholder.
    await expect(page.getByPlaceholder("Send a message...")).toBeVisible({ timeout: 5_000 });
  });

  test("queued message executes after current turn completes", async ({ page, request }) => {
    await seedFixture(
      request,
      interruptSeed({ behavior: [SCENARIO_QUICK, SCENARIO_FOLLOWUP] }),
    );
    const composer = await navigateToSession(page, SESSION_NAME);

    // Start the first turn.
    await sendQuery(page, composer, "Do the first task");

    // Wait for running state.
    await expect(page.getByText("Working on the initial request.")).toBeVisible({ timeout: 5_000 });

    // Send a follow-up while running (this should be enqueued/injected).
    await composer.fill("Now do the follow-up");
    await page.keyboard.press("Enter");

    // First turn should complete.
    await expect(page.getByText("Done with the first task.")).toBeVisible({ timeout: 10_000 });

    // The queued message should trigger the follow-up scenario.
    await expect(page.getByText("Got your follow-up message.")).toBeVisible({ timeout: 15_000 });
    await waitForState(request, SESSION_ID, "idle");
  });
});
