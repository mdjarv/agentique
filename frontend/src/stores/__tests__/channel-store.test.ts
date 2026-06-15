import { beforeEach, describe, expect, it } from "vitest";
import type { ChannelInfo, ChannelMember, ChannelMessage } from "~/lib/channel-actions";
import { useChannelStore } from "~/stores/channel-store";

function makeMember(overrides: Partial<ChannelMember> = {}): ChannelMember {
  return {
    sessionId: "sess-1",
    name: "Worker",
    role: "worker",
    state: "idle",
    connected: true,
    ...overrides,
  };
}

function makeChannel(overrides: Partial<ChannelInfo> = {}): ChannelInfo {
  return {
    id: "ch-1",
    projectId: "proj-1",
    name: "Test Channel",
    members: [makeMember()],
    createdAt: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeEvent(overrides: Partial<ChannelMessage> = {}): ChannelMessage {
  return {
    id: `msg-${Math.random().toString(36).slice(2)}`,
    channelId: "ch-1",
    senderType: "session",
    senderId: "sess-1",
    senderName: "Worker",
    content: "hello",
    createdAt: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("channel-store", () => {
  beforeEach(() => {
    useChannelStore.setState({ channels: {}, timelines: {} });
  });

  describe("setChannels", () => {
    it("replaces all channels", () => {
      const c1 = makeChannel({ id: "c1" });
      const c2 = makeChannel({ id: "c2" });
      useChannelStore.getState().setChannels([c1, c2]);
      expect(Object.keys(useChannelStore.getState().channels)).toEqual(["c1", "c2"]);
    });
  });

  describe("mergeChannels", () => {
    it("adds new channels", () => {
      useChannelStore.getState().mergeChannels([makeChannel({ id: "c1" })]);
      expect(useChannelStore.getState().channels.c1).toBeDefined();
    });

    it("does not overwrite channel with more members (stale check)", () => {
      const existing = makeChannel({
        id: "c1",
        members: [makeMember({ sessionId: "s1" }), makeMember({ sessionId: "s2" })],
      });
      useChannelStore.getState().setChannels([existing]);

      const stale = makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1" })] });
      useChannelStore.getState().mergeChannels([stale]);

      expect(useChannelStore.getState().channels.c1?.members).toHaveLength(2);
    });
  });

  describe("reconcileChannels", () => {
    it("prunes channels of the project absent from the fetched list", () => {
      useChannelStore
        .getState()
        .setChannels([makeChannel({ id: "keep" }), makeChannel({ id: "gone" })]);
      useChannelStore.getState().setTimeline("gone", [makeEvent({ channelId: "gone" })]);

      useChannelStore.getState().reconcileChannels([makeChannel({ id: "keep" })], "proj-1");

      expect(useChannelStore.getState().channels.keep).toBeDefined();
      expect(useChannelStore.getState().channels.gone).toBeUndefined();
      // Pruned channel's timeline is dropped too.
      expect(useChannelStore.getState().timelines.gone).toBeUndefined();
    });

    it("does not prune channels belonging to other projects", () => {
      useChannelStore
        .getState()
        .setChannels([
          makeChannel({ id: "mine", projectId: "proj-1" }),
          makeChannel({ id: "theirs", projectId: "proj-2" }),
        ]);

      // Fetched list for proj-1 is empty — must not touch proj-2's channel.
      useChannelStore.getState().reconcileChannels([], "proj-1");

      expect(useChannelStore.getState().channels.mine).toBeUndefined();
      expect(useChannelStore.getState().channels.theirs).toBeDefined();
    });

    it("adds new channels from the fetched list", () => {
      useChannelStore.getState().reconcileChannels([makeChannel({ id: "fresh" })], "proj-1");
      expect(useChannelStore.getState().channels.fresh).toBeDefined();
    });

    it("does not overwrite a channel with more members (stale check)", () => {
      const existing = makeChannel({
        id: "c1",
        members: [makeMember({ sessionId: "s1" }), makeMember({ sessionId: "s2" })],
      });
      useChannelStore.getState().setChannels([existing]);

      const stale = makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1" })] });
      useChannelStore.getState().reconcileChannels([stale], "proj-1");

      expect(useChannelStore.getState().channels.c1?.members).toHaveLength(2);
    });
  });

  describe("addChannel / removeChannel", () => {
    it("adds a channel", () => {
      useChannelStore.getState().addChannel(makeChannel({ id: "c1" }));
      expect(useChannelStore.getState().channels.c1).toBeDefined();
    });

    it("removes a channel and its timeline", () => {
      useChannelStore.getState().addChannel(makeChannel({ id: "c1" }));
      useChannelStore.getState().setTimeline("c1", [makeEvent()]);
      useChannelStore.getState().removeChannel("c1");
      expect(useChannelStore.getState().channels.c1).toBeUndefined();
      expect(useChannelStore.getState().timelines.c1).toBeUndefined();
    });
  });

  describe("addMember", () => {
    it("adds a member to a channel", () => {
      useChannelStore.getState().addChannel(makeChannel({ id: "c1", members: [] }));
      useChannelStore.getState().addMember("c1", makeMember({ sessionId: "s1" }));
      expect(useChannelStore.getState().channels.c1?.members).toHaveLength(1);
    });

    it("deduplicates by sessionId", () => {
      useChannelStore
        .getState()
        .addChannel(makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1" })] }));
      useChannelStore.getState().addMember("c1", makeMember({ sessionId: "s1" }));
      expect(useChannelStore.getState().channels.c1?.members).toHaveLength(1);
    });
  });

  describe("removeMember", () => {
    it("removes a member from a channel", () => {
      useChannelStore.getState().addChannel(
        makeChannel({
          id: "c1",
          members: [makeMember({ sessionId: "s1" }), makeMember({ sessionId: "s2" })],
        }),
      );
      useChannelStore.getState().removeMember("c1", "s1");
      const members = useChannelStore.getState().channels.c1?.members ?? [];
      expect(members).toHaveLength(1);
      expect(members[0]?.sessionId).toBe("s2");
    });
  });

  describe("updateMemberState", () => {
    it("updates member state across channels", () => {
      useChannelStore
        .getState()
        .addChannel(
          makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1", state: "idle" })] }),
        );
      useChannelStore.getState().updateMemberState("s1", "active");
      expect(useChannelStore.getState().channels.c1?.members[0]?.state).toBe("active");
    });

    it("no-op when state unchanged", () => {
      useChannelStore
        .getState()
        .addChannel(
          makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1", state: "idle" })] }),
        );
      const before = useChannelStore.getState();
      useChannelStore.getState().updateMemberState("s1", "idle");
      expect(useChannelStore.getState()).toBe(before);
    });

    it("updates connected flag", () => {
      useChannelStore
        .getState()
        .addChannel(
          makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1", connected: true })] }),
        );
      useChannelStore.getState().updateMemberState("s1", "idle", false);
      expect(useChannelStore.getState().channels.c1?.members[0]?.connected).toBe(false);
    });
  });

  describe("getChannelForSession", () => {
    it("finds channel containing member", () => {
      useChannelStore
        .getState()
        .addChannel(makeChannel({ id: "c1", members: [makeMember({ sessionId: "s1" })] }));
      expect(useChannelStore.getState().getChannelForSession("s1")?.id).toBe("c1");
    });

    it("returns undefined if not found", () => {
      expect(useChannelStore.getState().getChannelForSession("missing")).toBeUndefined();
    });
  });

  describe("timeline", () => {
    it("sets timeline events", () => {
      useChannelStore.getState().setTimeline("c1", [makeEvent()]);
      expect(useChannelStore.getState().timelines.c1).toHaveLength(1);
    });

    it("appends timeline event", () => {
      useChannelStore.getState().setTimeline("c1", [makeEvent()]);
      useChannelStore.getState().appendTimelineEvent("c1", makeEvent({ content: "second" }));
      expect(useChannelStore.getState().timelines.c1).toHaveLength(2);
    });
  });
});
