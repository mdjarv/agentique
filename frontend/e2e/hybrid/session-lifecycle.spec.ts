import { test, expect } from "@playwright/test";
import { TEST_API, resetFixture } from "./fixtures";

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Test mode endpoints", () => {
  test("health check works in test mode", async ({ request }) => {
    const resp = await request.get("http://localhost:8090/api/health");
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body.status).toBe("ok");
  });

  test("seed creates projects and sessions", async ({ request }) => {
    const resp = await request.post(`${TEST_API}/seed`, {
      data: {
        projects: [
          { id: "p1", name: "Test Project", path: "/tmp/test-project", slug: "test-project" },
        ],
        sessions: [
          { id: "s1", projectId: "p1", name: "Test Session", workDir: "/tmp/test-project", live: true },
        ],
      },
    });
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body.projects).toBe(1);
    expect(body.sessions).toBe(1);

    // Verify via state endpoint.
    const stateResp = await request.get(`${TEST_API}/state`);
    const states = await stateResp.json();
    expect(states).toHaveLength(1);
    expect(states[0].id).toBe("s1");
    expect(states[0].live).toBe(true);
  });

  test("inject-event pushes events to live session", async ({ request }) => {
    // Seed a live session.
    await request.post(`${TEST_API}/seed`, {
      data: {
        projects: [{ id: "p1", name: "Test", path: "/tmp/test", slug: "test" }],
        sessions: [{ id: "s1", projectId: "p1", name: "Sess", workDir: "/tmp/test", live: true }],
      },
    });

    // Inject a text event.
    const resp = await request.post(`${TEST_API}/inject-event`, {
      data: {
        sessionId: "s1",
        event: { type: "text", content: "Hello from test" },
      },
    });
    expect(resp.ok()).toBeTruthy();
  });

  test("reset clears all data", async ({ request }) => {
    // Seed some data.
    await request.post(`${TEST_API}/seed`, {
      data: {
        projects: [{ id: "p1", name: "Test", path: "/tmp/test", slug: "test" }],
        sessions: [{ id: "s1", projectId: "p1", name: "Sess", workDir: "/tmp/test" }],
      },
    });

    // Reset.
    const resetResp = await request.post(`${TEST_API}/reset`);
    expect(resetResp.ok()).toBeTruthy();

    // Verify empty.
    const stateResp = await request.get(`${TEST_API}/state`);
    const states = await stateResp.json();
    expect(states).toHaveLength(0);
  });
});
