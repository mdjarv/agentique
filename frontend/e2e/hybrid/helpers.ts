import { expect, type APIRequestContext, type Locator, type Page } from "@playwright/test";
import {
  type SeedRequest,
  type SeedSession,
  type Scenario,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  getTestState,
} from "./fixtures";

// --- Session factory ---

let sessionCounter = 0;

/**
 * Build a SeedRequest with a single session in the default project.
 * Each call increments a counter to generate unique IDs when tests
 * create multiple seeds within a single spec file.
 */
export function seed(
  overrides: Partial<SeedSession> & { behavior: Scenario[] },
): SeedRequest {
  const suffix = String(++sessionCounter).padStart(4, "0");
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: `eee00099-0000-4000-8000-0000000${suffix}`,
        projectId: TEST_PROJECT_ID,
        name: overrides.name ?? "Test Session",
        workDir: "/tmp/fixture-project",
        live: true,
        ...overrides,
      },
    ],
  };
}

/** Reset the session counter between test files (call in beforeEach). */
export function resetCounter(): void {
  sessionCounter = 0;
}

// --- Navigation helpers ---

/**
 * Navigate to the test project, click a session, and return the composer locator.
 */
export async function navigateToSession(page: Page, sessionName: string): Promise<Locator> {
  await page.goto(`/project/${TEST_PROJECT.slug}`);
  // Use button role for reliable sidebar click (avoids matching text elsewhere).
  const sessionBtn = page.getByRole("button", { name: sessionName, exact: true });
  await expect(sessionBtn).toBeVisible({ timeout: 10_000 });
  await sessionBtn.click();
  // Wait for URL to change to session path (prevents race with previously loaded session).
  await page.waitForURL(/\/session\//, { timeout: 5_000 });
  const composer = page.getByPlaceholder("Send a message...");
  await expect(composer).toBeVisible({ timeout: 5_000 });
  return composer;
}

/**
 * Type a prompt into the composer and press Enter.
 */
export async function sendQuery(page: Page, composer: Locator, prompt: string): Promise<void> {
  await composer.fill(prompt);
  await page.keyboard.press("Enter");
}

// --- State assertions ---

/**
 * Poll the test state API until the given session reaches the expected state.
 */
export async function waitForState(
  request: APIRequestContext,
  sessionId: string,
  expectedState: string,
  timeout = 10_000,
): Promise<void> {
  await expect(async () => {
    const states = await getTestState(request);
    const session = states.find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect(session!.state).toBe(expectedState);
  }).toPass({ timeout });
}
