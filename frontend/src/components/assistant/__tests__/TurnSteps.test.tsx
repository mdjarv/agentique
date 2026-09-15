/**
 * A turn's working renders the session transcript's way: one folded line
 * counting steps and thoughts, opening onto a row per step. The assertions are
 * about what a reader relies on when a reply went wrong — the refusal's own
 * sentence, the facts a recall returned, and while a turn runs, the verb it is
 * waiting on.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantConversation } from "~/components/assistant/AssistantConversation";
import { buildTimeline } from "~/lib/assistant/timeline";
import type { AssistantMessage, AssistantStep } from "~/lib/assistant/wire";

vi.mock("~/components/chat/Markdown", () => ({
  Markdown: ({ content }: { content: string }) => <p>{content}</p>,
}));

vi.mock("~/components/assistant/use-session-label", () => ({
  useSessionLabel: (id?: string) => (id ? "Fix the loader" : undefined),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a href="/assistant/memory">{children}</a>,
}));

afterEach(cleanup);

const STEPS: AssistantStep[] = [
  { seq: 1, kind: "thought", status: "done", encrypted: true },
  {
    seq: 2,
    kind: "verb",
    verb: "recall",
    detail: "release process",
    status: "done",
    outcome: "1 fact",
    durationMs: 140,
    facts: [{ id: "f1", scope: "global", text: "Tag first, then the GitHub release" }],
  },
  {
    seq: 3,
    kind: "verb",
    verb: "summarize_session",
    sessionId: "s-1",
    status: "refused",
    outcome: "That session is on a machine that is asleep.",
  },
];

const REPLY: AssistantMessage = {
  id: "m1",
  role: "assistant",
  text: "Tag first.",
  createdAt: "2026-09-15T08:00:00.000000000Z",
  steps: STEPS,
};

function items(messages: AssistantMessage[]) {
  return buildTimeline({ messages, journal: [], proposals: [], unseenOnArrival: 0 });
}

describe("TurnSteps in the conversation", () => {
  it("folds a reply's working into one counted line above the bubble", () => {
    render(<AssistantConversation items={items([REPLY])} streaming={null} />);
    expect(screen.getByText("2 steps, 1 thought")).toBeInTheDocument();
    expect(screen.getByText("Tag first.")).toBeInTheDocument();
    // Folded: the rows are not drawn until asked for.
    expect(screen.queryByText("release process")).toBeNull();
  });

  it("opens onto each step: the thought, the recall's facts, the refusal's sentence", () => {
    render(<AssistantConversation items={items([REPLY])} streaming={null} />);
    fireEvent.click(screen.getByText("2 steps, 1 thought"));

    expect(screen.getByText("Thinking (encrypted by the model)")).toBeInTheDocument();
    expect(screen.getByText("release process")).toBeInTheDocument();
    expect(screen.getByText("1 fact · 140ms")).toBeInTheDocument();
    expect(screen.getByTitle("Tag first, then the GitHub release")).toBeInTheDocument();
    // A verb about a session names it rather than printing its id.
    expect(screen.getByText("Fix the loader")).toBeInTheDocument();
    expect(screen.getByText("That session is on a machine that is asleep.")).toBeInTheDocument();
  });

  it("says how many steps the turn took beyond what it kept", () => {
    render(
      <AssistantConversation
        items={items([{ ...REPLY, steps: STEPS.slice(1, 2), stepsOmitted: 3 }])}
        streaming={null}
      />,
    );
    expect(screen.getByText("4 steps")).toBeInTheDocument();
    fireEvent.click(screen.getByText("4 steps"));
    expect(screen.getByText("3 more steps not kept")).toBeInTheDocument();
  });

  it("names the verb a running turn is waiting on instead of saying only Thinking", () => {
    render(
      <AssistantConversation
        items={items([])}
        streaming=""
        liveSteps={[
          { seq: 1, kind: "verb", verb: "list_sessions", detail: "running", status: "running" },
        ]}
      />,
    );
    expect(screen.getByText("list_sessions")).toBeInTheDocument();
    expect(screen.getByText("Working…")).toBeInTheDocument();
    expect(screen.queryByText("Thinking…")).toBeNull();
  });

  it("draws no working line for a reply whose turn did nothing", () => {
    render(
      <AssistantConversation items={items([{ ...REPLY, steps: undefined }])} streaming={null} />,
    );
    expect(screen.queryByText(/steps?|thoughts?/)).toBeNull();
  });

  it("keeps a silent heartbeat turn's working under its divider", () => {
    const woke: AssistantMessage = {
      id: "w1",
      role: "system",
      kind: "heartbeat",
      text: "The heartbeat woke the assistant.",
      createdAt: "2026-09-15T08:00:00.000000000Z",
      steps: STEPS.slice(1, 2),
    };
    render(<AssistantConversation items={items([woke])} streaming={null} />);
    expect(screen.getByText("The heartbeat woke the assistant.")).toBeInTheDocument();
    expect(screen.getByText("1 step")).toBeInTheDocument();
  });
});
