/**
 * Sidebar footer — one 30px line: account identity on the left, a three-column
 * instrument cluster (5h / 7d usage · disk) on the right, and a reconnecting
 * chip that only exists while the socket is down. Everything else — usage
 * detail, disk, machines, Claude account, theme, sign out — lives in one
 * consolidated popover that both the account button and the cluster open.
 */
import { Link } from "@tanstack/react-router";
import { Bot, HardDrive, Monitor, Moon, RefreshCw, Sun, User } from "lucide-react";
import { useEffect, useState } from "react";
import { AddMachineDialog, MachinesSection } from "~/components/machines/MachinesSection";
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
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { useConnectionStatus } from "~/hooks/useConnectionStatus";
import { useTheme } from "~/hooks/useTheme";
import { logout } from "~/lib/auth-api";
import { cn, formatBytes } from "~/lib/utils";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useClaudeAccountStore } from "~/stores/claude-account-store";
import type { RateLimitEntry } from "~/stores/rate-limit-store";
import { useRateLimitStore } from "~/stores/rate-limit-store";
import { useStorageStore } from "~/stores/storage-store";
import type { Theme } from "~/stores/ui-store";
import { ClaudeLoginDialog } from "./ClaudeLoginDialog";

// ── Meter tiers (shared by the cluster columns and the popover bars) ──

type Tier = "normal" | "warning" | "critical";

const TIER_FILL: Record<Tier, string> = {
  normal: "bg-primary",
  warning: "bg-warning",
  critical: "bg-destructive",
};

function usageTier(utilization: number): Tier {
  if (utilization >= 0.9) return "critical";
  if (utilization >= 0.7) return "warning";
  return "normal";
}

function effectiveUtilization(entry: RateLimitEntry | undefined): number {
  if (!entry) return 0;
  if (entry.resetsAt > 0 && Date.now() > entry.resetsAt * 1000) return 0;
  return entry.utilization;
}

function formatResetTime(resetsAt: number): string | null {
  const diffMs = resetsAt * 1000 - Date.now();
  if (diffMs <= 0) return null;
  const totalMin = Math.ceil(diffMs / 60_000);
  if (totalMin < 60) return `resets in ${totalMin}m`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `resets in ${h}h ${m}m` : `resets in ${h}h`;
}

const GB = 1024 ** 3;

function diskTier(freeBytes: number, totalBytes: number): Tier {
  const frac = totalBytes > 0 ? freeBytes / totalBytes : 1;
  if (frac < 0.05 || freeBytes < 5 * GB) return "critical";
  if (frac < 0.1 || freeBytes < 10 * GB) return "warning";
  return "normal";
}

export function SidebarFooter() {
  const [open, setOpen] = useState(false);
  const [addMachineOpen, setAddMachineOpen] = useState(false);
  const connection = useConnectionStatus();

  // Disk polling lives here because the cluster shows it permanently.
  const fetchDiskStats = useStorageStore((s) => s.fetchDiskStats);
  useEffect(() => {
    fetchDiskStats();
    const id = setInterval(fetchDiskStats, 60_000);
    return () => clearInterval(id);
  }, [fetchDiskStats]);

  return (
    <div className="border-t border-sidebar-border px-2 py-1.5">
      <Popover open={open} onOpenChange={setOpen}>
        <div className="flex items-center gap-1.5">
          <PopoverTrigger asChild>
            <AccountButton />
          </PopoverTrigger>
          <span className="flex-1" />
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
          <InstrumentCluster onOpen={() => setOpen(true)} />
        </div>
        <PopoverContent side="top" align="end" className="w-72 p-1.5">
          <SectionLabel>Usage</SectionLabel>
          <UsageDetail />
          <Separator />
          <SectionLabel>Machines</SectionLabel>
          <MachinesSection
            onAddMachine={() => {
              setOpen(false);
              setAddMachineOpen(true);
            }}
          />
          <Separator />
          <SectionLabel>Claude</SectionLabel>
          <ClaudeAccountRow />
          <Separator />
          <ThemeRow />
          <UserRow onNavigate={() => setOpen(false)} />
        </PopoverContent>
      </Popover>
      <AddMachineDialog open={addMachineOpen} onOpenChange={setAddMachineOpen} />
      <ClaudeLoginDialog />
    </div>
  );
}

