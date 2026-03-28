import { Check, ShieldAlert, X } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval, setAutoApprove } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";
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
      return "Agent wants to enter plan mode";
    case "ExitPlanMode":
      return "Agent wants to exit plan mode";
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
  const isMobile = useIsMobile();
  const [submitting, setSubmitting] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const summary = formatInput(approval.toolName, approval.input, projectPath, worktreePath);

  const handleAllow = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, true).catch((err) => {
      setSubmitting(false);
      toast.error(getErrorMessage(err, "Failed to approve tool"));
    });
  }, [ws, sessionId, approval.approvalId]);

  const handleAllowAll = useCallback(() => {
    setSubmitting(true);
    setAutoApprove(ws, sessionId, true)
      .then(() => resolveApproval(ws, sessionId, approval.approvalId, true))
      .catch((err) => {
        setSubmitting(false);
        toast.error(getErrorMessage(err, "Failed to approve tool"));
      });
  }, [ws, sessionId, approval.approvalId]);

  const handleDeny = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, false, "User denied").catch((err) => {
      setSubmitting(false);
      toast.error(getErrorMessage(err, "Failed to deny tool"));
    });
  }, [ws, sessionId, approval.approvalId]);

  const buttons = (
    <div className="flex items-center gap-1.5 shrink-0 max-md:ml-auto">
      <Button
        size="sm"
        variant="ghost"
        className="h-7 max-md:h-10 px-2 max-md:px-3 text-destructive hover:text-destructive hover:bg-destructive/10"
        onClick={handleDeny}
        disabled={submitting}
      >
        <X className="h-3.5 w-3.5 mr-1" />
        Deny
      </Button>
      <Button
        size="sm"
        className="h-7 max-md:h-10 px-2 max-md:px-3 bg-success hover:bg-success/90 text-background"
        onClick={handleAllow}
        disabled={submitting}
      >
        <Check className="h-3.5 w-3.5 mr-1" />
        Allow
      </Button>
      <Button
        size="sm"
        variant="ghost"
        className="h-7 max-md:h-10 px-2 max-md:px-3 text-muted-foreground hover:text-foreground"
        onClick={handleAllowAll}
        disabled={submitting}
      >
        Allow all
      </Button>
    </div>
  );

  return (
    <div className="mx-4 mb-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2">
      {isMobile ? (
        <div className="flex flex-col gap-2 text-sm">
          <div className="flex items-start gap-2">
            <ShieldAlert className="h-4 w-4 shrink-0 text-warning mt-0.5" />
            <div className="min-w-0">
              <span className="font-medium">{approval.toolName}</span>{" "}
              <button
                type="button"
                onClick={() => setExpanded(!expanded)}
                className={`text-muted-foreground text-left ${expanded ? "whitespace-pre-wrap break-all" : "truncate block w-full"}`}
              >
                {summary}
              </button>
            </div>
          </div>
          {buttons}
        </div>
      ) : (
        <div className="flex items-center gap-2 text-sm">
          <ShieldAlert className="h-4 w-4 shrink-0 text-warning" />
          <span className="font-medium shrink-0">{approval.toolName}</span>
          <span className="text-muted-foreground truncate min-w-0">{summary}</span>
          <div className="ml-auto">{buttons}</div>
        </div>
      )}
    </div>
  );
}
