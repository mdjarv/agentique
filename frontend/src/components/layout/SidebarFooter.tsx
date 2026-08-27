/**
 * Sidebar footer — one line: account identity on the left, the usage cluster on
 * the right, and a reconnecting chip that only exists while the socket is down.
 * Both open one popover.
 *
 * The cluster is one group per agent — a run of meters, then that vendor's mark
 * (docs/usage.md). Its numbers come from ONE server-side collector rather than
 * from whatever a running session happened to emit, so they are current with no
 * session open and they cover every provider rather than only the one that
 * volunteers events.
 *
 * The popover is deliberately thin: the meters it fronts, and the way through
 * to everything else. Machines, theme, the Claude account and sign-out moved
 * to /settings once they outgrew a 288px column — a popover is a glance, not
 * a control panel.
 */
import { Link } from "@tanstack/react-router";
import { Boxes, HardDrive, type LucideIcon, Settings as SettingsIcon, User } from "lucide-react";
import { useEffect, useState } from "react";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { UpdateChip } from "~/components/update/UpdateChip";
import { UpdateDialog } from "~/components/update/UpdateDialog";
import { UpdatePopoverRows } from "~/components/update/UpdatePopoverRows";
import { UsageCluster } from "~/components/usage/UsageCluster";
import { UsagePanel } from "~/components/usage/UsagePanel";
import { useConnectionStatus } from "~/hooks/useConnectionStatus";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { cn } from "~/lib/utils";
import { useAuthStore } from "~/stores/auth-store";
import { useUpdateStore } from "~/stores/update-store";
import { startUsagePolling, useUsageStore } from "~/stores/usage-store";
import { ClaudeLoginDialog } from "./ClaudeLoginDialog";

export function SidebarFooter() {
  const [open, setOpen] = useState(false);
  const [versionsOpen, setVersionsOpen] = useState(false);
  const connection = useConnectionStatus();
  const doc = useUsageStore((s) => s.doc);
  const version = useUpdateStore((s) => s.statuses[PRIMARY_MACHINE_KEY]?.current) ?? "";
  const close = () => setOpen(false);

  // One poll for the whole app, started here because the footer outlives every
  // route. The server holds the cache and does the probing, so this is a cheap
  // read rather than a round trip to a vendor.
  useEffect(() => startUsagePolling(), []);

  return (
    <div className="border-t border-sidebar-border px-2 py-1.5">
      <Popover open={open} onOpenChange={setOpen}>
        <div className="flex items-center gap-1.5">
          <PopoverTrigger asChild>
            <AccountButton />
          </PopoverTrigger>
          <span className="flex-1" />
          <UpdateChip />
          {connection !== "connected" && (
            <span
              className={cn(
                "rounded-full px-2 py-1 font-mono text-[10px] font-semibold",
                connection === "reconnecting"
                  ? "animate-pulse bg-warning/15 text-warning motion-reduce:animate-none"
                  : "bg-destructive/15 text-destructive",
              )}
            >
              {connection === "reconnecting" ? "reconnecting" : "disconnected"}
            </span>
          )}
          <PopoverTrigger asChild>
            <button
              type="button"
              aria-label="Subscription usage"
              className="flex h-6 cursor-pointer items-center rounded-md px-1.5 transition-colors hover:bg-muted/50"
            >
              <UsageCluster doc={doc} />
            </button>
          </PopoverTrigger>
        </div>
        {/* The content grows with what is true: an upgrade row, a window per
            allowance, the disk gauge and the ways out. On a short window that
            can exceed the space above the trigger, so it scrolls rather than
            clipping its own first row — which is the one carrying the verb. */}
        <PopoverContent
          side="top"
          align="end"
          collisionPadding={8}
          className="max-h-(--radix-popover-content-available-height) w-72 overflow-y-auto p-1.5"
        >
          {/* Above the meters, and only when populated: the verbs come first
              because they are the only thing here that can be acted on. */}
          <UpdatePopoverRows />
          <UsagePanel />
          <div className="mx-2 my-1 h-px bg-border/60" />
          <button
            type="button"
            onClick={() => {
              close();
              setVersionsOpen(true);
            }}
            className="flex w-full cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-xs text-foreground transition-colors hover:bg-muted/50"
          >
            <Boxes className="size-3.5 text-muted-foreground" />
            Versions
            <span className="ml-auto truncate font-mono text-[10px] text-muted-foreground-faint">
              {version}
            </span>
          </button>
          <NavRow to="/settings" icon={SettingsIcon} label="Settings" onNavigate={close} />
          <NavRow to="/storage" icon={HardDrive} label="Storage" onNavigate={close} />
        </PopoverContent>
      </Popover>
      {/* The fleet view. The popover carries this machine's verb; a row per
          machine needs the room only a dialog has. */}
      <UpdateDialog open={versionsOpen} onOpenChange={setVersionsOpen} />
      <ClaudeLoginDialog />
    </div>
  );
}

// ── Identity: the footer button and the popover's account rows ──

const AccountButton = ({ ...props }: React.ComponentPropsWithoutRef<"button">) => {
  const { authEnabled, user } = useAuthStore();
  const name = authEnabled && user ? user.displayName : "Account";
  return (
    <button
      type="button"
      {...props}
      className="flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
    >
      <Avatar className="h-5 w-5 shrink-0">
        <AvatarFallback className="bg-primary/20 text-primary">
          <User className="h-3 w-3" />
        </AvatarFallback>
      </Avatar>
      <span className="max-w-[110px] truncate">{name}</span>
    </button>
  );
};

/** A way out of the popover: one line, one destination. */
function NavRow({
  to,
  icon: Icon,
  label,
  onNavigate,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  onNavigate: () => void;
}) {
  return (
    <Link
      to={to}
      onClick={onNavigate}
      className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-xs text-foreground transition-colors hover:bg-muted/50"
    >
      <Icon className="size-3.5 text-muted-foreground" />
      {label}
    </Link>
  );
}
