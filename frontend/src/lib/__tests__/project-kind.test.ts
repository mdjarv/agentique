import { describe, expect, it } from "vitest";
import { isFolderProject } from "~/lib/project-kind";

describe("isFolderProject", () => {
  it("is a folder only when the server says so", () => {
    expect(isFolderProject({ kind: "folder" })).toBe(true);
    expect(isFolderProject({ kind: "git" })).toBe(false);
  });

  it("reads an absent kind as not reported — an older peer keeps its git features", () => {
    expect(isFolderProject({})).toBe(false);
    expect(isFolderProject(undefined)).toBe(false);
    expect(isFolderProject(null)).toBe(false);
  });
});
