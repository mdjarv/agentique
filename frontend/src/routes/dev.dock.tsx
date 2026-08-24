/**
 * The sync dock in every state it can reach, in a real 288px rail.
 *
 * The live dock only shows the drift your checkouts happen to have, so the
 * meter's three tones, the bulk button's label variants, and the below-the-rule
 * exceptions can't be eyeballed without a fixture. This route seeds the app and
 * machine stores with hand-built projects and git statuses — nothing is
 * fetched, and the dock's own actions will fail here, which is fine: it exists
 * to be looked at.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { SyncDock } from "~/components/layout/git/SyncDock";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";
import { useUIStore } from "~/stores/ui-store";

export const Route = createFileRoute("/dev/dock")({
  component: DevDock,
});

function project(id: string, slug: string, color: string, machineId?: string): Project {
  return {
    id,
    name: slug,
    path: `/home/codeuser/git/${slug}`,
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    created_at: "",
    updated_at: "",
    slug,
    sort_order: 0,
    default_behavior_presets: "",
    favorite: 0,
    color,
    icon: "",
    folder: "",
    max_sessions: 0,
    pinned: 0,
    remote_url: `git@github.com:acme/${slug}.git`,
    machineId,
  };
}

interface Scenario {
  label: string;
  projects: Project[];
  status: Record<string, { ahead: number; behind: number; uncommitted?: number }>;
  /** Omit a project here to leave it unfetched (the "unverified" case). */
  fetchedMinutesAgo?: number;
}

const AGENTIQUE = project("p-ag", "agentique", "orange");
const AGENTKIT = project("p-ak", "agentkit", "green");
const WEBTICKET = project("p-wt", "webticket-ui", "teal");
const ALLTIX = project("p-ax", "alltix-api", "blue");
const REMOTE = project("p-rm", "agentique~zbook", "orange", "m-zbook");

const SCENARIOS: Scenario[] = [
  {
    label: "all clear",
    projects: [AGENTIQUE, AGENTKIT],
    status: { "p-ag": { ahead: 0, behind: 0 }, "p-ak": { ahead: 0, behind: 0 } },
    fetchedMinutesAgo: 0,
  },
  {
    label: "never fetched",
    projects: [AGENTIQUE],
    status: { "p-ag": { ahead: 0, behind: 0 } },
  },
  {
    label: "pushes only",
    projects: [AGENTIQUE, AGENTKIT],
    status: { "p-ag": { ahead: 12, behind: 0 }, "p-ak": { ahead: 9, behind: 0 } },
    fetchedMinutesAgo: 6,
  },
  {
    label: "one pull",
    projects: [WEBTICKET],
    status: { "p-wt": { ahead: 0, behind: 3 } },
    fetchedMinutesAgo: 6,
  },
  {
    label: "mixed — push, pull, diverged, away",
    projects: [AGENTIQUE, AGENTKIT, WEBTICKET, ALLTIX, REMOTE],
    status: {
      "p-ag": { ahead: 12, behind: 0 },
      "p-ak": { ahead: 9, behind: 0 },
      "p-wt": { ahead: 0, behind: 3 },
      "p-ax": { ahead: 2, behind: 3, uncommitted: 1 },
      "p-rm": { ahead: 4, behind: 0 },
    },
    fetchedMinutesAgo: 6,
  },
  {
    label: "nothing mechanical left",
    projects: [ALLTIX, REMOTE],
    status: { "p-ax": { ahead: 2, behind: 3 }, "p-rm": { ahead: 4, behind: 0 } },
    fetchedMinutesAgo: 1,
  },
];

function DevDock() {
  const [active, setActive] = useState(4);
  const scenario = SCENARIOS[active] ?? SCENARIOS[0];

  useEffect(() => {
    if (!scenario) return;
    // Set the list directly: `setProjects` preserves machine-tagged projects
    // (a primary refetch must not wipe remote entries), which would stack the
    // remote fixture row up on every re-run.
    useAppStore.setState({ projects: scenario.projects, projectsLoaded: true });
    const app = useAppStore.getState();
    for (const [projectId, s] of Object.entries(scenario.status)) {
      app.setProjectGitStatus({
        projectId,
        branch: "master",
        hasRemote: true,
        aheadRemote: s.ahead,
        behindRemote: s.behind,
        uncommittedCount: s.uncommitted ?? 0,
      });
      if (scenario.fetchedMinutesAgo !== undefined) {
        app.markProjectFetched(projectId, Date.now() - scenario.fetchedMinutesAgo * 60_000);
      }
    }
    useMachineStore.setState({
      machines: {
        "m-zbook": {
          id: "m-zbook",
          label: "zbook",
          icon: "",
          host: "",
          lastSeen: "",
        } as never,
      },
      statuses: { "m-zbook": "disconnected" },
    });
    useUIStore.getState().setSyncDockExpanded(true);
  }, [scenario]);

  return (
    <div className="flex min-h-screen gap-8 bg-background p-8">
      <div className="flex w-72 flex-col justify-end rounded-xl border border-sidebar-border bg-sidebar">
        <div className="flex-1" />
        <SyncDock />
        <div className="flex items-center gap-2 border-t border-sidebar-border px-3 py-2 text-[12.5px] text-muted-foreground">
          <span className="grid size-4 place-items-center rounded-full bg-secondary text-[8px]">
            M
          </span>
          Mathias Djärv
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <h1 className="font-mono text-[11px] uppercase tracking-widest text-muted-foreground">
          Sync dock states
        </h1>
        {SCENARIOS.map((s, i) => (
          <button
            key={s.label}
            type="button"
            onClick={() => setActive(i)}
            className={`cursor-pointer rounded-md px-3 py-1.5 text-left text-[13px] ${
              i === active
                ? "bg-sidebar-accent text-foreground-bright"
                : "text-muted-foreground hover:bg-sidebar-accent/50"
            }`}
          >
            {s.label}
          </button>
        ))}
        <p className="mt-2 max-w-sm font-mono text-[10.5px] leading-relaxed text-muted-foreground-faint">
          Hover the bulk button to preview its reach. Actions will fail here — there is no server
          behind this fixture.
        </p>
      </div>
    </div>
  );
}
