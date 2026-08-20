import { createFileRoute } from "@tanstack/react-router";
import { AgentsView } from "~/components/chat/AgentsView";
import type { AgentRun } from "~/lib/agent-runs";

export const Route = createFileRoute("/dev/agents")({
  component: DevAgents,
});

const runs: AgentRun[] = [
  {
    toolUseId: "tu_1",
    title: "t3code connection runtime research",
    agentType: "Explore",
    state: "done",
    preview:
      "I have a complete picture. Here is the research report. — # t3code multi-environment architecture **One sentence:** an environment is one machine running a t3 server, identified by a server-generated UUID persisted to its state dir.",
    totalTokens: 245_000,
    toolUses: 91,
    durationMs: 397_000,
  },
  {
    toolUseId: "tu_2",
    title: "t3code project/machine UX research",
    agentType: "Explore",
    state: "done",
    preview:
      "I have what I need. Here's the full report. — # t3code multi-machine (environment) UX — research report. Vocabulary first, because the UI leans on it everywhere: environment, project group, checkout.",
    totalTokens: 200_000,
    toolUses: 104,
    durationMs: 473_000,
  },
  {
    toolUseId: "tu_3",
    title: "Agentique multi-machine readiness map",
    agentType: "Explore",
    state: "done",
    preview:
      "Here's the map. — # 1. Client-server coupling today. **Same-origin is a hard assumption.** There is no base-URL concept anywhere in the frontend; every fetch is relative.",
    totalTokens: 143_000,
    toolUses: 76,
    durationMs: 327_000,
  },
  {
    toolUseId: "tu_4",
    title: "Sweep the event pipeline for leaks",
    agentType: "general-purpose",
    state: "running",
    lastToolName: "Grep",
    totalTokens: 38_000,
    toolUses: 12,
    durationMs: 0,
  },
  {
    toolUseId: "tu_5",
    title: "Port the reaper to FreeBSD",
    state: "failed",
    preview: "Could not determine process group ownership without /proc — aborting.",
    totalTokens: 9_400,
    toolUses: 5,
    durationMs: 41_000,
  },
];

function DevAgents() {
  return (
    <div className="h-dvh flex flex-col bg-background">
      <div className="h-12 shrink-0 border-b flex items-center px-4 text-sm text-muted-foreground">
        dev/agents — subagent roster
      </div>
      <div className="flex-1 flex min-h-0">
        <div className="w-[480px] border-r flex flex-col min-h-0">
          <AgentsView runs={runs} />
        </div>
        <div className="flex-1 flex flex-col min-h-0">
          <AgentsView runs={[]} />
        </div>
      </div>
    </div>
  );
}
