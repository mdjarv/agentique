import { describe, expect, it } from "vitest";
import { sublineSubject } from "./subline";

describe("sublineSubject", () => {
  it("gives the line to live work over everything else", () => {
    expect(sublineSubject({ live: true, parked: false, badgeState: "running" })).toBe("work");
    // Agents still out with the run settled to idle is still work — the whole
    // reason `hasLiveWork` is separate from the state.
    expect(sublineSubject({ live: true, parked: false, badgeState: "idle" })).toBe("work");
  });

  it("says what a parked loop is waiting for rather than 'Stopped'", () => {
    expect(sublineSubject({ live: false, parked: true, badgeState: "stopped" })).toBe("parked");
  });

  it("keeps the state word for anything worth reading", () => {
    for (const badgeState of ["failed", "stopped", "approval", "merging", "done"] as const) {
      expect(sublineSubject({ live: false, parked: false, badgeState })).toBe("state");
    }
  });

  it("hands the idle line to the brain, which is what pays for the tools row", () => {
    expect(sublineSubject({ live: false, parked: false, badgeState: "idle" })).toBe("brain");
  });

  it("never reports the brain while something is happening", () => {
    // The guarantee the composer's missing row depends on: the model reading
    // may replace "Idle" and nothing else.
    expect(sublineSubject({ live: true, parked: false, badgeState: "idle" })).not.toBe("brain");
    expect(sublineSubject({ live: false, parked: true, badgeState: "idle" })).not.toBe("brain");
  });
});
