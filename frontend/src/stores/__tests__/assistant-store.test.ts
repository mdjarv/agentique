import { beforeEach, describe, expect, it } from "vitest";
import type {
  AssistantJournalEntry,
  AssistantMessage,
  AssistantPolicy,
  AssistantProposal,
} from "~/lib/assistant/wire";
import {
  EMPTY_JOURNAL,
  EMPTY_MESSAGES,
  EMPTY_POLICIES,
  EMPTY_PROPOSALS,
  selectAssistantJournal,
  selectAssistantMessages,
  selectAssistantOpenProposals,
  selectAssistantPolicies,
  selectAssistantProposals,
  selectAssistantReplying,
  useAssistantStore,
} from "~/stores/assistant-store";

function msg(overrides: Partial<AssistantMessage> = {}): AssistantMessage {
  return {
    id: "m1",
    role: "user",
    text: "hello",
    createdAt: "2026-01-01T00:00:00.000000000Z",
    ...overrides,
  };
}

function entry(overrides: Partial<AssistantJournalEntry> = {}): AssistantJournalEntry {
  return {
    id: 1,
    at: "2026-01-01T00:00:00Z",
    kind: "session_finished",
    sessionId: "sess-1",
    summary: "tests pass",
    ...overrides,
  };
}

function proposal(overrides: Partial<AssistantProposal> = {}): AssistantProposal {
  return {
    id: "p1",
    createdAt: "2026-01-01T00:00:00Z",
    verb: "merge_session",
    sessionId: "sess-1",
    rationale: "the branch is ahead and clean",
    evidence: { ahead: 2, behind: 0, dirty: false, mergeStatus: "clean", busy: false },
    status: "open",
    ...overrides,
  };
}

