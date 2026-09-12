import { HttpResponse, http } from "msw";
import { MOCK_PROJECTS } from "./data";
import {
  MOCK_BRAIN_GRAPH,
  MOCK_BRAIN_STATUS,
  MOCK_CLAUDE_ACCOUNT,
  MOCK_DISK,
  MOCK_MACHINE_PRESENTATION,
  MOCK_MEMORIES,
  MOCK_STORAGE_USAGE,
  MOCK_TEMPLATES,
  MOCK_USAGE,
  MOCK_VOICE_SETTINGS,
} from "./demo-data";

export const restHandlers = [
  http.get("/api/auth/status", () => {
    return HttpResponse.json({ authEnabled: false, authenticated: false, userCount: 0 });
  }),

  http.get("/api/preset-definitions", () => {
    return HttpResponse.json([
      {
        key: "autoCommit",
        title: "Auto-commit",
        description: "Commit after each milestone automatically",
      },
      {
        key: "suggestParallel",
        title: "Suggest parallel sessions",
        description: "Suggest session prompts for parallelizable work",
      },
      {
        key: "planFirst",
        title: "Plan first",
        description: "Start sessions in plan mode by default",
      },
      { key: "terse", title: "Terse output", description: "Minimize explanations and summaries" },
    ]);
  }),

  http.get("/api/projects/:id/files", ({ request }) => {
    const url = new URL(request.url);
    const subpath = url.searchParams.get("path") || "";
    if (subpath === "") {
      return HttpResponse.json({
        path: "",
        entries: [
          { name: "backend", isDir: true, size: 0, modTime: "2026-03-28T10:00:00Z" },
          { name: "frontend", isDir: true, size: 0, modTime: "2026-03-29T14:00:00Z" },
          { name: "docs", isDir: true, size: 0, modTime: "2026-03-25T08:00:00Z" },
          { name: "CLAUDE.md", isDir: false, size: 2048, modTime: "2026-03-29T12:00:00Z" },
          { name: "README.md", isDir: false, size: 4096, modTime: "2026-03-27T16:00:00Z" },
          { name: "justfile", isDir: false, size: 512, modTime: "2026-03-28T09:00:00Z" },
          { name: "go.mod", isDir: false, size: 256, modTime: "2026-03-28T10:00:00Z" },
          { name: ".gitignore", isDir: false, size: 128, modTime: "2026-03-20T08:00:00Z" },
        ],
      });
    }
    return HttpResponse.json({
      path: subpath,
      entries: [
        { name: "main.go", isDir: false, size: 1024, modTime: "2026-03-29T10:00:00Z" },
        { name: "server.go", isDir: false, size: 3072, modTime: "2026-03-29T11:00:00Z" },
      ],
    });
  }),

  http.get("/api/projects/:id/files/content", () => {
    return new HttpResponse(
      "# Agentique\n\nLightweight GUI for managing concurrent Claude Code agents.\n\n## Quick Start\n\n```bash\njust dev\n```\n",
      { headers: { "Content-Type": "text/plain" } },
    );
  }),

  http.get("/api/projects", () => {
    return HttpResponse.json(MOCK_PROJECTS);
  }),

  http.post("/api/projects", () => {
    return HttpResponse.json(MOCK_PROJECTS[0], { status: 201 });
  }),

  http.patch("/api/projects/:id", () => {
    return HttpResponse.json(MOCK_PROJECTS[0]);
  }),

  http.delete("/api/projects/:id", () => {
    return new HttpResponse(null, { status: 204 });
  }),

  http.get("/api/health", () => {
    return HttpResponse.json({
      status: "ok",
      features: { browser: false, teams: true, brain: true },
    });
  }),

  // --- Storage, usage, and the rest of the non-session surfaces ---
  //
  // Without these a mock-mode browser gets a 502 on every page outside the
  // session world, because an unhandled /api path falls through to a backend
  // that mock mode exists not to need.

  http.get("/api/storage/disk", () => HttpResponse.json(MOCK_DISK)),

  http.get("/api/storage/usage", () => HttpResponse.json(MOCK_STORAGE_USAGE)),

  http.post("/api/storage/reclaim", async ({ request }) => {
    const { sessionIds } = (await request.json()) as { sessionIds: string[] };
    return HttpResponse.json({
      removed: sessionIds.map((sessionId) => ({
        kind: "worktree",
        path: `~/.local/share/agentique/worktrees/${sessionId}`,
        sessionId,
        bytes: 2 * 1024 ** 3,
      })),
      skipped: [],
      failed: [],
      freedBytes: sessionIds.length * 2 * 1024 ** 3,
    });
  }),

  http.post("/api/storage/backups/trim", async ({ request }) => {
    const { keep } = (await request.json()) as { keep: number };
    return HttpResponse.json({ removed: [], freedBytes: 0, kept: keep });
  }),

  http.delete("/api/storage/worktrees", () => new HttpResponse(null, { status: 204 })),
  http.delete("/api/storage/scratchpads", () => new HttpResponse(null, { status: 204 })),

  http.get("/api/usage", () => HttpResponse.json(MOCK_USAGE)),

  http.get("/api/templates", () => HttpResponse.json(MOCK_TEMPLATES)),
  http.get("/api/templates/:id", ({ params }) => {
    const found = MOCK_TEMPLATES.find((t) => t.id === params.id);
    return found ? HttpResponse.json(found) : new HttpResponse(null, { status: 404 });
  }),
  http.post("/api/templates", () => HttpResponse.json(MOCK_TEMPLATES[0], { status: 201 })),
  http.put("/api/templates/:id", () => HttpResponse.json(MOCK_TEMPLATES[0])),
  http.delete("/api/templates/:id", () => new HttpResponse(null, { status: 204 })),

  http.get("/api/brain/status", () => HttpResponse.json(MOCK_BRAIN_STATUS)),
  http.get("/api/brain/memories", ({ request }) => {
    const scope = new URL(request.url).searchParams.get("scope");
    const list = scope ? MOCK_MEMORIES.filter((m) => m.scope === scope) : MOCK_MEMORIES;
    return HttpResponse.json(list);
  }),
  http.get("/api/brain/graph", () => HttpResponse.json(MOCK_BRAIN_GRAPH)),
  http.get("/api/brain/search", ({ request }) => {
    const q = (new URL(request.url).searchParams.get("q") ?? "").toLowerCase();
    const hit = (m: (typeof MOCK_MEMORIES)[number]) => m.text.toLowerCase().includes(q);
    return HttpResponse.json({
      pinned: MOCK_MEMORIES.filter((m) => m.pinned && hit(m)),
      recalled: MOCK_MEMORIES.filter((m) => !m.pinned && hit(m)),
    });
  }),
  http.get("/api/brain/consolidate/job", () => HttpResponse.json({ running: false })),

  http.get("/api/voice/settings", () => HttpResponse.json(MOCK_VOICE_SETTINGS)),
  http.put("/api/voice/settings", () => HttpResponse.json(MOCK_VOICE_SETTINGS)),

  http.get("/api/claude-account", () => HttpResponse.json(MOCK_CLAUDE_ACCOUNT)),

  http.get("/api/machines", () => HttpResponse.json([])),
  http.get("/api/machine/presentation", () => HttpResponse.json(MOCK_MACHINE_PRESENTATION)),
];
