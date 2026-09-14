import { describe, expect, it } from "vitest";
import type { DiskStats, UsageAgent } from "~/lib/generated-types";
import {
  isLowDisk,
  LOW_DISK_BYTES,
  lowRemotes,
  resolveStorageMachine,
  type StorageMachine,
  storageLinkTarget,
  withDiskFloor,
} from "~/lib/storage/fleet";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { limitTier } from "~/lib/usage-api";

const GiB = 1024 ** 3;

function disk(freeBytes: number, totalBytes = 1000 * GiB): DiskStats {
  return {
    path: "/data",
    totalBytes,
    freeBytes,
    usedBytes: totalBytes - freeBytes,
    usagePercent: ((totalBytes - freeBytes) / totalBytes) * 100,
  };
}

function machine(key: string, over: Partial<StorageMachine> = {}): StorageMachine {
  return { key, label: key, icon: "", online: true, disk: disk(100 * GiB), ...over };
}

describe("LOW_DISK_BYTES", () => {
  // Chosen by the operator, not derived — nothing else would catch a drift.
  it("is 3 GiB", () => {
    expect(LOW_DISK_BYTES).toBe(3 * GiB);
  });
});

describe("isLowDisk", () => {
  it("judges absolute free space, whatever the volume size", () => {
    expect(isLowDisk(disk(646 * 1024 ** 2, 75 * GiB))).toBe(true);
    expect(isLowDisk(disk(2.9 * GiB, 2000 * GiB))).toBe(true);
    expect(isLowDisk(disk(3 * GiB, 75 * GiB))).toBe(false);
    // 88% used on a small disk is its normal state, not news.
    expect(isLowDisk(disk(9 * GiB, 75 * GiB))).toBe(false);
  });

  it("is never low without a reading", () => {
    expect(isLowDisk(undefined)).toBe(false);
    expect(isLowDisk(null)).toBe(false);
    expect(isLowDisk(disk(0, 0))).toBe(false);
  });
});

describe("lowRemotes", () => {
  it("names reachable remotes below the floor and never the primary", () => {
    const low = lowRemotes([
      machine(PRIMARY_MACHINE_KEY, { disk: disk(GiB) }),
      machine("zbook", { disk: disk(GiB) }),
      machine("nas"),
    ]);
    expect(low.map((m) => m.key)).toEqual(["zbook"]);
  });

  it("ignores an away machine's last reading", () => {
    expect(lowRemotes([machine("zbook", { online: false, disk: disk(GiB) })])).toEqual([]);
  });
});

describe("storageLinkTarget", () => {
  it("stays on this machine when this machine is low", () => {
    expect(
      storageLinkTarget([
        machine(PRIMARY_MACHINE_KEY, { disk: disk(GiB) }),
        machine("zbook", { disk: disk(GiB) }),
      ]),
    ).toBeUndefined();
  });

  it("leads to the first low remote otherwise", () => {
    expect(
      storageLinkTarget([
        machine(PRIMARY_MACHINE_KEY),
        machine("nas"),
        machine("zbook", { disk: disk(GiB) }),
      ]),
    ).toBe("zbook");
  });

  it("is this machine when nothing is low", () => {
    expect(storageLinkTarget([machine(PRIMARY_MACHINE_KEY), machine("nas")])).toBeUndefined();
  });
});

describe("resolveStorageMachine", () => {
  const keys = [PRIMARY_MACHINE_KEY, "zbook"];
  it("accepts a known machine", () => {
    expect(resolveStorageMachine("zbook", keys)).toBe("zbook");
  });
  it("falls back to this machine for an unknown or absent one", () => {
    expect(resolveStorageMachine("gone", keys)).toBe(PRIMARY_MACHINE_KEY);
    expect(resolveStorageMachine(undefined, keys)).toBe(PRIMARY_MACHINE_KEY);
  });
});

describe("withDiskFloor", () => {
  const gauge: UsageAgent = {
    id: "storage",
    name: "Disk",
    kind: "gauge",
    ready: true,
    limits: [{ label: "Data directory", percent: 0.99, detail: "646 MB free" }],
  };

  it("leaves a gauge above the floor at normal, however full", () => {
    const agent = withDiskFloor(gauge, disk(9 * GiB, 75 * GiB));
    expect(agent).toBe(gauge);
    expect(limitTier(agent.limits?.[0] ?? { label: "", percent: 0 }, true)).toBe("normal");
  });

  it("escalates the gauge below the floor", () => {
    const agent = withDiskFloor(gauge, disk(GiB, 75 * GiB));
    expect(limitTier(agent.limits?.[0] ?? { label: "", percent: 0 }, true)).toBe("warning");
  });
});
