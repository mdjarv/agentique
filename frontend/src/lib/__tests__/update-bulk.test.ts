import { describe, expect, it } from "vitest";
import type { UpdateSourceStatus, UpdateStatus } from "~/lib/generated-types";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { type BulkCandidate, planUpgradeAll, runUpgradeAll } from "~/lib/update-bulk";

function status(over: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    current: "v0.6.0",
    latest: "v0.7.0",
    behind: true,
    channel: "stable",
    asset: "agentique-linux-amd64",
    supported: true,
    platform: "linux/amd64",
    checkedAt: "",
    installable: true,
    busy: false,
    busyTurns: 0,
    ...over,
  };
}

function src(over: Partial<UpdateSourceStatus> = {}): UpdateSourceStatus {
  return {
    dir: "/home/u/git/agentique",
    branch: "master",
    ahead: 0,
    behind: false,
    dirty: false,
    staged: false,
    buildable: false,
    origin: "local",
    ...over,
  };
}

function machine(key: string, over: Partial<BulkCandidate> = {}): BulkCandidate {
  return { key, label: key, online: true, inFlight: false, status: status(), ...over };
}

describe("planUpgradeAll", () => {
  it("runs the remotes first and the primary last", () => {
    const plan = planUpgradeAll([machine(PRIMARY_MACHINE_KEY), machine("zbook"), machine("nas")]);
    expect(plan.map((s) => s.key)).toEqual(["zbook", "nas", PRIMARY_MACHINE_KEY]);
  });

  it("arms a busy machine and never forces one", () => {
    const [step] = planUpgradeAll([
      machine("zbook", { status: status({ busy: true, busyTurns: 2 }) }),
    ]);
    expect(step).toEqual({ key: "zbook", label: "zbook", whenIdle: true });
    expect(step).not.toHaveProperty("force");
  });

  it("offers only what the row offers", () => {
    const plan = planUpgradeAll([
      machine("away", { online: false }),
      machine("flying", { inFlight: true }),
      machine("unknown", { status: undefined }),
      machine("armed", {
        status: status({ armed: { target: "v0.7.0", armedAt: "", deadlineAt: "" } }),
      }),
      machine("manual", { status: status({ installable: false }) }),
      machine("current", { status: status({ behind: false }) }),
      machine("ok"),
    ]);
    expect(plan.map((s) => s.key)).toEqual(["ok"]);
  });

  it("takes the checkout's own action on a local build", () => {
    const dev = { behind: false, installable: false, channel: "dev" };
    const plan = planUpgradeAll([
      machine("rebuild", {
        status: status({ ...dev, source: src({ behind: true, buildable: true, ahead: 3 }) }),
      }),
      machine("restart", {
        status: status({ ...dev, source: src({ staged: true, stagedIsCurrent: true }) }),
      }),
      machine("blocked", { status: status({ ...dev, source: src({ behind: true, ahead: 3 }) }) }),
    ]);
    expect(plan).toEqual([
      { key: "rebuild", label: "rebuild", kind: "source", whenIdle: false },
      { key: "restart", label: "restart", kind: "restart", whenIdle: false },
    ]);
  });
});

describe("runUpgradeAll", () => {
  it("starts the primary only after every remote has answered", async () => {
    const order: string[] = [];
    let releaseRemote: () => void = () => {};
    const remoteGate = new Promise<void>((r) => {
      releaseRemote = r;
    });
    const plan = planUpgradeAll([machine(PRIMARY_MACHINE_KEY), machine("zbook")]);

    const done = runUpgradeAll(plan, async (key) => {
      order.push(`start ${key}`);
      if (key === "zbook") await remoteGate;
      order.push(`done ${key}`);
    });
    await Promise.resolve();
    expect(order).toEqual(["start zbook"]);
    releaseRemote();
    await done;
    expect(order).toEqual([
      "start zbook",
      "done zbook",
      `start ${PRIMARY_MACHINE_KEY}`,
      `done ${PRIMARY_MACHINE_KEY}`,
    ]);
  });

  it("reports each refusal without stopping the rest", async () => {
    const started: string[] = [];
    const plan = planUpgradeAll([machine(PRIMARY_MACHINE_KEY), machine("zbook"), machine("nas")]);
    const failures = await runUpgradeAll(plan, async (key, opts) => {
      started.push(key);
      expect(opts).toEqual({});
      if (key === "nas") throw new Error("busy");
    });
    expect(started).toHaveLength(3);
    expect(failures.map((f) => f.label)).toEqual(["nas"]);
  });
});
