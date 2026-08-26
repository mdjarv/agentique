import { Bot, ListTodo, X } from "lucide-react";
import { useMemo, useState } from "react";
import { AgentsView } from "~/components/chat/AgentsView";
import { DockSection } from "~/components/chat/dock/DockSection";
import { TodoItemRow } from "~/components/chat/TodoPanel";
import { WorkflowActivity } from "~/components/chat/WorkflowActivity";
import { useAgentRuns } from "~/hooks/useAgentRuns";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import { agentBadgeState } from "~/lib/agent-runs";
import { collectLatestWorkflow } from "~/lib/workflow-events";
import type { TodoItem } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface WorkViewProps {
  sessionId: string;
  todos: TodoItem[] | null;
  /** Scopes the roster: landed this turn shows, older folds away. */
  latestTurnIndex?: number;
  /** Turn whose agent failures the user has already seen — see `agentBadgeState`. */
  seenFailureTurn?: number;
}

/**
 * `Work` — the one grouped dock view: the plan for this turn, and who is out
 * working it. Both are true at once, which is why they are stacked sections
 * rather than two more tabs.
 *
 * A workflow is not a peer of either. Its agents ride its own progress events
 * rather than the `Agent` tool stream (`collectAgentRuns` skips
 * `local_workflow` deliberately), so the two cannot share a row type — but they
 * are the same subject at two altitudes, and they belong under one heading.
 */
export function WorkView({ sessionId, todos, latestTurnIndex, seenFailureTurn }: WorkViewProps) {
  const runs = useAgentRuns(sessionId);
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns);
  const streamingEvents = useChatStore((s) => s.sessions[sessionId]?.streamingEvents);
  const workflowEvents = useMemo(
    () => collectLatestWorkflow(turns, streamingEvents),
    [turns, streamingEvents],
  );
  const badge = useMemo(
    () => agentBadgeState(runs, latestTurnIndex, seenFailureTurn),
    [runs, latestTurnIndex, seenFailureTurn],
  );

  const hasTodos = !!todos && todos.length > 0;
  const hasAgents = runs.length > 0 || workflowEvents.length > 0;
  const [todosOpen, setTodosOpen] = useState(true);
  const [agentsOpen, setAgentsOpen] = useState(true);
  const [animateRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);

  if (!hasTodos && !hasAgents) {
    return (
      <div className="flex flex-1 items-center justify-center px-6 text-center text-muted-foreground-faint text-xs">
        Nothing in flight. Todos and subagents show up here as the turn runs.
      </div>
    );
  }

  const completed = todos?.filter((t) => t.status === "completed").length ?? 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col divide-y">
      {hasTodos && todos && (
        <DockSection
          icon={<ListTodo className="size-3.5" />}
          title="Todos"
          mark={
            <span className="text-[10px] text-muted-foreground tabular-nums">
              {completed}/{todos.length}
            </span>
          }
          open={todosOpen}
          onToggle={() => setTodosOpen((v) => !v)}
        >
          <div ref={animateRef} className="px-3 pb-2">
            {todos.map((item) => (
              <TodoItemRow key={`${item.status}-${item.content}`} item={item} />
            ))}
          </div>
        </DockSection>
      )}

      {hasAgents && (
        <DockSection
          icon={<Bot className="size-3.5" />}
          title="Agents"
          mark={<AgentsMark running={badge.running} failed={badge.failed} />}
          open={agentsOpen}
          onToggle={() => setAgentsOpen((v) => !v)}
          grow
        >
          <AgentsView
            runs={runs}
            latestTurnIndex={latestTurnIndex}
            workflow={
              workflowEvents.length > 0 ? (
                <WorkflowActivity taskEvents={workflowEvents} bare />
              ) : undefined
            }
          />
        </DockSection>
      )}
    </div>
  );
}

/** Same rule as the tab badge: state that can be acted on, never a lifetime tally. */
function AgentsMark({ running, failed }: { running: number; failed: number }) {
  if (running > 0) {
    return (
      <span className="flex items-center gap-1" title={`${running} still out`}>
        <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
        <span className="font-medium text-[10px] text-agent tabular-nums">{running}</span>
      </span>
    );
  }
  if (failed > 0) {
    return (
      <span className="flex items-center gap-0.5 text-destructive" title={`${failed} failed`}>
        <X className="size-3" />
        <span className="font-medium text-[10px] tabular-nums">{failed}</span>
      </span>
    );
  }
  return null;
}
