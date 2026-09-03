import { describe, expect, it } from "vitest";
import { sublineSubject } from "./subline";

describe("sublineSubject", () => {
  it("gives the line to live work over everything else", () => {
    expect(sublineSubject({ live: true, parked: false })).toBe("work");
  });

  it("says what a parked loop is waiting for rather than 'Stopped'", () => {
    expect(sublineSubject({ live: false, parked: true })).toBe("parked");
  });

  it("keeps the resting state word when nothing is happening", () => {
    expect(sublineSubject({ live: false, parked: false })).toBe("state");
  });

  it("prefers parked over live, so the two can never both be claimed", () => {
    // A parked session is stopped, so `live` is false whenever `parked` is
    // true. The order keeps that true by construction rather than by luck.
    expect(sublineSubject({ live: true, parked: true })).toBe("parked");
  });
});
