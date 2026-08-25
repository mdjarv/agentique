/**
 * Every physical checkout a session could start in, derived from the logical
 * project rows. The one place a launch surface asks "where can this run" —
 * see `lib/machines/launch-targets.ts` for why listing and launching differ.
 *
 * The derivation is memoized outside the selectors and the selectors return
 * the stores' own references (the repo's Zustand stable-reference rule).
 */
import { useMemo } from "react";
import { useLogicalProjects } from "~/hooks/useLogicalProjects";
import type { LaunchTarget } from "~/lib/machines/launch-targets";
import { launchTargets } from "~/lib/machines/launch-targets";
import { useFeatureStore } from "~/stores/feature-store";

export function useLaunchTargets(): LaunchTarget[] {
  const rows = useLogicalProjects();
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  return useMemo(() => launchTargets(rows, primaryLabel || "This machine"), [rows, primaryLabel]);
}
