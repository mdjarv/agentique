import { describe, expect, it } from "vitest";
import type { Project } from "~/lib/types";
import { siblingCheckouts } from "../sync-sweep";

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: "p-1",
    name: "Agentique",
    path: "/repo",
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    created_at: "",
    updated_at: "",
    slug: "agentique",
    sort_order: 0,
    default_behavior_presets: "",
    favorite: 0,
    color: "",
    icon: "",
    folder: "",
    max_sessions: 0,
    pinned: 0,
    remote_url: "github.com/org/agentique",
    ...overrides,
  };
}

describe("siblingCheckouts", () => {
  const local = project();
  const remote = project({ id: "p-1-zbook", slug: "agentique~ad3e932", machineId: "m-z" });
  const other = project({ id: "p-2", slug: "agentkit", remote_url: "github.com/org/agentkit" });

  it("finds the same repo's other checkouts by canonical remote", () => {
    expect(siblingCheckouts([local, remote, other], "p-1").map((p) => p.id)).toEqual(["p-1-zbook"]);
    expect(siblingCheckouts([local, remote, other], "p-1-zbook").map((p) => p.id)).toEqual(["p-1"]);
  });

  it("never returns the pushed checkout itself", () => {
    expect(siblingCheckouts([local], "p-1")).toEqual([]);
  });

  it("groups nothing when the project has no remote — a local-only repo can't drift elsewhere", () => {
    const noRemote = project({ id: "p-3", remote_url: "" });
    const alsoNoRemote = project({ id: "p-4", slug: "other", remote_url: "" });
    expect(siblingCheckouts([noRemote, alsoNoRemote], "p-3")).toEqual([]);
  });

  it("is empty for an unknown project id", () => {
    expect(siblingCheckouts([local, remote], "nope")).toEqual([]);
  });
});
