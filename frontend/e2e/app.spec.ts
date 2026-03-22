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
      page.getByText("Select a project or create one to get started")
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

    // Fill in the form -- use a temp directory that exists
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "agentique-test-"));
    await page.getByLabel("Name").fill("Test Project");
    await page.getByLabel("Path").fill(tempDir);

    // Submit
    await page.getByRole("button", { name: "Create" }).click();

    // Dialog should close and project should appear in sidebar
    await expect(page.getByText("Test Project")).toBeVisible();

    // Should navigate to the project's chat view
    await expect(page.getByText("Session 1")).toBeVisible();

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
    await expect(page.getByText("Session 1")).toBeVisible();
    await expect(page.getByText("Session 2")).toBeVisible();
    await expect(page.getByPlaceholder("Send a message...")).toBeVisible();
  });
});

test.describe("Static chat UI", () => {
  test("renders hardcoded messages", async ({ page }) => {
    await page.goto("/");

    // Navigate to a project
    await page.getByText("Test Project").click();

    // Check for hardcoded conversation content
    await expect(
      page.getByText("Can you explain the project structure?")
    ).toBeVisible();
    await expect(
      page.getByText("standard monorepo structure", { exact: false })
    ).toBeVisible();
  });

  test("renders session tabs with badges", async ({ page }) => {
    await page.goto("/");
    await page.getByText("Test Project").click();

    await expect(page.getByText("idle")).toBeVisible();
    await expect(page.getByText("running")).toBeVisible();
  });

  test("message composer is visible but disabled", async ({ page }) => {
    await page.goto("/");
    await page.getByText("Test Project").click();

    const textarea = page.getByPlaceholder("Send a message...");
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeDisabled();
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

    // Click the delete button (trash icon) - it's inside the same container
    const projectButton = projectItem.locator("..").locator("..");
    const deleteButton = projectButton.locator("button").last();
    await deleteButton.click();

    // Project should be removed from sidebar
    await expect(page.getByText("No projects yet")).toBeVisible({ timeout: 5000 });
  });
});
