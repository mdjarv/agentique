import { useMemo } from "react";
import type { StorageMachine } from "~/lib/storage/fleet";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";
import { useStorageStore } from "~/stores/storage-store";

/**
 * Every machine as storage surfaces show it: this machine first, then each
 * paired remote in catalog order. The order never follows free space — a list
 * that re-sorts itself moves a row out from under the pointer the moment a
 * reclaim lands.
 */
export function useStorageMachines(): StorageMachine[] {
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  const primaryIcon = useFeatureStore((s) => s.machineIcon);
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const lastSeenAt = useMachineStore((s) => s.lastSeenAt);
  const disks = useStorageStore((s) => s.disks);

  return useMemo(() => {
    const primary: StorageMachine = {
      key: PRIMARY_MACHINE_KEY,
      label: primaryLabel || "This machine",
      icon: primaryIcon || "",
      online: true,
      disk: disks[PRIMARY_MACHINE_KEY],
    };
    const remotes = Object.values(machines).map<StorageMachine>((entry) => ({
      key: entry.machineId,
      label: entry.label || entry.machineId.slice(0, 8),
      icon: entry.icon ?? "",
      online: statuses[entry.machineId] === "connected",
      lastSeenAt: lastSeenAt[entry.machineId],
      disk: disks[entry.machineId],
    }));
    return [primary, ...remotes];
  }, [primaryLabel, primaryIcon, machines, statuses, lastSeenAt, disks]);
}
