import { beforeEach, describe, expect, it } from "vitest";
import type { TeamInfo, TeamMember, TimelineEvent } from "~/lib/team-actions";
import { useTeamStore } from "~/stores/team-store";

function makeMember(overrides: Partial<TeamMember> = {}): TeamMember {
  return {
    sessionId: "sess-1",
    name: "Worker",
    role: "worker",
    state: "idle",
    connected: true,
    ...overrides,
  };
}

function makeTeam(overrides: Partial<TeamInfo> = {}): TeamInfo {
  return {
    id: "team-1",
    projectId: "proj-1",
    name: "Test Team",
    members: [makeMember()],
    createdAt: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeEvent(overrides: Partial<TimelineEvent> = {}): TimelineEvent {
  return {
    direction: "sent",
    senderSessionId: "sess-1",
    senderName: "Worker",
    targetSessionId: "sess-2",
    targetName: "Lead",
    content: "hello",
    ...overrides,
  };
}

describe("team-store", () => {
  beforeEach(() => {
    useTeamStore.setState({ teams: {}, timelines: {} });
  });

  describe("setTeams", () => {
    it("replaces all teams", () => {
      const t1 = makeTeam({ id: "t1" });
      const t2 = makeTeam({ id: "t2" });
      useTeamStore.getState().setTeams([t1, t2]);
      expect(Object.keys(useTeamStore.getState().teams)).toEqual(["t1", "t2"]);
    });
  });

  describe("mergeTeams", () => {
    it("adds new teams", () => {
      useTeamStore.getState().mergeTeams([makeTeam({ id: "t1" })]);
      expect(useTeamStore.getState().teams.t1).toBeDefined();
    });

    it("does not overwrite team with more members (stale check)", () => {
      const existing = makeTeam({
        id: "t1",
        members: [makeMember({ sessionId: "s1" }), makeMember({ sessionId: "s2" })],
      });
      useTeamStore.getState().setTeams([existing]);

      const stale = makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1" })] });
      useTeamStore.getState().mergeTeams([stale]);

      expect(useTeamStore.getState().teams.t1?.members).toHaveLength(2);
    });
  });

  describe("addTeam / removeTeam", () => {
    it("adds a team", () => {
      useTeamStore.getState().addTeam(makeTeam({ id: "t1" }));
      expect(useTeamStore.getState().teams.t1).toBeDefined();
    });

    it("removes a team and its timeline", () => {
      useTeamStore.getState().addTeam(makeTeam({ id: "t1" }));
      useTeamStore.getState().setTimeline("t1", [makeEvent()]);
      useTeamStore.getState().removeTeam("t1");
      expect(useTeamStore.getState().teams.t1).toBeUndefined();
      expect(useTeamStore.getState().timelines.t1).toBeUndefined();
    });
  });

  describe("addMember", () => {
    it("adds a member to a team", () => {
      useTeamStore.getState().addTeam(makeTeam({ id: "t1", members: [] }));
      useTeamStore.getState().addMember("t1", makeMember({ sessionId: "s1" }));
      expect(useTeamStore.getState().teams.t1?.members).toHaveLength(1);
    });

    it("deduplicates by sessionId", () => {
      useTeamStore
        .getState()
        .addTeam(makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1" })] }));
      useTeamStore.getState().addMember("t1", makeMember({ sessionId: "s1" }));
      expect(useTeamStore.getState().teams.t1?.members).toHaveLength(1);
    });
  });

  describe("removeMember", () => {
    it("removes a member from a team", () => {
      useTeamStore.getState().addTeam(
        makeTeam({
          id: "t1",
          members: [makeMember({ sessionId: "s1" }), makeMember({ sessionId: "s2" })],
        }),
      );
      useTeamStore.getState().removeMember("t1", "s1");
      const members = useTeamStore.getState().teams.t1?.members ?? [];
      expect(members).toHaveLength(1);
      expect(members[0]?.sessionId).toBe("s2");
    });
  });

  describe("updateMemberState", () => {
    it("updates member state across teams", () => {
      useTeamStore
        .getState()
        .addTeam(makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1", state: "idle" })] }));
      useTeamStore.getState().updateMemberState("s1", "active");
      expect(useTeamStore.getState().teams.t1?.members[0]?.state).toBe("active");
    });

    it("no-op when state unchanged", () => {
      useTeamStore
        .getState()
        .addTeam(makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1", state: "idle" })] }));
      const before = useTeamStore.getState();
      useTeamStore.getState().updateMemberState("s1", "idle");
      expect(useTeamStore.getState()).toBe(before);
    });

    it("updates connected flag", () => {
      useTeamStore
        .getState()
        .addTeam(
          makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1", connected: true })] }),
        );
      useTeamStore.getState().updateMemberState("s1", "idle", false);
      expect(useTeamStore.getState().teams.t1?.members[0]?.connected).toBe(false);
    });
  });

  describe("getTeamForSession", () => {
    it("finds team containing member", () => {
      useTeamStore
        .getState()
        .addTeam(makeTeam({ id: "t1", members: [makeMember({ sessionId: "s1" })] }));
      expect(useTeamStore.getState().getTeamForSession("s1")?.id).toBe("t1");
    });

    it("returns undefined if not found", () => {
      expect(useTeamStore.getState().getTeamForSession("missing")).toBeUndefined();
    });
  });

  describe("timeline", () => {
    it("sets timeline events", () => {
      useTeamStore.getState().setTimeline("t1", [makeEvent()]);
      expect(useTeamStore.getState().timelines.t1).toHaveLength(1);
    });

    it("appends timeline event", () => {
      useTeamStore.getState().setTimeline("t1", [makeEvent()]);
      useTeamStore.getState().appendTimelineEvent("t1", makeEvent({ content: "second" }));
      expect(useTeamStore.getState().timelines.t1).toHaveLength(2);
    });
  });
});
