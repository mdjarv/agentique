/**
 * A session the assistant started says so on the row.
 *
 * It is the one fact about a row nothing else can hint at — the name, the
 * project, the state and the hue all read identically either way — and it is
 * asserted at REST as well as running, because a mark that only showed while a
 * session was working would be gone at exactly the moment somebody wonders
 * where the session came from.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ThreadRow } from "~/components/layout/thread-sidebar/ThreadRow";
import type { ThreadRowVM } from "~/components/layout/thread-sidebar/types";

afterEach(cleanup);

function vm(over: Partial<ThreadRowVM> = {}): ThreadRowVM {
  return {
    sessionId: "s-1",
    name: "Fix the overnight test failures",
    untitled: false,
    depth: 0,
    projectSlug: "agentique",
    projectLabel: "agentique",
    projectInitials: "AG",
    workspace: "linked",
    originAssistant: false,
    projectColorBg: "#73daca",
    projectColorFg: "#73daca",
    badge: null,
    awake: false,
    hued: true,
    live: false,
    restToken: "stopped",
    parked: true,
    timeLabel: "31m",
    struck: false,
    unread: false,
    pinned: false,
    archived: false,
    canArchive: true,
    lastActivity: 0,
    ...over,
  };
}

function draw(over: Partial<ThreadRowVM> = {}) {
  render(
    <ThreadRow
      vm={vm(over)}
      selected={false}
      onClick={vi.fn()}
      onTogglePin={vi.fn()}
      onArchive={vi.fn()}
    />,
  );
}

describe("ThreadRow origin", () => {
  it("says '· assistant' and names it in the aria-label, at rest", () => {
    draw({ originAssistant: true });
    expect(screen.getByText("· assistant")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /started by the assistant/ })).toBeInTheDocument();
  });

  it("keeps the mark beside a live phrase", () => {
    draw({
      originAssistant: true,
      awake: true,
      badge: "working",
      live: true,
      livePhrase: { text: "editing service.go", tone: "work" },
    });
    expect(screen.getByText("editing service.go")).toBeInTheDocument();
    expect(screen.getByText("· assistant")).toBeInTheDocument();
  });

  it("says nothing at all for a session a person started", () => {
    draw();
    expect(screen.queryByText("· assistant")).toBeNull();
    expect(screen.queryByRole("button", { name: /started by the assistant/ })).toBeNull();
  });
});
