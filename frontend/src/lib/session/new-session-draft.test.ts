import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUIStore } from "~/stores/ui-store";
import { newSessionDraftKey, openPrefilledNewSession } from "./new-session-draft";

type NavigateArg = Parameters<typeof openPrefilledNewSession>[0];

describe("openPrefilledNewSession", () => {
  beforeEach(() => {
    useUIStore.setState({ drafts: {} });
  });

  it("sets the new-session draft and routes to the target project's new-session view", () => {
    const navigate = vi.fn();
    openPrefilledNewSession(navigate as unknown as NavigateArg, {
      projectId: "p1",
      projectSlug: "proj-one",
      text: "echo hi",
    });

    expect(useUIStore.getState().drafts[newSessionDraftKey("p1")]).toBe("echo hi");
    expect(navigate).toHaveBeenCalledWith({
      to: "/project/$projectSlug/session/new",
      params: { projectSlug: "proj-one" },
    });
  });

  it("appends to an existing draft rather than clobbering an in-progress prompt", () => {
    useUIStore.getState().setDraft(newSessionDraftKey("p1"), "existing prompt");
    const navigate = vi.fn();
    openPrefilledNewSession(navigate as unknown as NavigateArg, {
      projectId: "p1",
      projectSlug: "proj-one",
      text: "second block",
    });

    expect(useUIStore.getState().drafts[newSessionDraftKey("p1")]).toBe(
      "existing prompt\n\nsecond block",
    );
  });

  it("treats a whitespace-only existing draft as empty (no separator)", () => {
    useUIStore.getState().setDraft(newSessionDraftKey("p1"), "   \n  ");
    const navigate = vi.fn();
    openPrefilledNewSession(navigate as unknown as NavigateArg, {
      projectId: "p1",
      projectSlug: "proj-one",
      text: "block",
    });

    expect(useUIStore.getState().drafts[newSessionDraftKey("p1")]).toBe("block");
  });

  it("targets another project by slug for a cross-project hand-off", () => {
    const navigate = vi.fn();
    openPrefilledNewSession(navigate as unknown as NavigateArg, {
      projectId: "p2",
      projectSlug: "other-repo",
      text: "spec for the other repo",
    });

    expect(useUIStore.getState().drafts[newSessionDraftKey("p2")]).toBe("spec for the other repo");
    expect(navigate).toHaveBeenCalledWith({
      to: "/project/$projectSlug/session/new",
      params: { projectSlug: "other-repo" },
    });
  });
});
