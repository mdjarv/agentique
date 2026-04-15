import { create } from "zustand";
import type { AgentProfileInfo, PersonaInteraction, TeamInfo } from "~/lib/team-actions";

interface TeamState {
  profiles: Record<string, AgentProfileInfo>;
  teams: Record<string, TeamInfo>;
  interactions: Record<string, PersonaInteraction[]>; // keyed by teamId
  loaded: boolean;

  setProfiles: (profiles: AgentProfileInfo[]) => void;
  addProfile: (profile: AgentProfileInfo) => void;
  updateProfile: (profile: AgentProfileInfo) => void;
  removeProfile: (id: string) => void;

  setTeams: (teams: TeamInfo[]) => void;
  addTeam: (team: TeamInfo) => void;
  updateTeam: (team: TeamInfo) => void;
  removeTeam: (id: string) => void;

  setInteractions: (teamId: string, interactions: PersonaInteraction[]) => void;
  addInteraction: (interaction: PersonaInteraction) => void;
}

export const useTeamStore = create<TeamState>((set) => ({
  profiles: {},
  teams: {},
  interactions: {},
  loaded: false,

  setProfiles: (profiles) =>
    set({
      profiles: Object.fromEntries(profiles.map((p) => [p.id, p])),
      loaded: true,
    }),

  addProfile: (profile) => set((s) => ({ profiles: { ...s.profiles, [profile.id]: profile } })),

  updateProfile: (profile) =>
    set((s) => ({
      profiles: { ...s.profiles, [profile.id]: profile },
      // Also update the profile within any team that contains it
      teams: Object.fromEntries(
        Object.entries(s.teams).map(([id, t]) => [
          id,
          { ...t, members: t.members.map((m) => (m.id === profile.id ? profile : m)) },
        ]),
      ),
    })),

  removeProfile: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.profiles;
      return {
        profiles: rest,
        // Also remove the profile from any team that contains it
        teams: Object.fromEntries(
          Object.entries(s.teams).map(([tid, t]) => [
            tid,
            { ...t, members: t.members.filter((m) => m.id !== id) },
          ]),
        ),
      };
    }),

  setTeams: (teams) =>
    set({
      teams: Object.fromEntries(teams.map((t) => [t.id, t])),
      loaded: true,
    }),

  addTeam: (team) => set((s) => ({ teams: { ...s.teams, [team.id]: team } })),

  updateTeam: (team) => set((s) => ({ teams: { ...s.teams, [team.id]: team } })),

  removeTeam: (id) =>
    set((s) => {
      const { [id]: _, ...rest } = s.teams;
      return { teams: rest };
    }),

  setInteractions: (teamId, interactions) =>
    set((s) => ({
      interactions: { ...s.interactions, [teamId]: interactions },
    })),

  addInteraction: (interaction) =>
    set((s) => {
      const existing = s.interactions[interaction.teamId] ?? [];
      return {
        interactions: {
          ...s.interactions,
          [interaction.teamId]: [interaction, ...existing],
        },
      };
    }),
}));
