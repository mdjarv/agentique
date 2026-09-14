import { describe, expect, it } from "vitest";
import { detectTrigger } from "~/hooks/useAutocomplete";

const at = (text: string) => detectTrigger(text, text.length);

describe("detectTrigger", () => {
  it("reads an @ query through slashes, so a path completes a directory at a time", () => {
    expect(at("check @sites/b")).toEqual({ type: "@", start: 6, query: "sites/b" });
    expect(at("@sites/blog/")).toEqual({ type: "@", start: 0, query: "sites/blog/" });
  });

  it("keeps a slash command at the start of the text", () => {
    expect(at("/rev")).toEqual({ type: "/", start: 0, query: "rev" });
  });

  it("is no command when the word at the start is a path", () => {
    expect(at("/usr/bin")).toBeNull();
  });

  it("ends at whitespace, and needs whitespace before an @", () => {
    expect(at("check sites/b")).toBeNull();
    expect(at("mail@example")).toBeNull();
    expect(at("@a b")).toBeNull();
  });
});