// ── The instrument cluster: 5h · 7d · disk as three 3px columns ──

function InstrumentCluster({ onOpen }: { onOpen: () => void }) {
  const fiveHour = useRateLimitStore((s) => s.entries.five_hour);
  const sevenDay = useRateLimitStore((s) => s.entries.seven_day);
  const disk = useStorageStore((s) => s.disk);

  const fiveUtil = effectiveUtilization(fiveHour);
  const sevenUtil = effectiveUtilization(sevenDay);
  const diskPct = disk && disk.totalBytes > 0 ? Math.min(disk.usagePercent, 100) / 100 : 0;
  const dTier = disk ? diskTier(disk.freeBytes, disk.totalBytes) : "normal";

  const title = [
    `5h ${Math.round(fiveUtil * 100)}%`,
    `7d ${Math.round(sevenUtil * 100)}%`,
    disk ? `disk ${formatBytes(disk.freeBytes)} free` : "disk —",
  ].join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={`System meters: ${title}`}
          onClick={onOpen}
          className="flex h-6 cursor-pointer items-end gap-[3px] rounded-md px-1.5 pb-1 pt-1 transition-colors hover:bg-muted/50"
        >
          <MeterColumn fraction={fiveUtil} tier={usageTier(fiveUtil)} />
          <MeterColumn fraction={sevenUtil} tier={usageTier(sevenUtil)} />
          <MeterColumn fraction={diskPct} tier={dTier} />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top">{title}</TooltipContent>
    </Tooltip>
  );
}

function MeterColumn({ fraction, tier }: { fraction: number; tier: Tier }) {
  // A sliver stays visible at zero so the instrument reads as present, not broken.
  const pct = Math.max(Math.round(fraction * 100), 8);
  return (
    <span className="relative h-full w-[3px] overflow-hidden rounded-full bg-border/80">
      <span
        className={cn("absolute inset-x-0 bottom-0 rounded-full", TIER_FILL[tier])}
        style={{ height: `${pct}%`, opacity: fraction === 0 ? 0.35 : 1 }}
      />
    </span>
  );
}

// ── Popover building blocks ──

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint">
      {children}
    </div>
  );
}

function Separator() {
  return <div className="mx-2 my-1 h-px bg-border/60" />;
}

function UsageDetail() {
  const fiveHour = useRateLimitStore((s) => s.entries.five_hour);
  const sevenDay = useRateLimitStore((s) => s.entries.seven_day);
  const disk = useStorageStore((s) => s.disk);

  return (
    <div className="flex flex-col gap-1.5 px-3 py-1">
      <UsageRow label="5h" entry={fiveHour} />
      <UsageRow label="7d" entry={sevenDay} />
      {disk && disk.totalBytes > 0 && (
        <Link
          to="/storage"
          className="group flex items-center gap-2"
          title={`${formatBytes(disk.freeBytes)} free of ${formatBytes(disk.totalBytes)}`}
        >
          <HardDrive className="size-3 shrink-0 text-muted-foreground" />
          <MeterBar
            fraction={Math.min(disk.usagePercent, 100) / 100}
            tier={diskTier(disk.freeBytes, disk.totalBytes)}
          />
          <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground group-hover:text-foreground">
            {formatBytes(disk.freeBytes)} free
          </span>
        </Link>
      )}
    </div>
  );
}

function UsageRow({ label, entry }: { label: string; entry: RateLimitEntry | undefined }) {
  const util = effectiveUtilization(entry);
  const pct = Math.round(util * 100);
  const resetLabel = entry?.resetsAt ? formatResetTime(entry.resetsAt) : null;
  return (
    <div className="flex items-center gap-2">
      <span className="w-3 shrink-0 font-mono text-[10px] text-muted-foreground-faint">
        {label}
      </span>
      <MeterBar fraction={util} tier={usageTier(util)} />
      <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground">
        {pct}%{resetLabel ? ` · ${resetLabel}` : ""}
      </span>
    </div>
  );
}

