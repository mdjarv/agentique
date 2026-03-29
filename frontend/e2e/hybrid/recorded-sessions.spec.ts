import { test, expect } from "@playwright/test";
import { resetFixture, seedFixture, getTestState } from "./fixtures";
import { loadFixture, fixtureToSeed, fixturePrompts } from "./fixture-loader";

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Recorded session replay", () => {
  test("basic-chat: text and tool use render correctly", async ({ page, request }) => {
    const fixture = loadFixture("basic-chat");
    const seed = fixtureToSeed(fixture);
    await seedFixture(request, seed);

    await page.goto(`/project/${seed.projects[0]!.slug}`);

    // Click into the session.
    const sessionLink = page.getByText(fixture.metadata.sessionName);
    await expect(sessionLink).toBeVisible({ timeout: 10_000 });
    await sessionLink.click();

    // Send the prompt.
    const composer = page.getByPlaceholder("Send a message...");
    await expect(composer).toBeVisible({ timeout: 5_000 });

    const prompts = fixturePrompts(fixture);
    await composer.fill(prompts[0]!);
    await page.keyboard.press("Enter");

    // Verify assistant response renders.
    await expect(page.getByText("examine the project layout")).toBeVisible({ timeout: 10_000 });

    // Verify tool call rendered.
    await expect(page.getByText("1 tool call")).toBeVisible({ timeout: 10_000 });

    // Verify final text.
    await expect(page.getByText("clean TypeScript setup")).toBeVisible({ timeout: 10_000 });

    // Verify session returns to idle.
    await expect(async () => {
      const states = await getTestState(request);
      const session = states.find((s) => s.id === seed.sessions[0]!.id);
      expect(session).toBeDefined();
      expect(session!.state).toBe("idle");
    }).toPass({ timeout: 5_000 });
  });

  test("multi-tool: multiple tool calls in one turn", async ({ page, request }) => {
    const fixture = loadFixture("multi-tool");
    const seed = fixtureToSeed(fixture);
    await seedFixture(request, seed);

    await page.goto(`/project/${seed.projects[0]!.slug}`);

    const sessionLink = page.getByText(fixture.metadata.sessionName);
    await expect(sessionLink).toBeVisible({ timeout: 10_000 });
    await sessionLink.click();

    const composer = page.getByPlaceholder("Send a message...");
    await expect(composer).toBeVisible({ timeout: 5_000 });

    const prompts = fixturePrompts(fixture);
    await composer.fill(prompts[0]!);
    await page.keyboard.press("Enter");

    // Verify the user message appears.
    await expect(page.getByText("Fix the lint errors")).toBeVisible({ timeout: 5_000 });

    // Verify multiple tool calls rendered (Read + Edit = 2).
    await expect(page.getByText("2 tool calls")).toBeVisible({ timeout: 10_000 });

    // Verify the final assistant text.
    await expect(page.getByText("removed the unused variable")).toBeVisible({ timeout: 10_000 });

    // Session should be idle.
    await expect(async () => {
      const states = await getTestState(request);
      const session = states.find((s) => s.id === seed.sessions[0]!.id);
      expect(session?.state).toBe("idle");
    }).toPass({ timeout: 5_000 });
  });

  test("multi-turn: conversation progresses across turns", async ({ page, request }) => {
    const fixture = loadFixture("multi-turn");
    const seed = fixtureToSeed(fixture);
    await seedFixture(request, seed);

    await page.goto(`/project/${seed.projects[0]!.slug}`);

    const sessionLink = page.getByText(fixture.metadata.sessionName);
    await expect(sessionLink).toBeVisible({ timeout: 10_000 });
    await sessionLink.click();

    const composer = page.getByPlaceholder("Send a message...");
    await expect(composer).toBeVisible({ timeout: 5_000 });

    const prompts = fixturePrompts(fixture);

    // Turn 1: ask about main.ts
    await composer.fill(prompts[0]!);
    await page.keyboard.press("Enter");

    await expect(page.getByText("imports a `helper` function")).toBeVisible({ timeout: 10_000 });

    // Wait for idle before sending next turn.
    await expect(async () => {
      const states = await getTestState(request);
      const session = states.find((s) => s.id === seed.sessions[0]!.id);
      expect(session?.state).toBe("idle");
    }).toPass({ timeout: 5_000 });

    // Turn 2: add validation
    await composer.fill(prompts[1]!);
    await page.keyboard.press("Enter");

    await expect(page.getByText("guard clause")).toBeVisible({ timeout: 10_000 });

    // Both user messages should be visible.
    await expect(page.getByText("What does the main.ts file do?")).toBeVisible();
    await expect(page.getByText("Add input validation")).toBeVisible();
  });
});
