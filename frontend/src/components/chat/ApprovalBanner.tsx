import { Check, ShieldAlert, X } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval, setAutoApprove } from "~/lib/session-actions";
import type { PendingApproval } from "~/stores/chat-store";

interface ApprovalBannerProps {
  sessionId: string;
  approval: PendingApproval;
  projectPath?: string;
  worktreePath?: string;
}

function stripPrefix(path: string, projectPath?: string, worktreePath?: string): string {
  for (const prefix of [worktreePath, projectPath]) {
    if (prefix && path.startsWith(prefix)) {
      const stripped = path.slice(prefix.length);
      return stripped.startsWith("/") ? stripped.slice(1) : stripped;
    }
  }
  return path;
}

function formatInput(
  toolName: string,
  input: unknown,
  projectPath?: string,
  worktreePath?: string,
): string {
  if (typeof input === "string") return input;
  if (!input || typeof input !== "object") return JSON.stringify(input);

  const obj = input as Record<string, unknown>;
  const strip = (p: string) => stripPrefix(p, projectPath, worktreePath);

  switch (toolName) {
    case "Read":
    case "Write":
    case "Edit":
      return strip(String(obj.file_path ?? ""));
    case "Glob":
      return String(obj.pattern ?? "");
    case "Grep":
      return `${obj.pattern ?? ""}${obj.path ? ` in ${strip(String(obj.path))}` : ""}`;
    case "Bash":
      return String(obj.command ?? obj.description ?? "");
    case "Agent":
      return String(obj.description ?? obj.prompt ?? "").slice(0, 120);
    case "WebFetch":
    case "WebSearch":
      return String(obj.url ?? obj.query ?? "");
    case "EnterPlanMode":
    case "ExitPlanMode":
      return "Agent wants to enter plan mode";
    default: {
      // Try common field names before falling back to JSON
      const desc =
        obj.description ?? obj.command ?? obj.file_path ?? obj.path ?? obj.pattern ?? obj.prompt;
      if (desc) return strip(String(desc)).slice(0, 120);
      // Last resort: show key names instead of raw JSON
      const keys = Object.keys(obj).filter((k) => k !== "type");
      return keys.length > 0 ? `(${keys.join(", ")})` : toolName;
    }
  }
}

export function ApprovalBanner({
  sessionId,
  approval,
  projectPath,
  worktreePath,
}: ApprovalBannerProps) {
  const ws = useWebSocket();
  const [submitting, setSubmitting] = useState(false);
  const summary = formatInput(approval.toolName, approval.input, projectPath, worktreePath);

  const handleAllow = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, true).catch((err) => {
      setSubmitting(false);
      toast.error(err instanceof Error ? err.message : "Failed to approve tool");
    });
  }, [ws, sessionId, approval.approvalId]);

  const handleAllowAll = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, true)
      .then(() => setAutoApprove(ws, sessionId, true))
      .catch((err) => {
        setSubmitting(false);
        toast.error(err instanceof Error ? err.message : "Failed to approve tool");
      });
  }, [ws, sessionId, approval.approvalId]);

  const handleDeny = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, false, "User denied").catch((err) => {
      setSubmitting(false);
      toast.error(err instanceof Error ? err.message : "Failed to deny tool");
    });
  }, [ws, sessionId, approval.approvalId]);

  return (
    <div className="mx-4 mb-2 rounded-md border border-yellow-500/40 bg-yellow-500/10 px-3 py-2">
      <div className="flex items-center gap-2 text-sm">
        <ShieldAlert className="h-4 w-4 shrink-0 text-yellow-500" />
        <span className="font-medium shrink-0">{approval.toolName}</span>
        <span className="text-muted-foreground truncate min-w-0">{summary}</span>
        <div className="flex items-center gap-1.5 ml-auto shrink-0">
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
            onClick={handleDeny}
            disabled={submitting}
          >
            <X className="h-3.5 w-3.5 mr-1" />
            Deny
          </Button>
          <Button
            size="sm"
            className="h-7 px-2 bg-green-600 hover:bg-green-700 text-white"
            onClick={handleAllow}
            disabled={submitting}
          >
            <Check className="h-3.5 w-3.5 mr-1" />
            Allow
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-muted-foreground hover:text-foreground"
            onClick={handleAllowAll}
            disabled={submitting}
          >
            Allow all
          </Button>
        </div>
      </div>
    </div>
  );
}
