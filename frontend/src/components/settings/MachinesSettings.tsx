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
import { AddMachineDialog } from "~/components/machines/MachinesSection";
import { MachineIdentityDialog } from "~/components/settings/MachineIdentityDialog";
import { SettingsSection } from "~/components/settings/SettingsLayout";
import { Button } from "~/components/ui/button";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { cn, getErrorMessage } from "~/lib/utils";
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

function StatusDot({ status }: { status: MachineStatus }) {
  return (
    <span
      title={status}
      className={cn(
        "size-2 shrink-0 rounded-full",
        status === "connected" && "bg-success",
        status === "reconnecting" && "bg-warning animate-pulse motion-reduce:animate-none",
        status === "disconnected" && "bg-destructive",
      )}
    />
  );
}

function MachineRow({
  face,
  name,
  detail,
  status,
  onEdit,
  onRemove,
}: {
  face: React.ReactNode;
  name: string;
  detail: string;
  status: MachineStatus;
  onEdit: () => void;
  onRemove?: () => void;
}) {
  return (
    <div className="group flex items-center gap-3 rounded-lg border border-border/60 bg-card px-3.5 py-3">
      {face}
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-[13px] font-medium text-foreground-bright">{name}</span>
        <span className="truncate font-mono text-[11px] text-muted-foreground-faint">{detail}</span>
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1.5">
        <StatusDot status={status} />
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
  const renameMachine = useMachineStore((s) => s.renameMachine);
  const removeMachine = useMachineStore((s) => s.removeMachine);
  const projects = useAppStore((s) => s.projects);

  const hostLabel = useFeatureStore((s) => s.machineLabel);
  const hostIcon = useFeatureStore((s) => s.machineIcon);
  const hostPinned = useFeatureStore((s) => s.machineLabelPinned);
  const saveHostPresentation = useFeatureStore((s) => s.saveHostPresentation);

  const [editing, setEditing] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);

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
          <Button size="sm" variant="ghost" onClick={() => setAddOpen(true)}>
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
              detail={`${m.baseUrl} · ${projectCount(m.machineId)} projects`}
              status={statuses[m.machineId] ?? "disconnected"}
              onEdit={() => setEditing(m.machineId)}
              onRemove={() => removeMachine(m.machineId)}
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

      <AddMachineDialog open={addOpen} onOpenChange={setAddOpen} />
    </div>
  );
}
