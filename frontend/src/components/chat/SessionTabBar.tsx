import {
  ArrowDown,
  ArrowUp,
  Bot,
  Circle,
  Clock,
  FileDiff,
  ListTodo,
  MessageSquare,
  TriangleAlert,
  X,
} from "lucide-react";
import type { LoopAttentionKind, LoopBadgeState } from "~/lib/loop-attention";
import { cn } from "~/lib/utils";
import type { SessionTab } from "./ChatPanel";

interface SessionTabBarProps {
  activeTab: SessionTab;
  onTabChange: (tab: SessionTab) => void;
  hasTodos: boolean;
  todosCompleted?: number;
  todosTotal?: number;
  hasGitContent: boolean;
  ahead?: number;
  behind?: number;
  uncommittedCount?: number;
  hasChanges: boolean;
  totalAdd?: number;
  totalDel?: number;
  /** Session has schedules — shows the Loops tab. */
  hasLoops?: boolean;
  /** What this session's loops need from the user — see `loopBadgeState`. */
  loopsAttention?: LoopBadgeState | null;
  /** Session has spawned at least one subagent — shows the Agents tab. */
  hasAgents?: boolean;
  /** Subagents still out. The only count the badge shows. */
  agentsRunning?: number;
  /** Failures still worth raising — see `agentBadgeState`. */
  agentsFailed?: number;
  /** Project accent color hex — used for active tab indicator. */
  accentColor?: string;
}

/**
 * Glyphs follow the sidebar's vocabulary (`ThreadRow`) so one state reads the
 * same wherever it appears: a triangle means someone is waiting on you, an X
 * means it stopped. Nothing here pulses — in this strip a pulse already means
 * "live activity" (agents out), and one mark cannot mean two things.
 */
const LOOP_ATTENTION: Record<
  LoopAttentionKind,
  { glyph: typeof Clock; tone: string; label: (n: number) => string }
> = {
  blocked: {
    glyph: TriangleAlert,
    tone: "text-warning",
    label: (n) => `${n} loop${n === 1 ? " is" : "s are"} waiting on you`,
  },
  paused: {
    glyph: X,
    tone: "text-destructive",
    label: (n) => `${n} loop${n === 1 ? "" : "s"} paused after repeated failures`,
  },
};

function LoopAttentionBadge({ attention }: { attention: LoopBadgeState }) {
  const { glyph: Glyph, tone, label } = LOOP_ATTENTION[attention.kind];
  return (
    <span className={cn("flex items-center gap-1", tone)} title={label(attention.count)}>
      <Glyph className="size-3" />
      <span className="font-medium text-xs tabular-nums">{attention.count}</span>
    </span>
  );
}

export function SessionTabBar({
  activeTab,
  onTabChange,
  hasTodos,
  todosCompleted = 0,
  todosTotal = 0,
  hasGitContent,
  ahead = 0,
  behind = 0,
  uncommittedCount = 0,
  hasChanges,
  totalAdd = 0,
  totalDel = 0,
  hasLoops = false,
  loopsAttention = null,
  hasAgents = false,
  agentsRunning = 0,
  agentsFailed = 0,
  accentColor,
}: SessionTabBarProps) {
  const showChangesTab = hasGitContent || hasChanges;
  const color = accentColor || "var(--primary)";

  function Tab({ tab, children }: { tab: SessionTab; children: React.ReactNode }) {
    const active = activeTab === tab;
    return (
      <button
        type="button"
        onClick={() => onTabChange(tab)}
        className={cn(
          "relative flex items-center gap-1.5 px-4 mt-1.5 rounded-t-md text-sm transition-colors cursor-pointer self-stretch",
          active ? "font-medium" : "text-muted-foreground hover:text-foreground hover:bg-muted/30",
        )}
        style={
          active
            ? {
                backgroundColor: `${color}18`,
                color: `color-mix(in srgb, ${color}, var(--foreground) 40%)`,
                boxShadow: `inset 0 -2px 0 ${color}`,
              }
            : undefined
        }
      >
        {children}
      </button>
    );
  }

  return (
    <>
      <Tab tab="chat">
        <MessageSquare className="size-3.5" />
        Chat
      </Tab>

      {hasTodos && (
        <Tab tab="todos">
          <ListTodo className="size-3.5" />
          Todos
          <span className="text-xs text-muted-foreground tabular-nums">
            {todosCompleted}/{todosTotal}
          </span>
        </Tab>
      )}

      {showChangesTab && (
        <Tab tab="changes">
          <FileDiff className="size-3.5" />
          Changes
          {(ahead > 0 || behind > 0 || uncommittedCount > 0 || totalAdd > 0 || totalDel > 0) && (
            <span className="flex items-center gap-1 ml-0.5 text-xs">
              {ahead > 0 && (
                <span className="flex items-center gap-0.5 text-success">
                  <ArrowUp className="size-2.5" />
                  {ahead}
                </span>
              )}
              {behind > 0 && (
                <span className="flex items-center gap-0.5 text-orange">
                  <ArrowDown className="size-2.5" />
                  {behind}
                </span>
              )}
              {uncommittedCount > 0 && (
                <span className="flex items-center gap-0.5 text-warning">
                  <Circle className="size-1.5 fill-current" />
                  {uncommittedCount}
                </span>
              )}
              {totalAdd > 0 && <span className="text-success hidden sm:inline">+{totalAdd}</span>}
              {totalDel > 0 && (
                <span className="text-destructive hidden sm:inline">-{totalDel}</span>
              )}
            </span>
          )}
        </Tab>
      )}

      {hasAgents && (
        <Tab tab="agents">
          <Bot className="size-3.5" />
          Agents
          {/* State, never a lifetime count: agents out, or failures from this
              turn you have not looked at. Neither, and the tab says nothing —
              a badge that only ever counts up trains you to stop reading it. */}
          {agentsRunning > 0 ? (
            <span
              className="flex items-center gap-1.5"
              title={`${agentsRunning} agent${agentsRunning === 1 ? "" : "s"} still running`}
            >
              <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
              <span className="font-medium text-agent text-xs tabular-nums">{agentsRunning}</span>
            </span>
          ) : agentsFailed > 0 ? (
            <span
              className="flex items-center gap-1 text-destructive"
              title={`${agentsFailed} agent${agentsFailed === 1 ? "" : "s"} failed this turn`}
            >
              {/* X, not a warning triangle: the sidebar (`ThreadRow`) already
                  spends X on "it failed" and the triangle on "someone is
                  waiting on you", and one glyph must mean one thing. */}
              <X className="size-3" />
              <span className="font-medium text-xs tabular-nums">{agentsFailed}</span>
            </span>
          ) : null}
        </Tab>
      )}

      {hasLoops && (
        <Tab tab="loops">
          <Clock className="size-3.5" />
          Loops
          {/* Same rule as Agents, the other way round: this tab was silent
              about a state the store already tracks. A paused loop is dead
              until someone acts, so it must say so from whatever tab you are
              on — not only from inside itself. */}
          {loopsAttention && <LoopAttentionBadge attention={loopsAttention} />}
        </Tab>
      )}
    </>
  );
}
