import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RunBlockButton } from "~/components/chat/RunBlockButton";
import type { Project } from "~/lib/generated-types";
import { newSessionDraftKey } from "~/lib/session/new-session-draft";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateMock,
}));

function project(id: string, slug: string, name: string): Project {
  return { id, slug, name } as unknown as Project;
}

beforeEach(() => {
  navigateMock.mockReset();
  useAppStore.setState({ projects: [] });
  useUIStore.setState({ drafts: {} });
  useChatStore.setState({ activeSessionId: null });
});

afterEach(cleanup);

describe("RunBlockButton", () => {
  it("renders nothing when there are no projects", () => {
    const { container } = render(<RunBlockButton code="echo hi" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for a blank block", () => {
    useAppStore.setState({ projects: [project("p1", "one", "One")] });
    const { container } = render(<RunBlockButton code={"   \n  "} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("is a one-click button with a single project, prefilling and navigating", () => {
    useAppStore.setState({ projects: [project("p1", "one", "One")] });
    render(<RunBlockButton code="echo hi" />);

    const btn = screen.getByRole("button", { name: "Run as new session" });
    fireEvent.click(btn);

    expect(useUIStore.getState().drafts[newSessionDraftKey("p1")]).toBe("echo hi");
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/project/$projectSlug/session/new",
      params: { projectSlug: "one" },
    });
  });

  it("renders a dropdown trigger when multiple projects exist", () => {
    useAppStore.setState({
      projects: [project("p1", "one", "One"), project("p2", "two", "Two")],
    });
    render(<RunBlockButton code="echo hi" />);

    // The trigger is present; individual project targets live inside the menu.
    expect(screen.getByRole("button", { name: "Run as new session" })).toBeInTheDocument();
  });
});
