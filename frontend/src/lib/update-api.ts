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

/** Ask one machine to upgrade itself. Resolves as soon as the server has
 *  accepted (202) — the narration then arrives over the WS global topic.
 *  `force` overrides the drain gate and costs the running turns. */
export async function applyUpdate(key: string, expect: string, force = false): Promise<void> {
  const resp = await apiFetch(targetFor(key), "/api/update/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expect, force }),
  });
  if (!resp.ok) throw new Error(await errorText(resp));
}

/** Cancel an armed or in-flight upgrade. Refused past `replacing`. */
export async function cancelUpdate(key: string): Promise<void> {
  const resp = await apiFetch(targetFor(key), "/api/update/apply", { method: "DELETE" });
  if (!resp.ok) throw new Error(await errorText(resp));
}

async function errorText(resp: Response): Promise<string> {
  try {
    const body = (await resp.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // Not JSON — fall through to the status line.
  }
  return `request failed (${resp.status})`;
}

/**
 * Wait for a machine to come back on a new version.
 *
 * Success looks like a disconnect: the process serving the reply is the
 * process being replaced. So the client stops trusting the socket and polls
 * the UNAUTHENTICATED descriptor — which is also how it verifies the upgrade
 * worked. It reports the version it actually found, never the one it hoped
 * for.
 */
export async function awaitRestart(
  baseUrl: string,
  was: string,
  opts: { timeoutMs?: number; intervalMs?: number } = {},
): Promise<{ version: string; changed: boolean }> {
  const timeoutMs = opts.timeoutMs ?? 120_000;
  const intervalMs = opts.intervalMs ?? 2_000;
  const until = Date.now() + timeoutMs;
  let last = was;

  while (Date.now() < until) {
    await new Promise((r) => setTimeout(r, intervalMs));
    try {
      const resp = await fetch(`${baseUrl}/.well-known/agentique/environment`, {
        signal: AbortSignal.timeout(4_000),
        cache: "no-store",
      });
      if (!resp.ok) continue;
      const body = (await resp.json()) as { version?: string };
      if (body.version) {
        last = body.version;
        if (body.version !== was) return { version: body.version, changed: true };
      }
    } catch {
      // Still down — that is what a restart looks like from here.
    }
  }
  return { version: last, changed: false };
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
