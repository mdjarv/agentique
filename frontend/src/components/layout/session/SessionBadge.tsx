import {
  Check,
  Circle,
  GitMerge,
  ListChecks,
  Loader,
  MessageSquare,
  Pause,
  PenLine,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import { cn } from "~/lib/utils";
import { StatusDot } from "../StatusDot";

// --- Types ---

export type BadgeState =
  | "idle"
  | "running"
  | "done"
  | "stopped"
  | "failed"
  | "merging"
  | "approval"
  | "question"
  | "plan"
  | "planning"
  | "unseen"
  | "channel_msg";

export type BadgeSize = "sm" | "md" | "lg";

export interface BadgeConfig {
  bg: string;
  text: string;
  label: string;
  pulseRing?: string;
}

// --- Config (single source of truth) ---

const CONFIG: Record<BadgeState, BadgeConfig> = {
  idle: { bg: "bg-success/15", text: "text-success", label: "Idle" },
  running: { bg: "bg-teal/15", text: "text-teal", label: "Running" },
  done: { bg: "bg-success/15", text: "text-success", label: "Done" },
  stopped: { bg: "bg-foreground/10", text: "text-foreground/80", label: "Stopped" },
  failed: { bg: "bg-destructive/15", text: "text-destructive", label: "Failed" },
  merging: { bg: "bg-primary/15", text: "text-primary", label: "Merging" },
  approval: {
    bg: "bg-orange/15",
    text: "text-orange",
    label: "Approval",
    pulseRing: "ring-orange/30",
  },
  question: {
    bg: "bg-orange/15",
    text: "text-orange",
    label: "Approval",
    pulseRing: "ring-orange/30",
  },
  plan: {
    bg: "bg-orange/15",
    text: "text-orange",
    label: "Plan ready",
    pulseRing: "ring-orange/30",
  },
  planning: { bg: "bg-teal/15", text: "text-teal", label: "Planning" },
  unseen: { bg: "bg-success/15", text: "text-success", label: "New response" },
  channel_msg: { bg: "bg-warning/15", text: "text-warning", label: "Channel message" },
};

export function getBadgeConfig(state: BadgeState): BadgeConfig {
  return CONFIG[state];
}

/** Map session props → BadgeState. Shared by SessionStatusBadge and SessionStatusPill. */
export function resolveSessionState(props: {
  state: string;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
}): BadgeState {
  if (props.hasPendingApproval) return props.isPlanning ? "plan" : "approval";
  if (props.isPlanning && props.state === "running") return "planning";
  return props.state as BadgeState;
}

/** Resolve status label for a session — handles disconnected and git operation overrides. */
export function resolveStatusLabel(opts: {
  state: string;
  badgeState: BadgeState;
  connected?: boolean;
  gitOperation?: string;
}): string {
  if (opts.state === "idle" && opts.connected === false) return "Disconnected";
  if (opts.state === "merging" && opts.gitOperation === "rebasing") return "Rebasing";
  if (opts.state === "merging" && opts.gitOperation === "creating_pr") return "Creating PR";
  return CONFIG[opts.badgeState].label;
}

// --- Icon sizing ---

const ICON_SIZE: Record<BadgeSize, string> = { sm: "size-2", md: "size-2.5", lg: "size-3" };
const BARE_ICON_SIZE: Record<BadgeSize, string> = { sm: "size-3", md: "size-4", lg: "size-5" };
const IDLE_SIZE: Record<BadgeSize, string> = { sm: "size-1.5", md: "size-2", lg: "size-2" };
const QUESTION_SIZE: Record<BadgeSize, string> = {
  sm: "text-[8px]",
  md: "text-[9px]",
  lg: "text-[10px]",
};
const BARE_QUESTION_SIZE: Record<BadgeSize, string> = {
  sm: "text-[10px]",
  md: "text-xs",
  lg: "text-sm",
};
const CONTAINER_SIZE: Record<BadgeSize, string> = { sm: "size-3", md: "size-4", lg: "size-5" };

// --- Icon ---

interface BadgeIconProps {
  state: BadgeState;
  size?: BadgeSize;
  bare?: boolean;
  gitOperation?: string;
}

export function BadgeIcon({ state, size = "lg", bare, gitOperation }: BadgeIconProps) {
  const iconCls = bare ? BARE_ICON_SIZE[size] : ICON_SIZE[size];

  switch (state) {
    case "idle":
      return (
        <Circle className={cn(bare ? BARE_ICON_SIZE[size] : IDLE_SIZE[size], "fill-current")} />
      );
    case "running":
      return <Loader className={cn(iconCls, "animate-spin")} />;
    case "done":
    case "unseen":
      return <Check className={iconCls} />;
    case "stopped":
      return <Pause className={iconCls} />;
    case "failed":
      return <TriangleAlert className={iconCls} />;
    case "merging": {
      const Icon =
        gitOperation === "rebasing" ? RefreshCw : gitOperation === "merging" ? GitMerge : Loader;
      return <Icon className={cn(iconCls, "animate-spin")} />;
    }
    case "approval":
    case "question":
      return (
        <span
          className={cn("font-extrabold", bare ? BARE_QUESTION_SIZE[size] : QUESTION_SIZE[size])}
        >
          ?
        </span>
      );
    case "plan":
      return <ListChecks className={iconCls} />;
    case "planning":
      return <PenLine className={cn(iconCls, "animate-pulse")} />;
    case "channel_msg":
      return <MessageSquare className={iconCls} />;
  }
}

// --- Badge ---

const WORKING_STATES = new Set<BadgeState>(["running", "merging", "planning"]);

interface SessionBadgeProps {
  state: BadgeState;
  size?: BadgeSize;
  bare?: boolean;
  dim?: boolean;
  ring?: boolean;
  pulse?: boolean;
  /** Use opaque background with white icon for high-contrast contexts. */
  solid?: boolean;
  /** Hex color to override default badge colors for working states. */
  accentColor?: string;
  gitOperation?: string;
  title?: string;
  className?: string;
}

export function SessionBadge({
  state,
  size = "lg",
  bare,
  dim,
  ring,
  pulse,
  solid,
  accentColor,
  gitOperation,
  title,
  className,
}: SessionBadgeProps) {
  const cfg = CONFIG[state];
  const pulseActive = pulse && !!cfg.pulseRing;
  const useAccent = !!accentColor && WORKING_STATES.has(state);

  // Solid mode: black bg with colored icon for high-contrast contexts (e.g. rail badges)
  const bg = solid ? "bg-black" : useAccent ? "" : cfg.bg;
  const text = useAccent ? "" : cfg.text;
  const accentStyle: React.CSSProperties | undefined = useAccent
    ? {
        backgroundColor: solid ? undefined : `${accentColor}26`,
        color: accentColor,
      }
    : undefined;

  const dot = (
    <StatusDot
      bg={bg}
      text={text}
      bare={bare}
      dim={dim}
      ring={ring}
      title={title ?? cfg.label}
      className={cn(CONTAINER_SIZE[size], pulseActive && bare && "animate-pulse", className)}
      style={accentStyle}
    >
      <BadgeIcon state={state} size={size} bare={bare} gitOperation={gitOperation} />
    </StatusDot>
  );

  if (pulseActive && !bare) {
    return (
      <span className="relative shrink-0">
        <span className={cn("absolute inset-0 rounded-full ring-2 animate-pulse", cfg.pulseRing)} />
        {dot}
      </span>
    );
  }

  return dot;
}