describe("assistant-store", () => {
  beforeEach(() => {
    useAssistantStore.getState().reset();
  });

  describe("selectors", () => {
    it("return the same reference when nothing changed", () => {
      const s = useAssistantStore.getState();
      expect(selectAssistantMessages(s)).toBe(EMPTY_MESSAGES);
      expect(selectAssistantJournal(s)).toBe(EMPTY_JOURNAL);

      // A second read of an untouched store is the same array, not a copy —
      // the rule that keeps a subscriber from re-rendering forever.
      const again = useAssistantStore.getState();
      expect(selectAssistantMessages(again)).toBe(selectAssistantMessages(s));
      expect(selectAssistantJournal(again)).toBe(selectAssistantJournal(s));
    });

    it("keep the held journal reference when a read brings nothing new", () => {
      useAssistantStore.getState().applyJournal([entry()]);
      const first = selectAssistantJournal(useAssistantStore.getState());
      useAssistantStore.getState().applyJournal([entry()]);
      expect(selectAssistantJournal(useAssistantStore.getState())).toBe(first);
    });

    it("keep the held message reference when a page brings nothing", () => {
      useAssistantStore.getState().setHistory({ messages: [msg()] });
      const first = selectAssistantMessages(useAssistantStore.getState());
      useAssistantStore.getState().setHistory({ messages: [] });
      expect(selectAssistantMessages(useAssistantStore.getState())).toBe(first);
    });

    it("report a reply in flight from the streaming text alone", () => {
      expect(selectAssistantReplying(useAssistantStore.getState())).toBe(false);
      useAssistantStore.getState().beginReply();
      // Armed before the first token: an empty string is still a reply owed.
      expect(selectAssistantReplying(useAssistantStore.getState())).toBe(true);
      expect(useAssistantStore.getState().streaming).toBe("");
    });
  });

  describe("delta accumulation", () => {
    it("appends each delta's new text", () => {
      const store = useAssistantStore.getState();
      store.appendDelta("Look");
      store.appendDelta("ing at ");
      store.appendDelta("the branch");
      expect(useAssistantStore.getState().streaming).toBe("Looking at the branch");
    });

    it("starts from nothing when no reply was armed", () => {
      useAssistantStore.getState().appendDelta("hi");
      expect(useAssistantStore.getState().streaming).toBe("hi");
    });

    it("does not reset an armed reply when beginReply is called again", () => {
      const store = useAssistantStore.getState();
      store.beginReply();
      store.appendDelta("half a sentence");
      useAssistantStore.getState().beginReply();
      expect(useAssistantStore.getState().streaming).toBe("half a sentence");
    });

    it("is replaced by the stored assistant message", () => {
      const store = useAssistantStore.getState();
      store.appendDelta("partial");
      store.appendMessage(msg({ id: "m2", role: "assistant", text: "whole" }));
      const after = useAssistantStore.getState();
      expect(after.streaming).toBeNull();
      expect(after.messages.map((m) => m.text)).toEqual(["whole"]);
    });

    it("survives the operator's own message landing on the same push", () => {
      const store = useAssistantStore.getState();
      store.appendDelta("partial");
      store.appendMessage(msg({ id: "m3", role: "user" }));
      // Only the head's own turn ends the stream.
      expect(useAssistantStore.getState().streaming).toBe("partial");
    });

    it("clears on demand, for a failed turn or a reconnect", () => {
      useAssistantStore.getState().appendDelta("partial");
      useAssistantStore.getState().clearStreaming();
      expect(useAssistantStore.getState().streaming).toBeNull();
    });
  });

  describe("journal merge", () => {
    it("does not duplicate an entry that arrives twice", () => {
      const store = useAssistantStore.getState();
      store.addJournalEntry(entry());
      store.addJournalEntry(entry());
      expect(useAssistantStore.getState().journal).toHaveLength(1);
    });

    it("does not duplicate a pushed entry that the next read also carries", () => {
      useAssistantStore.getState().addJournalEntry(entry({ id: 7 }));
      useAssistantStore.getState().applyJournal([entry({ id: 7 }), entry({ id: 8 })]);
      expect(useAssistantStore.getState().journal.map((e) => e.id)).toEqual([7, 8]);
    });

    it("takes an empty read without disturbing what is held", () => {
      useAssistantStore.getState().addJournalEntry(entry({ id: 3 }));
      const held = selectAssistantJournal(useAssistantStore.getState());
      useAssistantStore.getState().applyJournal([]);
      expect(selectAssistantJournal(useAssistantStore.getState())).toBe(held);
    });

    it("keeps an entry with no id apart by its stamp and text", () => {
      const store = useAssistantStore.getState();
      store.addJournalEntry({ at: "2026-01-01T00:00:01Z", kind: "note", summary: "one" });
      store.addJournalEntry({ at: "2026-01-01T00:00:01Z", kind: "note", summary: "one" });
      store.addJournalEntry({ at: "2026-01-01T00:00:02Z", kind: "note", summary: "two" });
      expect(useAssistantStore.getState().journal.map((e) => e.summary)).toEqual(["one", "two"]);
    });

    it("takes the later copy of a row it already holds", () => {
      const store = useAssistantStore.getState();
      store.addJournalEntry(entry({ notable: false }));
      store.addJournalEntry(entry({ notable: true }));
      const held = useAssistantStore.getState().journal;
      expect(held).toHaveLength(1);
      expect(held[0]?.notable).toBe(true);
    });

    it("orders oldest first, as the wire does", () => {
      const store = useAssistantStore.getState();
      store.addJournalEntry(entry({ id: 2, at: "2026-01-02T00:00:00Z" }));
      store.addJournalEntry(entry({ id: 1, at: "2026-01-01T00:00:00Z" }));
      expect(useAssistantStore.getState().journal.map((e) => e.id)).toEqual([1, 2]);
    });

    it("raises the notch for news and never for the heartbeat's own row", () => {
      const store = useAssistantStore.getState();
      store.addJournalEntry(entry({ id: 1 }));
      expect(useAssistantStore.getState().unseen).toBe(1);
      // The assistant's own bookkeeping is readable in the strip and is not a
      // claim on attention — the server's count leaves the same kind out, so a
      // notch drawn here would disagree with the next connection's.
      store.addJournalEntry(entry({ id: 2, kind: "heartbeat", summary: "none" }));
      expect(useAssistantStore.getState().journal).toHaveLength(2);
      expect(useAssistantStore.getState().unseen).toBe(1);
      // A folded day is stamped at the day it is about, so it sorts below
      // everything the strip renders: a fold of thirty days would claim thirty
      // unread things with nothing new to look at. The `compaction` row that
      // records the fold is the news, and it counts.
      store.addJournalEntry(entry({ id: 3, kind: "day_summary", at: "2026-08-20T00:00:00Z" }));
      expect(useAssistantStore.getState().unseen).toBe(1);
      store.addJournalEntry(
        entry({ id: 4, kind: "compaction", summary: "the journal was folded" }),
      );
      expect(useAssistantStore.getState().unseen).toBe(2);
    });
  });

  describe("history", () => {
    it("does not lose a message pushed while the page was in flight", () => {
      const store = useAssistantStore.getState();
      store.appendMessage(msg({ id: "live", createdAt: "2026-01-01T00:00:09.000000000Z" }));
      store.setHistory({
        messages: [msg({ id: "old", createdAt: "2026-01-01T00:00:01.000000000Z" })],
        before: "cursor",
      });
      const after = useAssistantStore.getState();
      expect(after.messages.map((m) => m.id)).toEqual(["old", "live"]);
      expect(after.before).toBe("cursor");
      expect(after.loaded).toBe(true);
    });

    it("does not duplicate a message that is both pushed and paged", () => {
      const store = useAssistantStore.getState();
      store.appendMessage(msg({ id: "same" }));
      store.setHistory({ messages: [msg({ id: "same" })] });
      expect(useAssistantStore.getState().messages).toHaveLength(1);
    });

    it("merges an older page under what is held", () => {
      const store = useAssistantStore.getState();
      store.setHistory({
        messages: [msg({ id: "b", createdAt: "2026-01-02T00:00:00.000000000Z" })],
        before: "c1",
      });
      store.prependHistory({
        messages: [msg({ id: "a", createdAt: "2026-01-01T00:00:00.000000000Z" })],
        before: "",
      });
      const after = useAssistantStore.getState();
      expect(after.messages.map((m) => m.id)).toEqual(["a", "b"]);
      expect(after.before).toBe("");
    });
  });

  describe("proposals", () => {
    it("merges by id and takes the later copy", () => {
      const store = useAssistantStore.getState();
      store.applyProposals([proposal()]);
      store.applyProposal(proposal({ status: "accepted", outcome: "merged, fast-forward" }));
      const held = useAssistantStore.getState().proposals;
      expect(held).toHaveLength(1);
      expect(held[0]?.status).toBe("accepted");
      expect(held[0]?.outcome).toBe("merged, fast-forward");
    });

    it("keeps a decided row and drops it from the open list", () => {
      const store = useAssistantStore.getState();
      store.applyProposals([proposal(), proposal({ id: "p2" })]);
      expect(selectAssistantOpenProposals(useAssistantStore.getState())).toHaveLength(2);

      store.applyProposal(proposal({ id: "p2", status: "declined" }));
      const after = useAssistantStore.getState();
      // Still held — the card the reader pressed has to be able to say what
      // happened — and no longer open.
      expect(selectAssistantProposals(after)).toHaveLength(2);
      expect(selectAssistantOpenProposals(after).map((p) => p.id)).toEqual(["p1"]);
    });

    it("orders open first, then newest first", () => {
      useAssistantStore
        .getState()
        .applyProposals([
          proposal({ id: "old-open", createdAt: "2026-01-01T00:00:00Z" }),
          proposal({ id: "new-open", createdAt: "2026-01-03T00:00:00Z" }),
          proposal({ id: "newest-decided", createdAt: "2026-01-04T00:00:00Z", status: "accepted" }),
        ]);
      expect(useAssistantStore.getState().proposals.map((p) => p.id)).toEqual([
        "new-open",
        "old-open",
        "newest-decided",
      ]);
    });

    it("drops a row with no id rather than offering a press nothing can send", () => {
      useAssistantStore.getState().applyProposals([proposal({ id: undefined })]);
      expect(useAssistantStore.getState().proposals).toHaveLength(0);
    });

    it("treats an absent status as decided, not as open", () => {
      useAssistantStore.getState().applyProposals([proposal({ status: undefined })]);
      const after = useAssistantStore.getState();
      expect(after.proposals).toHaveLength(1);
      expect(selectAssistantOpenProposals(after)).toBe(EMPTY_PROPOSALS);
    });

    it("returns the same references when a read brings nothing new", () => {
      const fresh = useAssistantStore.getState();
      expect(selectAssistantProposals(fresh)).toBe(EMPTY_PROPOSALS);
      expect(selectAssistantOpenProposals(fresh)).toBe(EMPTY_PROPOSALS);

      useAssistantStore.getState().applyProposals([proposal()]);
      const all = selectAssistantProposals(useAssistantStore.getState());
      const open = selectAssistantOpenProposals(useAssistantStore.getState());

      useAssistantStore.getState().applyProposals([proposal()]);
      expect(selectAssistantProposals(useAssistantStore.getState())).toBe(all);
      expect(selectAssistantOpenProposals(useAssistantStore.getState())).toBe(open);

      useAssistantStore.getState().applyProposals([]);
      expect(selectAssistantProposals(useAssistantStore.getState())).toBe(all);
      expect(selectAssistantOpenProposals(useAssistantStore.getState())).toBe(open);
    });

    it("keeps the open reference when a decided row arrives beside it", () => {
      const store = useAssistantStore.getState();
      store.applyProposals([proposal()]);
      const open = selectAssistantOpenProposals(useAssistantStore.getState());
      store.applyProposal(proposal({ id: "p9", status: "accepted" }));
      expect(selectAssistantOpenProposals(useAssistantStore.getState())).toBe(open);
    });

    it("is cleared by a reset", () => {
      useAssistantStore.getState().applyProposals([proposal()]);
      useAssistantStore.getState().reset();
      expect(useAssistantStore.getState().proposals).toBe(EMPTY_PROPOSALS);
      expect(useAssistantStore.getState().openProposals).toBe(EMPTY_PROPOSALS);
    });
  });
});

