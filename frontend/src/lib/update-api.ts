/**
 * Version status per machine (docs/upgrades.md).
 *
 * Every server answers for ITSELF — only it knows its platform, its install
 * method and whether it is busy — so the client asks each one and does no
 * version arithmetic beyond comparing strings it was handed.
 */

import type { UpdateStatus } from "~/lib/generated-types";
import { apiFetch } from "~/lib/machines/api";

/** Store key for the machine serving this SPA — apiFetch's `undefined` target.
 *  Paired machines are keyed by their UUID, so this can never collide. */
export const PRIMARY_MACHINE_KEY = "primary";

/** The machineId to route with for a store key. */
export function targetFor(key: string): string | undefined {
  return key === PRIMARY_MACHINE_KEY ? undefined : key;
}

/** Every machine to ask: the primary first, then each paired remote. */
export function machineKeys(machines: Record<string, unknown>): string[] {
  return [PRIMARY_MACHINE_KEY, ...Object.keys(machines)];
}

/** Ask one machine what it is running and what is published. `refresh` forces
 *  a check rather than reading that server's hourly cache. */
export async function fetchUpdateStatus(key: string, refresh = false): Promise<UpdateStatus> {
  const path = `/api/update/status${refresh ? "?refresh=1" : ""}`;
  const resp = await apiFetch(targetFor(key), path);
  if (!resp.ok) throw new Error(`update status failed (${resp.status})`);
  return (await resp.json()) as UpdateStatus;
}

/** How long ago a check ran, for the "as of" line an offline answer needs. */
export function checkedAgo(checkedAt: string, now = Date.now()): string | null {
  if (!checkedAt) return null;
  const then = Date.parse(checkedAt);
  if (Number.isNaN(then)) return null;
  const mins = Math.floor((now - then) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
