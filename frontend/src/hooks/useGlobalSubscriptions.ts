import { useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { useSessionSubscriptions } from "~/hooks/session/useSessionSubscriptions";
import { useBrainSubscriptions } from "~/hooks/useBrainSubscriptions";
import { useChannelSubscriptions } from "~/hooks/useChannelSubscriptions";
import { useDiscussionSubscriptions } from "~/hooks/useDiscussionSubscriptions";
import { useScheduleSubscriptions } from "~/hooks/useScheduleSubscriptions";
import { useTeamSubscriptions } from "~/hooks/useTeamSubscriptions";
import { useUpdateSubscriptions } from "~/hooks/useUpdateSubscriptions";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listChannels } from "~/lib/channel-actions";
import type { ListSessionsResult } from "~/lib/generated-types";
import { getProjectGitStatus, setProjectPinned } from "~/lib/project-actions";
import { listSchedules } from "~/lib/schedule-actions";
import { listProviderModels } from "~/lib/session/actions";
import { loadSessionHistory } from "~/lib/session/history";
import type { TeamInfo } from "~/lib/team-actions";
import { listAgentProfiles, listPersonaInteractions, listTeams } from "~/lib/team-actions";
import type { Project } from "~/lib/types";
import { getErrorMessage } from "~/lib/utils";
import { readArchivedAt } from "~/lib/wire-compat";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useEventSeqStore } from "~/stores/event-seq";
import { useProviderStore } from "~/stores/provider-store";
import { useScheduleStore } from "~/stores/schedule-store";
import { useStreamingStore } from "~/stores/streaming-store";
import { useTeamStore } from "~/stores/team-store";
import { useUIStore } from "~/stores/ui-store";

function loadPersonaInteractions(ws: ReturnType<typeof useWebSocket>, teams: TeamInfo[]) {
  for (const team of teams) {
    listPersonaInteractions(ws, { teamId: team.id, limit: 50 })
      .then((interactions) => useTeamStore.getState().setInteractions(team.id, interactions))
      .catch((err) => console.error("listPersonaInteractions failed", err));
  }
}

export interface LoadProjectOptions {
  /** Reconnect: the fetched state is authoritative and held history is stale. */
  force?: boolean;
  /** The session the operator is looking at, as an id or id prefix. It is
   *  the one session that loads its full history, and it loads first. Falls
   *  back to the store's active session. */
  prioritySession?: string;
}

/** The session a prefix names, if it is in this list. */
function findSession(sessions: SessionMetadata[], idOrPrefix?: string | null) {
  if (!idOrPrefix) return undefined;
  return sessions.find((s) => s.id.startsWith(idOrPrefix));
}

// Also drives per-project loading for remote machines (useMachineConnections
// passes that machine's own client instead of the routing facade).
//
// Resolves once the session list has been applied and the priority
// session's first snapshot has landed (at once, when there is none) — never
// rejects, so a caller sequencing projects behind this one is not stalled by
// a machine that is asleep.
export function subscribeAndLoad(
  ws: ReturnType<typeof useWebSocket>,
  projectId: string,
  opts: LoadProjectOptions = {},
): Promise<void> {
  const { force = false } = opts;
  // Background sync never raises a toast. It is driven by lifecycle, not by
  // the operator, and its dominant failure is a machine that is simply
  // asleep — an everyday state, not an error. Connection state already says
  // so where it matters (the machine's dot, its dimmed rows); a stack of
  // "Failed to load sessions" popups says it worse, N times per machine.
  ws.request("project.subscribe", { projectId }, 10_000).catch((err) => {
    console.error("project.subscribe failed", err);
  });
  const listed = ws
    .request<ListSessionsResult>("session.list", { projectId }, 10_000)
    .then((result) => {
      // The wire boundary for the list: a peer on a release from before the
      // archive rename says `completedAt`, and reading only `archivedAt` would
      // land every archived session on that machine in the Open section.
      const sessions = result.sessions.map((s) => ({
        ...s,
        archivedAt: readArchivedAt(s),
      })) as SessionMetadata[];

      // force is true only on reconnect: make session.list authoritative
      // for pending approval/question state so requests resolved while
      // disconnected are cleared (not just added).
      useChatStore.getState().setSessions(sessions, projectId, force);

      // The session on screen goes first, whole, and ALONE: its siblings'
      // tails are sent only once its own first snapshot has landed. The
      // socket runs reads concurrently, so "first on the wire" no longer
      // means "served first" — a request sent alongside eight others shares
      // the server with them. What the operator is looking at gets the
      // server to itself for the ~100ms it needs.
      const priority = findSession(
        sessions,
        opts.prioritySession ?? useChatStore.getState().activeSessionId,
      );
      const painted = priority ? loadSessionHistory(ws, priority.id, { force }) : Promise.resolve();
      return painted.then(() => {
        for (const session of sessions) {
          if (session.id === priority?.id || session.archivedAt) continue;
          loadSessionHistory(ws, session.id, { force, tail: true });
        }
      });
    })
    .catch((err) => {
      console.error("session.list failed", err);
    });
  getProjectGitStatus(ws, projectId)
    .then((status) => useAppStore.getState().setProjectGitStatus(status))
    .catch((err) => console.error("getProjectGitStatus failed", err));
  listChannels(ws, projectId)
    .then((channels) => {
      // On reconnect the fetched list is authoritative for this project: prune
      // channels deleted while disconnected. On the normal subscribe path use
      // mergeChannels to avoid a stale-RPC-vs-fresh-broadcast race.
      const store = useChannelStore.getState();
      if (force) store.reconcileChannels(channels, projectId);
      else store.mergeChannels(channels);
    })
    .catch((err) => console.error("listChannels failed", err));
  return listed;
}

