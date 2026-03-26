// biome-ignore lint/suspicious/noExplicitAny: WebSocket payloads are untyped
type PushHandler = (payload: any) => void;

interface PendingRequest {
  // biome-ignore lint/suspicious/noExplicitAny: resolve accepts any response shape
  resolve: (payload: any) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export type ConnectionState = "connected" | "reconnecting" | "disconnected";

export class WsClient {
  private ws: WebSocket | null = null;
  private url: string;
  private requestId = 0;
  private pending = new Map<string, PendingRequest>();
  private pushHandlers = new Map<string, Set<PushHandler>>();
  private reconnectDelay = 500;
  private shouldReconnect = true;
  private connectListeners = new Set<() => void>();
  private disconnectListeners = new Set<() => void>();
  private _connectionState: ConnectionState = "disconnected";
  private connectionStateListeners = new Set<() => void>();
  private visibilityBound = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

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
    console.log("[WsClient] connecting to", this.url);
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      console.log("[WsClient] connected");
      this.reconnectDelay = 500;
      this.setConnectionState("connected");
      for (const fn of this.connectListeners) fn();
    };

    this.ws.onmessage = (ev) => {
      this.handleMessage(ev.data as string);
    };

    this.ws.onclose = (ev) => {
      console.log("[WsClient] closed:", ev.code, ev.reason);
      this.ws = null;
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
          this.connect();
        }, this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30000);
      } else {
        this.setConnectionState("disconnected");
      }
    };

    this.ws.onerror = (ev) => {
      console.log("[WsClient] error:", ev);
    };
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.setConnectionState("disconnected");
    this.ws?.close();
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
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Request ${type} timed out`));
      }, timeoutMs);

      this.pending.set(id, { resolve, reject, timer });
      this.ws?.send(msg);
    });
  }

  subscribe(type: string, handler: PushHandler): () => void {
    let handlers = this.pushHandlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.pushHandlers.set(type, handlers);
    }
    handlers.add(handler);

    return () => {
      handlers?.delete(handler);
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
  }

  private handleVisibilityChange = (): void => {
    if (document.visibilityState !== "visible" || !this.shouldReconnect) return;
    if (this.ws?.readyState === WebSocket.OPEN) return;

    // Page became visible with a dead/missing socket — reconnect immediately
    this.reconnectDelay = 500;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (!this.ws) {
      this.connect();
    }
  };

  private handleMessage(data: string): void {
    try {
      const msg = JSON.parse(data);

      // Response to a pending request.
      if (msg.id && msg.type === "response") {
        const pending = this.pending.get(msg.id);
        if (pending) {
          clearTimeout(pending.timer);
          this.pending.delete(msg.id);
          if (msg.error) {
            pending.reject(new Error(msg.error.message));
          } else {
            pending.resolve(msg.payload);
          }
        }
        return;
      }

      // Push event.
      if (msg.type) {
        const handlers = this.pushHandlers.get(msg.type);
        if (handlers) {
          for (const handler of handlers) {
            handler(msg.payload);
          }
        }
      }
    } catch (err) {
      console.error("Failed to parse WebSocket message:", err);
    }
  }
}
