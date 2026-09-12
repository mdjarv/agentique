import { beforeEach, describe, expect, it } from "vitest";
import type { AssistantJournalEntry, AssistantMessage } from "~/lib/assistant/wire";
import {
  EMPTY_JOURNAL,
  EMPTY_MESSAGES,
  selectAssistantJournal,
  selectAssistantMessages,
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
});