/** Where the operator is: the route's project, and the session within it. */
export interface RouteFocus {
  projectSlug?: string;
  sessionShortId?: string;
}

/**
 * Loads a set of projects with the one the operator is looking at first.
 *
 * Firing all projects at once put the open session behind twenty-two other
 * projects' lists, their git status and every other session's history. The
 * focused project goes alone; the rest are sent once its session list AND
 * the focused session's first snapshot have landed, so that session has the
 * server to itself. With no focus (the landing page) everything goes at
 * once, as before.
 */
export function loadProjectsInOrder(
  ws: ReturnType<typeof useWebSocket>,
  projects: Project[],
  focus: RouteFocus,
  force = false,
): Promise<void> {
  const first = focus.projectSlug ? projects.find((p) => p.slug === focus.projectSlug) : undefined;
  const rest = projects.filter((p) => p !== first);
  const loadRest = () => {
    for (const project of rest) subscribeAndLoad(ws, project.id, { force });
  };
  if (!first) {
    loadRest();
    return Promise.resolve();
  }
  return subscribeAndLoad(ws, first.id, {
    force,
    prioritySession: focus.sessionShortId,
  }).finally(loadRest);
}

export function useGlobalSubscriptions(projects: Project[]) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const subscribedRef = useRef(new Set<string>());
  const projectsRef = useRef(projects);
  projectsRef.current = projects;
  // Read loosely: this hook lives in the root layout, above the route that
  // owns these params, and they are absent everywhere but a session page.
  const focus = useParams({ strict: false }) as RouteFocus;
  const focusRef = useRef(focus);
  focusRef.current = focus;

  // Domain-specific subscription hooks
  useSessionSubscriptions(ws, navigate);
  useChannelSubscriptions(ws);
  useDiscussionSubscriptions(ws);
  useTeamSubscriptions(ws);
  useBrainSubscriptions(ws);
  useScheduleSubscriptions(ws);
  useUpdateSubscriptions(ws);

  // Load teams once on mount
  const teamsLoadedRef = useRef(false);
  useEffect(() => {
    if (teamsLoadedRef.current) return;
    teamsLoadedRef.current = true;
    listTeams(ws)
      .then((teams) => {
        useTeamStore.getState().setTeams(teams);
        loadPersonaInteractions(ws, teams);
      })
      .catch((err) => console.error("listTeams failed", err));
    listAgentProfiles(ws)
      .then((profiles) => useTeamStore.getState().setProfiles(profiles))
      .catch((err) => console.error("listAgentProfiles failed", err));
    listProviderModels(ws)
      .then((result) => useProviderStore.getState().setProviders(result.providers))
      .catch((err) => console.error("listProviderModels failed", err));
    listSchedules(ws)
      .then((schedules) => useScheduleStore.getState().setSchedules(schedules))
      .catch((err) => {
        console.error("listSchedules failed", err);
        useScheduleStore.getState().setLoadError(getErrorMessage(err, "Failed to load schedules"));
      });
  }, [ws]);

  // Subscribe to new projects as they appear — the PRIMARY machine's only.
  // A remote machine's projects are driven by useMachineConnections on that
  // machine's own socket, on connect; subscribing to them here as well meant
  // every cached project of a sleeping laptop fired a doomed request that
  // could only ever time out.
  useEffect(() => {
    const pending = projects.filter((p) => !p.machineId && !subscribedRef.current.has(p.id));
    if (pending.length === 0) return;
    for (const project of pending) subscribedRef.current.add(project.id);
    loadProjectsInOrder(ws, pending, focusRef.current);
  }, [ws, projects]);

  // One-time migration of pre-server-side pinned project IDs from localStorage.
  // Once projects are loaded, push each known ID to the backend, then clear the
  // local cache. Unknown IDs (deleted projects, other workspaces) are dropped.
  const pinMigrationDoneRef = useRef(false);
  useEffect(() => {
    if (pinMigrationDoneRef.current) return;
    if (projects.length === 0) return;
    const legacy = useUIStore.getState().legacyPinnedProjectIds;
    if (legacy.length === 0) {
      pinMigrationDoneRef.current = true;
      return;
    }
    pinMigrationDoneRef.current = true;
    const known = new Set(projects.map((p) => p.id));
    Promise.all(
      legacy
        .filter((id) => known.has(id))
        .map((id) =>
          setProjectPinned(ws, id, true).catch((err) =>
            console.error("legacy pin migration failed", id, err),
          ),
        ),
    ).finally(() => useUIStore.getState().clearLegacyPinnedProjectIds());
  }, [ws, projects]);

  // Project-level events and reconnect handlers. Visibility-driven history
  // refresh is owned by the WS client (force-reconnect after >=5s hidden ⇒
  // onConnect ⇒ subscribeAndLoad with forceHistory=true) below.
  useEffect(() => {
    const unsubProjectGit = ws.subscribe("project.git-status", (payload) => {
      useAppStore.getState().setProjectGitStatus(payload);
    });

    const unsubProjectUpdated = ws.subscribe("project.updated", (payload) => {
      useAppStore.getState().updateProject(payload);
    });

    // First-time browser use on a host without Chrome auto-installs one; surface
    // progress so a paused browser tool call doesn't look stuck. Fires even when
    // the browser panel feature is off (the agent provisions headlessly).
    const unsubBrowserProvisioning = ws.subscribe(
      "browser.provisioning",
      (payload: { sessionId: string; state: string }) => {
        const id = `browser-provisioning-${payload.sessionId}`;
        if (payload.state === "installing") {
          toast.loading("Setting up browser (one-time download)…", { id });
        } else if (payload.state === "ready") {
          toast.success("Browser ready", { id, duration: 2000 });
        } else if (payload.state === "failed") {
          toast.error("Browser setup failed — see the session for details", { id });
        }
      },
    );

    const unsubReconnect = ws.onConnect(() => {
      // Clear orphaned streaming data from the previous connection.
      useStreamingStore.getState().reset();
      // Drop stale wire-seq tracking — the forced history reloads below reseed
      // it authoritatively from each session's high-water mark.
      useEventSeqStore.getState().reset();
      subscribedRef.current.clear();
      // This is the PRIMARY's reconnect; a remote machine re-syncs on its
      // own socket's onConnect and must never be reset from here (a flaky
      // remote and a flaky primary are separate failures).
      const local = projectsRef.current.filter((p) => !p.machineId);
      for (const project of local) subscribedRef.current.add(project.id);
      loadProjectsInOrder(ws, local, focusRef.current, true);
      listTeams(ws)
        .then((teams) => {
          useTeamStore.getState().setTeams(teams);
          loadPersonaInteractions(ws, teams);
        })
        .catch((err) => console.error("listTeams (reconnect) failed", err));
      listAgentProfiles(ws)
        .then((profiles) => useTeamStore.getState().setProfiles(profiles))
        .catch((err) => console.error("listAgentProfiles (reconnect) failed", err));
      // Schedules mutate server-side while disconnected (fires, auto-pauses)
      // — refetch or the panel goes stale.
      listSchedules(ws)
        .then((schedules) => useScheduleStore.getState().setSchedules(schedules))
        .catch((err) => {
          console.error("listSchedules (reconnect) failed", err);
          useScheduleStore
            .getState()
            .setLoadError(getErrorMessage(err, "Failed to load schedules"));
        });
    });

    return () => {
      unsubProjectGit();
      unsubProjectUpdated();
      unsubBrowserProvisioning();
      unsubReconnect();
    };
  }, [ws]);
}
