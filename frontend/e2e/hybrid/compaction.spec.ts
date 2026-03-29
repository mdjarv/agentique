import { test, expect } from "@playwright/test";
import { compactChatSeed, resetFixture, seedFixture, COMPACT_SESSION_ID } from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Context compaction", () => {
  test("compact_boundary renders auto-compaction divider", async ({ page, request }) => {
    await seedFixture(request, compactChatSeed());

    const composer = await navigateToSession(page, "Compaction Test");

    // Turn 1: Grep + text.
    await sendQuery(page, composer, "Find the TODOs");
    await expect(page.getByText("I found several files that need updating.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForState(request, COMPACT_SESSION_ID, "idle");

    // Turn 2: includes compact_boundary at the start.
    await sendQuery(page, composer, "Fix them");
    await expect(page.getByText("Auto-compacted")).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText(/from 95k/)).toBeVisible();

    // Verify the post-compaction turn's content renders.
    await expect(page.getByText("All TODO items have been resolved.")).toBeVisible({
      timeout: 10_000,
    });
    await waitForState(request, COMPACT_SESSION_ID, "idle");
  });
});
