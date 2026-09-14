/**
 * Whether a project surface names machines, and what it calls each one.
 *
 * Once any machine is paired, every project row names the machine it lives on
 * — including this one. Naming the machine only where a repo spans machines
 * left two unrelated projects that share a name on two machines looking
 * identical, with no way to tell which one a rename would hit. On a server
 * with nothing paired there is only one machine and nothing is named.
 *
 * Selectors return primitives (the repo's Zustand stable-reference rule).
 */
import { useCallback } from "react";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";

/** The machine facts a member or target already carries. */
export interface MachineRef {
  /** Absent = the primary serving this UI. */
  machineId?: string;
  /** "" for the primary on a member VM. */
  machineLabel: string;
  machineIcon?: string;
  machinePlatform?: string;
}

export interface NamedMachine {
  label: string;
  icon: string;
  platform: string;
}

export function useMachineNaming(): {
  /** More than one machine is in the picture, so rows name theirs. */
  named: boolean;
  nameOf: (ref: MachineRef) => NamedMachine;
} {
  const named = useMachineStore((s) => Object.keys(s.machines).length > 0);
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  const primaryIcon = useFeatureStore((s) => s.machineIcon);
  const primaryPlatform = useFeatureStore((s) => s.machinePlatformOs);

  const nameOf = useCallback(
    (ref: MachineRef): NamedMachine =>
      ref.machineId
        ? {
            label: ref.machineLabel,
            icon: ref.machineIcon ?? "",
            platform: ref.machinePlatform ?? "",
          }
        : {
            label: primaryLabel || "This machine",
            icon: primaryIcon,
            platform: primaryPlatform,
          },
    [primaryLabel, primaryIcon, primaryPlatform],
  );

  return { named, nameOf };
}
