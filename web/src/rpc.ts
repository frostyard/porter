// The connection to porterd's API: JSON-RPC 2.0 over lodge's /app/porter/api
// WebSocket, one message per text frame, each ending in "\n", with
// rpc.hello first (schema/rpc/rpc.yaml). It implements Transport, so the UI
// never sees the socket.

import { Code, RpcError, type Transport } from "./api";

/** The protocol major the bundle speaks. */
export const PROTOCOL = 1;

/** What the connection needs of a socket. */
export interface Socket {
  send(data: string): void;
  close(): void;
}

export interface SocketEvents {
  open(): void;
  message(data: string): void;
  close(): void;
}

/** Dial opens a socket to url and reports its events. */
export type Dial = (url: string, on: SocketEvents) => Socket;

export const dialWebSocket: Dial = (url, on) => {
  const ws = new WebSocket(url);
  ws.onopen = () => on.open();
  ws.onmessage = (e) => on.message(String(e.data));
  ws.onclose = () => on.close();
  return ws;
};

export interface ConnectionOptions {
  /** The API's URL, read on every connect: lodge.apiURL(). */
  url: () => string;
  /** The client name rpc.hello sends, for porterd's logs. */
  client: string;
  dial?: Dial;
  /** After rpc.hello succeeds. */
  onReady(): void;
  /** When an open or opening connection closes. */
  onClose(): void;
  /** For each notification. */
  onNotification(method: string, params: unknown): void;
  /** When rpc.hello fails; a protocol mismatch stops reconnecting. */
  onFailure?(message: string): void;
  /** Whether the page is visible, which reconnecting waits for. */
  visible?: () => boolean;
}

interface Pending {
  resolve(v: unknown): void;
  reject(e: Error): void;
}

const MAX_BACKOFF = 30_000;

export class Connection implements Transport {
  #o: ConnectionOptions;
  #dial: Dial;
  #socket: Socket | null = null;
  #ready = false;
  #stopped = false;
  #next = 0;
  #pending = new Map<number, Pending>();
  #retry: ReturnType<typeof setTimeout> | undefined;
  #backoff = 1000;

  constructor(o: ConnectionOptions) {
    this.#o = o;
    this.#dial = o.dial ?? dialWebSocket;
  }

  /** Whether rpc.hello has succeeded on the open socket. */
  get ready(): boolean {
    return this.#ready;
  }

  /** connect opens the socket unless one is open or opening. */
  connect(): void {
    if (this.#socket !== null || this.#stopped) return;
    clearTimeout(this.#retry);
    this.#retry = undefined;
    let socket: Socket | null = null;
    const on: SocketEvents = {
      open: () => {
        if (socket === null || this.#socket !== socket) return;
        const s = socket;
        this.#request(s, "rpc.hello", { protocol: PROTOCOL, client: this.#o.client }).then(
          () => {
            if (this.#socket !== s) return;
            this.#ready = true;
            this.#backoff = 1000;
            this.#o.onReady();
          },
          (err: unknown) => {
            if (err instanceof RpcError && err.code === Code.protocolMismatch) this.#stopped = true;
            this.#o.onFailure?.(err instanceof Error ? err.message : String(err));
            s.close();
          },
        );
      },
      message: (data) => {
        if (socket !== null && this.#socket === socket) this.#receive(data);
      },
      close: () => {
        if (socket === null || this.#socket !== socket) return;
        this.#socket = null;
        this.#ready = false;
        const pending = [...this.#pending.values()];
        this.#pending.clear();
        for (const p of pending) p.reject(new RpcError(Code.disconnected, "not connected"));
        this.#o.onClose();
        this.#scheduleRetry();
      },
    };
    try {
      socket = this.#dial(this.#o.url(), on);
    } catch {
      this.#scheduleRetry();
      return;
    }
    this.#socket = socket;
  }

  call<T>(method: string, params: object = {}): Promise<T> {
    const s = this.#socket;
    if (!this.#ready || s === null) return Promise.reject(new RpcError(Code.disconnected, "not connected"));
    return this.#request(s, method, params) as Promise<T>;
  }

  /** ping sends rpc.ping when connected, so lodge keeps the session. */
  ping(): void {
    if (this.#ready) this.call("rpc.ping").catch(() => {});
  }

  #scheduleRetry(): void {
    if (this.#stopped || this.#retry !== undefined) return;
    // Hidden, wait for the page to be visible again (connect is called then).
    if (this.#o.visible !== undefined && !this.#o.visible()) return;
    const delay = this.#backoff;
    this.#backoff = Math.min(this.#backoff * 2, MAX_BACKOFF);
    this.#retry = setTimeout(() => {
      this.#retry = undefined;
      this.connect();
    }, delay);
  }

  #request(s: Socket, method: string, params: object): Promise<unknown> {
    const id = ++this.#next;
    return new Promise((resolve, reject) => {
      this.#pending.set(id, { resolve, reject });
      try {
        s.send(`${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`);
      } catch {
        this.#pending.delete(id);
        reject(new RpcError(Code.disconnected, "not connected"));
      }
    });
  }

  #receive(data: string): void {
    for (const line of data.split("\n")) {
      if (line.trim() === "") continue;
      let m: unknown;
      try {
        m = JSON.parse(line);
      } catch {
        continue;
      }
      if (typeof m !== "object" || m === null) continue;
      const msg = m as { id?: unknown; method?: unknown; params?: unknown; result?: unknown; error?: unknown };
      if (typeof msg.method === "string" && msg.id === undefined) {
        this.#o.onNotification(msg.method, msg.params);
        continue;
      }
      if (typeof msg.id !== "number") continue;
      const p = this.#pending.get(msg.id);
      if (p === undefined) continue;
      this.#pending.delete(msg.id);
      if (msg.error !== undefined && msg.error !== null) {
        const e = msg.error as { code?: unknown; message?: unknown };
        p.reject(
          new RpcError(typeof e.code === "number" ? e.code : Code.internal, typeof e.message === "string" ? e.message : "error"),
        );
      } else {
        p.resolve(msg.result ?? null);
      }
    }
  }
}
