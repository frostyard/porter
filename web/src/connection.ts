// The connection to porterd's API: @frostyard/lodge-client/rpc's
// connectLodgeAPI over lodge's /app/porter/api WebSocket (rpc.hello first,
// rpc.ping every 30 s while the page is visible), as a Transport for the UI.
//
// Connection adds what Porter needs on top of it:
// - it owns reconnecting, so the UI runs onReady after every rpc.hello
//   (the client's own reconnect gives no such event): when a socket drops,
//   the client's connection is closed and a new one made, after a growing
//   delay (1 s, doubling to 30 s), and only while the page is visible;
// - it retries a first attempt that fails, which the client does not;
// - a protocol mismatch in rpc.hello stops it, and onFailure says why;
// - it keeps api.ts's errors: Code.disconnected for a call made without a
//   connection, and RpcError with the daemon's code.

import { CodeNotConnected, connectLodgeAPI, type LodgeAPI, RPCError, type VisibilityDocument } from "@frostyard/lodge-client/rpc";
import { Code, RpcError, type Transport } from "./api";

/** The protocol major the bundle speaks (schema/rpc/rpc.yaml). */
export const PROTOCOL = 1;

const MAX_BACKOFF = 30_000;
const FIRST_BACKOFF = 1000;

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

interface HelloFailure {
  code: number;
  message: string;
}

/**
 * helloFailure reads an error answer out of a frame. The client turns an
 * rpc.hello failure into a plain Error, so the code that says a protocol
 * mismatch is read here, from the first error answer on a socket (rpc.hello
 * is the only call before the connection is ready).
 */
function helloFailure(data: unknown): HelloFailure | null {
  for (const line of String(data).split("\n")) {
    if (line.trim() === "") continue;
    let m: { id?: unknown; error?: { code?: unknown; message?: unknown } | null };
    try {
      m = JSON.parse(line);
    } catch {
      continue;
    }
    if (typeof m !== "object" || m === null || typeof m.id !== "number") continue;
    const e = m.error;
    if (typeof e !== "object" || e === null) continue;
    return { code: typeof e.code === "number" ? e.code : Code.internal, message: typeof e.message === "string" ? e.message : "error" };
  }
  return null;
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
    let failure: HelloFailure | null = null;
    const Base = this.#o.webSocket ?? WebSocket;
    class Sniffing extends Base {
      constructor(url: string | URL) {
        super(url);
        this.addEventListener("message", (e) => {
          failure ??= helloFailure(e.data);
        });
      }
    }
    connectLodgeAPI(
      { apiURL: this.#o.url },
      {
        webSocket: Sniffing,
        hello: { protocol: PROTOCOL, client: this.#o.client },
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
      () => {
        this.#active = false;
        if (failure !== null) {
          if (failure.code === Code.protocolMismatch) this.#stopped = true;
          this.#o.onFailure?.(failure.message);
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
