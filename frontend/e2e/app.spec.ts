import { test, expect } from "@playwright/test";
import fs from "fs";
import os from "os";
import path from "path";

test.describe("App loads", () => {
  test("shows sidebar with Agentique title", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByText("Agentique", { exact: true })).toBeVisible();
  });

  test("shows empty state message", async ({ page }) => {
    await page.goto("/");
    await expect(
      page.getByText("Select a project or create one to get started"),
    ).toBeVisible();
  });

  test("shows New Project button", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByText("New Project")).toBeVisible();
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
  test("starts with empty project list", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByText("No projects yet")).toBeVisible();
  });

  test("can create a project via the dialog", async ({ page }) => {
    await page.goto("/");

    // Open the new project dialog
    await page.getByText("New Project").click();
    await expect(page.getByText("Create New Project")).toBeVisible();

    // Create button should be disabled without a path
    await expect(page.getByRole("button", { name: "Create" })).toBeDisabled();

    // Fill in the directory -- name should auto-fill from path
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "agentique-test-"));
    await page.getByLabel("Directory").fill(tempDir);

    // Override the auto-filled name
    await page.getByLabel("Name").fill("Test Project");

    // Create button should now be enabled
    await expect(page.getByRole("button", { name: "Create" })).toBeEnabled();

    // Submit
    await page.getByRole("button", { name: "Create" }).click();

    // Dialog should close and project should appear in sidebar
    await expect(page.getByText("Test Project")).toBeVisible();

    // Should navigate to the project's chat view with message composer
    await expect(page.getByPlaceholder("Send a message...")).toBeVisible();

    // Clean up temp dir
    fs.rmdirSync(tempDir);
  });

  test("project appears in sidebar after creation", async ({ page }) => {
    await page.goto("/");

    // The project created in the previous test persists via SQLite
    await expect(page.getByText("Test Project")).toBeVisible({ timeout: 5000 });
  });

  test("can click a project to see chat panel", async ({ page }) => {
    await page.goto("/");

    // Wait for project list to load and click the project
    await page.getByText("Test Project").click();

    // Verify chat panel components render
    await expect(page.getByPlaceholder("Send a message...")).toBeVisible();
  });
});

test.describe("Chat UI", () => {
  test("shows empty chat state", async ({ page }) => {
    await page.goto("/");
    await page.getByText("Test Project").click();

    // Should show "Send a message to start chatting" empty state
    await expect(page.getByText("Send a message to start chatting")).toBeVisible();
  });

  test("message composer is visible and enabled", async ({ page }) => {
    await page.goto("/");
    await page.getByText("Test Project").click();

    const textarea = page.getByPlaceholder("Send a message...");
    await expect(textarea).toBeVisible();
    // Composer should not be disabled (it's only disabled while running)
    await expect(textarea).not.toBeDisabled();
  });

  test("new session button visible on project hover", async ({ page }) => {
    await page.goto("/");

    // Hover over the project to reveal new session button
    const projectItem = page.getByText("Test Project").first();
    await projectItem.hover();

    await expect(page.getByLabel("New session")).toBeVisible();
  });
});

test.describe("SPA routing", () => {
  test("handles direct navigation to project route", async ({ page }) => {
    // Navigate directly to a project URL -- should not 404
    await page.goto("/project/nonexistent-id");

    // The SPA should load (sidebar should be visible)
    await expect(page.getByText("Agentique", { exact: true })).toBeVisible();
  });

  test("handles page refresh on project route", async ({ page }) => {
    await page.goto("/");
    await page.getByText("Test Project").click();

    // Reload the page
    await page.reload();

    // SPA should re-render correctly
    await expect(page.getByText("Agentique", { exact: true })).toBeVisible();
  });
});

test.describe("Delete project", () => {
  test("can delete a project from the sidebar", async ({ page }) => {
    await page.goto("/");

    // Wait for the project to appear
    await expect(page.getByText("Test Project")).toBeVisible();

    // Hover over the project to reveal the delete button
    const projectItem = page.getByText("Test Project").first();
    await projectItem.hover();

    // Click the delete button (trash icon) - force click since it's opacity-0 until hover
    await page.getByLabel("Delete project").click({ force: true });

    // Project should be removed from sidebar
    await expect(page.getByText("No projects yet")).toBeVisible({ timeout: 5000 });
  });
});
