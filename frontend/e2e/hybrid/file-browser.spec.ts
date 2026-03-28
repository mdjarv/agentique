import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { expect, test } from "@playwright/test";
import { resetFixture, seedFixture } from "./fixtures";
import type { SeedRequest } from "./fixtures";

const FB_PROJECT_ID = "eee00010-0000-4000-8000-000000000010";

let tempDir: string;

test.beforeEach(async ({ request }) => {
  await resetFixture(request);

  // Create a temp directory with test files.
  tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "agentique-fb-"));

  // Directories
  fs.mkdirSync(path.join(tempDir, "src"));
  fs.mkdirSync(path.join(tempDir, ".git")); // should be hidden

  // Files
  fs.writeFileSync(
    path.join(tempDir, "README.md"),
    "# Test Project\n\nHello world.",
  );
  fs.writeFileSync(
    path.join(tempDir, "main.go"),
    'package main\n\nfunc main() {\n\tfmt.Println("hi")\n}',
  );
  fs.writeFileSync(
    path.join(tempDir, "src", "app.ts"),
    'export const greeting = "hello";',
  );

  const seed: SeedRequest = {
    projects: [
      {
        id: FB_PROJECT_ID,
        name: "File Browser Test",
        path: tempDir,
        slug: "file-browser-test",
      },
    ],
    sessions: [],
  };
  await seedFixture(request, seed);
});

test.afterEach(() => {
  if (tempDir) {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

// Scope locators to the main content area to avoid sidebar matches.
function mainContent(page: import("@playwright/test").Page) {
  return page.getByRole("main");
}

test("file browser: lists directories and files, hides .git", async ({ page }) => {
  await page.goto("/project/file-browser-test/files");

  const main = mainContent(page);

  // Should see "src" directory and files (but not .git).
  await expect(main.getByText("src")).toBeVisible({ timeout: 5_000 });
  await expect(main.getByText("README.md")).toBeVisible();
  await expect(main.getByText("main.go")).toBeVisible();

  // .git should not appear in the file list.
  await expect(main.getByText(".git")).not.toBeVisible();
});

test("file browser: navigate into subdirectory and back", async ({ page }) => {
  await page.goto("/project/file-browser-test/files");

  const main = mainContent(page);

  // Click into src directory.
  await main.getByText("src").click();

  // Should see the file in src.
  await expect(main.getByText("app.ts")).toBeVisible({ timeout: 5_000 });

  // Breadcrumbs should show "src" segment.
  await expect(main.getByRole("button", { name: "src" })).toBeVisible();

  // Click the project root breadcrumb to go back.
  await main.getByRole("button", { name: "File Browser Test" }).click();

  // Should see root files again.
  await expect(main.getByText("README.md")).toBeVisible({ timeout: 5_000 });
});

test("file browser: preview markdown file with copy button", async ({ page }) => {
  await page.goto("/project/file-browser-test/files");

  const main = mainContent(page);

  // Click on README.md.
  await main.getByText("README.md").click();

  // Should render markdown content.
  await expect(main.getByText("Hello world.")).toBeVisible({ timeout: 5_000 });

  // Copy button should be visible.
  await expect(main.getByRole("button", { name: /Copy/ })).toBeVisible();
});

test("file browser: preview code file with syntax highlighting", async ({ page }) => {
  await page.goto("/project/file-browser-test/files");

  const main = mainContent(page);

  // Click on main.go.
  await main.getByText("main.go").click();

  // Should show the file content.
  await expect(main.getByText("package main")).toBeVisible({ timeout: 5_000 });
});

test("file browser: deep link to file via URL", async ({ page }) => {
  await page.goto("/project/file-browser-test/files?file=README.md");

  // Preview should be open.
  await expect(mainContent(page).getByText("Hello world.")).toBeVisible({
    timeout: 5_000,
  });
});

test("file browser: mobile back button navigates from preview to list", async ({
  page,
}) => {
  // Set mobile viewport.
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto("/project/file-browser-test/files");

  const main = mainContent(page);

  // Click a file.
  await main.getByText("README.md").click();

  // Should see the file content.
  await expect(main.getByText("Hello world.")).toBeVisible({ timeout: 5_000 });

  // Should see back arrow with "Files" text in the page header.
  const backButton = page.locator("header").getByText("Files");
  await expect(backButton).toBeVisible();

  // Click back.
  await backButton.click();

  // Should see the file list again.
  await expect(main.getByText("main.go")).toBeVisible({ timeout: 5_000 });
});
