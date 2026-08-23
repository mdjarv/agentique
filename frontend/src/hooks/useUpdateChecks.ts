import { useEffect, useMemo } from "react";
import { machineKeys } from "~/lib/update-api";
import { useMachineStore } from "~/stores/machine-store";
import { useUpdateStore } from "~/stores/update-store";

/**
 * Keeps each machine's version status current (docs/upgrades.md).
 *
 * Every server polls GitHub hourly for itself; this only re-reads those
 * cached answers, so the beat can be brisk without costing anyone a request.
 * It re-runs when the machine catalog changes — a machine paired mid-session
 * gets asked immediately rather than at the next tick.
 */
const RECHECK_MS = 15 * 60_000;

export function useUpdateChecks(): void {
  const machines = useMachineStore((s) => s.machines);
  const fetchAll = useUpdateStore((s) => s.fetchAll);
  const keys = useMemo(() => machineKeys(machines), [machines]);

  useEffect(() => {
    void fetchAll(keys);
    const id = setInterval(() => void fetchAll(keys), RECHECK_MS);
    return () => clearInterval(id);
  }, [fetchAll, keys]);
}
