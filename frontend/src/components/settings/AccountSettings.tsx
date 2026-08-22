/**
 * Settings › Account — the Claude account sessions run as, and the sign-in to
 * agentique itself. Both used to live in the sidebar footer's popover.
 */
import { Bot, RefreshCw, User } from "lucide-react";
import { useEffect, useState } from "react";
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
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
import { Button } from "~/components/ui/button";
import { logout } from "~/lib/auth-api";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useClaudeAccountStore } from "~/stores/claude-account-store";

/** Sessions that a Claude account switch could disturb. */
function useActiveSessionCount(): number {
  return useChatStore((s) => {
    let count = 0;
    for (const session of Object.values(s.sessions)) {
      const st = session.meta.state;
      if (st === "running" || st === "idle") count++;
      if (session.pendingApproval || session.pendingQuestion) count++;
    }
    return count;
  });
}

export function AccountSettings() {
  const { loggedIn, email, orgName, loading, fetchStatus, switchAccount, loginAccount } =
    useClaudeAccountStore();
  const { authEnabled, user, clearAuth } = useAuthStore();
  const activeSessions = useActiveSessionCount();
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const claudeLabel = email ? (orgName ? `${email} (${orgName})` : email) : "Not authenticated";

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection title="Claude" description="The account provider CLIs authenticate as.">
        <SettingsRow
          label={loading ? "Checking…" : claudeLabel}
          description={loggedIn ? "Sessions run as this account." : "Log in to start sessions."}
          control={
            <div className="flex items-center gap-2">
              <Avatar className="size-7">
                <AvatarFallback
                  className={
                    loggedIn
                      ? "bg-orange-500/20 text-orange-700 dark:text-orange-400"
                      : "bg-muted text-muted-foreground"
                  }
                >
                  <Bot className="size-3.5" />
                </AvatarFallback>
              </Avatar>
              {loggedIn ? (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => (activeSessions > 0 ? setConfirmOpen(true) : switchAccount())}
                >
                  <RefreshCw className="size-3.5" />
                  Switch
                </Button>
              ) : (
                <Button size="sm" onClick={loginAccount}>
                  Log in
                </Button>
              )}
            </div>
          }
        />
      </SettingsSection>

      {authEnabled && user && (
        <SettingsSection title="Agentique" description="Your sign-in to this server.">
          <SettingsRow
            label={user.displayName}
            control={
              <div className="flex items-center gap-2">
                <Avatar className="size-7">
                  <AvatarFallback className="bg-primary/20 text-primary">
                    <User className="size-3.5" />
                  </AvatarFallback>
                </Avatar>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={async () => {
                    await logout();
                    clearAuth();
                    window.location.reload();
                  }}
                >
                  Sign out
                </Button>
              </div>
            }
          />
        </SettingsSection>
      )}

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
    </div>
  );
}
