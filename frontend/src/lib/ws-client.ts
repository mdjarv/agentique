// biome-ignore lint/suspicious/noExplicitAny: WebSocket payloads are untyped
type PushHandler = (payload: any) => void;

interface PendingRequest {
  // biome-ignore lint/suspicious/noExplicitAny: resolve accepts any response shape
  resolve: (payload: any) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

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

  constructor(url: string) {
    this.url = url;
  }

  connect(): void {
    if (this.ws) return;
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.reconnectDelay = 500;
      for (const fn of this.connectListeners) fn();
    };

    this.ws.onmessage = (ev) => {
      this.handleMessage(ev.data as string);
    };

    this.ws.onclose = () => {
      this.ws = null;
      for (const fn of this.disconnectListeners) fn();
      // Reject all pending requests.
      for (const [id, req] of this.pending) {
        clearTimeout(req.timer);
        req.reject(new Error("WebSocket closed"));
        this.pending.delete(id);
      }
      if (this.shouldReconnect) {
        setTimeout(() => this.connect(), this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, 8000);
      }
    };

    this.ws.onerror = () => {
      // onclose will fire after this.
    };
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.ws?.close();
  }

  async request<T = unknown>(type: string, payload: unknown = {}): Promise<T> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket not connected");
    }

    const id = `req-${++this.requestId}`;
    const msg = JSON.stringify({ id, type, payload });

    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Request ${type} timed out`));
      }, 30000);

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
