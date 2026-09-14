import { describe, expect, it } from "vitest";
import { buildTimeline, type TimelineItem } from "~/lib/assistant/timeline";
import type {
  AssistantJournalEntry,
  AssistantMessage,
  AssistantProposal,
} from "~/lib/assistant/wire";

const msg = (id: string, at: string, extra: Partial<AssistantMessage> = {}): AssistantMessage => ({
  id,
  role: "assistant",
  text: id,
  createdAt: at,
  ...extra,
});

let nextId = 1;
const entry = (at: string, extra: Partial<AssistantJournalEntry> = {}): AssistantJournalEntry => ({
  id: nextId++,
  kind: "session_finished",
  sessionId: "s1",
  at,
  ...extra,
});

function shape(items: TimelineItem[]): string[] {
  return items.map((item) => {
    switch (item.type) {
      case "message":
        return `m:${item.message.id}`;
      case "event":
        return `e:${item.entry.at?.slice(11, 16)}`;
      case "fold":
        return `fold:${item.entries.length}`;
      case "digest":
        return `digest:${item.entries.length}`;
      case "proposal":
        return `p:${item.proposal.id}`;
      default:
        return item.type;
    }
  });
}

const build = (
  messages: AssistantMessage[],
  journal: AssistantJournalEntry[],
  proposals: AssistantProposal[] = [],
  unseenOnArrival = 0,
) => buildTimeline({ messages, journal, proposals, unseenOnArrival });

describe("buildTimeline", () => {
  // The conversation's stamps carry nanoseconds and the journal's do not, so a
  // string comparison would put 10:00:00.5Z after 10:00:01Z.
  it("orders by parsed time across the two stamp formats", () => {
    const items = build(
      [
        msg("late", "2026-09-14T10:02:00.500000000Z"),
        msg("early", "2026-09-14T10:00:00.500000000Z"),
      ].sort((a, b) => (a.createdAt ?? "").localeCompare(b.createdAt ?? "")),
      [entry("2026-09-14T10:01:00Z")],
    );
    expect(shape(items)).toEqual(["m:early", "e:10:01", "m:late"]);
  });

  it("folds a run of three or more updates, and leaves a shorter run alone", () => {
    const items = build(
      [msg("a", "2026-09-14T10:00:00Z"), msg("b", "2026-09-14T11:00:00Z")],
      [
        entry("2026-09-14T10:10:00Z"),
        entry("2026-09-14T10:20:00Z"),
        entry("2026-09-14T10:30:00Z"),
        entry("2026-09-14T11:10:00Z"),
        entry("2026-09-14T11:20:00Z"),
      ],
    );
    expect(shape(items)).toEqual(["m:a", "fold:3", "m:b", "e:11:10", "e:11:20"]);
  });

  // The visit exists to show what was missed; hiding it behind a fold defeats it.
  it("never folds what arrived after the reader last looked", () => {
    const journal = [
      entry("2026-09-14T09:00:00Z"),
      entry("2026-09-14T09:10:00Z"),
      entry("2026-09-14T09:20:00Z"),
      entry("2026-09-14T12:00:00Z"),
      entry("2026-09-14T12:10:00Z"),
      entry("2026-09-14T12:20:00Z"),
    ];
    const items = build([msg("a", "2026-09-14T08:00:00Z")], journal, [], 3);
    expect(shape(items)).toEqual(["m:a", "fold:3", "unseen", "e:12:00", "e:12:10", "e:12:20"]);
  });

  // A busy afternoon past the last turn is one fold and the newest few lines,
  // not a screen of lines and not a single closed fold hiding what is current.
  it("keeps the newest updates open at the end and folds what is ahead of them", () => {
    const journal = Array.from({ length: 8 }, (_, i) => entry(`2026-09-14T1${i}:00:00Z`));
    const items = build([msg("a", "2026-09-14T09:00:00Z")], journal);
    expect(shape(items)).toEqual([
      "m:a",
      "fold:3",
      "e:13:00",
      "e:14:00",
      "e:15:00",
      "e:16:00",
      "e:17:00",
    ]);
  });

  it("counts the divider over the server's attention rows, not the rendered ones", () => {
    const journal = [
      entry("2026-09-14T09:00:00Z"),
      // Not news, and not counted by the server either.
      entry("2026-09-14T09:30:00Z", { kind: "heartbeat" }),
      entry("2026-09-14T10:00:00Z"),
    ];
    const items = build([], journal, [], 1);
    expect(shape(items)).toEqual(["e:09:00", "unseen", "e:10:00"]);
  });

  it("puts the updates a digest retells under the digest", () => {
    const items = build(
      [
        msg("hello", "2026-09-14T08:00:00Z", { role: "user" }),
        msg("d1", "2026-09-14T09:00:00Z", { kind: "digest" }),
        msg("d2", "2026-09-14T12:00:00Z", { kind: "digest" }),
      ],
      [
        entry("2026-09-14T07:00:00Z"),
        entry("2026-09-14T08:30:00Z"),
        entry("2026-09-14T10:00:00Z"),
        entry("2026-09-14T13:00:00Z"),
      ],
    );
    // A message between two updates stays where it was; only update rows move.
    expect(shape(items)).toEqual(["m:hello", "digest:2", "digest:1", "e:13:00"]);
  });

  it("lets a held proposal's card stand for its journal rows", () => {
    const proposal: AssistantProposal = {
      id: "p1",
      verb: "merge_session",
      status: "accepted",
      createdAt: "2026-09-14T10:00:00Z",
    };
    const items = build(
      [],
      [
        entry("2026-09-14T10:00:00Z", { kind: "proposal_made", payload: { proposalId: "p1" } }),
        entry("2026-09-14T10:05:00Z", { kind: "proposal_decided", payload: { proposalId: "p1" } }),
        // One the client does not hold still says what happened.
        entry("2026-09-14T10:06:00Z", { kind: "proposal_made", payload: { proposalId: "gone" } }),
      ],
      [proposal],
    );
    expect(shape(items)).toEqual(["p:p1", "e:10:06"]);
  });

  it("leaves out bookkeeping and folded days", () => {
    const items = build(
      [msg("a", "2026-09-14T10:00:00Z")],
      [
        entry("2026-09-14T10:01:00Z", { kind: "heartbeat" }),
        entry("2026-08-20T00:00:00Z", { kind: "day_summary" }),
      ],
    );
    expect(shape(items)).toEqual(["m:a"]);
  });
});
