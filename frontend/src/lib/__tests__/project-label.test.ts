import { describe, expect, it } from "vitest";
import { projectInitials, projectLabel } from "~/lib/project-label";
import { slugify } from "~/lib/utils";

describe("projectLabel", () => {
  it("is the name, not the slug", () => {
    expect(projectLabel("Träffbild", "traffbild")).toBe("Träffbild");
    expect(projectLabel("The Pint", "the-pint")).toBe("The Pint");
  });

  it("falls back to the slug rather than rendering an empty row", () => {
    expect(projectLabel("", "traffbild")).toBe("traffbild");
    expect(projectLabel("   ", "traffbild")).toBe("traffbild");
  });
});

describe("projectInitials", () => {
  it("takes one letter from each of the first two words", () => {
    expect(projectInitials("The Pint")).toBe("TP");
    expect(projectInitials("meta-spec")).toBe("MS");
    expect(projectInitials("Åsa Öberg")).toBe("ÅÖ");
    expect(projectInitials("R&D")).toBe("RD");
  });

  it("takes two letters from a single word, since one letter names nothing", () => {
    expect(projectInitials("Träffbild")).toBe("TR");
    expect(projectInitials("Agentique")).toBe("AG");
  });

  it("never renders blank", () => {
    expect(projectInitials("")).toBe("?");
    expect(projectInitials("---")).toBe("?");
  });
});

describe("slugify", () => {
  // The rename dialog derives the slug with this, so it is what actually
  // produced `tr-ffbild` from "Träffbild".
  it("transliterates a letter rather than cutting the name in two", () => {
    expect(slugify("Träffbild")).toBe("traffbild");
    expect(slugify("Åsa Öberg")).toBe("asa-oberg");
    expect(slugify("Größe")).toBe("grosse");
    expect(slugify("København")).toBe("kobenhavn");
    expect(slugify("Ærø")).toBe("aero");
    expect(slugify("Łódź")).toBe("lodz");
    expect(slugify("Þórsmörk")).toBe("thorsmork");
  });

  it("keeps the plain cases unchanged", () => {
    expect(slugify("My Project")).toBe("my-project");
    expect(slugify("UPPERCASE")).toBe("uppercase");
    expect(slugify("special!@#chars")).toBe("special-chars");
    expect(slugify("R&D")).toBe("r-and-d");
  });

  it("falls back when nothing survives", () => {
    expect(slugify("")).toBe("project");
    expect(slugify("---")).toBe("project");
    expect(slugify("日本語")).toBe("project");
  });

  it("only ever produces a valid slug", () => {
    const valid = /^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$/;
    for (const name of [
      "Träffbild",
      "Åsa Öberg",
      "R&D",
      "日本語",
      "---",
      "",
      "ß",
      "  -- Ærø -- ",
    ]) {
      expect(slugify(name)).toMatch(valid);
    }
  });
});
