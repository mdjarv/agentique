import { describe, expect, it } from "vitest";
import { semanticBadge, semanticFindingDetail } from "~/lib/brain-semantic";

describe("semanticBadge", () => {
  it("reads Semantic only while attached", () => {
    expect(semanticBadge({ semantic: true, semanticState: "on" })).toMatchObject({
      label: "Semantic",
      tone: "on",
    });
  });

  it("keeps keyword-by-choice and keyword-while-connecting quiet", () => {
    expect(semanticBadge({ semantic: false, semanticState: "off" }).tone).toBe("quiet");
    expect(semanticBadge({ semantic: false, semanticState: "connecting" }).tone).toBe("quiet");
  });

  it("warns for a configured backend that is gone, naming the half that failed", () => {
    const badge = semanticBadge({
      semantic: false,
      semanticState: "unreachable",
      semanticReason: "embedder-unreachable",
      semanticDownSince: "2026-09-14T21:36:00Z",
    });
    expect(badge).toMatchObject({ label: "Keyword", tone: "warning" });
    expect(badge.title).toContain("embedding service");
    expect(badge.title).toContain("since");
  });

  it("reads an older peer's bare boolean without inventing a state", () => {
    expect(semanticBadge({ semantic: true })).toMatchObject({ label: "Semantic", tone: "on" });
    expect(semanticBadge({ semantic: false })).toMatchObject({ label: "Keyword", tone: "quiet" });
  });
});

describe("semanticFindingDetail", () => {
  it("says what failed and what it costs, and survives missing facts", () => {
    expect(semanticFindingDetail("chroma-unreachable", "2026-09-14T21:36:00Z")).toMatch(
      /^Chroma is not answering since .+consolidation is paused/,
    );
    expect(semanticFindingDetail(undefined, undefined)).toMatch(
      /^The vector index is unreachable, so/,
    );
  });
});
