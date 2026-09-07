import { describe, expect, it } from "vitest";
import { formatSummary } from "~/components/chat/ToolUseBlock";

describe("formatSummary — task tools", () => {
  it("shows the subject for TaskCreate", () => {
    expect(formatSummary("TaskCreate", { subject: "Build demo", description: "…" })).toBe(
      "Build demo",
    );
  });

  it("shows the status transition for TaskUpdate", () => {
    expect(formatSummary("TaskUpdate", { taskId: "1", status: "in_progress" })).toBe(
      "#1 → in_progress",
    );
  });

  it("shows a renamed subject for TaskUpdate without a status", () => {
    expect(formatSummary("TaskUpdate", { taskId: "2", subject: "Renamed" })).toBe("#2 Renamed");
  });

  it("labels TaskList and TaskGet", () => {
    expect(formatSummary("TaskList", {})).toBe("Listing tasks");
    expect(formatSummary("TaskGet", { taskId: "3" })).toBe("#3");
  });
});

// agentkit v0.5.0 splits a codex compound shell line into one tool use per
// segment, typed Read / Grep / Glob / Bash. The typed rows carry the whole line
// as `command` and the parsed intent beside it — and a `listFiles` segment has
// no pattern at all, because a bare `ls` has none.
describe("formatSummary — codex command segments", () => {
  it("names the directory a Glob segment listed, where claude's Glob has a pattern", () => {
    expect(
      formatSummary("Glob", { path: "/repo/frontend/src", command: "ls -la frontend/src" }),
    ).toBe("/repo/frontend/src");
    expect(formatSummary("Glob", { pattern: "**/*.ts" })).toBe("**/*.ts");
  });

  it("falls back to the command when a Glob segment has neither", () => {
    expect(formatSummary("Glob", { command: "ls" })).toBe("ls");
  });

  it("still renders something for a Glob with nothing at all", () => {
    expect(formatSummary("Glob", {})).toBe("");
  });

  it("reads a Read segment's absolute path and a Grep segment's query", () => {
    expect(formatSummary("Read", { file_path: "/repo/a.go", command: "cat a.go" })).toBe(
      "/repo/a.go",
    );
    expect(formatSummary("Grep", { pattern: "TODO", path: "/repo", command: "rg TODO" })).toBe(
      "TODO in /repo",
    );
  });
});
