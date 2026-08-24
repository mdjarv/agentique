import { describe, expect, it } from "vitest";
import {
  classifyProbe,
  clearedByIdentityProof,
  createIdentityNonce,
  faultLabel,
  readBoundedMachineJSON,
  verifyIdentityProof,
} from "../health";

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

describe("verifyIdentityProof", () => {
  it("accepts only a proof for the pinned key, machine id, and nonce", async () => {
    const keys = await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, true, [
      "sign",
      "verify",
    ]);
    const publicKey = new Uint8Array(await crypto.subtle.exportKey("spki", keys.publicKey));
    const encode = (bytes: Uint8Array) => {
      let binary = "";
      for (const byte of bytes) binary += String.fromCharCode(byte);
      return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    };
    const nonce = createIdentityNonce();
    const message = new TextEncoder().encode(`agentique-machine-proof-v1\n${ME}\n${nonce}`);
    const signature = new Uint8Array(
      await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, keys.privateKey, message),
    );

    await expect(
      verifyIdentityProof(encode(publicKey), ME, nonce, encode(signature)),
    ).resolves.toBe(true);
    await expect(
      verifyIdentityProof(encode(publicKey), "machine-b", nonce, encode(signature)),
    ).resolves.toBe(false);
  });
});

describe("clearedByIdentityProof", () => {
  // Every retry re-proves identity before presenting the credential. If that
  // proof cleared a credential-rejected fault, the diagnosis would be erased
  // within a second of being recorded and the Re-pair button would never show.
  it("keeps a rejected credential — identity says nothing about it", () => {
    expect(clearedByIdentityProof("credential-rejected")).toBe(false);
  });

  it("clears the faults a passing proof actually disproves", () => {
    for (const kind of [
      "wrong-machine",
      "not-agentique",
      "identity-unpinned",
      "identity-proof-invalid",
    ] as const) {
      expect(clearedByIdentityProof(kind)).toBe(true);
    }
  });
});

describe("readBoundedMachineJSON", () => {
  it("rejects an oversized untrusted machine response", async () => {
    const response = new Response(JSON.stringify({ padding: "x".repeat(70 << 10) }));
    await expect(readBoundedMachineJSON(response)).rejects.toThrow(/too large/i);
  });
});
