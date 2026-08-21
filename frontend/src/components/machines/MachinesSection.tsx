import { Plus, Server, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { pairMachine } from "~/lib/machines/pairing";
import { cn } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import type { MachineStatus } from "~/stores/machine-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * The machines block of the sidebar-footer popover (multi-machine): this
 * machine, each paired machine with its live connection dot and a remove
 * action, and an Add-machine entry. The caller owns the AddMachineDialog.
 */
export function MachinesSection({ onAddMachine }: { onAddMachine: () => void }) {
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const removeMachine = useMachineStore((s) => s.removeMachine);
  const primaryLabel = useFeatureStore((s) => s.machineLabel);

  const entries = Object.values(machines).sort((a, b) => a.label.localeCompare(b.label));

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 px-3 py-2">
        <StatusDot status="connected" />
        <span className="text-xs text-foreground truncate flex-1">
          {primaryLabel || "This machine"}
        </span>
        <span className="text-[10px] text-muted-foreground">this machine</span>
      </div>
      {entries.map((m) => (
        <div key={m.machineId} className="flex items-center gap-2 px-3 py-2">
          <StatusDot status={statuses[m.machineId] ?? "disconnected"} />
          <div className="min-w-0 flex-1">
            <div className="text-xs text-foreground truncate">{m.label}</div>
            <div className="text-[10px] text-muted-foreground truncate">{m.baseUrl}</div>
          </div>
          <button
            type="button"
            title="Remove machine"
            onClick={() => removeMachine(m.machineId)}
            className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors shrink-0"
          >
            <X className="size-3" />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={onAddMachine}
        className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors cursor-pointer rounded-md"
      >
        <Plus className="size-3.5" />
        Add machine
      </button>
    </div>
  );
}

function StatusDot({ status }: { status: MachineStatus }) {
  return (
    <span
      className={cn(
        "inline-block h-2 w-2 rounded-full shrink-0",
        status === "connected" && "bg-success",
        status === "reconnecting" && "bg-warning animate-pulse",
        status === "disconnected" && "bg-destructive",
      )}
    />
  );
}

interface DiscoveredPeer {
  machineId: string;
  label: string;
  url: string;
  version: string;
  pairing: boolean;
}

export function AddMachineDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [address, setAddress] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Tailnet peer discovery: the primary probes online peers for agentique
  // descriptors; hits become one-click suggestions. Purely a hint —
  // pairing/auth is unchanged.
  const paired = useMachineStore((s) => s.machines);
  const [discovered, setDiscovered] = useState<DiscoveredPeer[]>([]);
  const [discovering, setDiscovering] = useState(false);
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setDiscovering(true);
    fetch("/api/machines/discover")
      .then((res) => (res.ok ? res.json() : []))
      .then((peers: DiscoveredPeer[]) => {
        if (!cancelled) setDiscovered(peers);
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setDiscovering(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);
  const suggestions = discovered.filter((p) => !paired[p.machineId]);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      await pairMachine(address, token);
      setAddress("");
      setToken("");
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add machine</DialogTitle>
          <DialogDescription>
            On the other machine, run <code className="font-mono">agentique pair</code> and enter
            its address and the one-time token here.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          {(suggestions.length > 0 || discovering) && (
            <div className="flex flex-col gap-1.5">
              <Label>Found on your tailnet</Label>
              {discovering && suggestions.length === 0 && (
                <p className="text-xs text-muted-foreground">Scanning…</p>
              )}
              {suggestions.map((p) => (
                <button
                  key={p.machineId}
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setAddress(p.url);
                    setError(null);
                  }}
                  className={cn(
                    "flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-left text-xs transition-colors cursor-pointer",
                    address === p.url
                      ? "border-primary/50 bg-primary/10"
                      : "border-border/60 hover:bg-muted/40",
                  )}
                >
                  <Server className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="font-medium">{p.label}</span>
                  <span className="truncate text-muted-foreground">{p.url}</span>
                  <span className="ml-auto shrink-0 text-[10px] text-muted-foreground-faint">
                    {p.pairing ? "needs token" : "no auth"}
                  </span>
                </button>
              ))}
            </div>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="machine-address">Address</Label>
            <Input
              id="machine-address"
              placeholder="https://machine.tailnet.ts.net:9201"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="machine-token">Pairing token</Label>
            <Input
              id="machine-token"
              placeholder="ABCD2345WXYZ (empty for auth-disabled machines)"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              disabled={busy}
              onKeyDown={(e) => {
                if (e.key === "Enter" && address && !busy) submit();
              }}
            />
          </div>
          {error && <p className="text-xs text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !address.trim()}>
            {busy ? "Pairing…" : "Pair"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
