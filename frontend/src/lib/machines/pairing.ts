import { getMachineClient, peekMachineClient } from "~/lib/machines/registry";
import { useFeatureStore } from "~/stores/feature-store";
import type { MachineEntry } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Client side of the pairing flow (multi-machine M1): probe the machine's
 * descriptor, exchange the one-time token from `agentique pair` for a bearer
 * session, save the catalog entry, and start the connection.
 */

interface Descriptor {
  machineId: string;
  label: string;
  version: string;
  capabilities?: Record<string, boolean>;
}

function normalizeAddress(address: string): string {
  let a = address.trim().replace(/\/+$/, "");
  if (!/^https?:\/\//.test(a)) a = `https://${a}`;
  return a;
}

export async function pairMachine(address: string, token: string): Promise<MachineEntry> {
  const baseUrl = normalizeAddress(address);

  let descriptor: Descriptor;
  try {
    const resp = await fetch(`${baseUrl}/.well-known/agentique/environment`, {
      signal: AbortSignal.timeout(8000),
    });
    if (!resp.ok) throw new Error(`status ${resp.status}`);
    descriptor = (await resp.json()) as Descriptor;
  } catch (err) {
    throw new Error(
      `No agentique server reachable at ${baseUrl} — check the address (and that it uses HTTPS when this app does). ${err instanceof Error ? err.message : ""}`,
    );
  }
  if (!descriptor.machineId) {
    throw new Error(`${baseUrl} answered but is not an agentique server`);
  }
  if (descriptor.machineId === useFeatureStore.getState().machineId) {
    throw new Error("That address is this machine — pairing targets other machines");
  }

  const primaryLabel = useFeatureStore.getState().machineLabel || "agentique";
  const resp = await fetch(`${baseUrl}/api/auth/pair`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token: token.trim(), label: `${primaryLabel} web` }),
    signal: AbortSignal.timeout(8000),
  });
  if (!resp.ok) {
    const detail = await resp
      .json()
      .then((d: { error?: string }) => d.error)
      .catch(() => undefined);
    throw new Error(detail ?? `Pairing failed (${resp.status})`);
  }
  const { token: bearer } = (await resp.json()) as { token: string };

  const entry: MachineEntry = {
    machineId: descriptor.machineId,
    label: descriptor.label,
    baseUrl,
    token: bearer,
    addedAt: new Date().toISOString(),
  };
  // Re-pairing an already-known machine refreshes its token in place. The
  // ticket minter reads the token from the store per attempt, so an existing
  // client picks the new credential up on its next connect.
  useMachineStore.getState().addMachine(entry);
  const existing = peekMachineClient(entry.machineId);
  if (existing) existing.forceReconnect();
  else getMachineClient(entry.machineId);
  return entry;
}
