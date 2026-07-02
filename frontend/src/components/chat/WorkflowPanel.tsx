import { Workflow as WorkflowIcon, X } from "lucide-react";
import { useMemo } from "react";
import { WorkflowActivity } from "~/components/chat/WorkflowActivity";
import { Button } from "~/components/ui/button";
import { collectLatestWorkflow } from "~/lib/workflow-events";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

/**
 * Right-panel view of the session's most-recent dynamic workflow — its phases
 * and per-agent progress, live. Sources the workflow's task events from the chat
 * store (referentially-stable `turns` / `streamingEvents`, folded once via
 * `useMemo` — never inside the selector). Empty state until a workflow runs.
 */
export function WorkflowPanel({ sessionId }: { sessionId: string }) {
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns);
  const streamingEvents = useChatStore((s) => s.sessions[sessionId]?.streamingEvents);
  const setCollapsed = useUIStore((s) => s.setRightPanelCollapsed);

  const events = useMemo(
    () => collectLatestWorkflow(turns, streamingEvents),
    [turns, streamingEvents],
  );

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div className="h-12 border-b flex items-center gap-2 px-3 shrink-0">
        <WorkflowIcon className="h-4 w-4 text-agent/80 shrink-0" />
        <span className="text-sm font-medium">Workflow</span>
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto h-7 w-7"
          onClick={() => setCollapsed(true)}
          aria-label="Close workflow panel"
        >
          <X className="h-4 w-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto p-2">
        {events.length > 0 ? (
          <WorkflowActivity taskEvents={events} bare />
        ) : (
          <div className="flex h-full items-center justify-center px-6 text-center text-xs text-muted-foreground-faint">
            No workflow in this session yet. Run one (e.g. “ultracode …” or a /workflow command) and
            its phases &amp; agents show here live.
          </div>
        )}
      </div>
    </div>
  );
}
