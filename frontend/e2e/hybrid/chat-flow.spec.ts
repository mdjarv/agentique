import { expect, test } from "@playwright/test";
import {
  basicChatSeed,
  getTestState,
  immediate,
  resetFixture,
  result,
  seedFixture,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  TEST_SESSION_ID,
  text,
  withDelay,
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
    expect(session?.state).toBe("idle");
  }).toPass({ timeout: 5_000 });
});

test("chat follow mode re-engages after sending from scrolled-up history", async ({
  page,
  request,
}) => {
  const longText = Array.from(
    { length: 70 },
    (_, i) =>
      `Scroll filler paragraph ${i + 1}. This gives the message list enough height to exercise bottom follow behavior.`,
  ).join("\n\n");

  await seedFixture(request, {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: TEST_SESSION_ID,
        projectId: TEST_PROJECT_ID,
        name: "Scroll Follow Test",
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [
          { events: [immediate(text(longText)), immediate(result())] },
          {
            events: [
              withDelay(20, text("Second turn landed at the bottom.")),
              withDelay(10, result()),
            ],
          },
        ],
        autoApproveMode: "auto",
      },
    ],
  });

  await page.goto("/project/fixture-project");
  await page.getByText("Scroll Follow Test").click();

  const composer = page.getByPlaceholder("Send a message...");
  await expect(composer).toBeVisible({ timeout: 5_000 });
  await composer.fill("Make a long response");
  await page.keyboard.press("Enter");

  await expect(page.getByText("Scroll filler paragraph 70")).toBeVisible({ timeout: 10_000 });
  await expect(async () => {
    const states = await getTestState(request);
    const session = states.find((s) => s.id === TEST_SESSION_ID);
    expect(session?.state).toBe("idle");
  }).toPass({ timeout: 5_000 });

  const scroller = page.getByTestId("message-list-scroll");
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThan(8);

  await scroller.hover();
  await page.mouse.wheel(0, -1200);
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeGreaterThan(200);

  await composer.fill("Follow this new turn");
  await page.keyboard.press("Enter");
  await expect(page.getByText("Second turn landed at the bottom.")).toBeVisible({
    timeout: 10_000,
  });
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThan(8);
});
