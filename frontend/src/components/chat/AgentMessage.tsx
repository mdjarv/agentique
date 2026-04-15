import { ArrowRight } from "lucide-react";
import { memo } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { useTheme } from "~/hooks/useTheme";
import { getSessionIconComponent } from "~/lib/session/icons";

// Deterministic color palette for agent avatars.
export type AgentColor = {
  bg: string;
  text: string;
  border: string;
  from: string;
  to: string;
};

const COLORS_DARK: AgentColor[] = [
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

const COLORS_LIGHT: AgentColor[] = [
  {
    bg: "bg-amber-500/15",
    text: "text-amber-700",
    border: "border-amber-500/25",
    from: "from-amber-500/10",
    to: "to-amber-500/5",
  },
  {
    bg: "bg-cyan-500/15",
    text: "text-cyan-700",
    border: "border-cyan-500/25",
    from: "from-cyan-500/10",
    to: "to-cyan-500/5",
  },
  {
    bg: "bg-rose-500/15",
    text: "text-rose-700",
    border: "border-rose-500/25",
    from: "from-rose-500/10",
    to: "to-rose-500/5",
  },
  {
    bg: "bg-emerald-500/15",
    text: "text-emerald-700",
    border: "border-emerald-500/25",
    from: "from-emerald-500/10",
    to: "to-emerald-500/5",
  },
  {
    bg: "bg-violet-500/15",
    text: "text-violet-700",
    border: "border-violet-500/25",
    from: "from-violet-500/10",
    to: "to-violet-500/5",
  },
  {
    bg: "bg-orange-500/15",
    text: "text-orange-700",
    border: "border-orange-500/25",
    from: "from-orange-500/10",
    to: "to-orange-500/5",
  },
];

const DEFAULT_DARK: AgentColor = COLORS_DARK[0] as AgentColor;
const DEFAULT_LIGHT: AgentColor = COLORS_LIGHT[0] as AgentColor;

function hashString(s: string): number {
  let hash = 0;
  for (let i = 0; i < s.length; i++) {
    hash = Math.imul(31, hash) + s.charCodeAt(i);
  }
  return Math.abs(hash);
}

export function getAgentColors(resolvedTheme: "light" | "dark"): AgentColor[] {
  return resolvedTheme === "dark" ? COLORS_DARK : COLORS_LIGHT;
}

export function getAgentColor(sessionId: string, resolvedTheme: "light" | "dark"): AgentColor {
  const palette = getAgentColors(resolvedTheme);
  const fallback = resolvedTheme === "dark" ? DEFAULT_DARK : DEFAULT_LIGHT;
  return palette[hashString(sessionId) % palette.length] ?? fallback;
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
  messageType?: "plan" | "progress" | "done" | "message";
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
  messageType,
}: AgentMessageProps) {
  const { resolvedTheme } = useTheme();

  if (direction === "sent") {
    const color = getAgentColor(targetSessionId, resolvedTheme);
    const TargetIcon = getSessionIconComponent(targetIcon);
    return (
      <div className="flex gap-3 justify-end max-md:gap-2">
        <div className="flex-1 max-w-[85%] max-md:max-w-full min-w-0 flex flex-col items-end">
          <span className="text-[10px] font-medium text-muted-foreground-faint mb-0.5 flex items-center gap-1">
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

  const color = getAgentColor(senderSessionId, resolvedTheme);
  const SenderIcon = getSessionIconComponent(senderIcon);
  return (
    <div className="flex gap-3 max-md:gap-2">
      <Avatar className="h-8 w-8 shrink-0 max-md:h-6 max-md:w-6">
        <AvatarFallback className={`${color.bg} ${color.text}`}>
          <SenderIcon className="h-4 w-4 max-md:h-3 max-md:w-3" />
        </AvatarFallback>
      </Avatar>
      <div className="flex-1 max-w-[85%] max-md:max-w-full min-w-0">
        <span className={`text-[10px] font-medium ${color.text} mb-0.5 flex items-center gap-1.5`}>
          {senderName}
          {messageType && messageType !== "message" && (
            <span className="text-[9px] uppercase tracking-wide opacity-60 bg-foreground/5 px-1 py-px rounded">
              {messageType}
            </span>
          )}
        </span>
        <div
          className={`rounded-lg px-4 py-2 bg-gradient-to-br ${color.from} ${color.to} shadow-lg shadow-black/30 border ${color.border}`}
        >
          <Markdown content={content} />
        </div>
      </div>
    </div>
  );
});
