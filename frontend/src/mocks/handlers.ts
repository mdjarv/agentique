import { HttpResponse, http } from "msw";
import { MOCK_PROJECTS } from "./data";

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
      features: { browser: false, teams: false },
    });
  }),
];
