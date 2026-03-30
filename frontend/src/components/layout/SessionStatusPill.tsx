import {
  BellRing,
  Check,
  Circle,
  GitMerge,
  ListChecks,
  Loader,
  Pause,
  PenLine,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

interface SessionStatusPillProps {
  state: SessionState;
  connected?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
  gitOperation?: string;
}

interface PillConfig {
  icon: React.ElementType;
  label: string;
  bg: string;
  text: string;
  iconClass?: string;
  pulse?: boolean;
}

function getPillConfig({
  state,
  connected = true,
  hasPendingApproval,
  isPlanning,
  gitOperation,
}: SessionStatusPillProps): PillConfig {
  if (hasPendingApproval) {
    return {
      icon: isPlanning ? ListChecks : BellRing,
      label: isPlanning ? "Plan ready" : "Approval",
      bg: "bg-orange/15",
      text: "text-orange",
      pulse: true,
    };
  }
  if (isPlanning && state === "running") {
    return {
      icon: PenLine,
      label: "Planning",
      bg: "bg-teal/15",
      text: "text-teal",
      iconClass: "animate-pulse",
    };
  }

  switch (state) {
    case "running":
      return {
        icon: Loader,
        label: "Running",
        bg: "bg-teal/15",
        text: "text-teal",
        iconClass: "animate-spin",
      };
    case "idle":
      return {
        icon: Circle,
        label: connected ? "Idle" : "Disconnected",
        bg: "bg-success/15",
        text: "text-success",
      };
    case "done":
      return { icon: Check, label: "Done", bg: "bg-success/15", text: "text-success" };
    case "stopped":
      return { icon: Pause, label: "Stopped", bg: "bg-foreground/10", text: "text-foreground/80" };
    case "failed":
      return {
        icon: TriangleAlert,
        label: "Failed",
        bg: "bg-destructive/15",
        text: "text-destructive",
      };
    case "merging": {
      const label =
        gitOperation === "rebasing"
          ? "Rebasing"
          : gitOperation === "creating_pr"
            ? "Creating PR"
            : "Merging";
      const OpIcon =
        gitOperation === "rebasing" ? RefreshCw : gitOperation === "merging" ? GitMerge : Loader;
      return {
        icon: OpIcon,
        label,
        bg: "bg-primary/15",
        text: "text-primary",
        iconClass: "animate-spin",
      };
    }
  }
}

export function SessionStatusPill(props: SessionStatusPillProps) {
  const { icon: Icon, label, bg, text, iconClass, pulse } = getPillConfig(props);
  const dim = !props.hasPendingApproval && props.connected === false;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium shrink-0",
        bg,
        text,
        dim && "opacity-40",
      )}
    >
      {pulse ? (
        <span className="relative flex size-3 shrink-0">
          <span
            className={cn("absolute inset-0 rounded-full animate-pulse ring-1", "ring-current/30")}
          />
          <Icon className="size-3" />
        </span>
      ) : (
        <Icon className={cn("size-3 shrink-0", iconClass)} />
      )}
      {label}
    </span>
  );
}
