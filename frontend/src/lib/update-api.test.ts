import { describe, expect, it } from "vitest";
import type { UpdateCLIStatus } from "~/lib/generated-types";
import { cliPublishedVerdict } from "~/lib/update-api";

const base: UpdateCLIStatus = {
  tool: "claude",
  installed: "2.1.241",
  path: "/home/u/.local/bin/claude",
  method: "native",
  selfManaged: true,
};

describe("cliPublishedVerdict", () => {
  it("is unchecked when nobody has looked", () => {
    expect(cliPublishedVerdict(base)).toEqual({ kind: "unchecked" });
  });

  it("keeps no verdict apart from current", () => {
    const cli = {
      ...base,
      published: { version: "2.1.231", reason: "different channel", checkedAt: "x" },
    };
    expect(cliPublishedVerdict(cli).kind).toBe("unknown");
  });

  it("never calls a verdict without a published version", () => {
    const cli = { ...base, published: { status: "current", checkedAt: "x" } };
    expect(cliPublishedVerdict(cli).kind).toBe("unknown");
  });

  it("is handled only when the updater is on and last succeeded", () => {
    const published = { status: "behind", version: "2.1.245", checkedAt: "x" };
    const on = { enabled: true, lastSucceeded: true };
    expect(cliPublishedVerdict({ ...base, published, autoUpdate: on })).toMatchObject({
      kind: "behind",
      handled: true,
    });
    const failed = { enabled: true, lastOutcome: "install_failed" };
    expect(cliPublishedVerdict({ ...base, published, autoUpdate: failed })).toMatchObject({
      handled: false,
    });
    expect(
      cliPublishedVerdict({ ...base, selfManaged: false, published, autoUpdate: on }),
    ).toMatchObject({ handled: false });
  });
});
