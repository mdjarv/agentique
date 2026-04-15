import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { useTeamStore } from "~/stores/team-store";

export function useTeamSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubProfileCreated = ws.subscribe("agent-profile.created", (payload) => {
      useTeamStore.getState().addProfile(payload);
    });
    const unsubProfileUpdated = ws.subscribe("agent-profile.updated", (payload) => {
      useTeamStore.getState().updateProfile(payload);
    });
    const unsubProfileDeleted = ws.subscribe("agent-profile.deleted", (payload) => {
      useTeamStore.getState().removeProfile(payload.id);
    });
    const unsubTeamCreated = ws.subscribe("team.created", (payload) => {
      useTeamStore.getState().addTeam(payload);
    });
    const unsubTeamUpdated = ws.subscribe("team.updated", (payload) => {
      useTeamStore.getState().updateTeam(payload);
    });
    const unsubTeamDeleted = ws.subscribe("team.deleted", (payload) => {
      useTeamStore.getState().removeTeam(payload.id);
    });
    const unsubPersonaInteraction = ws.subscribe("persona.interaction", (payload) => {
      useTeamStore.getState().addInteraction(payload);
    });

    return () => {
      unsubProfileCreated();
      unsubProfileUpdated();
      unsubProfileDeleted();
      unsubTeamCreated();
      unsubTeamUpdated();
      unsubTeamDeleted();
      unsubPersonaInteraction();
    };
  }, [ws]);
}
