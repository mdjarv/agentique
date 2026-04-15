import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { useSessionSubscriptions } from "~/hooks/session/useSessionSubscriptions";
import { useChannelSubscriptions } from "~/hooks/useChannelSubscriptions";
import { useTeamSubscriptions } from "~/hooks/useTeamSubscriptions";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listChannels } from "~/lib/channel-actions";
import type { ListSessionsResult } from "~/lib/generated-types";
import { getProjectGitStatus } from "~/lib/project-actions";
import { loadSessionHistory } from "~/lib/session/history";
import type { TeamInfo } from "~/lib/team-actions";
import { listAgentProfiles, listPersonaInteractions, listTeams } from "~/lib/team-actions";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";
import { useTeamStore } from "~/stores/team-store";

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
      useChatStore.getState().setSessions(result.sessions as SessionMetadata[], projectId);
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
    .then((channels) => useChannelStore.getState().mergeChannels(channels))
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
  useTeamSubscriptions(ws);

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
  }, [ws]);

  // Subscribe to new projects as they appear
  useEffect(() => {
    for (const project of projects) {
      if (subscribedRef.current.has(project.id)) continue;
      subscribedRef.current.add(project.id);
      subscribeAndLoad(ws, project.id);
    }
  }, [ws, projects]);

  // Project-level events, reconnect, and visibility handlers
  useEffect(() => {
    const unsubProjectGit = ws.subscribe("project.git-status", (payload) => {
      useAppStore.getState().setProjectGitStatus(payload);
    });

    const unsubProjectUpdated = ws.subscribe("project.updated", (payload) => {
      useAppStore.getState().updateProject(payload);
    });

    const unsubReconnect = ws.onConnect(() => {
      // Clear orphaned streaming data from the previous connection.
      useStreamingStore.getState().reset();
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

    let hiddenAt = 0;
    const handleVisibility = () => {
      if (document.visibilityState === "hidden") {
        hiddenAt = Date.now();
        return;
      }
      if (Date.now() - hiddenAt < 5000) return;
      const activeId = useChatStore.getState().activeSessionId;
      if (activeId && ws.connectionState === "connected") {
        loadSessionHistory(ws, activeId, true);
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);

    return () => {
      unsubProjectGit();
      unsubProjectUpdated();
      unsubReconnect();
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [ws]);
}
