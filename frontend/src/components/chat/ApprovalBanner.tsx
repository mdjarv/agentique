import { Check, ShieldAlert, X } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval, setAutoApproveMode } from "~/lib/session-actions";
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

interface FormattedInput {
  summary: string;
  description?: string;
}

function formatInput(
  toolName: string,
  input: unknown,
  projectPath?: string,
  worktreePath?: string,
): FormattedInput {
  if (typeof input === "string") return { summary: input };
  if (!input || typeof input !== "object") return { summary: JSON.stringify(input) };

  const obj = input as Record<string, unknown>;
  const strip = (p: string) => stripPrefix(p, projectPath, worktreePath);

  switch (toolName) {
    case "Read":
    case "Write":
    case "Edit":
      return { summary: strip(String(obj.file_path ?? "")) };
    case "Glob":
      return { summary: String(obj.pattern ?? "") };
    case "Grep":
      return { summary: `${obj.pattern ?? ""}${obj.path ? ` in ${strip(String(obj.path))}` : ""}` };
    case "Bash":
      return {
        summary: String(obj.command ?? obj.description ?? ""),
        description: obj.command && obj.description ? String(obj.description) : undefined,
      };
    case "Agent":
      return { summary: String(obj.description ?? obj.prompt ?? "").slice(0, 120) };
    case "WebFetch":
    case "WebSearch":
      return { summary: String(obj.url ?? obj.query ?? "") };
    case "EnterPlanMode":
      return { summary: "Agent wants to enter plan mode" };
    case "ExitPlanMode":
      return { summary: "Agent wants to exit plan mode" };
    default: {
      const desc =
        obj.description ?? obj.command ?? obj.file_path ?? obj.path ?? obj.pattern ?? obj.prompt;
      if (desc) return { summary: strip(String(desc)).slice(0, 120) };
      const keys = Object.keys(obj).filter((k) => k !== "type");
      return { summary: keys.length > 0 ? `(${keys.join(", ")})` : toolName };
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
  const { summary, description } = formatInput(
    approval.toolName,
    approval.input,
    projectPath,
    worktreePath,
  );

  const handleAllow = useCallback(() => {
    setSubmitting(true);
    resolveApproval(ws, sessionId, approval.approvalId, true).catch((err) => {
      setSubmitting(false);
      toast.error(getErrorMessage(err, "Failed to approve tool"));
    });
  }, [ws, sessionId, approval.approvalId]);

  const handleAllowAll = useCallback(() => {
    setSubmitting(true);
    setAutoApproveMode(ws, sessionId, "auto")
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
        size="xs"
        variant="ghost"
        className="text-destructive hover:text-destructive hover:bg-destructive/10 max-md:h-10 max-md:px-3"
        onClick={handleDeny}
        disabled={submitting}
      >
        <X className="h-3.5 w-3.5 mr-1" />
        Deny
      </Button>
      <Button
        size="xs"
        className="bg-success hover:bg-success/90 text-background max-md:h-10 max-md:px-3"
        onClick={handleAllow}
        disabled={submitting}
      >
        <Check className="h-3.5 w-3.5 mr-1" />
        Allow
      </Button>
      <Button
        size="xs"
        variant="outline"
        className="max-md:h-10 max-md:px-3"
        onClick={handleAllowAll}
        disabled={submitting}
      >
        Allow all
      </Button>
    </div>
  );

  return (
    <div className="mx-4 mb-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2.5 shrink-0">
      {isMobile ? (
        <div className="flex flex-col gap-2 text-sm">
          <div className="flex items-start gap-2">
            <ShieldAlert className="h-4 w-4 shrink-0 text-warning mt-0.5" />
            <div className="min-w-0">
              <span className="font-mono text-xs font-medium bg-warning/15 text-warning px-1.5 py-0.5 rounded">
                {approval.toolName}
              </span>
              {description && (
                <span className="ml-2 text-muted-foreground text-xs">{description}</span>
              )}
              <button
                type="button"
                onClick={() => setExpanded(!expanded)}
                className={`font-mono text-foreground/70 text-left mt-1 ${expanded ? "whitespace-pre-wrap break-all" : "truncate block w-full"}`}
              >
                {summary}
              </button>
            </div>
          </div>
          {buttons}
        </div>
      ) : (
        <div className="flex flex-col gap-1.5 text-sm">
          <div className="flex items-center gap-2">
            <ShieldAlert className="h-4 w-4 shrink-0 text-warning" />
            <span className="font-mono text-xs font-medium bg-warning/15 text-warning px-1.5 py-0.5 rounded shrink-0">
              {approval.toolName}
            </span>
            {description && (
              <span className="text-muted-foreground text-xs shrink-0">{description}</span>
            )}
            <div className="ml-auto">{buttons}</div>
          </div>
          <div className="font-mono text-xs text-foreground/70 pl-6 break-all">
            {summary}
          </div>
        </div>
      )}
    </div>
  );
}
