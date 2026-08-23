/**
 * What every machine is running, and what is published (docs/upgrades.md).
 *
 * One row per machine — the primary first, then each paired remote. Every
 * server answers for itself, so a row is that machine's own account of its
 * version, its platform and whether it could upgrade in place. An offline
 * machine is not a problem to solve: it shows the version it was last known to
 * be running, greyed, and gets offered the upgrade when it comes back.
 */
import { RefreshCw } from "lucide-react";
import { useMemo } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { UpdateRowAction } from "~/components/update/UpdateRowAction";
import type { UpdateStatus } from "~/lib/generated-types";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { checkedAgo, machineKeys, PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { cn, relativeTime } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";
import type { Flight } from "~/stores/update-store";
import { useUpdateStore } from "~/stores/update-store";

/** One machine's row, assembled from the three stores that know about it. */
interface Row {
  key: string;
  label: string;
  icon: string;
  online: boolean;
  /** Epoch ms this machine was last connected — only meaningful when away. */
  lastSeenAt?: number;
  /** Its own answer, when it could give one. */
  status?: UpdateStatus;
  /** The version it last reported, even if it cannot answer now. */
  lastKnownVersion?: string;
  /** An upgrade running on it right now. */
  flight?: Flight;
}

/** The one-line verdict for a row, and whether it wants attention.
 *  An away machine keeps its last-known verdict but never asks for anything:
 *  it is not a problem to solve, and it gets offered the upgrade when it
 *  comes back. */
function verdict(row: Row): { text: string; strong: boolean } {
  const attention = row.online;
  if (!row.status) {
    return { text: row.online ? "no answer" : "away", strong: false };
  }
  if (row.status.channel === "dev") return { text: "dev build", strong: false };
  if (!row.status.latest) return { text: "not checked yet", strong: false };
  if (!row.status.behind) return { text: "up to date", strong: false };
  // Behind, but this platform has never been verified for in-app apply — say
  // so rather than offering an action that cannot work.
  if (!row.status.supported) return { text: "manual upgrade", strong: attention };
  return { text: `${row.status.latest} available`, strong: attention };
}

function MachineRow({ row }: { row: Row }) {
  const Icon = getMachineIcon(row.icon) ?? DEFAULT_MACHINE_ICON;
  const version = row.status?.current || row.lastKnownVersion || "unknown";
  const age = row.status ? checkedAgo(row.status.checkedAt) : null;
  const apply = useUpdateStore((s) => s.apply);
  const cancel = useUpdateStore((s) => s.cancel);
  const clearFlight = useUpdateStore((s) => s.clearFlight);

  const detail = [
    row.online
      ? null
      : row.lastSeenAt
        ? `last seen ${relativeTime(new Date(row.lastSeenAt).toISOString())}`
        : "away",
    row.status?.checkError ? "check failed" : null,
    age && row.status?.behind ? `checked ${age}` : null,
    row.status?.platform && !row.status.supported && row.status.behind ? row.status.platform : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div
      className={cn(
        "flex items-center gap-3 overflow-hidden rounded-lg border border-border/60 bg-card px-3.5 py-3",
        !row.online && "opacity-60",
      )}
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-secondary text-muted-foreground">
        <Icon className="size-4" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[13px] font-medium text-foreground">{row.label}</span>
        <span className="truncate font-mono text-[11px] text-muted-foreground-faint">
          {version}
          {detail ? ` · ${detail}` : ""}
        </span>
      </div>
      {/* Shrinkable, min-w-0, and capped at half the row. A checksum mismatch
          names two 64-char digests: without min-w-0 it stretched the row past
          the dialog, and without the cap it ate the machine's own name — and a
          row that cannot say WHICH machine failed is worse than one that
          truncates why. */}
      <span className="flex min-w-0 max-w-[50%] justify-end">
        <UpdateRowAction
          status={row.status}
          flight={row.flight}
          online={row.online}
          verdict={verdict(row)}
          onApply={(opts) => apply(row.key, opts)}
          onCancel={() => cancel(row.key)}
          onDismiss={() => clearFlight(row.key)}
        />
      </span>
    </div>
  );
}

export function UpdateDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  const primaryIcon = useFeatureStore((s) => s.machineIcon);
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const versions = useMachineStore((s) => s.versions);
  const lastSeenAt = useMachineStore((s) => s.lastSeenAt);

  const updates = useUpdateStore((s) => s.statuses);
  const checking = useUpdateStore((s) => s.checking);
  const flights = useUpdateStore((s) => s.flights);
  const fetchAll = useUpdateStore((s) => s.fetchAll);

  const busy = Object.keys(checking).length > 0;

  const rows = useMemo<Row[]>(() => {
    const primary: Row = {
      key: PRIMARY_MACHINE_KEY,
      label: primaryLabel || "This machine",
      icon: primaryIcon || "",
      online: true,
      status: updates[PRIMARY_MACHINE_KEY],
      flight: flights[PRIMARY_MACHINE_KEY],
    };
    const remotes = Object.values(machines).map<Row>((entry) => ({
      key: entry.machineId,
      label: entry.label || entry.machineId.slice(0, 8),
      icon: entry.icon ?? "",
      online: statuses[entry.machineId] === "connected",
      lastSeenAt: lastSeenAt[entry.machineId],
      status: updates[entry.machineId],
      lastKnownVersion: versions[entry.machineId],
      flight: flights[entry.machineId],
    }));
    return [primary, ...remotes];
  }, [primaryLabel, primaryIcon, machines, statuses, versions, lastSeenAt, updates, flights]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Versions</DialogTitle>
          <DialogDescription>
            Each machine checks for itself and upgrades itself — versions drift independently, and
            that is fine.
          </DialogDescription>
        </DialogHeader>

        {/* min-w-0: DialogContent is a grid, and a grid item's default
            min-width:auto lets content push it past its track — which is how a
            long error message widened the whole dialog. */}
        <div className="flex min-w-0 flex-col gap-2">
          {rows.map((row) => (
            <MachineRow key={row.key} row={row} />
          ))}
        </div>

        <div className="flex justify-end">
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => void fetchAll(machineKeys(machines), true)}
          >
            <RefreshCw className={cn("size-3.5", busy && "animate-spin")} />
            Check again
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
