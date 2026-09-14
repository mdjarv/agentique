import { describe, expect, it } from "vitest";
import type { FileEntry } from "~/lib/api";
import { dirCompletions, splitDirQuery } from "~/lib/dir-complete";

function entry(name: string, isDir = false): FileEntry {
  return { name, isDir, size: 0, modTime: "" };
}

describe("splitDirQuery", () => {
  it("reads everything up to the last slash as the directory", () => {
    expect(splitDirQuery("")).toEqual({ dir: "", prefix: "" });
    expect(splitDirQuery("not")).toEqual({ dir: "", prefix: "not" });
    expect(splitDirQuery("sites/")).toEqual({ dir: "sites/", prefix: "" });
    expect(splitDirQuery("sites/blog/ind")).toEqual({ dir: "sites/blog/", prefix: "ind" });
  });
});

describe("dirCompletions", () => {
  const home = [
    entry("notes.txt"),
    entry("sites", true),
    entry(".config", true),
    entry("Nginx", true),
    entry(".bashrc"),
    entry("new-idea", true),
  ];

  it("lists directories first, then files, and marks a directory with a trailing slash", () => {
    expect(dirCompletions(home, { dir: "", prefix: "" }, 20)).toEqual([
      { value: "new-idea/", isDir: true },
      { value: "Nginx/", isDir: true },
      { value: "sites/", isDir: true },
      { value: "notes.txt", isDir: false },
    ]);
  });

  it("filters by a case-insensitive prefix and keeps the directory in the value", () => {
    expect(dirCompletions(home, { dir: "work/", prefix: "n" }, 20).map((c) => c.value)).toEqual([
      "work/new-idea/",
      "work/Nginx/",
      "work/notes.txt",
    ]);
  });

  it("shows dotfiles only when the prefix asks for them", () => {
    expect(dirCompletions(home, { dir: "", prefix: "." }, 20).map((c) => c.value)).toEqual([
      ".config/",
      ".bashrc",
    ]);
  });

  it("caps the list", () => {
    expect(dirCompletions(home, { dir: "", prefix: "" }, 2)).toHaveLength(2);
  });
});
