import { CheckCircle, Loader2, Pause, Play, TriangleAlert } from "lucide-react";
import { Button } from "~/components/ui/button";

interface ResumeBannerProps {
  state: "stopped" | "failed" | "done";
  onResume: () => void;
  resuming: boolean;
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

export function ResumeBanner({ state, onResume, resuming }: ResumeBannerProps) {
  const c = config[state];
  const Icon = c.icon;

  return (
    <div className={`mx-4 mb-2 rounded-md border ${c.border} ${c.bg} px-3 py-2 shrink-0`}>
      <div className="flex items-center gap-2 text-sm">
        <Icon className={`h-4 w-4 shrink-0 ${c.iconColor}`} />
        <span className="text-muted-foreground">{c.label}</span>
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
          {resuming ? "Resuming..." : c.button}
        </Button>
      </div>
    </div>
  );
}
