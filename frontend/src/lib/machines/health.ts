/**
 * Telling a machine that is away from a machine that is broken.
 *
 * A suspended laptop and a mis-paired address look identical from a failed
 * socket: both just stop answering. But every agentique server publishes an
 * unauthenticated descriptor (`/.well-known/agentique/environment`) — the same
 * probe pairing runs before it trusts an address — and re-running it against
 * the pinned machineId separates "not answering" from "answering wrongly".
 *
 * Three faults can be *proven*; everything else is away, which is ordinary and
 * stays silent (docs/multi-machine.md). A proven fault never resolves itself,
 * so it is worth saying out loud and worth naming its fix.
 */
import type { MachineEntry } from "~/stores/machine-store";

export type MachineFaultKind =
  | "wrong-machine"
  | "credential-rejected"
  | "not-agentique"
  | "identity-unpinned"
  | "identity-proof-invalid";

export interface MachineFault {
  kind: MachineFaultKind;
  /** One sentence: what is wrong and what fixes it. */
  detail: string;
  /** Epoch ms the fault was last proven. */
  at: number;
}

/** The unauthenticated descriptor a machine publishes about itself. */
export interface MachineDescriptor {
  machineId?: string;
  identityKey?: string;
  /** Build version — already on the wire; kept so a machine's version is
   *  known without an authenticated call, and survives it going away. */
  version?: string;
  platform?: { os?: string; arch?: string };
}

/** What a descriptor probe found, before it is interpreted. */
export interface ProbeResult {
  /** The address answered with a parseable agentique descriptor. */
  descriptor?: MachineDescriptor;
  /** The address answered HTTP, but not with a descriptor. */
  answeredNotAgentique?: boolean;
  /** The credential this host holds was refused (401/403). */
  credentialRefused?: boolean;
}

/** A probe's verdict plus what the machine said about itself. */
export interface IdentityProbe {
  fault: MachineFault | null;
  descriptor?: MachineDescriptor;
}

/**
 * Classify a probe against the machine we expect to find. Pure, so the
 * decision table is testable without a network.
 *
 * Anything unproven returns null: an unreachable host, a proxy hiccup, a
 * transient 500. Away is the assumption, and it has to be — the alternative is
 * calling a sleeping laptop broken every night.
 */
export function classifyProbe(expectedMachineId: string, probe: ProbeResult): MachineFault | null {
  const at = Date.now();

  if (probe.answeredNotAgentique) {
    return {
      kind: "not-agentique",
      detail: "Something is listening at that address, but it isn't agentique — check the port.",
      at,
    };
  }

  const found = probe.descriptor?.machineId;
  if (found && expectedMachineId && found !== expectedMachineId) {
    return {
      kind: "wrong-machine",
      detail: "That address belongs to a different machine now — re-pair it to reconnect.",
      at,
    };
  }

  // Only meaningful once the descriptor confirmed we are talking to the right
  // machine: a refusal from the wrong machine says nothing about our pairing.
  if (probe.credentialRefused && found && found === expectedMachineId) {
    return {
      kind: "credential-rejected",
      detail: "This device's credential was rejected — re-pair the machine to reconnect.",
      at,
    };
  }

  return null;
}

/** Short label for the fault, for places with a tag's worth of room. */
export function faultLabel(kind: MachineFaultKind): string {
  switch (kind) {
    case "wrong-machine":
      return "wrong machine";
    case "credential-rejected":
      return "needs re-pairing";
    case "not-agentique":
      return "not agentique";
    case "identity-unpinned":
      return "needs re-pairing";
    case "identity-proof-invalid":
      return "identity rejected";
  }
}

const PROBE_TIMEOUT_MS = 8000;
const MAX_MACHINE_RESPONSE_BYTES = 64 << 10;

export type IdentityCheck =
  | { status: "verified"; descriptor?: MachineDescriptor }
  | { status: "unavailable" }
  | { status: "fault"; fault: MachineFault };

interface IdentityProof extends MachineDescriptor {
  proof?: string;
}

/** Decode a small JSON control response without letting an untrusted peer
 * make the browser buffer an arbitrarily large body. The limit applies after
 * content decoding because the stream exposes the bytes the app consumes. */
export async function readBoundedMachineJSON<T>(response: Response): Promise<T> {
  const declared = Number(response.headers.get("Content-Length"));
  if (Number.isFinite(declared) && declared > MAX_MACHINE_RESPONSE_BYTES) {
    throw new Error("machine response is too large");
  }

  const reader = response.body?.getReader();
  if (!reader) {
    const raw = new Uint8Array(await response.arrayBuffer());
    if (raw.byteLength > MAX_MACHINE_RESPONSE_BYTES) {
      throw new Error("machine response is too large");
    }
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(raw)) as T;
  }

  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > MAX_MACHINE_RESPONSE_BYTES) {
        await reader.cancel();
        throw new Error("machine response is too large");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const raw = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    raw.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(raw)) as T;
}

