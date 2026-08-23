import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { MachineEntry, MachineStatus } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Resolves the remote machine a project lives on, or null for the primary.
 * Selectors return primitives/store references only (stable across renders).
 */
export function useProjectMachine(projectId: string | undefined): MachineEntry | null {
  const machineId = useAppStore((s) =>
    projectId ? s.projects.find((p) => p.id === projectId)?.machineId : undefined,
  );
  return useMachineStore((s) => (machineId ? (s.machines[machineId] ?? null) : null));
}

export function useMachineStatus(machineId: string | undefined): MachineStatus {
  return useMachineStore((s) =>
    machineId ? (s.statuses[machineId] ?? "disconnected") : "connected",
  );
}

/** A proven fault on that machine, or null. Away has none — that's the point. */
export function useMachineFault(machineId: string | undefined) {
  return useMachineStore((s) => (machineId ? (s.faults[machineId] ?? null) : null));
}

/**
 * Read-only pill naming the machine a session/project lives on. Renders
 * nothing for the primary machine — multi-machine chrome stays invisible
 * until a session actually runs elsewhere. The dot mirrors that machine's
 * live connection state.
 */
export function MachineChip({
  projectId,
  className,
}: {
  projectId: string | undefined;
  className?: string;
}) {
  const machine = useProjectMachine(projectId);
  const status = useMachineStatus(machine?.machineId);
  const fault = useMachineFault(machine?.machineId);
  if (!machine) return null;
  const Icon = getMachineIcon(machine.icon ?? "") ?? DEFAULT_MACHINE_ICON;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border shrink-0 min-w-0",
        fault
          ? "border-destructive/40 bg-destructive/10 text-destructive"
          : "border-border/40 bg-muted/40 text-muted-foreground",
        className,
      )}
      title={
        fault
          ? `${machine.label}: ${fault.detail}`
          : `Runs on ${machine.label} (${machine.baseUrl})${status === "connected" ? "" : ` — ${status}`}`
      }
    >
      <Icon className="h-2.5 w-2.5 shrink-0" />
      <span className="truncate max-w-[10ch]">{machine.label}</span>
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full shrink-0",
          fault && "bg-destructive",
          !fault && status === "connected" && "bg-success",
          !fault && status === "reconnecting" && "bg-warning animate-pulse",
          !fault && status === "disconnected" && "bg-muted-foreground",
        )}
      />
    </span>
  );
}
