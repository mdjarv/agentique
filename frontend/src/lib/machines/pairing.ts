import {
  createIdentityNonce,
  proveMachineIdentity,
  readBoundedMachineJSON,
  verifyIdentityProof,
} from "~/lib/machines/health";
import { getMachineClient, peekMachineClient } from "~/lib/machines/registry";
import { useFeatureStore } from "~/stores/feature-store";
import type { MachineEntry } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Client side of the pairing flow (multi-machine): probe the machine's
 * descriptor, exchange the one-time token from `agentique pair` for a bearer
 * session, save the catalog entry, and start the connection.
 */

interface Descriptor {
  machineId: string;
  identityKey: string;
  label: string;
  version: string;
  capabilities?: Record<string, boolean>;
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function normalizeAddress(address: string): string {
  let a = address.trim().replace(/\/+$/, "");
  if (!/^https?:\/\//.test(a)) a = `https://${a}`;
  const parsed = new URL(a);
  if (
    parsed.pathname !== "/" ||
    parsed.search ||
    parsed.hash ||
    parsed.username ||
    parsed.password
  ) {
    throw new Error("Machine address must contain only a scheme, host, and optional port");
  }
  const loopback =
    parsed.hostname === "localhost" ||
    parsed.hostname === "127.0.0.1" ||
    parsed.hostname === "[::1]" ||
    parsed.hostname === "::1";
  if (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && loopback)) {
    throw new Error("Remote machines require HTTPS");
  }
  return parsed.origin;
}

export async function pairMachine(address: string, token: string): Promise<MachineEntry> {
  const baseUrl = normalizeAddress(address);

  let descriptor: Descriptor;
  try {
    const resp = await fetch(`${baseUrl}/.well-known/agentique/environment`, {
      signal: AbortSignal.timeout(8000),
    });
    if (!resp.ok) throw new Error(`status ${resp.status}`);
    descriptor = await readBoundedMachineJSON<Descriptor>(resp);
  } catch (err) {
    throw new Error(
      `No agentique server reachable at ${baseUrl} — check the address (and that it uses HTTPS when this app does). ${err instanceof Error ? err.message : ""}`,
    );
  }
  if (
    !UUID_PATTERN.test(descriptor.machineId) ||
    !descriptor.identityKey ||
    descriptor.identityKey.length > 1024 ||
    typeof descriptor.label !== "string" ||
    [...descriptor.label].length > 64
  ) {
    throw new Error(`${baseUrl} answered but is not an agentique server`);
  }
  if (descriptor.machineId === useFeatureStore.getState().machineId) {
    throw new Error("That address is this machine — pairing targets other machines");
  }

  if (descriptor.capabilities?.pairing === false) {
    throw new Error(
      `${descriptor.label} has authentication disabled and cannot be paired remotely`,
    );
  }
  if (token.trim() === "") {
    throw new Error(`${descriptor.label} requires a pairing token — run "agentique pair" there`);
  }

  // Prove possession of the descriptor's key before disclosing even the
  // short-lived pairing credential.
  const preflightIdentity = await proveMachineIdentity(
    baseUrl,
    descriptor.machineId,
    descriptor.identityKey,
  );
  if (preflightIdentity.status !== "verified") {
    throw new Error(
      preflightIdentity.status === "fault"
        ? preflightIdentity.fault.detail
        : "The machine could not prove its signing identity",
    );
  }

  const primaryLabel = useFeatureStore.getState().machineLabel || "agentique";
  const nonce = createIdentityNonce();
  const previous = useMachineStore.getState().machines[descriptor.machineId];
  const resp = await fetch(`${baseUrl}/api/auth/pair`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      token: token.trim(),
      label: `${primaryLabel} web`,
      nonce,
      replaceSessionId: previous?.sessionId ?? "",
    }),
    signal: AbortSignal.timeout(8000),
  });
  if (!resp.ok) {
    const detail = await readBoundedMachineJSON<{ error?: string }>(resp)
      .then((d) => d.error)
      .catch(() => undefined);
    throw new Error(detail ?? `Pairing failed (${resp.status})`);
  }
  const paired = await readBoundedMachineJSON<{
    token: string;
    sessionId: string;
    machineId: string;
    identityKey: string;
    proof: string;
  }>(resp);
  if (
    !paired.token ||
    !paired.sessionId ||
    paired.machineId !== descriptor.machineId ||
    paired.identityKey !== descriptor.identityKey ||
    !(await verifyIdentityProof(descriptor.identityKey, descriptor.machineId, nonce, paired.proof))
  ) {
    if (paired.token) {
      await revokeBearerIfIdentityMatches(
        baseUrl,
        descriptor.machineId,
        descriptor.identityKey,
        paired.token,
      );
    }
    throw new Error("Pairing response failed machine identity verification");
  }

  return saveEntry({
    machineId: descriptor.machineId,
    label: descriptor.label,
    baseUrl,
    token: paired.token,
    sessionId: paired.sessionId,
    identityKey: paired.identityKey,
    addedAt: new Date().toISOString(),
  });
}

// Re-pairing an already-known machine refreshes its token in place. The
// ticket minter reads the token from the store per attempt, so an existing
// client picks the new credential up on its next connect.
async function saveEntry(entry: MachineEntry): Promise<MachineEntry> {
  try {
    await useMachineStore.getState().addMachine(entry);
  } catch (err) {
    // The remote session was already created. Revoke it if the primary could
    // not persist the catalog row, otherwise a failed pair leaves a hidden
    // long-lived credential behind.
    const revoked = await revokeBearerIfIdentityMatches(
      entry.baseUrl,
      entry.machineId,
      entry.identityKey,
      entry.token,
    );
    if (!revoked) {
      const detail = err instanceof Error ? err.message : String(err);
      throw new Error(
        `${detail}; the new remote credential could not be revoked — inspect "agentique auth sessions" on that machine`,
      );
    }
    throw err;
  }
  const existing = peekMachineClient(entry.machineId);
  if (existing) existing.forceReconnect();
  else getMachineClient(entry.machineId);
  return entry;
}

async function revokeBearerIfIdentityMatches(
  baseUrl: string,
  machineId: string,
  identityKey: string,
  token: string,
): Promise<boolean> {
  const identity = await proveMachineIdentity(baseUrl, machineId, identityKey);
  if (identity.status !== "verified") return false;
  try {
    const response = await fetch(`${baseUrl}/api/auth/session`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(8000),
    });
    return response.ok;
  } catch {
    return false;
  }
}
