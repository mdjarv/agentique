import { Bot, RefreshCw, User } from "lucide-react";
import { useEffect, useState } from "react";
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
import { logout } from "~/lib/auth-api";
import { cn } from "~/lib/utils";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useClaudeAccountStore } from "~/stores/claude-account-store";
import { ClaudeLoginDialog } from "./ClaudeLoginDialog";
import { ConnectionIndicator } from "./ConnectionIndicator";
import { ThemeToggle } from "./ThemeToggle";
import { UsageBars } from "./UsageBars";

export function SidebarFooter() {
  const { authEnabled, user, clearAuth } = useAuthStore();

  return (
    <div className="px-3 py-2 border-t border-sidebar-border">
      <UsageBars />
      <div className="flex items-center gap-2">
        <ConnectionIndicator />
        <ThemeToggle />
        <UserButton authEnabled={authEnabled} user={user} clearAuth={clearAuth} />
      </div>
    </div>
  );
}

// --- Unified user button with popover ---

function UserButton({
  authEnabled,
  user,
  clearAuth,
}: {
  authEnabled: boolean;
  user: { displayName: string } | null;
  clearAuth: () => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="ml-auto flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors cursor-pointer"
        >
          <User className="size-3.5" />
          {authEnabled && user ? (
            <span className="truncate max-w-[80px]">{user.displayName}</span>
          ) : (
            <span>Account</span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-64 p-0">
        <div className="flex flex-col">
          {/* Claude account section */}
          <ClaudeAccountSection />

          {/* App user section */}
          {authEnabled && user && (
            <>
              <div className="h-px bg-border" />
              <div className="flex items-center gap-2 px-3 py-2.5">
                <Avatar className="h-5 w-5 shrink-0">
                  <AvatarFallback className="bg-primary/20 text-primary">
                    <User className="h-3 w-3" />
                  </AvatarFallback>
                </Avatar>
                <span className="text-xs text-foreground truncate flex-1">{user.displayName}</span>
                <button
                  type="button"
                  onClick={async () => {
                    await logout();
                    clearAuth();
                    window.location.reload();
                  }}
                  className="cursor-pointer rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
                >
                  Sign out
                </button>
              </div>
            </>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ClaudeAccountSection() {
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
      <div className="flex items-center gap-2 px-3 py-2.5">
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
            <span className="text-xs text-foreground truncate flex-1" title={label ?? undefined}>
              {label ?? "Claude"}
            </span>
            <button
              type="button"
              onClick={handleSwitch}
              className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors shrink-0"
              title="Switch Claude account"
            >
              <RefreshCw className="size-3" />
            </button>
          </>
        ) : (
          <>
            <span className="text-xs text-muted-foreground-faint flex-1">Not authenticated</span>
            <button
              type="button"
              onClick={loginAccount}
              className="cursor-pointer rounded px-1.5 py-0.5 text-xs font-medium text-orange-700 dark:text-orange-400 hover:bg-orange-500/10 transition-colors"
            >
              Login
            </button>
          </>
        )}
      </div>
      <ClaudeLoginDialog />

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
