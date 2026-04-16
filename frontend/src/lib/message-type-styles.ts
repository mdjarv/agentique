import { AlertCircle, Check, GitCommitHorizontal, MapIcon } from "lucide-react";
import type { ComponentType } from "react";
import type { AgentMessageType } from "~/lib/channel-actions";

export interface MessageTypeStyle {
  border: string;
  badge: string;
  icon?: ComponentType<{ className?: string }>;
}

export const MESSAGE_TYPE_STYLES: Record<string, MessageTypeStyle> = {
  plan: {
    border: "border-l-2 border-l-blue-400/40",
    badge: "text-blue-500/70 bg-blue-500/10",
    icon: MapIcon,
  },
  progress: { border: "", badge: "text-muted-foreground/60 bg-foreground/5" },
  done: {
    border: "border-l-2 border-l-emerald-400/50",
    badge: "text-emerald-600/70 bg-emerald-500/10",
    icon: Check,
  },
  clarification: {
    border: "border-l-2 border-l-amber-400/50",
    badge: "text-amber-600/70 bg-amber-500/10",
    icon: AlertCircle,
  },
  introduction: {
    border: "border-l-2 border-l-muted-foreground/20",
    badge: "text-muted-foreground/50 bg-muted-foreground/10",
  },
};

const DEFAULT_STYLE: MessageTypeStyle = {
  border: "",
  badge: "text-muted-foreground/60 bg-foreground/5",
};

export function getMessageTypeStyle(
  messageType?: AgentMessageType,
  hasCommits?: boolean,
): MessageTypeStyle {
  if (!messageType || messageType === "message") return DEFAULT_STYLE;
  const style = MESSAGE_TYPE_STYLES[messageType] ?? DEFAULT_STYLE;
  if (messageType === "progress" && hasCommits) {
    return { ...style, icon: GitCommitHorizontal };
  }
  return style;
}
