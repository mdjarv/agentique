import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { expect, test } from "@playwright/test";

// Smoke coverage for the app shell and project CRUD — the parts the hybrid
// suite does not touch, since it seeds projects through the test API instead of
// the UI. Tests share one server and run in order: the project created below is
// the subject of the later tests and is deleted by the last one.

const NEW_PROJECT_NAME = "Test Project";

/**
 * Open a project and return its slug. Navigation goes through the dashboard's
 * project link: the sidebar's project name is a toggle for the session list,
 * not a router link.
 */
async function openProject(page: import("@playwright/test").Page, name: string): Promise<string> {
  await page.goto("/");
  await page.getByRole("link", { name: new RegExp(`^${name}\\b`) }).click();
  await expect(page).toHaveURL(/\/project\//);
  return new URL(page.url()).pathname.split("/")[2] as string;
}

test.describe("App loads", () => {
  test("shows sidebar with Agentique title", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Agentique", exact: true })).toBeVisible();
  });

  test("root route shows the sessions dashboard", async ({ page }) => {
    await page.goto("/");
    // "/" is a dashboard, not an empty state: it summarises projects and
    // sessions even before anything is running.
    const main = page.getByRole("main");
    await expect(main.getByRole("heading", { name: "Sessions", level: 1 })).toBeVisible();
    await expect(main.getByText("Needs attention")).toBeVisible();
  });

  test("shows the new-project button", async ({ page }) => {
    await page.goto("/");
    // Icon-only button — identified by its accessible name, not by its text.
    await expect(page.getByRole("button", { name: "New project" })).toBeVisible();
  });
});

test.describe("Health check", () => {
  test("API health endpoint returns ok", async ({ request }) => {
    const response = await request.get("/api/health");
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.status).toBe("ok");
  });
});

test.describe("Project management", () => {
  test("starts with default project from cwd", async ({ page }) => {
    await page.goto("/");
    // Default project is auto-created from cwd on first launch.
    await expect(page.getByText("agentique", { exact: true }).first()).toBeVisible();
  });

  test("can create a project via the dialog", async ({ page }) => {
    await page.goto("/");

    await page.getByRole("button", { name: "New project" }).click();
    await expect(page.getByRole("heading", { name: "Create New Project" })).toBeVisible();

    // Create button should be disabled without a path.
    await expect(page.getByRole("button", { name: "Create", exact: true })).toBeDisabled();

    // Fill in the directory — name auto-fills from the path.
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "agentique-test-"));
    await page.getByLabel("Directory").fill(tempDir);

    // Override the auto-filled name.
    await page.getByLabel("Name").fill(NEW_PROJECT_NAME);

    await expect(page.getByRole("button", { name: "Create", exact: true })).toBeEnabled();
    await page.getByRole("button", { name: "Create", exact: true }).click();

    // Dialog closes and the project appears in the sidebar.
    await expect(page.getByText(NEW_PROJECT_NAME).first()).toBeVisible();

    fs.rmdirSync(tempDir);
  });

  test("project appears in sidebar after creation", async ({ page }) => {
    await page.goto("/");
    // The project created in the previous test persists via SQLite.
    await expect(page.getByText(NEW_PROJECT_NAME).first()).toBeVisible({ timeout: 5000 });
  });

  test("clicking a project opens its overview", async ({ page }) => {
    await openProject(page, NEW_PROJECT_NAME);

    // A project with no sessions lands on the project overview, not a chat —
    // the composer lives on a session route.
    await expect(page.getByRole("main").getByText(NEW_PROJECT_NAME).first()).toBeVisible();
  });
});

test.describe("Chat UI", () => {
  test("new-session route shows an enabled composer", async ({ page }) => {
    const slug = await openProject(page, NEW_PROJECT_NAME);
    await page.goto(`/project/${slug}/session/new`);

    const textarea = page.getByPlaceholder("Send a message...");
    await expect(textarea).toBeVisible();
    // Only disabled while a turn is running.
    await expect(textarea).not.toBeDisabled();
  });
});

test.describe("SPA routing", () => {
  test("handles direct navigation to project route", async ({ page }) => {
    // Navigate directly to a project URL — should not 404.
    await page.goto("/project/nonexistent-id");
    await expect(page.getByRole("link", { name: "Agentique", exact: true })).toBeVisible();
  });

  test("handles page refresh on project route", async ({ page }) => {
    await openProject(page, NEW_PROJECT_NAME);
    await page.reload();
    await expect(page.getByRole("link", { name: "Agentique", exact: true })).toBeVisible();
  });
});

test.describe("Delete project", () => {
  test("can delete a project from its settings page", async ({ page }) => {
    const slug = await openProject(page, NEW_PROJECT_NAME);

    // Deleting moved out of the sidebar hover row and behind project settings,
    // where it is confirmed by a dialog.
    await page.goto(`/project/${slug}/settings`);

    await page.getByRole("button", { name: "Delete project" }).click();
    await expect(page.getByRole("heading", { name: "Delete project" })).toBeVisible();
    await page
      .getByRole("button", { name: /^Delete/ })
      .last()
      .click();

    await expect(page.getByText(NEW_PROJECT_NAME)).toHaveCount(0, { timeout: 5000 });
  });
});
