/**
 * Settings › Machines — this host and every machine paired to it.
 *
 * The name and icon here are this host's presentation of each machine, stored
 * locally (docs/multi-machine.md): satellites neither publish nor read them,
 * so a rename never has to reach the machine it renames.
 */
import { Pencil, Plus, X } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { AddMachineDialog } from "~/components/machines/AddMachineDialog";
import { MachineIdentityDialog } from "~/components/settings/MachineIdentityDialog";
import { SettingsSection } from "~/components/settings/SettingsLayout";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { cn, getErrorMessage, relativeTime } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useFeatureStore } from "~/stores/feature-store";
import type { MachineStatus } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

/** Machine avatar: its icon, or the generic server glyph when unset. */
function MachineFace({ iconId, className }: { iconId: string; className?: string }) {
  const Icon = getMachineIcon(iconId) ?? DEFAULT_MACHINE_ICON;
  return (
    <span
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-lg bg-secondary text-muted-foreground",
        className,
      )}
    >
      <Icon className="size-4" />
    </span>
  );
}

function StatusDot({ status, fault }: { status: MachineStatus; fault?: boolean }) {
  return (
    <span
      title={fault ? "needs attention" : status}
      className={cn(
        "size-2 shrink-0 rounded-full",
        // Away is grey, not red: red is reserved for something that will never
        // come back on its own.
        fault && "bg-destructive",
        !fault && status === "connected" && "bg-success",
        !fault &&
          status === "reconnecting" &&
          "bg-warning animate-pulse motion-reduce:animate-none",
        !fault && status === "disconnected" && "bg-muted-foreground",
      )}
    />
  );
}

