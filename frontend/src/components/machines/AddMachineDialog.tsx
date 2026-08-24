import { Server } from "lucide-react";
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
import { useMachineStore } from "~/stores/machine-store";

/**
 * Add (pair) a machine: probe an address, exchange the one-time token from
 * `agentique pair`, save the catalog entry. Lives beside the catalog rather
 * than inside a settings panel because re-pairing a faulted machine opens the
 * same dialog.
 */
/** One tailnet peer that answered the descriptor probe — a pairing hint. */
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
  repairMachineId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Re-pairing this catalog entry rather than adding a new machine. */
  repairMachineId?: string;
}) {
  const repairing = useMachineStore((s) =>
    repairMachineId ? s.machines[repairMachineId] : undefined,
  );
  const [address, setAddress] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Re-pairing already knows the address — only the token is new. Seeding on
  // open (not on every render) leaves the field editable, which matters when
  // the machine moved and the stale address is the thing being corrected.
  useEffect(() => {
    if (!open) return;
    setToken("");
    setError(null);
    setAddress(repairing?.baseUrl ?? "");
  }, [open, repairing?.baseUrl]);

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
  // A paired machine is not a pairing suggestion — unless it is the very one
  // being re-paired, which is otherwise filtered out of its own dialog.
  const suggestions = discovered.filter(
    (p) => p.pairing && (!paired[p.machineId] || p.machineId === repairMachineId),
  );

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
          <DialogTitle>
            {repairing ? `Re-pair ${repairing.label || "machine"}` : "Add machine"}
          </DialogTitle>
          <DialogDescription>
            {repairing ? (
              <>
                This machine no longer accepts our credential. Run{" "}
                <code className="font-mono">agentique pair</code> on {repairing.label || "it"} and
                enter the new one-time token — its projects and history are kept.
              </>
            ) : (
              <>
                On the other machine, run <code className="font-mono">agentique pair</code> and
                enter its address and the one-time token here.
              </>
            )}
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
              placeholder="ABCD2345WXYZ"
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
            {busy ? "Pairing…" : repairing ? "Re-pair" : "Pair"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
