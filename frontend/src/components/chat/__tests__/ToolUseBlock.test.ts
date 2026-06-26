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
