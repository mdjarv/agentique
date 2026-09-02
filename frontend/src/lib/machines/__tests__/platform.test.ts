import { describe, expect, it } from "vitest";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import {
  PLATFORM_GLYPH,
  parsePlatform,
  platformLabel,
  resolveMachineGlyph,
} from "~/lib/machines/platform";

describe("parsePlatform", () => {
  it("accepts the GOOS vocabulary and the GOOS/GOARCH form", () => {
    expect(parsePlatform("linux")).toBe("linux");
    expect(parsePlatform("windows")).toBe("windows");
    expect(parsePlatform("Darwin")).toBe("darwin");
    expect(parsePlatform("linux/amd64")).toBe("linux");
  });

  it("answers null for unknown — silence draws no mark", () => {
    expect(parsePlatform("")).toBeNull();
    expect(parsePlatform(undefined)).toBeNull();
    expect(parsePlatform("freebsd")).toBeNull();
    expect(parsePlatform("TempleOS")).toBeNull();
  });
});

describe("resolveMachineGlyph", () => {
  it("lets a user-picked icon outrank the platform mark", () => {
    expect(resolveMachineGlyph("laptop", "windows")).toBe(getMachineIcon("laptop"));
  });

  it("falls back to the platform mark, then the generic glyph", () => {
    expect(resolveMachineGlyph("", "windows")).toBe(PLATFORM_GLYPH.windows);
    expect(resolveMachineGlyph(undefined, "linux")).toBe(PLATFORM_GLYPH.linux);
    expect(resolveMachineGlyph("", "")).toBe(DEFAULT_MACHINE_ICON);
    expect(resolveMachineGlyph("not-an-icon-id", "also-not-an-os")).toBe(DEFAULT_MACHINE_ICON);
  });
});

describe("platformLabel", () => {
  it("names the platform or says nothing", () => {
    expect(platformLabel("darwin")).toBe("macOS");
    expect(platformLabel("windows")).toBe("Windows");
    expect(platformLabel("plan9")).toBe("");
  });
});
