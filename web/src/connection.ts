// The connection to porterd's API: @frostyard/lodge-client/rpc's
// connectLodgeAPI over lodge's /app/porter/api WebSocket (rpc.hello first,
// rpc.ping every 30 s while the page is visible, and a ping unanswered in
// 10 s), as a Transport for the UI.
//
// Connection adds what Porter needs on top of it:
// - it owns reconnecting, so the UI runs onReady after every rpc.hello
//   (the client's own reconnect gives no such event): when a socket drops,
//   the client's connection is closed and a new one made, after a growing
//   delay (1 s, doubling to 30 s), and only while the page is visible;
// - it retries a first attempt that fails, which the client does not;
// - a protocol mismatch in rpc.hello stops it, and onFailure says why: the
//   client rejects a failed rpc.hello with that call's own RPCError, so
//   Porter reads its code (client 0.3.2, which stopped losing it);
// - it keeps api.ts's errors: Code.disconnected for a call made without a
//   connection, and RpcError with the daemon's code.

import { CodeNotConnected, connectLodgeAPI, type LodgeAPI, RPCError, type VisibilityDocument } from "@frostyard/lodge-client/rpc";
import { Code, RpcError, type Transport } from "./api";

/** The protocol major the bundle speaks (schema/rpc/rpc.yaml). */
export const PROTOCOL = 1;

const MAX_BACKOFF = 30_000;
const FIRST_BACKOFF = 1000;

/**
 * PING_TIMEOUT_MS is how long a ping may go unanswered before the socket is
 * closed as dropped. Lodge ends a session after 90 s without a frame, so the
 * page pings every 30 s; a phone that sleeps drops a connection without a
 * close, and a ping unanswered in 10 s means the socket is dead, so the
 * client closes it, and the reconnect runs.
 */
export const PING_TIMEOUT_MS = 10_000;

export interface ConnectionOptions {
  /** The API's URL, read on every connect: lodge.apiURL(). */
  url: () => string;
  /** The client name rpc.hello sends, for porterd's logs. */
  client: string;
  /** After rpc.hello succeeds, on each connection. */
  onReady(): void;
  /** When an open or opening connection closes. */
  onClose(): void;
  /** For each "event" notification's params: lodge's kit envelope. */
  onEvent(params: unknown): void;
  /** When rpc.hello fails; a protocol mismatch stops reconnecting. */
  onFailure?(message: string): void;
  /** Whether the page is visible, which reconnecting waits for. */
  visible?: () => boolean;
  /** The WebSocket constructor and the document the client uses; the browser's by default. */
  webSocket?: typeof WebSocket;
  document?: VisibilityDocument;
}

/** disconnected is what a call fails with when there is no connection. */
function disconnected(): RpcError {
  return new RpcError(Code.disconnected, "not connected");
}

/** apiError gives an error from the client the code and class api.ts has. */
function apiError(err: unknown): RpcError {
  if (err instanceof RPCError && err.code !== CodeNotConnected) return new RpcError(err.code, err.message);
  return disconnected();
}

export class Connection implements Transport {
  #o: ConnectionOptions;
  #api: LodgeAPI | null = null;
  /** Whether an attempt is open or in flight. */
  #active = false;
  #stopped = false;
  #retry: ReturnType<typeof setTimeout> | undefined;
  #backoff = FIRST_BACKOFF;

  constructor(o: ConnectionOptions) {
    this.#o = o;
  }

  /** Whether rpc.hello has succeeded on the open socket. */
  get ready(): boolean {
    return this.#api?.ready === true;
  }

  /** connect opens the socket unless one is open or opening. */
  connect(): void {
    if (this.#active || this.#stopped) return;
    clearTimeout(this.#retry);
    this.#retry = undefined;
    this.#active = true;
    connectLodgeAPI(
      { apiURL: this.#o.url },
      {
        webSocket: this.#o.webSocket,
        hello: { protocol: PROTOCOL, client: this.#o.client },
        pingTimeoutMs: PING_TIMEOUT_MS,
        ...(this.#o.document === undefined ? {} : { document: this.#o.document }),
      },
    ).then(
      (api) => {
        this.#api = api;
        this.#backoff = FIRST_BACKOFF;
        api.onEvent((e) => this.#o.onEvent(e));
        api.onClose(() => {
          // This class owns reconnecting: stop the client's, so that two sockets never live.
          api.close();
          if (this.#api !== api) return;
          this.#api = null;
          this.#active = false;
          this.#o.onClose();
          this.#scheduleRetry();
        });
        this.#o.onReady();
      },
      (err: unknown) => {
        this.#active = false;
        // A failed rpc.hello rejects with its own RPCError (rpc.hello is the
        // only call before the connection is ready): its code says whether
        // the page and porterd speak one protocol, and its message is the
        // daemon's. A plain Error is a socket that never opened, which says
        // nothing new of the daemon.
        if (err instanceof RPCError) {
          if (err.code === Code.protocolMismatch) this.#stopped = true;
          this.#o.onFailure?.(err.message);
        }
        this.#o.onClose();
        this.#scheduleRetry();
      },
    );
  }

  async call<T>(method: string, params: object = {}): Promise<T> {
    const api = this.#api;
    if (api === null || !api.ready) throw disconnected();
    try {
      return await api.call<T>(method, params);
    } catch (err) {
      throw apiError(err);
    }
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
}
