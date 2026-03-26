import {
  Check,
  Circle,
  CircleHelp,
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
  isPlanning?: boolean;
  gitOperation?: string;
}

export function SessionStatusBadge({
  state,
  connected = true,
  hasPendingApproval,
  isPlanning,
  gitOperation,
}: SessionStatusBadgeProps) {
  // Attention overrides
  if (hasPendingApproval) {
    const Icon = isPlanning ? ListChecks : CircleHelp;
    const title = isPlanning ? "Plan ready for review" : "Waiting for approval";
    return (
      <Badge bg="bg-[#ff9e64]/15" text="text-[#ff9e64]" pulse title={title}>
        <Icon className="size-3" />
      </Badge>
    );
  }
  if (isPlanning && state === "running") {
    return (
      <Badge bg="bg-[#73daca]/15" text="text-[#73daca]" title="Planning">
        <PenLine className="size-3 animate-pulse" />
      </Badge>
    );
  }

  // State icons — dimmed when process is disconnected
  const dim = !connected;

  switch (state) {
    case "running":
      return (
        <Badge bg="bg-[#73daca]/15" text="text-[#73daca]" title="Running" dim={dim}>
          <Loader className="size-3 animate-spin" />
        </Badge>
      );
    case "idle":
      return (
        <Badge
          bg="bg-[#9ece6a]/15"
          text="text-[#9ece6a]"
          title={connected ? "Idle" : "Idle (disconnected)"}
          dim={dim}
        >
          <Circle className="size-2 fill-current" />
        </Badge>
      );
    case "done":
      return (
        <Badge bg="bg-emerald-500/15" text="text-emerald-500" title="Done" dim={dim}>
          <Check className="size-3" />
        </Badge>
      );
    case "stopped":
      return (
        <Badge bg="bg-[#a9b1d6]/10" text="text-[#a9b1d6]/80" title="Stopped" dim={dim}>
          <Pause className="size-3" />
        </Badge>
      );
    case "failed":
      return (
        <Badge bg="bg-[#f7768e]/15" text="text-[#f7768e]" title="Failed" dim={dim}>
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
        <Badge bg="bg-[#7aa2f7]/15" text="text-[#7aa2f7]" title={opLabel} dim={dim}>
          <OpIcon className="size-3 animate-spin" />
        </Badge>
      );
    }
  }
}

function Badge({
  bg,
  text,
  pulse,
  dim,
  title,
  children,
}: {
  bg: string;
  text: string;
  pulse?: boolean;
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
        pulse && "animate-pulse",
        dim && "opacity-40",
      )}
      title={title}
    >
      {children}
    </span>
  );
}
