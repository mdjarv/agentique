import type { PushEventMap, PushEventType } from "~/lib/ws-push-schemas";
import { isKnownPushEvent, validatePushPayload } from "~/lib/ws-push-schemas";

/** Handler for a typed push event. */
type PushHandler<K extends PushEventType = PushEventType> = (payload: PushEventMap[K]) => void;

/** Internal untyped handler stored in the map. */
type AnyPushHandler = (payload: unknown) => void;

interface PendingRequest {
  resolve: (payload: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export type ConnectionState = "connected" | "reconnecting" | "disconnected";

export class WsClient {
  private ws: WebSocket | null = null;
  private url: string;
  private requestId = 0;
  private pending = new Map<string, PendingRequest>();
  private pushHandlers = new Map<string, Set<AnyPushHandler>>();
  private reconnectDelay = 500;
  private shouldReconnect = true;
  private connectListeners = new Set<() => void>();
  private disconnectListeners = new Set<() => void>();
  private _connectionState: ConnectionState = "disconnected";
  private connectionStateListeners = new Set<() => void>();
  private visibilityBound = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private hiddenSince: number | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private heartbeatMisses = 0;

  // Microtask batching for push events: multiple WS messages arriving in the
  // same JS event loop tick get dispatched together so React can batch renders.
  private pushBuffer: Array<{ type: string; payload: unknown }> = [];
  private flushScheduled = false;
  // Backpressure cap: under an extreme synchronous burst the buffer would grow
  // unbounded between microtasks, so once it reaches this size we flush inline.
  private static readonly MAX_PUSH_BUFFER = 1000;

  constructor(url: string) {
    this.url = url;
  }

  get connectionState(): ConnectionState {
    return this._connectionState;
  }

  private setConnectionState(s: ConnectionState): void {
    if (this._connectionState === s) return;
    this._connectionState = s;
    for (const fn of this.connectionStateListeners) fn();
  }

  onConnectionStateChange(fn: () => void): () => void {
    this.connectionStateListeners.add(fn);
    return () => this.connectionStateListeners.delete(fn);
  }

  connect(): void {
    if (this.ws) return;
    this.setupVisibilityHandler();
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.reconnectDelay = 500;
      this.setConnectionState("connected");
      this.startHeartbeat();
      for (const fn of this.connectListeners) fn();
    };

    this.ws.onmessage = (ev) => {
      this.handleMessage(ev.data as string);
    };

    this.ws.onclose = (ev) => {
      if (ev.code !== 1000 && ev.code !== 1001) {
        console.warn("[WsClient] closed:", ev.code, ev.reason);
      }
      this.ws = null;
      this.stopHeartbeat();
      for (const fn of this.disconnectListeners) fn();
      // Reject all pending requests.
      for (const [id, req] of this.pending) {
        clearTimeout(req.timer);
        req.reject(new Error("WebSocket closed"));
        this.pending.delete(id);
      }
      if (this.shouldReconnect) {
        this.setConnectionState("reconnecting");
        this.reconnectTimer = setTimeout(() => {
          this.reconnectTimer = null;
          if (!this.shouldReconnect) return;
          this.connect();
        }, this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30000);
      } else {
        this.setConnectionState("disconnected");
      }
    };

    this.ws.onerror = (ev) => {
      console.error("[WsClient] error:", ev);
    };
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.setConnectionState("disconnected");
    this.stopHeartbeat();
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    if (this.visibilityBound) {
      document.removeEventListener("visibilitychange", this.handleVisibilityChange);
      window.removeEventListener("online", this.handleOnline);
      this.visibilityBound = false;
    }
  }

  /** Wait for the WebSocket to be connected, with a timeout. */
  private waitForConnection(timeoutMs = 5000): Promise<void> {
    if (this.ws?.readyState === WebSocket.OPEN) return Promise.resolve();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        unsub();
        reject(new Error("WebSocket connection timeout"));
      }, timeoutMs);
      const unsub = this.onConnect(() => {
        clearTimeout(timer);
        unsub();
        resolve();
      });
    });
  }

  async request<T = unknown>(type: string, payload: unknown = {}, timeoutMs = 30000): Promise<T> {
    await this.waitForConnection();

    const id = `req-${++this.requestId}`;
    const msg = JSON.stringify({ id, type, payload });

    return new Promise<T>((resolve, reject) => {
      // The socket can close during the awaited waitForConnection tick. Guard
      // against a dead/closing socket so the request rejects immediately instead
      // of hanging until the timeout with a dangling pending entry + timer.
      if (this.ws?.readyState !== WebSocket.OPEN) {
        reject(new Error(`Request ${type} failed: socket not open`));
        return;
      }

      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Request ${type} timed out`));
      }, timeoutMs);

      this.pending.set(id, { resolve: resolve as (p: unknown) => void, reject, timer });
      try {
        this.ws.send(msg);
      } catch (err) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(err instanceof Error ? err : new Error(`Request ${type} failed to send`));
      }
    });
  }

  /** Subscribe to a known push event type (validated). */
  subscribe<K extends PushEventType>(type: K, handler: PushHandler<K>): () => void;
  /** Subscribe to an arbitrary event type (unvalidated). */
  subscribe(type: string, handler: (payload: unknown) => void): () => void;
  subscribe(type: string, handler: AnyPushHandler): () => void {
    let handlers = this.pushHandlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.pushHandlers.set(type, handlers);
    }
    handlers.add(handler as AnyPushHandler);

    return () => {
      handlers?.delete(handler as AnyPushHandler);
      if (handlers?.size === 0) {
        this.pushHandlers.delete(type);
      }
    };
  }

  onConnect(fn: () => void): () => void {
    this.connectListeners.add(fn);
    return () => this.connectListeners.delete(fn);
  }

  onDisconnect(fn: () => void): () => void {
    this.disconnectListeners.add(fn);
    return () => this.disconnectListeners.delete(fn);
  }

  private setupVisibilityHandler(): void {
    if (this.visibilityBound) return;
    this.visibilityBound = true;
    document.addEventListener("visibilitychange", this.handleVisibilityChange);
    window.addEventListener("online", this.handleOnline);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    if (document.visibilityState === "hidden") return;
    this.heartbeatMisses = 0;
    this.heartbeatTimer = setInterval(() => {
      this.request("ping", {}, 10_000).then(
        () => {
          this.heartbeatMisses = 0;
        },
        () => {
          // Tolerate a single miss — one slow pong (e.g. the server busy
          // streaming a large turn) shouldn't tear down a live socket and
          // trigger a full history re-fetch. Reconnect only on two in a row.
          this.heartbeatMisses += 1;
          if (this.heartbeatMisses >= 2) {
            console.warn("[WsClient] heartbeat missed twice, force-reconnecting");
            this.forceReconnect();
          }
        },
      );
    }, 25_000);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private handleVisibilityChange = (): void => {
    if (document.visibilityState === "hidden") {
      this.hiddenSince = Date.now();
      this.stopHeartbeat();
      return;
    }

    if (!this.shouldReconnect) return;

    const hiddenMs = this.hiddenSince ? Date.now() - this.hiddenSince : 0;
    this.hiddenSince = null;

    // After >=5s hidden, force-reconnect — the socket may be a zombie
    // (readyState reports OPEN but the underlying TCP connection is dead).
    if (hiddenMs >= 5000) {
      this.forceReconnect();
      return;
    }

    // Short absence — trust readyState, only reconnect if socket is actually dead.
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.startHeartbeat();
      return;
    }
    this.reconnectDelay = 500;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (!this.ws) {
      this.connect();
    }
  };

  private handleOnline = (): void => {
    if (!this.shouldReconnect) return;
    this.forceReconnect();
  };

  /** Force-close and reconnect. Catches zombie sockets that report OPEN but are dead. */
  forceReconnect(): void {
    this.reconnectDelay = 500;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      // close() triggers onclose -> normal reconnect flow
      this.ws.close();
    } else {
      this.connect();
    }
  }

  private flushPushBuffer(): void {
    this.flushScheduled = false;
    const batch = this.pushBuffer;
    this.pushBuffer = [];
    for (const { type, payload } of batch) {
      // Validate known push events; unknown types pass through unvalidated.
      let validated: unknown = payload;
      if (isKnownPushEvent(type)) {
        const result = validatePushPayload(type, payload);
        if (result === undefined) continue; // validation failed — logged + dropped
        validated = result;
      }

      const handlers = this.pushHandlers.get(type);
      if (handlers) {
        for (const handler of handlers) {
          // Isolate handlers: one throwing subscriber must not abort the rest
          // of the batch (which may carry unrelated sessions'/channels' events
          // coalesced into the same tick).
          try {
            handler(validated);
          } catch (err) {
            console.error("[WsClient] push handler threw", type, err);
          }
        }
      }
    }
  }

  private handleMessage(data: string): void {
    try {
      const parseId = data.length > 500_000 ? `ws:parse (${(data.length / 1024) | 0}KB)` : "";
      if (parseId) performance.mark(`${parseId}:start`);
      const msg = JSON.parse(data);
      if (parseId) {
        performance.mark(`${parseId}:end`);
        performance.measure(parseId, `${parseId}:start`, `${parseId}:end`);
      }

      // Response to a pending request — dispatch immediately (callers await these).
      if (msg.id && msg.type === "response") {
        const pending = this.pending.get(msg.id);
        if (pending) {
          clearTimeout(pending.timer);
          this.pending.delete(msg.id);
          if (msg.error) {
            pending.reject(new Error(msg.error.message || "Server error (no details)"));
          } else {
            pending.resolve(msg.payload);
          }
        }
        return;
      }

      // Push event — buffer and flush via microtask so React can batch renders.
      if (msg.type) {
        this.pushBuffer.push({ type: msg.type, payload: msg.payload });
        if (this.pushBuffer.length >= WsClient.MAX_PUSH_BUFFER) {
          // Backpressure: a synchronous burst this large would keep growing the
          // buffer before any microtask runs, so drain it now (any already-
          // scheduled microtask becomes a harmless no-op on the empty buffer).
          this.flushPushBuffer();
        } else if (!this.flushScheduled) {
          this.flushScheduled = true;
          queueMicrotask(() => this.flushPushBuffer());
        }
      }
    } catch (err) {
      console.error("Failed to parse WebSocket message:", err);
    }
  }
}
