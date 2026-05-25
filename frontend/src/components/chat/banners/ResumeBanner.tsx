import { CheckCircle, Loader2, Pause, Play, TriangleAlert } from "lucide-react";
import { Button } from "~/components/ui/button";

interface ResumeBannerProps {
  state: "stopped" | "failed" | "done";
  onResume: () => void;
  resuming: boolean;
  branchMissing?: boolean;
  /** When the provider cannot resume, the resume action starts a fresh
   *  session that loses prior conversation context. We surface that in the
   *  label + button copy so the user isn't surprised. */
  resumeUnsupported?: boolean;
}

const config = {
  stopped: {
    icon: Pause,
    label: "Session interrupted",
    button: "Resume",
    border: "border-muted-foreground/30",
    bg: "bg-muted/50",
    iconColor: "text-muted-foreground",
  },
  failed: {
    icon: TriangleAlert,
    label: "Session failed",
    button: "Resume",
    border: "border-destructive/30",
    bg: "bg-destructive/5",
    iconColor: "text-destructive",
  },
  done: {
    icon: CheckCircle,
    label: "Session complete",
    button: "Continue",
    border: "border-success/30",
    bg: "bg-success/5",
    iconColor: "text-success",
  },
} as const;

export function ResumeBanner({
  state,
  onResume,
  resuming,
  branchMissing,
  resumeUnsupported,
}: ResumeBannerProps) {
  const c = config[state];
  const Icon = branchMissing || resumeUnsupported ? TriangleAlert : c.icon;
  const label = branchMissing
    ? "Branch deleted — will resume on fresh worktree"
    : resumeUnsupported
      ? "Provider can't resume — will start a fresh turn"
      : c.label;
  const button = branchMissing
    ? "Resume on latest"
    : resumeUnsupported
      ? "Reconnect (fresh)"
      : c.button;

  const warning = branchMissing || resumeUnsupported;
  return (
    <div className={`mx-4 mb-2 rounded-md border ${c.border} ${c.bg} px-3 py-2 shrink-0`}>
      <div className="flex items-center gap-2 text-sm">
        <Icon className={`h-4 w-4 shrink-0 ${warning ? "text-warning" : c.iconColor}`} />
        <span className="text-muted-foreground">{label}</span>
        <Button
          size="sm"
          className="h-7 max-md:h-10 px-2 max-md:px-3 ml-auto"
          onClick={onResume}
          disabled={resuming}
        >
          {resuming ? (
            <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />
          ) : (
            <Play className="h-3.5 w-3.5 mr-1" />
          )}
          {resuming ? "Resuming..." : button}
        </Button>
      </div>
    </div>
  );
}
