import { create } from "zustand";
import type { TeamInfo, TeamMember, TimelineEvent } from "~/lib/team-actions";

interface TeamState {
  teams: Record<string, TeamInfo>;
  timelines: Record<string, TimelineEvent[]>;

  setTeams: (teams: TeamInfo[]) => void;
  mergeTeams: (teams: TeamInfo[]) => void;
  addTeam: (team: TeamInfo) => void;
  removeTeam: (teamId: string) => void;
  updateTeamName: (teamId: string, name: string) => void;

  addMember: (teamId: string, member: TeamMember) => void;
  removeMember: (teamId: string, sessionId: string) => void;
  updateMemberState: (sessionId: string, state: string, connected?: boolean) => void;

  setTimeline: (teamId: string, events: TimelineEvent[]) => void;
  appendTimelineEvent: (teamId: string, event: TimelineEvent) => void;

  getTeamForSession: (sessionId: string) => TeamInfo | undefined;
}

export const useTeamStore = create<TeamState>((set, get) => ({
  teams: {},
  timelines: {},

  setTeams: (teams) =>
    set({
      teams: Object.fromEntries(teams.map((t) => [t.id, t])),
    }),

  mergeTeams: (teams) =>
    set((s) => {
      const merged = { ...s.teams };
      for (const t of teams) {
        const existing = merged[t.id];
        // Don't overwrite a team that has more members (stale RPC vs fresh broadcast).
        if (existing && existing.members.length > t.members.length) continue;
        merged[t.id] = t;
      }
      return { teams: merged };
    }),

  addTeam: (team) => set((s) => ({ teams: { ...s.teams, [team.id]: team } })),

  removeTeam: (teamId) =>
    set((s) => {
      const { [teamId]: _, ...rest } = s.teams;
      const { [teamId]: __, ...restTimelines } = s.timelines;
      return { teams: rest, timelines: restTimelines };
    }),

  updateTeamName: (teamId, name) =>
    set((s) => {
      const team = s.teams[teamId];
      if (!team) return s;
      return { teams: { ...s.teams, [teamId]: { ...team, name } } };
    }),

  addMember: (teamId, member) =>
    set((s) => {
      const team = s.teams[teamId];
      if (!team) return s;
      if (team.members.some((m) => m.sessionId === member.sessionId)) return s;
      return {
        teams: { ...s.teams, [teamId]: { ...team, members: [...team.members, member] } },
      };
    }),

  removeMember: (teamId, sessionId) =>
    set((s) => {
      const team = s.teams[teamId];
      if (!team) return s;
      return {
        teams: {
          ...s.teams,
          [teamId]: { ...team, members: team.members.filter((m) => m.sessionId !== sessionId) },
        },
      };
    }),

  updateMemberState: (sessionId, state, connected) =>
    set((s) => {
      let changed = false;
      const teams = { ...s.teams };
      for (const [tid, team] of Object.entries(teams)) {
        const idx = team.members.findIndex((m) => m.sessionId === sessionId);
        if (idx === -1) continue;
        const prev = team.members[idx];
        if (!prev) continue;
        if (prev.state === state && (connected === undefined || prev.connected === connected))
          continue;
        const patched: TeamMember = { ...prev, state };
        if (connected !== undefined) patched.connected = connected;
        const updated = [...team.members];
        updated[idx] = patched;
        teams[tid] = { ...team, members: updated };
        changed = true;
      }
      return changed ? { teams } : s;
    }),

  setTimeline: (teamId, events) =>
    set((s) => ({
      timelines: { ...s.timelines, [teamId]: events },
    })),

  appendTimelineEvent: (teamId, event) =>
    set((s) => ({
      timelines: {
        ...s.timelines,
        [teamId]: [...(s.timelines[teamId] ?? []), event],
      },
    })),

  getTeamForSession: (sessionId) => {
    const teams = get().teams;
    return Object.values(teams).find((t) => t.members.some((m) => m.sessionId === sessionId));
  },
}));
