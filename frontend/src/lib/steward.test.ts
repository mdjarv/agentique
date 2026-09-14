import { afterEach, describe, expect, it, vi } from "vitest";
import {
  type Finding,
  fetchFindings,
  findingRow,
  findingsLabel,
  footerFindings,
} from "~/lib/steward";

vi.mock("~/lib/machines/api", () => ({ apiFetch: vi.fn() }));

const signedOut: Finding = {
  kind: "cli-signed-out",
  subject: "claude",
  severity: "warning",
  remedy: "hand",
  facts: { agent: "Claude", help: "Run `claude auth login` to restore usage." },
};

describe("footerFindings", () => {
  it("keeps only the kinds nothing else on the line already reports", () => {
    const all: Finding[] = [
      signedOut,
      { kind: "update-waiting", severity: "notice", remedy: "update" },
      { kind: "disk-low", severity: "warning", remedy: "reclaim" },
      { kind: "session-blocked-long", severity: "notice", remedy: "hand" },
      { kind: "loop-paused", severity: "warning", remedy: "hand", facts: { loop: "nightly" } },
      { kind: "backup-failing", severity: "warning", remedy: "hand" },
    ];
    expect(footerFindings(all).map((f) => f.kind)).toEqual([
      "cli-signed-out",
      "loop-paused",
      "backup-failing",
    ]);
  });
});

describe("findingsLabel", () => {
  it("is null with nothing to stand for, the finding's own words for one, a count for more", () => {
    expect(findingsLabel([])).toBeNull();
    expect(
      findingsLabel([{ kind: "update-waiting", severity: "notice", remedy: "update" }]),
    ).toBeNull();
    expect(findingsLabel([signedOut])).toBe("Claude is signed out");
    expect(
      findingsLabel([signedOut, { kind: "backup-failing", severity: "warning", remedy: "hand" }]),
    ).toBe("2 things on this machine need a look");
  });

  it("gives a row the fix in words", () => {
    expect(findingRow(signedOut).detail).toContain("claude auth login");
  });
});

describe("fetchFindings", () => {
  afterEach(() => vi.resetAllMocks());

  it("treats a server with no steward route — the SPA's HTML 200 — as no findings", async () => {
    const { apiFetch } = await import("~/lib/machines/api");
    vi.mocked(apiFetch).mockResolvedValue(
      new Response("<!doctype html>", { status: 200, headers: { "content-type": "text/html" } }),
    );
    await expect(fetchFindings()).resolves.toEqual([]);
  });

  it("reads the findings a steward serves", async () => {
    const { apiFetch } = await import("~/lib/machines/api");
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ findings: [signedOut] }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    await expect(fetchFindings()).resolves.toEqual([signedOut]);
  });
});
