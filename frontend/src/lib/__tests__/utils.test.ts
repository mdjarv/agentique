import { describe, expect, it } from "vitest";
import { getErrorMessage, relativeTime, sessionShortId, uuid } from "~/lib/utils";

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
