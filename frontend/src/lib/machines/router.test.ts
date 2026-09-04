import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The routing facade fans subscriptions in from every machine's socket,
 * including machines paired after subscribe time. These lock the unsubscribe
 * half of that contract: the leak this guards against was handlers replayed
 * onto later-created clients that no unsubscriber knew about, stacking one
 * copy per remount for the tab's lifetime.
 */

vi.mock("~/lib/machines/registry", () => {
  type Handler = (payload: unknown) => void;
  class FakeClient {
    private subs = new Map<string, Set<Handler>>();
    subscribe(type: string, handler: Handler): () => void {
      let handlers = this.subs.get(type);
      if (!handlers) {
        handlers = new Set();
        this.subs.set(type, handlers);
      }
      handlers.add(handler);
      return () => handlers?.delete(handler);
    }
    count(type: string): number {
      return this.subs.get(type)?.size ?? 0;
    }
  }
  const primary = new FakeClient();
  const machines = new Map<string, FakeClient>();
  const listeners = new Set<(machineId: string, client: FakeClient) => void>();
  return {
    getPrimaryClient: () => primary,
    machineClients: () => machines,
    getMachineClient: (id: string) => machines.get(id),
    onMachineClientCreated: (fn: (machineId: string, client: FakeClient) => void) => {
      listeners.add(fn);
    },
    __primary: primary,
    __createMachine: (id: string) => {
      const client = new FakeClient();
      machines.set(id, client);
      for (const fn of listeners) fn(id, client);
      return client;
    },
    __reset: () => machines.clear(),
  };
});

vi.mock("~/stores/app-store", () => ({ useAppStore: { getState: () => ({ projects: [] }) } }));
vi.mock("~/stores/chat-store", () => ({ useChatStore: { getState: () => ({ sessions: {} }) } }));
vi.mock("~/stores/machine-store", () => ({
  useMachineStore: { getState: () => ({ machines: {} }) },
}));

import * as registryModule from "~/lib/machines/registry";
import { getRoutingClient } from "~/lib/machines/router";

interface Countable {
  count(type: string): number;
}
const registry = registryModule as unknown as {
  __primary: Countable;
  __createMachine: (id: string) => Countable;
  __reset: () => void;
};

const noop = () => {};

beforeEach(() => {
  registry.__reset();
});

describe("RoutingWsClient.subscribe", () => {
  it("fans a handler in to the primary and every existing machine", () => {
    const machine = registry.__createMachine("m-existing");
    const unsub = getRoutingClient().subscribe("session.state", noop);

    expect(registry.__primary.count("session.state")).toBe(1);
    expect(machine.count("session.state")).toBe(1);
    unsub();
  });

  it("replays a live handler onto a machine paired after subscribe time", () => {
    const unsub = getRoutingClient().subscribe("session.state", noop);
    const late = registry.__createMachine("m-late");

    expect(late.count("session.state")).toBe(1);
    unsub();
  });

  // The bug: the closure returned by subscribe only unsubbed the clients
  // captured at subscribe time, so handlers replayed onto later-created
  // clients could never be detached.
  it("unsubscribing detaches from machines created after subscribe time", () => {
    const unsub = getRoutingClient().subscribe("session.state", noop);
    const late = registry.__createMachine("m-late");

    unsub();

    expect(registry.__primary.count("session.state")).toBe(0);
    expect(late.count("session.state")).toBe(0);
  });

  it("does not replay an unsubscribed handler onto machines paired later", () => {
    const unsub = getRoutingClient().subscribe("session.state", noop);
    unsub();

    const late = registry.__createMachine("m-late");
    expect(late.count("session.state")).toBe(0);
  });

  // The stacking symptom: each remount subscribed and unsubscribed, and every
  // cycle left one more copy behind on the next machine to pair.
  it("remount cycles leave nothing behind for the next machine to inherit", () => {
    for (let i = 0; i < 5; i++) {
      getRoutingClient().subscribe("session.state", noop)();
    }
    const survivor = getRoutingClient().subscribe("session.state", noop);
    const late = registry.__createMachine("m-late");

    expect(late.count("session.state")).toBe(1);
    survivor();
    expect(late.count("session.state")).toBe(0);
  });

  it("unsubscribing twice is safe and touches nothing else", () => {
    const first = getRoutingClient().subscribe("session.state", noop);
    const second = getRoutingClient().subscribe("session.state", () => {});

    first();
    first();

    expect(registry.__primary.count("session.state")).toBe(1);
    second();
  });
});