function base64URLToBytes(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  const binary = atob(padded);
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

function bytesToBase64URL(value: Uint8Array): string {
  let binary = "";
  for (const byte of value) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function createIdentityNonce(): string {
  return bytesToBase64URL(crypto.getRandomValues(new Uint8Array(32)));
}

export async function verifyIdentityProof(
  identityKey: string,
  machineId: string,
  nonce: string,
  proof: string,
): Promise<boolean> {
  try {
    const key = await crypto.subtle.importKey(
      "spki",
      base64URLToBytes(identityKey),
      { name: "ECDSA", namedCurve: "P-256" },
      false,
      ["verify"],
    );
    const message = new TextEncoder().encode(`agentique-machine-proof-v1\n${machineId}\n${nonce}`);
    return crypto.subtle.verify(
      { name: "ECDSA", hash: "SHA-256" },
      key,
      base64URLToBytes(proof),
      message,
    );
  } catch {
    return false;
  }
}

function identityFault(kind: "identity-unpinned" | "identity-proof-invalid"): MachineFault {
  return {
    kind,
    detail:
      kind === "identity-unpinned"
        ? "This pairing predates machine identity pins. Re-pair it before reconnecting."
        : "The server could not prove the identity pinned during pairing. Check the address and re-pair only if the machine changed intentionally.",
    at: Date.now(),
  };
}

/** Ask an endpoint holding the expected private key to sign a fresh nonce. */
export async function proveMachineIdentity(
  baseUrl: string,
  machineId: string,
  identityKey: string,
): Promise<IdentityCheck> {
  const nonce = createIdentityNonce();
  let response: IdentityProof;
  try {
    const resp = await fetch(`${baseUrl}/api/auth/identity-proof`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ nonce }),
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    });
    if (!resp.ok) return { status: "unavailable" };
    response = await readBoundedMachineJSON<IdentityProof>(resp);
  } catch {
    return { status: "unavailable" };
  }

  if (
    response.machineId !== machineId ||
    response.identityKey !== identityKey ||
    !response.proof ||
    !(await verifyIdentityProof(identityKey, machineId, nonce, response.proof))
  ) {
    return { status: "fault", fault: identityFault("identity-proof-invalid") };
  }
  return { status: "verified" };
}

/** Prove the pinned signing key before any request may send a bearer token. */
export async function checkMachineIdentity(entry: MachineEntry): Promise<IdentityCheck> {
  if (!entry.identityKey || !entry.sessionId) {
    return { status: "fault", fault: identityFault("identity-unpinned") };
  }
  // Credentials deliberately never persist in localStorage. During startup the
  // public catalog can hydrate before the primary returns the bearer; treat
  // that as temporarily unavailable, not as a broken legacy pairing.
  if (!entry.token) return { status: "unavailable" };

  let descriptor: MachineDescriptor;
  try {
    const resp = await fetch(`${entry.baseUrl}/.well-known/agentique/environment`, {
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    });
    if (resp.status === 404) {
      return {
        status: "fault",
        fault: {
          kind: "not-agentique",
          detail: "Something is listening at that address, but it isn't agentique. Check the port.",
          at: Date.now(),
        },
      };
    }
    if (!resp.ok) return { status: "unavailable" };
    descriptor = await readBoundedMachineJSON<MachineDescriptor>(resp);
  } catch {
    return { status: "unavailable" };
  }

  if (descriptor.machineId !== entry.machineId || descriptor.identityKey !== entry.identityKey) {
    return {
      status: "fault",
      fault: {
        kind: "wrong-machine",
        detail: "That address no longer presents the machine identity pinned during pairing.",
        at: Date.now(),
      },
    };
  }

  const proof = await proveMachineIdentity(entry.baseUrl, entry.machineId, entry.identityKey);
  return proof.status === "verified" ? { ...proof, descriptor } : proof;
}

/**
 * Ask an address who it is. Used both to diagnose a machine that won't connect
 * and — critically — to check identity on every connect: an address that
 * answers is not the same thing as the machine we paired with.
 *
 * Returns the descriptor alongside the verdict: the machine's version is
 * already in that payload, and throwing it away is why nothing could show what
 * each machine was running (docs/upgrades.md).
 */
export async function probeIdentity(entry: MachineEntry): Promise<IdentityProbe> {
  const result = await checkMachineIdentity(entry);
  if (result.status === "fault") return { fault: result.fault };
  if (result.status === "unavailable") return { fault: null };
  return { fault: null, descriptor: result.descriptor };
}

/**
 * The full diagnosis for a machine that won't connect: who is answering, and
 * if it is the right machine, whether it still accepts our credential.
 */
export async function probeMachine(entry: MachineEntry): Promise<MachineFault | null> {
  const identity = await checkMachineIdentity(entry);
  if (identity.status === "fault") return identity.fault;
  if (identity.status === "unavailable") return null;

  const probe: ProbeResult = { descriptor: { machineId: entry.machineId } };
  // Only reachable machines get this far, and only a credential we actually
  // hold can be refused.
  if (entry.token) {
    try {
      const resp = await fetch(`${entry.baseUrl}/api/auth/ws-ticket`, {
        method: "POST",
        headers: { Authorization: `Bearer ${entry.token}` },
        signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
      });
      if (resp.status === 401 || resp.status === 403) probe.credentialRefused = true;
    } catch {
      // Reachable descriptor but a failed ticket call: transient, stay away.
    }
  }

  return classifyProbe(entry.machineId, probe);
}
