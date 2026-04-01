import { ArrowRight } from "lucide-react";
import { memo } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { getSessionIconComponent } from "~/lib/session-icons";

// Deterministic color palette for agent avatars.
export type AgentColor = {
  bg: string;
  text: string;
  border: string;
  from: string;
  to: string;
};

const DEFAULT_COLOR: AgentColor = {
  bg: "bg-amber-500/20",
  text: "text-amber-400",
  border: "border-amber-500/20",
  from: "from-amber-500/12",
  to: "to-amber-500/6",
};

export const AGENT_COLORS: AgentColor[] = [
  {
    bg: "bg-amber-500/20",
    text: "text-amber-400",
    border: "border-amber-500/20",
    from: "from-amber-500/12",
    to: "to-amber-500/6",
  },
  {
    bg: "bg-cyan-500/20",
    text: "text-cyan-400",
    border: "border-cyan-500/20",
    from: "from-cyan-500/12",
    to: "to-cyan-500/6",
  },
  {
    bg: "bg-rose-500/20",
    text: "text-rose-400",
    border: "border-rose-500/20",
    from: "from-rose-500/12",
    to: "to-rose-500/6",
  },
  {
    bg: "bg-emerald-500/20",
    text: "text-emerald-400",
    border: "border-emerald-500/20",
    from: "from-emerald-500/12",
    to: "to-emerald-500/6",
  },
  {
    bg: "bg-violet-500/20",
    text: "text-violet-400",
    border: "border-violet-500/20",
    from: "from-violet-500/12",
    to: "to-violet-500/6",
  },
  {
    bg: "bg-orange-500/20",
    text: "text-orange-400",
    border: "border-orange-500/20",
    from: "from-orange-500/12",
    to: "to-orange-500/6",
  },
];

function hashString(s: string): number {
  let hash = 0;
  for (let i = 0; i < s.length; i++) {
    hash = Math.imul(31, hash) + s.charCodeAt(i);
  }
  return Math.abs(hash);
}

export function getAgentColor(sessionId: string) {
  return AGENT_COLORS[hashString(sessionId) % AGENT_COLORS.length] ?? DEFAULT_COLOR;
}

interface AgentMessageProps {
  direction: "sent" | "received";
  senderName: string;
  senderSessionId: string;
  senderIcon?: string;
  targetName: string;
  targetSessionId: string;
  targetIcon?: string;
  content: string;
}

export const AgentMessage = memo(function AgentMessage({
  direction,
  senderName,
  senderSessionId,
  senderIcon,
  targetName,
  targetSessionId,
  targetIcon,
  content,
}: AgentMessageProps) {
  if (direction === "sent") {
    const color = getAgentColor(targetSessionId);
    const TargetIcon = getSessionIconComponent(targetIcon);
    return (
      <div className="flex gap-3 justify-end max-md:flex-col max-md:gap-1">
        <div className="flex-1 max-w-[85%] max-md:max-w-full min-w-0 flex flex-col items-end">
          <span className="text-[10px] font-medium text-muted-foreground/60 mb-0.5 flex items-center gap-1">
            <ArrowRight className="h-2.5 w-2.5" />
            {targetName}
          </span>
          <div className="rounded-lg px-4 py-2 bg-muted/30 border border-border/50 opacity-70">
            <Markdown content={content} />
          </div>
        </div>
        <Avatar className="h-8 w-8 shrink-0 max-md:h-6 max-md:w-6">
          <AvatarFallback className={`${color.bg} ${color.text}`}>
            <TargetIcon className="h-4 w-4 max-md:h-3 max-md:w-3" />
          </AvatarFallback>
        </Avatar>
      </div>
    );
  }

  const color = getAgentColor(senderSessionId);
  const SenderIcon = getSessionIconComponent(senderIcon);
  return (
    <div className="flex gap-3 max-md:flex-col max-md:gap-1">
      <Avatar className="h-8 w-8 shrink-0 max-md:h-6 max-md:w-6">
        <AvatarFallback className={`${color.bg} ${color.text}`}>
          <SenderIcon className="h-4 w-4 max-md:h-3 max-md:w-3" />
        </AvatarFallback>
      </Avatar>
      <div className="flex-1 max-w-[85%] max-md:max-w-full min-w-0">
        <span className={`text-[10px] font-medium ${color.text} mb-0.5 block`}>{senderName}</span>
        <div
          className={`rounded-lg px-4 py-2 bg-gradient-to-br ${color.from} ${color.to} shadow-lg shadow-black/30 border ${color.border}`}
        >
          <Markdown content={content} />
        </div>
      </div>
    </div>
  );
});
