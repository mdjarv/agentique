import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WsClient } from "~/lib/ws-client";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readyState = MockWebSocket.CONNECTING;
  onopen: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  url: string;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code: 1000, reason: "" } as CloseEvent);
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.({} as Event);
  }

  simulateMessage(data: string) {
    this.onmessage?.({ data } as MessageEvent);
  }

  simulateClose(code = 1000, reason = "") {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code, reason } as CloseEvent);
  }

  static instances: MockWebSocket[] = [];

  static last(): MockWebSocket {
    const inst = MockWebSocket.instances[MockWebSocket.instances.length - 1];
    if (!inst) throw new Error("No MockWebSocket instances");
    return inst;
  }

  static reset() {
    MockWebSocket.instances = [];
  }
}

describe("WsClient", () => {
  const clients: WsClient[] = [];

  function createClient(url = "ws://localhost/ws"): WsClient {
    const c = new WsClient(url);
    clients.push(c);
    return c;
  }

  beforeEach(() => {
    MockWebSocket.reset();
    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    for (const c of clients) c.disconnect();
    clients.length = 0;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("connect creates WebSocket with correct URL", () => {
    const client = createClient();
    client.connect();
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.last().url).toBe("ws://localhost/ws");
  });

  it("connect is no-op when already connected", () => {
    const client = createClient();
    client.connect();
    client.connect();
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("connectionState becomes connected after open", () => {
    const client = createClient();
    expect(client.connectionState).toBe("disconnected");
    client.connect();
    MockWebSocket.last().simulateOpen();
    expect(client.connectionState).toBe("connected");
  });

  it("onConnect listeners fire on open", () => {
    const client = createClient();
    const fn = vi.fn();
    client.onConnect(fn);
    client.connect();
    MockWebSocket.last().simulateOpen();
    expect(fn).toHaveBeenCalledOnce();
  });

  it("request sends JSON with incrementing IDs", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const p1 = client.request("foo", { bar: 1 });
    const p2 = client.request("baz");
    // Flush microtasks so await waitForConnection() resolves and send() fires.
    await vi.advanceTimersByTimeAsync(0);

    const ws = MockWebSocket.last();
    expect(ws.sent).toHaveLength(2);
    expect(JSON.parse(ws.sent[0] ?? "")).toEqual({ id: "req-1", type: "foo", payload: { bar: 1 } });
    expect(JSON.parse(ws.sent[1] ?? "")).toEqual({ id: "req-2", type: "baz", payload: {} });

    ws.simulateMessage(JSON.stringify({ id: "req-1", type: "response", payload: { ok: true } }));
    ws.simulateMessage(JSON.stringify({ id: "req-2", type: "response", payload: {} }));
    await expect(p1).resolves.toEqual({ ok: true });
    await expect(p2).resolves.toEqual({});
  });

  it("request resolves on matching response", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const p = client.request("test");
    await vi.advanceTimersByTimeAsync(0);
    MockWebSocket.last().simulateMessage(
      JSON.stringify({ id: "req-1", type: "response", payload: { result: 42 } }),
    );
    await expect(p).resolves.toEqual({ result: 42 });
  });

  it("request rejects on error response", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const p = client.request("test");
    await vi.advanceTimersByTimeAsync(0);
    MockWebSocket.last().simulateMessage(
      JSON.stringify({ id: "req-1", type: "response", error: { message: "bad" } }),
    );
    await expect(p).rejects.toThrow("bad");
  });

  it("request rejects on timeout", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const p = client.request("test");
    // Catch immediately to prevent unhandled rejection during timer advance.
    const rejection = p.catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(30_001);
    const err = await rejection;
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toContain("timed out");
  });

  it("request rejects when connection closes", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const p = client.request("test");
    await vi.advanceTimersByTimeAsync(0);
    MockWebSocket.last().simulateClose();
    await expect(p).rejects.toThrow("WebSocket closed");
  });

  it("subscribe dispatches push events", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const handler = vi.fn();
    client.subscribe("session.update", handler);

    MockWebSocket.last().simulateMessage(
      JSON.stringify({ type: "session.update", payload: { id: "s1" } }),
    );
    // Push events are dispatched via queueMicrotask for batching.
    await vi.advanceTimersByTimeAsync(0);
    expect(handler).toHaveBeenCalledWith({ id: "s1" });
  });

  it("unsubscribe removes handler", async () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    const handler = vi.fn();
    const unsub = client.subscribe("session.update", handler);
    unsub();

    MockWebSocket.last().simulateMessage(JSON.stringify({ type: "session.update", payload: {} }));
    await vi.advanceTimersByTimeAsync(0);
    expect(handler).not.toHaveBeenCalled();
  });

  it("disconnect closes ws and prevents reconnect", () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    client.disconnect();
    expect(client.connectionState).toBe("disconnected");

    vi.advanceTimersByTime(60_000);
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("disconnect clears a pending reconnect timer", () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    MockWebSocket.last().simulateClose();
    expect(client.connectionState).toBe("reconnecting");

    client.disconnect();
    vi.advanceTimersByTime(500);

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(client.connectionState).toBe("disconnected");
  });

  it("reconnects with exponential backoff after close", () => {
    const client = createClient();
    client.connect();
    MockWebSocket.last().simulateOpen();

    // Close triggers reconnect.
    MockWebSocket.last().simulateClose();
    expect(client.connectionState).toBe("reconnecting");

    // After 500ms, should reconnect.
    vi.advanceTimersByTime(500);
    expect(MockWebSocket.instances).toHaveLength(2);

    // Close again — next delay should be 1000ms.
    MockWebSocket.last().simulateClose();
    vi.advanceTimersByTime(500);
    expect(MockWebSocket.instances).toHaveLength(2); // not yet
    vi.advanceTimersByTime(500);
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it("connectionState transitions through lifecycle", () => {
    const client = createClient();
    const listener = vi.fn();
    client.onConnectionStateChange(listener);

    client.connect();
    MockWebSocket.last().simulateOpen();
    expect(listener).toHaveBeenCalledTimes(1); // disconnected -> connected

    MockWebSocket.last().simulateClose();
    expect(listener).toHaveBeenCalledTimes(2); // connected -> reconnecting

    vi.advanceTimersByTime(500);
    MockWebSocket.last().simulateOpen();
    expect(listener).toHaveBeenCalledTimes(3); // reconnecting -> connected
  });

  describe("visibility / mobile resume", () => {
    function fireVisibility(state: "hidden" | "visible") {
      Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
      document.dispatchEvent(new Event("visibilitychange"));
    }

    it("force-reconnects after ≥5s hidden", () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const initialWs = MockWebSocket.last();

      fireVisibility("hidden");
      vi.advanceTimersByTime(6000);
      fireVisibility("visible");

      // Old socket should have been closed, triggering reconnect
      expect(initialWs.readyState).toBe(MockWebSocket.CLOSED);
      vi.advanceTimersByTime(500);
      expect(MockWebSocket.instances.length).toBeGreaterThan(1);
    });

    it("preserves connection for short hidden periods (<5s)", () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();

      fireVisibility("hidden");
      vi.advanceTimersByTime(2000);
      fireVisibility("visible");

      // Socket should still be the original, open one
      expect(MockWebSocket.instances).toHaveLength(1);
      expect(MockWebSocket.last().readyState).toBe(MockWebSocket.OPEN);
    });

    it("forceReconnect resets backoff delay", () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();

      // Drive backoff up via several close/reconnect cycles
      MockWebSocket.last().simulateClose();
      vi.advanceTimersByTime(500); // reconnect at 500ms
      MockWebSocket.last().simulateOpen();
      MockWebSocket.last().simulateClose();
      // Next delay would be 1000ms

      const countBefore = MockWebSocket.instances.length;
      client.forceReconnect();
      // forceReconnect resets delay → should reconnect at 500ms, not 1000ms
      vi.advanceTimersByTime(500);
      expect(MockWebSocket.instances.length).toBeGreaterThan(countBefore);
    });

    it("online event triggers force-reconnect", () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const initialWs = MockWebSocket.last();

      window.dispatchEvent(new Event("online"));

      expect(initialWs.readyState).toBe(MockWebSocket.CLOSED);
      vi.advanceTimersByTime(500);
      expect(MockWebSocket.instances.length).toBeGreaterThan(1);
    });
  });

  describe("heartbeat resilience", () => {
    const failPing = (ws: MockWebSocket) => {
      const ping = ws.sent
        .map((s) => JSON.parse(s))
        .filter((m) => m.type === "ping")
        .at(-1);
      expect(ping).toBeDefined();
      ws.simulateMessage(
        JSON.stringify({ id: ping.id, type: "response", error: { message: "no pong" } }),
      );
    };

    it("tolerates a single heartbeat miss without reconnecting", async () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const ws = MockWebSocket.last();

      await vi.advanceTimersByTimeAsync(25_000); // first heartbeat tick → ping sent
      failPing(ws);
      await vi.advanceTimersByTimeAsync(0); // let the rejection handler run

      // One miss must NOT tear down a live socket.
      expect(MockWebSocket.instances).toHaveLength(1);
      expect(ws.readyState).toBe(MockWebSocket.OPEN);
    });

    it("force-reconnects after two consecutive heartbeat misses", async () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const ws = MockWebSocket.last();

      await vi.advanceTimersByTimeAsync(25_000);
      failPing(ws);
      await vi.advanceTimersByTimeAsync(0); // miss 1

      await vi.advanceTimersByTimeAsync(25_000);
      failPing(ws);
      await vi.advanceTimersByTimeAsync(0); // miss 2 → forceReconnect

      expect(ws.readyState).toBe(MockWebSocket.CLOSED);
      await vi.advanceTimersByTimeAsync(500); // reconnect backoff
      expect(MockWebSocket.instances.length).toBeGreaterThan(1);
    });

    it("resets the miss counter after a successful pong", async () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const ws = MockWebSocket.last();

      await vi.advanceTimersByTimeAsync(25_000);
      failPing(ws); // miss 1
      await vi.advanceTimersByTimeAsync(0);

      // Next ping succeeds → counter resets.
      await vi.advanceTimersByTimeAsync(25_000);
      const ping = ws.sent
        .map((s) => JSON.parse(s))
        .filter((m) => m.type === "ping")
        .at(-1);
      ws.simulateMessage(JSON.stringify({ id: ping.id, type: "response", payload: {} }));
      await vi.advanceTimersByTimeAsync(0);

      // A subsequent single miss should again be tolerated (counter was reset).
      await vi.advanceTimersByTimeAsync(25_000);
      failPing(ws);
      await vi.advanceTimersByTimeAsync(0);
      expect(MockWebSocket.instances).toHaveLength(1);
      expect(ws.readyState).toBe(MockWebSocket.OPEN);
    });
  });

  describe("push buffer backpressure", () => {
    it("flushes synchronously once the buffer hits its cap", () => {
      const client = createClient();
      client.connect();
      MockWebSocket.last().simulateOpen();
      const ws = MockWebSocket.last();

      const handler = vi.fn();
      client.subscribe("custom.burst", handler);

      // A synchronous burst at the cap must dispatch WITHOUT awaiting a microtask.
      for (let i = 0; i < 1000; i++) {
        ws.simulateMessage(JSON.stringify({ type: "custom.burst", payload: { i } }));
      }
      expect(handler).toHaveBeenCalledTimes(1000);
    });
  });
});