function MeterBar({ fraction, tier }: { fraction: number; tier: Tier }) {
  return (
    <span className="h-[3px] flex-1 overflow-hidden rounded-full bg-border/80">
      <span
        className={cn("block h-full rounded-full", TIER_FILL[tier])}
        style={{ width: `${Math.min(Math.round(fraction * 100), 100)}%` }}
      />
    </span>
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

const THEME_CYCLE: Record<Theme, Theme> = { dark: "light", light: "system", system: "dark" };
const THEME_ICONS: Record<Theme, typeof Sun> = { dark: Moon, light: Sun, system: Monitor };
const THEME_LABELS: Record<Theme, string> = { dark: "Dark", light: "Light", system: "System" };

function ThemeRow() {
  const { theme, setTheme } = useTheme();
  const Icon = THEME_ICONS[theme];
  return (
    <button
      type="button"
      onClick={() => setTheme(THEME_CYCLE[theme])}
      className="flex w-full cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-xs text-foreground transition-colors hover:bg-muted/50"
    >
      <Icon className="size-3.5 text-muted-foreground" />
      Theme
      <span className="ml-auto text-[10px] text-muted-foreground">{THEME_LABELS[theme]}</span>
    </button>
  );
}

function UserRow({ onNavigate }: { onNavigate: () => void }) {
  const { authEnabled, user, clearAuth } = useAuthStore();
  if (!authEnabled || !user) return null;
  return (
    <div className="flex items-center gap-2 px-3 py-2">
      <Avatar className="h-5 w-5 shrink-0">
        <AvatarFallback className="bg-primary/20 text-primary">
          <User className="h-3 w-3" />
        </AvatarFallback>
      </Avatar>
      <span className="flex-1 truncate text-xs text-foreground">{user.displayName}</span>
      <button
        type="button"
        onClick={async () => {
          onNavigate();
          await logout();
          clearAuth();
          window.location.reload();
        }}
        className="cursor-pointer rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        Sign out
      </button>
    </div>
  );
}

// ── Claude account (folded in from the previous footer's popover) ──

function ClaudeAccountRow() {
  const { loggedIn, email, orgName, loading, fetchStatus, switchAccount, loginAccount } =
    useClaudeAccountStore();
  const activeSessions = useChatStore((s) => {
    let count = 0;
    for (const session of Object.values(s.sessions)) {
      const st = session.meta.state;
      if (st === "running" || st === "idle") count++;
      if (session.pendingApproval || session.pendingQuestion) count++;
    }
    return count;
  });

  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  if (loading) return null;

  const label = email ? (orgName ? `${email} (${orgName})` : email) : null;

  const handleSwitch = () => {
    if (activeSessions > 0) {
      setConfirmOpen(true);
    } else {
      switchAccount();
    }
  };

  return (
    <>
      <div className="flex items-center gap-2 px-3 py-2">
        <Avatar className="h-5 w-5 shrink-0">
          <AvatarFallback
            className={cn(
              loggedIn
                ? "bg-orange-500/20 text-orange-700 dark:text-orange-400"
                : "bg-muted text-muted-foreground",
            )}
          >
            <Bot className="h-3 w-3" />
          </AvatarFallback>
        </Avatar>
        {loggedIn ? (
          <>
            <span className="flex-1 truncate text-xs text-foreground" title={label ?? undefined}>
              {label ?? "Claude"}
            </span>
            <button
              type="button"
              onClick={handleSwitch}
              className="shrink-0 cursor-pointer rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="Switch Claude account"
            >
              <RefreshCw className="size-3" />
            </button>
          </>
        ) : (
          <>
            <span className="flex-1 text-xs text-muted-foreground-faint">Not authenticated</span>
            <button
              type="button"
              onClick={loginAccount}
              className="cursor-pointer rounded px-1.5 py-0.5 text-xs font-medium text-orange-700 transition-colors hover:bg-orange-500/10 dark:text-orange-400"
            >
              Login
            </button>
          </>
        )}
      </div>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Switch Claude account?</AlertDialogTitle>
            <AlertDialogDescription>
              There {activeSessions === 1 ? "is" : "are"} {activeSessions} active session
              {activeSessions === 1 ? "" : "s"}. Switching accounts won't stop them, but they may
              encounter authentication errors.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmOpen(false);
                switchAccount();
              }}
            >
              Switch
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
