import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectLaunchPicker } from "~/components/projects/ProjectLaunchPicker";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";

const REPO = "github.com/org/agentique";

function project(p: Partial<Project> & { id: string; slug: string }): Project {
  return {
    name: p.slug,
    path: `/x/${p.slug}`,
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    created_at: "",
    updated_at: "",
    sort_order: 0,
    default_behavior_presets: "",
    favorite: 0,
    color: "",
    icon: "",
    folder: "",
    max_sessions: 0,
    pinned: 0,
    remote_url: "",
    ...p,
  } as Project;
}

/** jsdom has neither of these; Radix's positioning and the row's
 *  keep-me-in-view both reach for them. */
function installBrowserMocks() {
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView ??= vi.fn();
  Element.prototype.hasPointerCapture ??= () => false;
}

function seed({ zbookOffline = false }: { zbookOffline?: boolean } = {}) {
  useAppStore.setState({
    projects: [
      project({ id: "p-local", slug: "agentique", name: "Agentique", remote_url: REPO }),
      project({
        id: "p-zbook",
        slug: "agentique-zbook",
        name: "agentique",
        remote_url: REPO,
        machineId: "m1",
      }),
      project({ id: "p-solo", slug: "hittat", name: "Hittat" }),
    ],
  });
  useMachineStore.setState({
    machines: {
      m1: {
        machineId: "m1",
        label: "zbook",
        baseUrl: "https://zbook.example",
        token: "",
        sessionId: "",
        identityKey: "",
        addedAt: "",
        icon: "laptop",
      },
    },
    statuses: { m1: zbookOffline ? "disconnected" : "connected" },
  });
  useFeatureStore.setState({ machineLabel: "desktop" });
}

function open(onPick = vi.fn()) {
  render(
    <ProjectLaunchPicker targetProjectId="p-local" onPick={onPick}>
      <button type="button">open</button>
    </ProjectLaunchPicker>,
  );
  fireEvent.click(screen.getByText("open"));
  return onPick;
}

describe("ProjectLaunchPicker", () => {
  beforeEach(() => {
    installBrowserMocks();
    seed();
  });
  afterEach(cleanup);

  it("lists one row per checkout, naming the machine only where there is a choice", () => {
    open();
    // The repo on two machines contributes two rows; the machine is named on
    // both. The single-machine repo names none — nothing to choose.
    expect(screen.getAllByText("Agentique")).toHaveLength(2);
    expect(screen.getByText("desktop")).toBeInTheDocument();
    expect(screen.getByText("zbook")).toBeInTheDocument();
    expect(screen.getByText("Hittat")).toBeInTheDocument();
  });

  it("picks the physical checkout, not the repo", () => {
    const onPick = open();
    fireEvent.click(screen.getByText("zbook"));
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick.mock.calls[0]?.[0]).toMatchObject({
      projectId: "p-zbook",
      slug: "agentique-zbook",
      // Presentation still comes from the representative.
      rowSlug: "agentique",
    });
  });

  it("searches across repo and machine together", () => {
    open();
    fireEvent.change(screen.getByPlaceholderText("Start in…"), {
      target: { value: "agentique zbook" },
    });
    expect(screen.getAllByText("Agentique")).toHaveLength(1);
    expect(screen.queryByText("Hittat")).not.toBeInTheDocument();
  });

  it("refuses a checkout whose machine is away", () => {
    cleanup();
    seed({ zbookOffline: true });
    const onPick = open();
    const row = screen.getByText("zbook").closest("button");
    expect(row).toBeDisabled();
    fireEvent.click(screen.getByText("zbook"));
    expect(onPick).not.toHaveBeenCalled();
  });
});
