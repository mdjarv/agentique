import { describe, expect, it } from "vitest";
import { formatDuration } from "~/lib/format";

describe("formatDuration", () => {
  it("formats 0ms", () => {
    expect(formatDuration(0)).toBe("0.0s");
  });

  it("formats sub-second values with one decimal", () => {
    expect(formatDuration(100)).toBe("0.1s");
    expect(formatDuration(500)).toBe("0.5s");
    expect(formatDuration(999)).toBe("1.0s");
  });

  it("formats exact seconds without decimal", () => {
    expect(formatDuration(1000)).toBe("1s");
    expect(formatDuration(5000)).toBe("5s");
    expect(formatDuration(30000)).toBe("30s");
  });

  it("formats fractional seconds with one decimal", () => {
    expect(formatDuration(1500)).toBe("1.5s");
    expect(formatDuration(12300)).toBe("12.3s");
    expect(formatDuration(59900)).toBe("59.9s");
  });

  it("formats exact minutes without seconds", () => {
    expect(formatDuration(60000)).toBe("1m");
    expect(formatDuration(120000)).toBe("2m");
    expect(formatDuration(300000)).toBe("5m");
  });

  it("formats minutes and seconds", () => {
    expect(formatDuration(61000)).toBe("1m 1s");
    expect(formatDuration(90000)).toBe("1m 30s");
    expect(formatDuration(125000)).toBe("2m 5s");
  });

  it("formats large values", () => {
    expect(formatDuration(3600000)).toBe("60m");
    expect(formatDuration(3661000)).toBe("61m 1s");
  });

  it("rounds seconds in minute range", () => {
    expect(formatDuration(61500)).toBe("1m 2s");
    expect(formatDuration(119400)).toBe("1m 59s");
  });
});
