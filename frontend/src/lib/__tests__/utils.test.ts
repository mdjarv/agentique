import { describe, expect, it } from "vitest";
import { formatBytes, getErrorMessage, relativeTime, sessionShortId, uuid } from "~/lib/utils";

describe("formatBytes", () => {
  it("returns '0 B' for zero, negatives, and non-finite input", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(-5)).toBe("0 B");
    expect(formatBytes(Number.NaN)).toBe("0 B");
    expect(formatBytes(Number.POSITIVE_INFINITY)).toBe("0 B");
  });

  it("formats bytes with no decimals", () => {
    expect(formatBytes(500)).toBe("500 B");
    expect(formatBytes(1023)).toBe("1023 B");
  });

  it("uses 2 decimals under 10, 1 decimal under 100, 0 at/above 100", () => {
    expect(formatBytes(1024)).toBe("1.00 KB");
    expect(formatBytes(1024 * 15)).toBe("15.0 KB");
    expect(formatBytes(1024 * 150)).toBe("150 KB");
  });

  it("scales through MB/GB/TB", () => {
    expect(formatBytes(1024 ** 2)).toBe("1.00 MB");
    expect(formatBytes(1024 ** 3)).toBe("1.00 GB");
    expect(formatBytes(1024 ** 4)).toBe("1.00 TB");
  });

  it("clamps to the largest unit (PB) rather than overflowing", () => {
    expect(formatBytes(1024 ** 6)).toContain("PB");
  });
});

describe("uuid", () => {
  it("returns valid v4 UUID format", () => {
    const id = uuid();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  it("returns unique values", () => {
    expect(uuid()).not.toBe(uuid());
  });
});

describe("relativeTime", () => {
  it("returns 'now' for less than 1 min ago", () => {
    expect(relativeTime(new Date(Date.now() - 30_000).toISOString())).toBe("now");
  });

  it("returns minutes", () => {
    expect(relativeTime(new Date(Date.now() - 5 * 60_000).toISOString())).toBe("5m");
  });

  it("returns hours", () => {
    expect(relativeTime(new Date(Date.now() - 2 * 3_600_000).toISOString())).toBe("2h");
  });

  it("returns days", () => {
    expect(relativeTime(new Date(Date.now() - 3 * 86_400_000).toISOString())).toBe("3d");
  });
});

describe("sessionShortId", () => {
  it("extracts first segment", () => {
    expect(sessionShortId("abc12345-xxxx-yyyy")).toBe("abc12345");
  });

  it("returns full string when no dashes", () => {
    expect(sessionShortId("abc")).toBe("abc");
  });
});

describe("getErrorMessage", () => {
  it("returns message from Error instance", () => {
    expect(getErrorMessage(new Error("boom"), "fallback")).toBe("boom");
  });

  it("returns string directly", () => {
    expect(getErrorMessage("oops", "fallback")).toBe("oops");
  });

  it("returns fallback for null", () => {
    expect(getErrorMessage(null, "fallback")).toBe("fallback");
  });

  it("returns fallback for empty object", () => {
    expect(getErrorMessage({}, "fallback")).toBe("fallback");
  });
});
