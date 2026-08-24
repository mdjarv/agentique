import { createFileRoute } from "@tanstack/react-router";
import { AgentFlightStrip } from "~/components/chat/AgentFlightStrip";
import { AgentsView } from "~/components/chat/AgentsView";
import { SessionTabBar } from "~/components/chat/SessionTabBar";
import type { AgentRun } from "~/lib/agent-runs";

export const Route = createFileRoute("/dev/agents")({
  component: DevAgents,
});

const now = Date.now();

const landed: AgentRun[] = [
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
    toolUseId: "tu_5",
    title: "Port the reaper to FreeBSD",
    state: "failed",
    preview: "Could not determine process group ownership without /proc — aborting.",
    totalTokens: 9_400,
    toolUses: 5,
    durationMs: 41_000,
  },
];

const inFlight: AgentRun[] = [
  {
    toolUseId: "tu_4",
    title: "Trace pin release on archive",
    agentType: "Explore",
    state: "running",
    lastToolName: "Grep",
    totalTokens: 38_000,
    toolUses: 18,
    durationMs: 0,
    startedAt: now - 72_000,
  },
  {
    toolUseId: "tu_6",
    title: "Audit wire-compat aliases",
    agentType: "general-purpose",
    state: "running",
    lastToolName: "Read",
    totalTokens: 21_000,
    toolUses: 11,
    durationMs: 0,
    startedAt: now - 58_000,
  },
  {
    toolUseId: "tu_7",
    title: "Check machines cache version",
    state: "running",
    lastToolName: "Bash",
    totalTokens: 2_100,
    toolUses: 2,
    durationMs: 0,
    startedAt: now - 6_000,
  },
];

const runs = [...landed, ...inFlight];

function Panel({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    // min-w-0: grid items default to min-width:auto and would otherwise be
    // widened by their own content instead of scrolling inside it.
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="font-medium text-[10px] text-muted-foreground-faint uppercase tracking-[0.12em]">
        {label}
      </span>
      <div className="min-w-0 overflow-hidden rounded-lg border bg-background">{children}</div>
    </div>
  );
}

function TabBarRow(props: { hasAgents: boolean; agentsRunning: number; agentsFailed: number }) {
  return (
    <div className="flex items-stretch h-10 px-2">
      <SessionTabBar
        activeTab="chat"
        onTabChange={() => {}}
        hasTodos={false}
        hasGitContent={false}
        hasChanges={false}
        {...props}
      />
    </div>
  );
}

function DevAgents() {
  return (
    <div className="h-dvh overflow-y-auto bg-background p-6">
      <div className="mx-auto flex max-w-6xl flex-col gap-8">
        <h1 className="text-sm text-muted-foreground">
          dev/agents — badge states, flight strip densities, roster
        </h1>

        <div className="grid gap-4 md:grid-cols-3">
          <Panel label="Badge — 3 running">
            <TabBarRow hasAgents agentsRunning={3} agentsFailed={0} />
          </Panel>
          <Panel label="Badge — 1 failed this turn">
            <TabBarRow hasAgents agentsRunning={0} agentsFailed={1} />
          </Panel>
          <Panel label="Badge — idle (silent)">
            <TabBarRow hasAgents agentsRunning={0} agentsFailed={0} />
          </Panel>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <Panel label="Strip — rail (desktop, follows you across tabs)">
            <AgentFlightStrip inFlight={inFlight} density="rail" />
          </Panel>
          <Panel label="Strip — line collapsed (mobile)">
            <AgentFlightStrip inFlight={inFlight} density="line" expanded={false} />
          </Panel>
          <Panel label="Strip — line expanded (mobile, tapped)">
            <AgentFlightStrip inFlight={inFlight} density="line" expanded={true} />
          </Panel>
          <Panel label="Strip — board (top of the Agents tab)">
            <AgentFlightStrip inFlight={inFlight} density="board" />
          </Panel>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <Panel label="Roster — in flight + landed">
            <div className="flex h-[26rem] flex-col">
              <AgentsView runs={runs} />
            </div>
          </Panel>
          <Panel label="Roster — all landed / empty">
            <div className="flex h-[26rem] flex-col">
              <AgentsView runs={landed} />
            </div>
            <div className="flex h-24 flex-col border-t">
              <AgentsView runs={[]} />
            </div>
          </Panel>
        </div>
      </div>
    </div>
  );
}
