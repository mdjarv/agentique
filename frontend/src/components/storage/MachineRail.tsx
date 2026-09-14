/**
 * Which machine the Storage page is about (docs/storage.md, "More than one
 * machine").
 *
 * One list, two presentations: a rail beside the page on desktop, and on a
 * phone a single picker that opens the same list in a sheet — a rail would take
 * a third of a 390px screen from rows that already wrap.
 *
 * Each row carries the machine's level, so machines compare without being
 * opened. The bar is neutral until the disk is below the floor (`isLowDisk`),
 * the same test the footer notch uses, so the rail and the footer can never
 * disagree about which machine is low. The row says free space in words
 * because a level bar alone cannot tell 9 GB from 900.
 */
import { Link } from "@tanstack/react-router";
import { ChevronDown } from "lucide-react";
import { useState } from "react";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "~/components/ui/sheet";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { isLowDisk, type StorageMachine } from "~/lib/storage/fleet";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { cn, formatBytes } from "~/lib/utils";

/** The `?machine=` a row links to; this machine is the bare page. */
function searchFor(key: string): { machine?: string } {
  return key === PRIMARY_MACHINE_KEY ? {} : { machine: key };
}

export function MachineRail({
  machines,
  selected,
}: {
  machines: StorageMachine[];
  selected: string;
}) {
  return (
    <nav
      aria-label="Machines"
      className="flex w-48 shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-border/60 p-2"
    >
      {machines.map((m) => (
        <MachineRow key={m.key} machine={m} selected={m.key === selected} />
      ))}
    </nav>
  );
}

export function MachinePicker({
  machines,
  selected,
}: {
  machines: StorageMachine[];
  selected: string;
}) {
  const [open, setOpen] = useState(false);
  const current = machines.find((m) => m.key === selected) ?? machines[0];
  if (!current) return null;
  // The notch says "another machine needs you", so it is about the machines
  // you are NOT looking at; the one on screen already shows its own level.
  // An away machine's reading is left out, as `lowRemotes` leaves it out of the
  // footer: nothing can be done about it until it is back.
  const elsewhereLow = machines.some((m) => m.key !== selected && m.online && isLowDisk(m.disk));

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={`Machine: ${current.label}${elsewhereLow ? " — another machine is low on disk" : ""}`}
        className="relative mx-3 mt-3 flex items-center gap-2 rounded-lg border bg-card/40 px-3 py-2 text-left"
      >
        <MachineIcon icon={current.icon} />
        <span className="min-w-0 flex-1 truncate text-sm text-foreground-bright">
          {current.label}
        </span>
        <FreeText machine={current} />
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        {elsewhereLow && (
          <span className="absolute -top-1 -right-1 size-2 rounded-full bg-warning ring-2 ring-background" />
        )}
      </button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent
          side="bottom"
          aria-describedby={undefined}
          className="max-h-[80vh] gap-0 pb-[env(safe-area-inset-bottom)]"
        >
          <SheetHeader>
            <SheetTitle>Machine</SheetTitle>
          </SheetHeader>
          <div className="flex flex-col gap-0.5 overflow-y-auto px-2 pb-3">
            {machines.map((m) => (
              <MachineRow
                key={m.key}
                machine={m}
                selected={m.key === selected}
                onSelect={() => setOpen(false)}
              />
            ))}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

function MachineRow({
  machine,
  selected,
  onSelect,
}: {
  machine: StorageMachine;
  selected: boolean;
  onSelect?: () => void;
}) {
  const disk = machine.disk;
  const low = isLowDisk(disk);
  const pct = disk ? Math.min(Math.max(disk.usagePercent, 0), 100) : 0;
  return (
    <Link
      to="/storage"
      search={searchFor(machine.key)}
      onClick={onSelect}
      aria-current={selected ? "page" : undefined}
      className={cn(
        "flex flex-col gap-1.5 rounded-md px-2.5 py-2 transition-colors",
        selected ? "bg-muted text-foreground-bright" : "text-muted-foreground hover:bg-muted/50",
        !machine.online && "opacity-55",
      )}
    >
      <span className="flex min-w-0 items-center gap-2">
        <MachineIcon icon={machine.icon} />
        <span className="min-w-0 flex-1 truncate text-xs">{machine.label}</span>
        <FreeText machine={machine} />
      </span>
      <span className="block h-[3px] overflow-hidden rounded-full bg-border/60">
        <span
          className={cn(
            "block h-full rounded-full",
            low ? "bg-warning" : selected ? "bg-foreground-dim" : "bg-muted-foreground-faint",
          )}
          style={{ width: `${pct}%` }}
        />
      </span>
    </Link>
  );
}

/** "231 GB free", "away", or nothing before the first reading. */
function FreeText({ machine }: { machine: StorageMachine }) {
  if (!machine.online) {
    return <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">away</span>;
  }
  if (!machine.disk) return null;
  return (
    <span
      className={cn(
        "shrink-0 font-mono text-[10px] tabular-nums",
        isLowDisk(machine.disk) ? "text-warning" : "text-muted-foreground-faint",
      )}
    >
      {formatBytes(machine.disk.freeBytes)} free
    </span>
  );
}

function MachineIcon({ icon }: { icon: string }) {
  const Icon = getMachineIcon(icon) ?? DEFAULT_MACHINE_ICON;
  return <Icon className="size-3.5 shrink-0" />;
}
