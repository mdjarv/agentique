import { iconNames } from "lucide-react/dynamic";
import { describe, expect, it } from "vitest";
import { ICON_GROUPS, iconGroupsFor } from "~/lib/icon-catalog";
import { resolveMachineGlyph } from "~/lib/machines/platform";

describe("icon catalog", () => {
  it("lists only ids lucide can load", () => {
    const known = new Set<string>(iconNames);
    const unknown = ICON_GROUPS.flatMap((g) => g.ids).filter((id) => !known.has(id));
    expect(unknown).toEqual([]);
  });

  it("offers an abundance to both pickers, each glyph once", () => {
    for (const kind of ["project", "machine"] as const) {
      const ids = iconGroupsFor(kind).flatMap((g) => g.ids);
      expect(ids.length).toBeGreaterThan(250);
      expect(new Set(ids).size).toBe(ids.length);
      expect(ids).toContain("cat");
    }
  });

  it("leads each picker with its own categories", () => {
    expect(iconGroupsFor("machine")[0]?.label).toBe("Devices");
    expect(iconGroupsFor("project")[0]?.label).toBe("Dev");
  });
});

describe("resolveMachineGlyph", () => {
  it("returns one stable component for an icon outside the static registry", () => {
    // A fresh component per call would remount the mark on every render.
    const a = resolveMachineGlyph("squirrel", "linux");
    expect(resolveMachineGlyph("squirrel", "linux")).toBe(a);
  });

  it("falls back for an id lucide does not know", () => {
    expect(resolveMachineGlyph("not-an-icon", undefined)).toBe(resolveMachineGlyph("", undefined));
  });
});
