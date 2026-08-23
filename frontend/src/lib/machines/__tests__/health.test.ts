import { describe, expect, it } from "vitest";
import { classifyProbe, faultLabel } from "../health";

const ME = "machine-a";

describe("classifyProbe", () => {
  // Away is the assumption, and has to be: the alternative is calling a
  // sleeping laptop broken every night.
  it("treats an unreachable machine as away, not broken", () => {
    expect(classifyProbe(ME, {})).toBeNull();
  });

  it("treats a descriptor that matches as away", () => {
    expect(classifyProbe(ME, { descriptor: { machineId: ME } })).toBeNull();
  });

  it("proves a wrong machine when the descriptor names someone else", () => {
    const fault = classifyProbe(ME, { descriptor: { machineId: "machine-b" } });
    expect(fault?.kind).toBe("wrong-machine");
    expect(fault?.detail).toMatch(/re-pair/i);
  });

  it("proves a rejected credential only once identity is confirmed", () => {
    const fault = classifyProbe(ME, {
      descriptor: { machineId: ME },
      credentialRefused: true,
    });
    expect(fault?.kind).toBe("credential-rejected");
  });

  // A refusal from the wrong machine says nothing about our pairing — the
  // identity mismatch is the real (and more alarming) finding.
  it("reports the identity mismatch when both are true", () => {
    const fault = classifyProbe(ME, {
      descriptor: { machineId: "machine-b" },
      credentialRefused: true,
    });
    expect(fault?.kind).toBe("wrong-machine");
  });

  it("ignores a refusal with no descriptor to confirm who answered", () => {
    expect(classifyProbe(ME, { credentialRefused: true })).toBeNull();
  });

  it("proves a non-agentique address", () => {
    expect(classifyProbe(ME, { answeredNotAgentique: true })?.kind).toBe("not-agentique");
  });

  it("names every fault in a tag's worth of room", () => {
    expect(faultLabel("wrong-machine")).toBe("wrong machine");
    expect(faultLabel("credential-rejected")).toBe("needs re-pairing");
    expect(faultLabel("not-agentique")).toBe("not agentique");
  });
});
