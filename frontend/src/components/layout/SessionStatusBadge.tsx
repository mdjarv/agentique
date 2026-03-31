import {
  BellRing,
  Check,
  ChevronsDownUp,
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

interface SessionStatusBadgeProps {
  state: SessionState;
  connected?: boolean;
  hasPendingApproval?: boolean;
  isCompacting?: boolean;
  isPlanning?: boolean;
  gitOperation?: string;
}

export function SessionStatusBadge({
  state,
  connected = true,
  hasPendingApproval,
  isCompacting,
  isPlanning,
  gitOperation,
}: SessionStatusBadgeProps) {
  // Attention overrides
  if (hasPendingApproval) {
    const Icon = isPlanning ? ListChecks : BellRing;
    const title = isPlanning ? "Plan ready for review" : "Waiting for approval";
    return (
      <span className="relative shrink-0">
        <span className="absolute inset-0 rounded-full ring-2 ring-orange/30 animate-pulse" />
        <Badge bg="bg-orange/15" text="text-orange" title={title}>
          <Icon className="size-3" />
        </Badge>
      </span>
    );
  }
  if (isCompacting) {
    return (
      <Badge bg="bg-primary/15" text="text-primary" title="Compacting context">
        <ChevronsDownUp className="size-3 animate-compact-squeeze" />
      </Badge>
    );
  }
  if (isPlanning && state === "running") {
    return (
      <Badge bg="bg-teal/15" text="text-teal" title="Planning">
        <PenLine className="size-3 animate-pulse" />
      </Badge>
    );
  }

  // State icons — dimmed when process is disconnected
  const dim = !connected;

  switch (state) {
    case "running":
      return (
        <Badge bg="bg-teal/15" text="text-teal" title="Running" dim={dim}>
          <Loader className="size-3 animate-spin" />
        </Badge>
      );
    case "idle":
      return (
        <Badge
          bg="bg-success/15"
          text="text-success"
          title={connected ? "Idle" : "Idle (disconnected)"}
          dim={dim}
        >
          <Circle className="size-2 fill-current" />
        </Badge>
      );
    case "done":
      return (
        <Badge bg="bg-success/15" text="text-success" title="Done" dim={dim}>
          <Check className="size-3" />
        </Badge>
      );
    case "stopped":
      return (
        <Badge bg="bg-foreground/10" text="text-foreground/80" title="Stopped" dim={dim}>
          <Pause className="size-3" />
        </Badge>
      );
    case "failed":
      return (
        <Badge bg="bg-destructive/15" text="text-destructive" title="Failed" dim={dim}>
          <TriangleAlert className="size-3" />
        </Badge>
      );
    case "merging": {
      const opLabel =
        gitOperation === "rebasing"
          ? "Rebasing"
          : gitOperation === "creating_pr"
            ? "Creating PR"
            : "Merging";
      const OpIcon =
        gitOperation === "rebasing" ? RefreshCw : gitOperation === "merging" ? GitMerge : Loader;
      return (
        <Badge bg="bg-primary/15" text="text-primary" title={opLabel} dim={dim}>
          <OpIcon className="size-3 animate-spin" />
        </Badge>
      );
    }
  }
}

function Badge({
  bg,
  text,
  dim,
  title,
  children,
}: {
  bg: string;
  text: string;
  dim?: boolean;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <span
      className={cn(
        "flex size-5 shrink-0 items-center justify-center rounded-full",
        bg,
        text,
        dim && "opacity-40",
      )}
      title={title}
    >
      {children}
    </span>
  );
}
