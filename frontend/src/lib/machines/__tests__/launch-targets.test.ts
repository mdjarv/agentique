import { describe, expect, it } from "vitest";
import { launchTargets, matchesLaunchTarget, preferredMember } from "~/lib/machines/launch-targets";
import type { LogicalMemberVM, LogicalProjectVM } from "~/lib/machines/logical-derive";

function member(m: Partial<LogicalMemberVM> & { projectId: string }): LogicalMemberVM {
  return {
    machineLabel: "",
    offline: false,
    path: `/x/${m.projectId}`,
    slug: m.projectId,
    ...m,
  };
}

function row(r: Partial<LogicalProjectVM> & { id: string }): LogicalProjectVM {
  const members = r.members ?? [member({ projectId: r.id })];
  return {
    slug: r.id,
    name: r.id,
    favorite: false,
    color: "",
    icon: "",
    folder: "",
    path: `/x/${r.id}`,
    away: members.every((m) => m.offline),
    spansMachines: members.length > 1,
    members,
    remoteMembers: members.filter((m) => !!m.machineId),
    ...r,
  };
}

describe("launchTargets", () => {
  it("gives a repo on three machines three targets, primary first", () => {
    const targets = launchTargets(
      [
        row({
          id: "p-local",
          name: "Agentique",
          slug: "agentique",
          members: [
            member({ projectId: "p-local", slug: "agentique" }),
            member({
              projectId: "p-zbook",
              slug: "agentique-zbook",
              machineId: "m1",
              machineLabel: "zbook",
            }),
            member({
              projectId: "p-vps",
              slug: "agentique-vps",
              machineId: "m2",
              machineLabel: "VPS",
              offline: true,
            }),
          ],
        }),
      ],
      "This machine",
    );

    expect(targets.map((t) => t.projectId)).toEqual(["p-local", "p-zbook", "p-vps"]);
    // The primary's label comes from the caller; the member VM leaves it blank.
    expect(targets[0]?.machineLabel).toBe("This machine");
    expect(targets[2]?.offline).toBe(true);
  });

  it("keeps the routing slug and the presentation slug apart", () => {
    const [, remote] = launchTargets(
      [
        row({
          id: "p-local",
          slug: "agentique",
          members: [
            member({ projectId: "p-local", slug: "agentique" }),
            member({
              projectId: "p-zbook",
              slug: "agentique-zbook",
              machineId: "m1",
              machineLabel: "zbook",
            }),
          ],
        }),
      ],
      "This machine",
    );

    expect(remote?.slug).toBe("agentique-zbook");
    expect(remote?.rowSlug).toBe("agentique");
  });

  it("orders favorites first, then by name", () => {
    const targets = launchTargets(
      [
        row({ id: "zebra", name: "Zebra" }),
        row({ id: "apple", name: "Apple" }),
        row({ id: "starred", name: "Starred", favorite: true }),
      ],
      "This machine",
    );
    expect(targets.map((t) => t.name)).toEqual(["Starred", "Apple", "Zebra"]);
  });
});

describe("preferredMember", () => {
  it("prefers a reachable checkout over the representative", () => {
    const r = row({
      id: "p-a",
      members: [
        member({ projectId: "p-a", machineId: "m1", machineLabel: "zbook", offline: true }),
        member({ projectId: "p-b", machineId: "m2", machineLabel: "VPS" }),
      ],
    });
    expect(preferredMember(r)?.projectId).toBe("p-b");
  });

  it("falls back to the representative when every machine is away", () => {
    const r = row({
      id: "p-a",
      members: [
        member({ projectId: "p-a", machineId: "m1", machineLabel: "zbook", offline: true }),
        member({ projectId: "p-b", machineId: "m2", machineLabel: "VPS", offline: true }),
      ],
    });
    expect(preferredMember(r)?.projectId).toBe("p-a");
  });
});

describe("matchesLaunchTarget", () => {
  const [local, remote] = launchTargets(
    [
      row({
        id: "p-local",
        name: "Agentique",
        slug: "agentique",
        members: [
          member({ projectId: "p-local", slug: "agentique", path: "/home/me/git/agentique" }),
          member({
            projectId: "p-zbook",
            slug: "agentique-zbook",
            machineId: "m1",
            machineLabel: "zbook",
            path: "/home/me/src/agentique",
          }),
        ],
      }),
    ],
    "This machine",
  );

  it("matches everything on an empty query", () => {
    expect(local && matchesLaunchTarget(local, "  ")).toBe(true);
  });

  it("narrows to one machine when the query names it", () => {
    expect(remote && matchesLaunchTarget(remote, "agentique zbook")).toBe(true);
    expect(local && matchesLaunchTarget(local, "agentique zbook")).toBe(false);
  });

  it("matches on path", () => {
    expect(local && matchesLaunchTarget(local, "git/agentique")).toBe(true);
  });
});
