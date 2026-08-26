import { Bot, ListTodo } from "lucide-react";
import { useState } from "react";
import { AgentsView } from "~/components/chat/AgentsView";
import { DockSection } from "~/components/chat/dock/DockSection";
import { TodoItemRow } from "~/components/chat/TodoPanel";
import { WorkflowActivity } from "~/components/chat/WorkflowActivity";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import type { AgentBadgeState, AgentRun } from "~/lib/agent-runs";
import type { TodoItem } from "~/stores/chat-store";
import type { TaskEvent } from "~/stores/chat-types";

export interface WorkSectionsProps {
  todos: TodoItem[] | null;
  runs: AgentRun[];
  /** The latest workflow's task events, or `[]` when the session has run none. */
  workflowEvents: TaskEvent[];
  badge: AgentBadgeState;
  /** Scopes the roster: landed this turn shows, older folds away. */
  latestTurnIndex?: number;
}

/**
 * The stacked body of the `Work` dock view — everything it renders, none of
 * where the data came from. Split from `WorkView` so the layout can be
 * exercised from fixtures (`/dev/agents`) without a live session behind it.
 */
export function WorkSections({
  todos,
  runs,
  workflowEvents,
  badge,
  latestTurnIndex,
}: WorkSectionsProps) {
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
          mark={<AgentsMark running={badge.running} />}
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

/**
 * Same rule as the tab badge: agents still out, and nothing else. A failed
 * subagent is the session's to handle, not the operator's — see
 * `agentBadgeState`.
 */
function AgentsMark({ running }: { running: number }) {
  if (running === 0) return null;
  return (
    <span className="flex items-center gap-1" title={`${running} still out`}>
      <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
      <span className="font-medium text-[10px] text-agent tabular-nums">{running}</span>
    </span>
  );
}
