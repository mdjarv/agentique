/**
 * The card is where the yes lives, so what it has to get right is the reading:
 * the verb in words, the target in its project, the server's facts quoted as
 * facts, and — once pressed — what actually happened, IN PLACE. A card that
 * vanished on a press would take `stale` with it, and `stale` means nothing was
 * done.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProposalCard } from "~/components/assistant/ProposalCard";
import type { AssistantProposal } from "~/lib/assistant/wire";
import { useAssistantStore } from "~/stores/assistant-store";

const decide = vi.fn();

vi.mock("~/hooks/useWebSocket", () => ({ useWebSocket: () => ({}) }));
vi.mock("~/lib/assistant/rpc", () => ({
  decide: (...args: unknown[]) => decide(...args),
}));

function proposal(over: Partial<AssistantProposal> = {}): AssistantProposal {
  return {
    id: "p1",
    verb: "merge_session",
    sessionId: "sess-1",
    sessionName: "fix the flaky test",
    projectName: "agentique",
    rationale: "it is two commits ahead and the tests passed",
    evidence: { ahead: 2, behind: 0, dirty: false, mergeStatus: "clean", busy: false },
    status: "open",
    ...over,
  };
}

beforeEach(() => {
  useAssistantStore.getState().reset();
  decide.mockReset();
  decide.mockResolvedValue(proposal({ status: "accepted", outcome: "merged, fast-forward" }));
});

afterEach(cleanup);

describe("ProposalCard", () => {
  it("names the verb in words, the target in its project, and quotes the reason", () => {
    render(<ProposalCard proposal={proposal()} />);
    expect(screen.getByRole("heading", { name: "Merge the branch" })).toBeTruthy();
    expect(screen.getByText(/fix the flaky test · agentique/)).toBeTruthy();
    expect(screen.getByText(/two commits ahead and the tests passed/)).toBeTruthy();
  });

  it("quotes the evidence as facts, in the server's own terms", () => {
    render(<ProposalCard proposal={proposal()} />);
    expect(screen.getByText("ahead")).toBeTruthy();
    expect(screen.getByText("2 commits")).toBeTruthy();
    expect(screen.getByText("behind")).toBeTruthy();
    expect(screen.getByText("nothing uncommitted")).toBeTruthy();
    expect(screen.getByText("clean")).toBeTruthy();
    expect(screen.getByText("no turn in flight")).toBeTruthy();
  });

  it("renders a channel proposal's own facts and marks it irreversible", () => {
    render(
      <ProposalCard
        proposal={proposal({
          verb: "dissolve_channel",
          sessionId: "",
          channelId: "chan-1",
          args: { channel: "the release crew", keep_history: true },
          evidence: { members: 3, busy: 0, channel: "the release crew" },
        })}
      />,
    );
    expect(screen.getByRole("heading", { name: "Dissolve the channel" })).toBeTruthy();
    expect(screen.getByText(/the release crew · agentique/)).toBeTruthy();
    expect(screen.getByText("3")).toBeTruthy();
    expect(screen.getByText("irreversible")).toBeTruthy();
  });

  it("accepts through the op and renders the outcome in place", async () => {
    const onDecided = vi.fn();
    render(<ProposalCard proposal={proposal()} onDecided={onDecided} />);
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));

    expect(decide).toHaveBeenCalledWith({}, "p1", true);
    await waitFor(() => expect(onDecided).toHaveBeenCalledWith("p1"));
    // The answer goes into the store, so every surface reads the same row.
    expect(useAssistantStore.getState().proposals[0]?.status).toBe("accepted");
  });

  it("declines through the same op with accept false", async () => {
    decide.mockResolvedValue(proposal({ status: "declined" }));
    render(<ProposalCard proposal={proposal()} />);
    fireEvent.click(screen.getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(decide).toHaveBeenCalledWith({}, "p1", false));
  });

  it("says nothing was done when the facts had moved, and offers no buttons", () => {
    render(
      <ProposalCard
        proposal={proposal({ status: "stale", outcome: "the branch is 2 commits behind" })}
      />,
    );
    expect(screen.getByText(/The facts had moved, so nothing was done/)).toBeTruthy();
    expect(screen.getByText(/the branch is 2 commits behind/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Accept" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Decline" })).toBeNull();
  });

  it("separates a failure from a staleness", () => {
    render(<ProposalCard proposal={proposal({ status: "failed", outcome: "conflict" })} />);
    expect(screen.getByText(/It was attempted and failed/)).toBeTruthy();
  });

  it("names a verb this build has never heard of rather than showing a blank", () => {
    render(<ProposalCard proposal={proposal({ verb: "set_session_priority" })} />);
    expect(screen.getByRole("heading", { name: "set session priority" })).toBeTruthy();
  });
});
