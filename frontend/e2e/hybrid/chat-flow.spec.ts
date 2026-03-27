import { test, expect } from "@playwright/test";
import {
  basicChatSeed,
  resetFixture,
  seedFixture,
  getTestState,
  TEST_SESSION_ID,
} from "./fixtures";

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("basic chat flow: query produces streamed response with tool use", async ({
  page,
  request,
}) => {
  // Seed project + live session with scripted scenario.
  await seedFixture(request, basicChatSeed());

  // Navigate to the seeded project.
  await page.goto("/project/fixture-project");

  // Wait for the session to appear in the sidebar.
  const sessionLink = page.getByText("Basic Chat Test");
  await expect(sessionLink).toBeVisible({ timeout: 10_000 });

  // Click into the session.
  await sessionLink.click();

  // Wait for the message composer to be ready.
  const composer = page.getByPlaceholder("Send a message...");
  await expect(composer).toBeVisible({ timeout: 5_000 });

  // Type and send a query.
  await composer.fill("Show me the project configuration");
  await page.keyboard.press("Enter");

  // Assert user message appears in the chat.
  await expect(page.getByText("Show me the project configuration")).toBeVisible({ timeout: 5_000 });

  // Assert streamed assistant text appears (from scenario text events).
  await expect(page.getByText("reading the main configuration file")).toBeVisible({
    timeout: 10_000,
  });

  // Assert activity group renders (collapsed tool use).
  await expect(page.getByText("1 tool call")).toBeVisible({ timeout: 10_000 });

  // Assert final assistant text after tool result.
  await expect(page.getByText("The configuration looks good")).toBeVisible({ timeout: 10_000 });

  // Verify session transitioned to idle via test state API.
  // Poll briefly in case the state push hasn't landed yet.
  await expect(async () => {
    const states = await getTestState(request);
    const session = states.find((s) => s.id === TEST_SESSION_ID);
    expect(session).toBeDefined();
    expect(session!.state).toBe("idle");
  }).toPass({ timeout: 5_000 });
});
