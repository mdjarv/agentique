import { ExternalLink, Loader } from "lucide-react";
import { useState } from "react";
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
import { useClaudeAccountStore } from "~/stores/claude-account-store";

export function ClaudeLoginDialog() {
  const {
    loginDialogOpen,
    switching,
    loginUrl,
    error,
    submittingCode,
    closeLoginDialog,
    submitCode,
  } = useClaudeAccountStore();
  const [code, setCode] = useState("");

  const handleSubmitCode = async () => {
    if (!code.trim()) return;
    await submitCode(code.trim());
    setCode("");
  };

  const handleOpenChange = (open: boolean) => {
    if (!open) closeLoginDialog();
  };

  return (
    <Dialog open={loginDialogOpen} onOpenChange={handleOpenChange}>
      <DialogContent showCloseButton={false} className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Claude Authentication</DialogTitle>
          <DialogDescription>
            Sign in to Claude by authorizing in the browser window.
          </DialogDescription>
        </DialogHeader>

        {switching && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader className="size-4 animate-spin shrink-0" />
            <span>Waiting for authentication...</span>
          </div>
        )}

        {loginUrl && (
          <a
            href={loginUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 text-sm text-primary hover:underline"
          >
            <ExternalLink className="size-3.5 shrink-0" />
            Open login page manually
          </a>
        )}

        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            After authorizing, paste the redirect URL from your browser or the authorization code
            shown on the page:
          </p>
          <div className="flex gap-2">
            <Input
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="Paste URL or authorization code"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSubmitCode();
              }}
              disabled={submittingCode}
            />
            <Button size="sm" onClick={handleSubmitCode} disabled={!code.trim() || submittingCode}>
              {submittingCode ? <Loader className="size-4 animate-spin" /> : "Submit"}
            </Button>
          </div>
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <p className="text-xs text-muted-foreground-faint leading-tight">
          To switch to a different account, sign out of claude.ai first.
        </p>

        <DialogFooter>
          <Button variant="outline" onClick={closeLoginDialog}>
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
