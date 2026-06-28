import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { useSessionSubscriptions } from "~/hooks/session/useSessionSubscriptions";
import { useBrainSubscriptions } from "~/hooks/useBrainSubscriptions";
import { useChannelSubscriptions } from "~/hooks/useChannelSubscriptions";
import { useDiscussionSubscriptions } from "~/hooks/useDiscussionSubscriptions";
import { useTeamSubscriptions } from "~/hooks/useTeamSubscriptions";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listChannels } from "~/lib/channel-actions";
import type { ListSessionsResult } from "~/lib/generated-types";
import { getProjectGitStatus, setProjectPinned } from "~/lib/project-actions";
import { listProviderModels } from "~/lib/session/actions";
import { loadSessionHistory } from "~/lib/session/history";
import type { TeamInfo } from "~/lib/team-actions";
import { listAgentProfiles, listPersonaInteractions, listTeams } from "~/lib/team-actions";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useEventSeqStore } from "~/stores/event-seq";
import { useProviderStore } from "~/stores/provider-store";
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

function subscribeAndLoad(
  ws: ReturnType<typeof useWebSocket>,
  projectId: string,
  forceHistory = false,
) {
  ws.request("project.subscribe", { projectId }, 10_000).catch((err) => {
    console.error("project.subscribe failed", err);
    toast.error("Failed to subscribe to project updates");
  });
  ws.request<ListSessionsResult>("session.list", { projectId }, 10_000)
    .then((result) => {
      // forceHistory is true only on reconnect: make session.list authoritative
      // for pending approval/question state so requests resolved while
      // disconnected are cleared (not just added).
      useChatStore
        .getState()
        .setSessions(result.sessions as SessionMetadata[], projectId, forceHistory);
      for (const session of result.sessions) {
        if (!session.completedAt) {
          loadSessionHistory(ws, session.id, forceHistory);
        }
      }
    })
    .catch((err) => {
      console.error("session.list failed", err);
      toast.error("Failed to load sessions");
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
      if (forceHistory) store.reconcileChannels(channels, projectId);
      else store.mergeChannels(channels);
    })
    .catch((err) => console.error("listChannels failed", err));
}

export function useGlobalSubscriptions(projects: Project[]) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const subscribedRef = useRef(new Set<string>());
  const projectsRef = useRef(projects);
  projectsRef.current = projects;

  // Domain-specific subscription hooks
  useSessionSubscriptions(ws, navigate);
  useChannelSubscriptions(ws);
  useDiscussionSubscriptions(ws);
  useTeamSubscriptions(ws);
  useBrainSubscriptions(ws);

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
  }, [ws]);

  // Subscribe to new projects as they appear
  useEffect(() => {
    for (const project of projects) {
      if (subscribedRef.current.has(project.id)) continue;
      subscribedRef.current.add(project.id);
      subscribeAndLoad(ws, project.id);
    }
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
      for (const project of projectsRef.current) {
        subscribedRef.current.add(project.id);
        subscribeAndLoad(ws, project.id, true);
      }
      listTeams(ws)
        .then((teams) => {
          useTeamStore.getState().setTeams(teams);
          loadPersonaInteractions(ws, teams);
        })
        .catch((err) => console.error("listTeams (reconnect) failed", err));
      listAgentProfiles(ws)
        .then((profiles) => useTeamStore.getState().setProfiles(profiles))
        .catch((err) => console.error("listAgentProfiles (reconnect) failed", err));
    });

    return () => {
      unsubProjectGit();
      unsubProjectUpdated();
      unsubBrowserProvisioning();
      unsubReconnect();
    };
  }, [ws]);
}
