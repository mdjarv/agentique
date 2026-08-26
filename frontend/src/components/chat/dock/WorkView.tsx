import { useMemo } from "react";
import { WorkSections } from "~/components/chat/dock/WorkSections";
import { useAgentRuns } from "~/hooks/useAgentRuns";
import { agentBadgeState } from "~/lib/agent-runs";
import { collectLatestWorkflow } from "~/lib/workflow-events";
import type { TodoItem } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface WorkViewProps {
  sessionId: string;
  todos: TodoItem[] | null;
  /** Scopes the roster: landed this turn shows, older folds away. */
  latestTurnIndex?: number;
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
 *
 * This reads the store; `WorkSections` renders. The fold happens in `useMemo`
 * over referentially-stable `turns`/`streamingEvents`, never inside a selector.
 */
export function WorkView({ sessionId, todos, latestTurnIndex }: WorkViewProps) {
  const runs = useAgentRuns(sessionId);
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns);
  const streamingEvents = useChatStore((s) => s.sessions[sessionId]?.streamingEvents);
  const workflowEvents = useMemo(
    () => collectLatestWorkflow(turns, streamingEvents),
    [turns, streamingEvents],
  );
  const badge = useMemo(() => agentBadgeState(runs), [runs]);

  return (
    <WorkSections
      todos={todos}
      runs={runs}
      workflowEvents={workflowEvents}
      badge={badge}
      latestTurnIndex={latestTurnIndex}
    />
  );
}
