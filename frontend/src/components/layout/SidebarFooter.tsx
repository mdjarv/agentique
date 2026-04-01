import {
  BellDot,
  Check,
  Circle,
  CircleHelp,
  Loader,
  LogOut,
  Pause,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import { type ComponentType, useEffect, useState } from "react";
import { useShallow } from "zustand/react/shallow";
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
import { logout } from "~/lib/auth-api";
import { cn } from "~/lib/utils";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useClaudeAccountStore } from "~/stores/claude-account-store";
import { ConnectionIndicator } from "./ConnectionIndicator";
import { UsageBars } from "./UsageBars";

type DisplayState =
  | "approval"
  | "unseen"
  | "running"
  | "merging"
  | "idle"
  | "done"
  | "stopped"
  | "failed";

const stateConfig: Record<
  DisplayState,
  { icon: ComponentType<{ className?: string }>; color: string; pulse?: boolean; label: string }
> = {
  approval: {
    icon: CircleHelp,
    color: "text-orange",
    pulse: true,
    label: "waiting for approval",
  },
  unseen: { icon: BellDot, color: "text-warning", label: "new response" },
  running: { icon: Loader, color: "text-teal", label: "running" },
  merging: { icon: Loader, color: "text-primary", label: "merging" },
  idle: { icon: Circle, color: "text-success", label: "idle" },
  done: { icon: Check, color: "text-success", label: "done" },
  stopped: { icon: Pause, color: "text-foreground/80", label: "stopped" },
  failed: { icon: TriangleAlert, color: "text-destructive", label: "failed" },
};

const displayOrder: DisplayState[] = [
  "approval",
  "unseen",
  "running",
  "merging",
  "idle",
  "done",
  "stopped",
  "failed",
];

export function SidebarFooter() {
  // Compute counts inside the selector — only re-renders when a count actually changes
  const counts = useChatStore(
    useShallow((s) => {
      const c: Record<DisplayState, number> = {
        approval: 0,
        unseen: 0,
        running: 0,
        merging: 0,
        idle: 0,
        done: 0,
        stopped: 0,
        failed: 0,
      };
      for (const session of Object.values(s.sessions)) {
        if (session.meta.worktreeMerged) continue;
        if (session.pendingApproval || session.pendingQuestion) {
          c.approval++;
        } else if (session.hasUnseenCompletion && session.meta.state === "idle") {
          c.unseen++;
        } else {
          const state = session.meta.state as DisplayState;
          if (state in c) c[state]++;
        }
      }
      return c;
    }),
  );
  const { authEnabled, user, clearAuth } = useAuthStore();

  const hasAny = displayOrder.some((s) => counts[s] > 0);

  return (
    <div className="px-3 py-2 border-t border-sidebar-border space-y-1">
      <UsageBars />
      <ClaudeAccountRow activeSessions={counts.running + counts.idle + counts.approval} />
      {hasAny && (
        <div className="flex items-center gap-3">
          {displayOrder.map((state) => {
            const count = counts[state];
            if (count === 0) return null;
            const cfg = stateConfig[state];
            const Icon = cfg.icon;
            return (
              <span
                key={state}
                className={cn("flex items-center gap-1 text-xs", cfg.color)}
                title={`${count} ${cfg.label}`}
              >
                <Icon
                  className={cn(
                    "size-3 shrink-0",
                    cfg.pulse && "animate-pulse",
                    (state === "running" || state === "merging") && "animate-spin",
                  )}
                />
                <span className="text-muted-foreground">{count}</span>
              </span>
            );
          })}
        </div>
      )}
      <div className="flex items-center justify-between">
        <ConnectionIndicator />
        {authEnabled && user && (
          <button
            type="button"
            onClick={async () => {
              await logout();
              clearAuth();
              window.location.reload();
            }}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            title={`Sign out ${user.displayName}`}
          >
            <span className="truncate max-w-24">{user.displayName}</span>
            <LogOut className="size-3 shrink-0" />
          </button>
        )}
      </div>
    </div>
  );
}

function ClaudeAccountRow({ activeSessions }: { activeSessions: number }) {
  const { loggedIn, email, orgName, loading, switching, fetchStatus, switchAccount, loginAccount } =
    useClaudeAccountStore();
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  if (loading) return null;

  const label = email ? (orgName ? `${email} (${orgName})` : email) : null;

  if (switching) {
    return (
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Loader className="size-3 shrink-0 animate-spin" />
        <span>Waiting for browser login...</span>
      </div>
    );
  }

  if (!loggedIn) {
    return (
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">Claude: not authenticated</span>
        <button
          type="button"
          onClick={loginAccount}
          className="text-primary hover:text-primary/80 transition-colors"
        >
          Login
        </button>
      </div>
    );
  }

  const handleSwitch = () => {
    if (activeSessions > 0) {
      setConfirmOpen(true);
    } else {
      switchAccount();
    }
  };

  return (
    <>
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground truncate max-w-40" title={label ?? undefined}>
          {label}
        </span>
        <button
          type="button"
          onClick={handleSwitch}
          className="flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors shrink-0"
          title="Switch Claude account"
        >
          <RefreshCw className="size-3" />
        </button>
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