function MachineRow({
  face,
  name,
  detail,
  fault,
  status,
  onEdit,
  onRepair,
  onRemove,
}: {
  face: React.ReactNode;
  name: string;
  detail: string;
  /** A proven fault: what is wrong, in a sentence, with its fix. */
  fault?: string;
  status: MachineStatus;
  onEdit: () => void;
  onRepair?: () => void;
  onRemove?: () => void;
}) {
  return (
    <div
      className={cn(
        "group flex items-center gap-3 rounded-lg border bg-card px-3.5 py-3",
        fault ? "border-destructive/40" : "border-border/60",
      )}
    >
      {face}
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-[13px] font-medium text-foreground-bright">{name}</span>
        <span
          className={cn(
            "truncate font-mono text-[11px]",
            fault ? "text-destructive" : "text-muted-foreground-faint",
          )}
        >
          {fault ?? detail}
        </span>
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1.5">
        {fault && onRepair && (
          <Button size="sm" variant="ghost" onClick={onRepair}>
            Re-pair
          </Button>
        )}
        <StatusDot status={status} fault={!!fault} />
        <button
          type="button"
          aria-label={`Rename ${name}`}
          title="Rename"
          onClick={onEdit}
          className="flex size-7 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
        >
          <Pencil className="size-3.5" />
        </button>
        {onRemove && (
          <button
            type="button"
            aria-label={`Remove ${name}`}
            title="Remove machine"
            onClick={onRemove}
            className="flex size-7 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
          >
            <X className="size-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

export function MachinesSettings() {
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const lastSeenAt = useMachineStore((s) => s.lastSeenAt);
  const faults = useMachineStore((s) => s.faults);
  const renameMachine = useMachineStore((s) => s.renameMachine);
  const removeMachine = useMachineStore((s) => s.removeMachine);
  const projects = useAppStore((s) => s.projects);

  const hostLabel = useFeatureStore((s) => s.machineLabel);
  const hostIcon = useFeatureStore((s) => s.machineIcon);
  const hostPinned = useFeatureStore((s) => s.machineLabelPinned);
  const saveHostPresentation = useFeatureStore((s) => s.saveHostPresentation);

  const [editing, setEditing] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  /** Set when the dialog was opened by a row's Re-pair, not by "Add machine". */
  const [repairing, setRepairing] = useState<string | null>(null);
  // Unpairing drops the pairing, its projects and its cached sessions — worth
  // one question before a stray click does it.
  const [removing, setRemoving] = useState<string | null>(null);

  const entries = useMemo(
    () => Object.values(machines).sort((a, b) => a.label.localeCompare(b.label)),
    [machines],
  );

  const projectCount = (machineId: string | undefined) =>
    projects.filter((p) => p.machineId === machineId).length;

  const editingEntry = editing && editing !== "self" ? machines[editing] : undefined;

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection title="This machine" description="The server serving this page.">
        <MachineRow
          face={<MachineFace iconId={hostIcon} />}
          name={hostLabel || "This machine"}
          detail={`${projectCount(undefined)} projects · this device`}
          status="connected"
          onEdit={() => setEditing("self")}
        />
      </SettingsSection>

      <SettingsSection
        title="Paired machines"
        description="Names and icons are local to this device — satellites never see them."
        action={
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setRepairing(null);
              setAddOpen(true);
            }}
          >
            <Plus className="size-3.5" />
            Add machine
          </Button>
        }
      >
        <div className="flex flex-col gap-2">
          {entries.map((m) => (
            <MachineRow
              key={m.machineId}
              face={<MachineFace iconId={m.icon ?? ""} />}
              name={m.label || m.machineId}
              detail={[
                m.baseUrl,
                `${projectCount(m.machineId)} projects`,
                // Away is the everyday state of a laptop, so the row says how
                // long rather than treating it as a fault.
                statuses[m.machineId] !== "connected"
                  ? lastSeenAt[m.machineId]
                    ? `last seen ${relativeTime(new Date(lastSeenAt[m.machineId] as number).toISOString())}`
                    : "not seen yet"
                  : "",
              ]
                .filter(Boolean)
                .join(" · ")}
              fault={faults[m.machineId]?.detail}
              status={statuses[m.machineId] ?? "disconnected"}
              onEdit={() => setEditing(m.machineId)}
              onRepair={() => {
                setRepairing(m.machineId);
                setAddOpen(true);
              }}
              onRemove={() => setRemoving(m.machineId)}
            />
          ))}
          {entries.length === 0 && (
            <p className="rounded-lg border border-dashed border-border/60 px-3.5 py-6 text-center text-[12.5px] text-muted-foreground-faint">
              No paired machines. Pair one with <code>agentique pair</code> on the other machine.
            </p>
          )}
        </div>
      </SettingsSection>

      <MachineIdentityDialog
        open={editing === "self"}
        onOpenChange={(open) => setEditing(open ? "self" : null)}
        title="This machine"
        initial={{ label: hostLabel, icon: hostIcon }}
        labelPinnedNote={
          hostPinned
            ? "Set by AGENTIQUE_MACHINE_LABEL — unset the env var to rename from here."
            : undefined
        }
        onSave={async ({ label, icon }) => {
          try {
            await saveHostPresentation(label, icon);
          } catch (err) {
            toast.error(getErrorMessage(err, "Could not save"));
            throw err;
          }
        }}
      />

      <MachineIdentityDialog
        open={!!editingEntry}
        onOpenChange={(open) => setEditing(open ? editing : null)}
        title={`Rename ${editingEntry?.label ?? "machine"}`}
        initial={{ label: editingEntry?.label ?? "", icon: editingEntry?.icon ?? "" }}
        onSave={async ({ label, icon }) => {
          if (!editingEntry) return;
          try {
            await renameMachine(editingEntry.machineId, { label, icon });
          } catch (err) {
            toast.error(getErrorMessage(err, "Could not rename machine"));
            throw err;
          }
        }}
      />

      <AddMachineDialog
        open={addOpen}
        onOpenChange={(open) => {
          setAddOpen(open);
          if (!open) setRepairing(null);
        }}
        repairMachineId={repairing ?? undefined}
      />

      <AlertDialog open={!!removing} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Remove {removing ? (machines[removing]?.label ?? "this machine") : "this machine"}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Agentique will revoke this pairing on the remote machine before removing its projects
              and cached sessions. The machine must be reachable.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!removing) return;
                try {
                  await removeMachine(removing);
                  setRemoving(null);
                } catch (err) {
                  toast.error(getErrorMessage(err, "Could not remove machine"));
                }
              }}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