describe("assistant store — policies", () => {
  const policy = (over: Partial<AssistantPolicy> = {}): AssistantPolicy => ({
    id: "pol-1",
    name: "keep the tests green",
    text: "When a session's tests go red, start one to fix them.",
    enabled: true,
    budgetInFlight: 1,
    budgetPerDay: 3,
    ...over,
  });

  beforeEach(() => {
    useAssistantStore.getState().reset();
  });

  it("merges a saved row by id and keeps the list in name order", () => {
    const store = useAssistantStore.getState();
    store.setPolicies([policy({ id: "b", name: "beta" }), policy({ id: "a", name: "alpha" })]);
    expect(selectAssistantPolicies(useAssistantStore.getState()).map((p) => p.id)).toEqual([
      "a",
      "b",
    ]);

    store.applyPolicy(policy({ id: "b", name: "beta", enabled: false }));
    const held = selectAssistantPolicies(useAssistantStore.getState());
    expect(held).toHaveLength(2);
    expect(held.find((p) => p.id === "b")?.enabled).toBe(false);
  });

  it("removes a row the push marks deleted", () => {
    const store = useAssistantStore.getState();
    store.setPolicies([policy(), policy({ id: "pol-2", name: "zeta" })]);
    store.applyPolicy({ id: "pol-1", deleted: true });
    expect(selectAssistantPolicies(useAssistantStore.getState()).map((p) => p.id)).toEqual([
      "pol-2",
    ]);
  });

  it("drops a row with no id rather than drawing controls nothing can name", () => {
    useAssistantStore.getState().applyPolicy({ name: "nameless" });
    expect(selectAssistantPolicies(useAssistantStore.getState())).toBe(EMPTY_POLICIES);
  });

  it("lets a list read drop a row deleted in another tab", () => {
    const store = useAssistantStore.getState();
    store.setPolicies([policy(), policy({ id: "pol-2", name: "zeta" })]);
    store.setPolicies([policy()]);
    expect(selectAssistantPolicies(useAssistantStore.getState()).map((p) => p.id)).toEqual([
      "pol-1",
    ]);
  });

  it("keeps the reference when a read or a push brings nothing new", () => {
    expect(selectAssistantPolicies(useAssistantStore.getState())).toBe(EMPTY_POLICIES);
    useAssistantStore.getState().setPolicies([policy()]);
    const held = selectAssistantPolicies(useAssistantStore.getState());
    useAssistantStore.getState().setPolicies([policy()]);
    expect(selectAssistantPolicies(useAssistantStore.getState())).toBe(held);
    useAssistantStore.getState().applyPolicy(policy());
    expect(selectAssistantPolicies(useAssistantStore.getState())).toBe(held);
  });

  it("is cleared by a reset", () => {
    useAssistantStore.getState().setPolicies([policy()]);
    useAssistantStore.getState().reset();
    expect(useAssistantStore.getState().policies).toBe(EMPTY_POLICIES);
  });
});
