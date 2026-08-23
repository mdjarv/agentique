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

export type MachineFaultKind = "wrong-machine" | "credential-rejected" | "not-agentique";

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
  }
}

const PROBE_TIMEOUT_MS = 8000;

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
  const probe: ProbeResult = {};
  try {
    const resp = await fetch(`${entry.baseUrl}/.well-known/agentique/environment`, {
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    });
    if (resp.status === 404) {
      probe.answeredNotAgentique = true;
    } else if (resp.ok) {
      let body: MachineDescriptor | null = null;
      try {
        body = (await resp.json()) as MachineDescriptor;
      } catch {
        // A 200 that isn't JSON is proof, not ambiguity: a dev server's
        // index.html, a captive portal, another app on a typo'd port.
        probe.answeredNotAgentique = true;
      }
      if (body?.machineId) probe.descriptor = body;
      else if (body) probe.answeredNotAgentique = true;
    } else {
      return { fault: null }; // 500/502/503 — a maybe, not a proof
    }
  } catch {
    return { fault: null }; // unreachable — asleep, off the network, or simply gone
  }
  return { fault: classifyProbe(entry.machineId, probe), descriptor: probe.descriptor };
}

/**
 * The full diagnosis for a machine that won't connect: who is answering, and
 * if it is the right machine, whether it still accepts our credential.
 */
export async function probeMachine(entry: MachineEntry): Promise<MachineFault | null> {
  const identity = await probeIdentity(entry);
  if (identity.fault) return identity.fault;

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
