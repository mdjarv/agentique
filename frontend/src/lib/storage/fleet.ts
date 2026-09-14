/**
 * Storage across machines (docs/storage.md, "More than one machine").
 *
 * Every paired machine answers `/api/storage/*` for itself, so the client asks
 * each one and never sums them: two disks are not one disk, and freeing space
 * on one does nothing for the other. The page shows one machine at a time and
 * the footer meter stays this machine's, with a notch when a remote runs low.
 *
 * The floor lives here and only here. The meter's colour, the footer notch and
 * the rail's level bars all read `isLowDisk`, so they cannot disagree about
 * which machine is low.
 */
import type { DiskStats, UsageAgent } from "~/lib/generated-types";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";

/**
 * Below this much free space a disk is low, whatever its size.
 *
 * A gauge's percentage never escalates — a small disk at 88% all year is its
 * normal state (docs/usage.md). An absolute floor is a different claim: under
 * 3 GiB the next dependency install in a worktree fails, on a 75 GB disk and a
 * 2 TB one alike. Chosen by the operator, so pinned by a test.
 */
export const LOW_DISK_BYTES = 3 * 1024 ** 3;

export function isLowDisk(disk: DiskStats | null | undefined): boolean {
  if (!disk || disk.totalBytes <= 0) return false;
  return disk.freeBytes < LOW_DISK_BYTES;
}

/** One machine as storage surfaces show it. */
export interface StorageMachine {
  /** `PRIMARY_MACHINE_KEY` or the paired machine's id. */
  key: string;
  label: string;
  /** Icon id; empty falls back to the generic server glyph. */
  icon: string;
  /** Reachable now. The primary is reachable by definition: it serves this page. */
  online: boolean;
  /** Epoch ms last connected — only meaningful while away. */
  lastSeenAt?: number;
  /** The last reading this tab holds, possibly from before it went away. */
  disk?: DiskStats;
}

/**
 * Reachable remotes below the floor — what the footer notch is about.
 *
 * An away machine's last reading is excluded: nothing can be done about it
 * from here until it is back, and a notch that leads to a page where every
 * verb is disabled is a claim on attention with nothing behind it.
 */
export function lowRemotes(machines: StorageMachine[]): StorageMachine[] {
  return machines.filter((m) => m.key !== PRIMARY_MACHINE_KEY && m.online && isLowDisk(m.disk));
}

/**
 * Where the footer meter leads: this machine when it is low itself (the meter
 * is red, so that is what a click is about), else the first low remote, else
 * this machine. `undefined` means this machine, which is the bare `/storage`.
 */
export function storageLinkTarget(machines: StorageMachine[]): string | undefined {
  const primary = machines.find((m) => m.key === PRIMARY_MACHINE_KEY);
  if (isLowDisk(primary?.disk)) return undefined;
  return lowRemotes(machines)[0]?.key;
}

/**
 * The machine the page shows for a `?machine=` value. Unknown or absent is this
 * machine: a link to a machine since unpaired must still open a page.
 */
export function resolveStorageMachine(requested: string | undefined, keys: string[]): string {
  if (requested && keys.includes(requested)) return requested;
  return PRIMARY_MACHINE_KEY;
}

/**
 * The disk gauge with the floor applied, for the meter and the panel.
 *
 * The collector reports the gauge without a severity, and `limitTier` keeps a
 * gauge `normal` unless one is given. The floor is judged here from the
 * machine's own byte counts rather than on the server, so it holds for a peer
 * on a release that predates it.
 */
export function withDiskFloor(agent: UsageAgent, disk: DiskStats | null | undefined): UsageAgent {
  if (!isLowDisk(disk)) return agent;
  return {
    ...agent,
    limits: (agent.limits ?? []).map((l) => ({ ...l, severity: "warning" })),
  };
}
