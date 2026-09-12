/**
 * The deck's band is ranked, and the ranking is the whole of what this file
 * asserts: a proposal comes after the two kinds that hold a live process idling
 * on an answer, and before an unread completion that holds nothing but
 * curiosity.
 *
 * `needsYou` is deliberately not involved in the proposal row — it answers for
 * a SESSION, and a proposal is a row in the assistant's own table that can be
 * about a channel instead.
 */
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  compareDeckRows,
  type DeckRow,
  deckRowKey,
  KIND_RANK,
  useDeckRows,
} from "~/components/home/use-deck-rows";
import type { AssistantProposal } from "~/lib/assistant/wire";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useAssistantStore } from "~/stores/assistant-store";
import type { SessionData } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

vi.mock("~/hooks/useTheme", () => ({ useTheme: () => ({ resolvedTheme: "dark" }) }));

const PROJECT = {
  id: "proj-1",
  slug: "agentique",
  name: "Agentique",
  path: "/repo",
  color: "",
} as unknown as Project;

function session(over: Record<string, unknown>): SessionData {
  const { id, ...rest } = over as { id: string } & Record<string, unknown>;
  return {
    meta: {
      id,
      projectId: "proj-1",
      name: `session ${id}`,
      state: "idle",
      updatedAt: "2026-01-01T00:00:00Z",
      ...(rest.meta as object | undefined),
    },
    turns: [],
    streamingEvents: [],
    hasUnseenCompletion: false,
    hasUnreadChannelMessage: false,
    pendingApproval: null,
    pendingQuestion: null,
    ...rest,
  } as unknown as SessionData;
}

function proposal(over: Partial<AssistantProposal> = {}): AssistantProposal {
  return {
    id: "p1",
    createdAt: "2026-01-01T00:00:00Z",
    verb: "merge_session",
    sessionId: "s-merge",
    projectId: "proj-1",
    rationale: "two commits ahead, tests green",
    status: "open",
    ...over,
  };
}

function row(over: Partial<DeckRow>): DeckRow {
  return {
    sessionId: "s",
    name: "",
    projectSlug: "agentique",
    projectLabel: "Agentique",
    projectColorFg: "#fff",
    kind: "unread",
    summary: "",
    restToken: "",
    lastActivity: 0,
    ...over,
  };
}

beforeEach(() => {
  useAssistantStore.getState().reset();
  useAppStore.setState({ projects: [PROJECT] });
  useChatStore.setState({ sessions: {} });
});

describe("deck ranking", () => {
  it("puts a proposal after a question and before an unread completion", () => {
    expect(KIND_RANK.approval).toBeLessThan(KIND_RANK.question);
    expect(KIND_RANK.question).toBeLessThan(KIND_RANK.proposal);
    expect(KIND_RANK.proposal).toBeLessThan(KIND_RANK.unread);
  });

  it("sorts a mixed band by kind first", () => {
    const rows = [
      row({ sessionId: "u", kind: "unread" }),
      row({ sessionId: "p", kind: "proposal", proposalId: "p1" }),
      row({ sessionId: "a", kind: "approval" }),
      row({ sessionId: "q", kind: "question" }),
    ];
    rows.sort(compareDeckRows);
    expect(rows.map((r) => r.kind)).toEqual(["approval", "question", "proposal", "unread"]);
  });

  it("keys a proposal row by its proposal, so one session can carry several", () => {
    expect(deckRowKey(row({ sessionId: "s1", kind: "proposal", proposalId: "p1" }))).toBe(
      "proposal:p1",
    );
    expect(deckRowKey(row({ sessionId: "s1" }))).toBe("session:s1");
  });
});

describe("useDeckRows", () => {
  it("lists an open proposal in the needs band, ranked between question and unread", () => {
    useChatStore.setState({
      sessions: {
        "s-approve": session({
          id: "s-approve",
          pendingApproval: { approvalId: "a1", toolName: "Bash", input: { command: "rm -rf x" } },
        }),
        "s-unread": session({ id: "s-unread", hasUnseenCompletion: true }),
      },
    });
    useAssistantStore.getState().applyProposals([proposal()]);

    const { result } = renderHook(() => useDeckRows());
    expect(result.current.needs.map((r) => r.kind)).toEqual(["approval", "proposal", "unread"]);

    const card = result.current.needs[1];
    expect(card?.proposalId).toBe("p1");
    expect(card?.verb).toBe("merge_session");
    // The reason rides the row as its summary; the card quotes it.
    expect(card?.summary).toBe("two commits ahead, tests green");
    expect(card?.projectLabel).toBe("Agentique");
  });

  it("names the session from the live list, not from the row's own copy", () => {
    useChatStore.setState({
      sessions: { "s-merge": session({ id: "s-merge", meta: { name: "renamed since" } }) },
    });
    useAssistantStore.getState().applyProposals([proposal({ sessionName: "the old name" })]);
    const { result } = renderHook(() => useDeckRows());
    expect(result.current.needs[0]?.name).toBe("renamed since");
  });

  it("lists a channel proposal with no session and no project row", () => {
    useAssistantStore.getState().applyProposals([
      proposal({
        verb: "dissolve_channel",
        sessionId: "",
        projectId: "",
        channelId: "chan-1",
        args: { channel: "the release crew" },
      }),
    ]);
    const { result } = renderHook(() => useDeckRows());
    const card = result.current.needs[0];
    expect(card?.kind).toBe("proposal");
    expect(card?.name).toBe("the release crew");
    // Nothing to open and no hue to be wrong about.
    expect(card?.projectSlug).toBe("");
    expect(card?.projectColorFg).toBe("");
  });

  it("keeps a session's own row and its proposal apart", () => {
    useChatStore.setState({
      sessions: { "s-merge": session({ id: "s-merge", hasUnseenCompletion: true }) },
    });
    useAssistantStore.getState().applyProposals([proposal()]);
    const { result } = renderHook(() => useDeckRows());
    // Two claims about one session: "you have not read the outcome" and "this
    // is waiting for your yes". Collapsing them would hide the decision.
    expect(result.current.needs.map((r) => r.kind)).toEqual(["proposal", "unread"]);
  });

  it("lists nothing for a decided proposal", () => {
    useAssistantStore.getState().applyProposals([proposal({ status: "accepted" })]);
    const { result } = renderHook(() => useDeckRows());
    expect(result.current.needs).toHaveLength(0);
  });
});
