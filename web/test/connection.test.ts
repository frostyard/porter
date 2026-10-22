import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, errorCode, RpcError } from "../src/api";
import { Connection, PROTOCOL } from "../src/connection";

/** A WebSocket the test drives: it opens, answers and closes when told. */
class FakeSocket {
  static all: FakeSocket[] = [];
  sent: Record<string, unknown>[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  #listeners: ((e: { data: string }) => void)[] = [];
  constructor(readonly url: string) {
    FakeSocket.all.push(this);
  }
  addEventListener(type: string, l: (e: { data: string }) => void): void {
    if (type === "message") this.#listeners.push(l);
  }
  send(data: string): void {
    expect(data.endsWith("\n")).toBe(true);
    this.sent.push(JSON.parse(data) as Record<string, unknown>);
  }
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.onclose?.({ code: 1000 });
  }
  opened = false;
  open(): void {
    if (this.opened) return;
    this.opened = true;
    this.onopen?.();
  }
  /** deliver gives the page one frame of lines. */
  deliver(...messages: unknown[]): void {
    const data = messages.map((m) => `${JSON.stringify(m)}\n`).join("");
    for (const l of this.#listeners) l({ data });
    this.onmessage?.({ data });
  }
  reply(id: unknown, result: unknown): void {
    this.deliver({ jsonrpc: "2.0", id, result });
  }
  fail(id: unknown, code: number, message: string): void {
    this.deliver({ jsonrpc: "2.0", id, error: { code, message } });
  }
}

const flush = () => vi.advanceTimersByTimeAsync(0);

function setup(visible = () => true) {
  const onReady = vi.fn();
  const onClose = vi.fn();
  const onEvent = vi.fn();
  const onFailure = vi.fn();
  const doc = { visibilityState: "visible" as const, addEventListener: vi.fn(), removeEventListener: vi.fn() };
  const conn = new Connection({
    url: () => "wss://lodge.example/app/porter/api",
    client: "porter-web",
    webSocket: FakeSocket as unknown as typeof WebSocket,
    document: doc,
    onReady,
    onClose,
    onEvent,
    onFailure,
    visible,
  });
  const socket = (n: number) => FakeSocket.all[n] as FakeSocket;
  /** ready opens socket n and answers its rpc.hello. */
  const ready = async (n: number) => {
    socket(n).open();
    socket(n).reply(socket(n).sent[0]?.id, { protocol: PROTOCOL, server: "porterd", caller: "session" });
    await flush();
  };
  return { conn, socket, ready, onReady, onClose, onEvent, onFailure };
}

beforeEach(() => {
  FakeSocket.all = [];
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

describe("Connection", () => {
  it("says rpc.hello first, then makes calls and passes events on", async () => {
    const { conn, socket, ready, onReady, onEvent } = setup();
    conn.connect();
    expect(socket(0).url).toBe("wss://lodge.example/app/porter/api");
    await expect(conn.call("thread.list")).rejects.toMatchObject({ code: Code.disconnected });
    socket(0).open();
    expect(socket(0).sent[0]).toMatchObject({ jsonrpc: "2.0", method: "rpc.hello", params: { protocol: 1, client: "porter-web" } });
    expect(conn.ready).toBe(false);
    await ready(0);
    expect(onReady).toHaveBeenCalledOnce();
    expect(conn.ready).toBe(true);

    const listed = conn.call("thread.list", { limit: 50 });
    expect(socket(0).sent[1]).toMatchObject({ method: "thread.list", params: { limit: 50 } });
    // Two messages in one frame, a notification between.
    socket(0).deliver(
      { jsonrpc: "2.0", method: "event", params: { event: "working", data: { working: true }, seq: 4 } },
      { jsonrpc: "2.0", id: socket(0).sent[1]?.id, result: { turns: [], more: false } },
    );
    await expect(listed).resolves.toEqual({ turns: [], more: false });
    expect(onEvent).toHaveBeenCalledWith({ event: "working", data: { working: true }, seq: 4 });

    // A call with no params sends {}.
    void conn.call("thread.pending");
    expect(socket(0).sent[2]).toMatchObject({ method: "thread.pending", params: {} });

    const sent = conn.call("thread.send", { text: "hi" });
    socket(0).fail(socket(0).sent[3]?.id, 1002, "busy");
    await expect(sent).rejects.toEqual(new RpcError(Code.conflict, "busy"));
    await expect(sent).rejects.toBeInstanceOf(RpcError);
  });

  it("pings every 30 s while visible, so lodge keeps the session", async () => {
    const { conn, socket, ready } = setup();
    conn.connect();
    await ready(0);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(socket(0).sent[1]).toMatchObject({ method: "rpc.ping" });
  });

  it("fails pending calls with Code.disconnected when the socket closes, and connects again when asked", async () => {
    const { conn, socket, ready, onClose, onReady } = setup(() => false);
    conn.connect();
    await ready(0);
    const pending = conn.call("thread.list");
    socket(0).close();
    await expect(pending).rejects.toMatchObject({ code: Code.disconnected, message: "not connected" });
    await expect(pending).rejects.toBeInstanceOf(RpcError);
    expect(errorCode(await conn.call("thread.list").catch((e: unknown) => e))).toBe(Code.disconnected);
    expect(onClose).toHaveBeenCalledOnce();
    expect(conn.ready).toBe(false);
    // The page is hidden: it waits to be asked.
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.all).toHaveLength(1);
    conn.connect();
    conn.connect();
    expect(FakeSocket.all).toHaveLength(2);
    await ready(1);
    expect(onReady).toHaveBeenCalledTimes(2);
  });

  it("reconnects after a drop with a growing delay, and the backoff starts over after rpc.hello", async () => {
    const { conn, socket, ready, onReady } = setup();
    conn.connect();
    await ready(0);
    socket(0).close();
    await vi.advanceTimersByTimeAsync(999);
    expect(FakeSocket.all).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeSocket.all).toHaveLength(2);
    // This one drops before it is ready, so the next wait is 2 s.
    socket(1).close();
    await vi.advanceTimersByTimeAsync(1999);
    expect(FakeSocket.all).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeSocket.all).toHaveLength(3);
    await ready(2);
    expect(onReady).toHaveBeenCalledTimes(2);
    // Only one socket lives: the client's own reconnect is off.
    socket(2).close();
    await vi.advanceTimersByTimeAsync(1000);
    expect(FakeSocket.all).toHaveLength(4);
    expect(FakeSocket.all.filter((s) => !s.closed)).toHaveLength(1);
  });

  it("caps the delay at 30 s", async () => {
    const { conn, socket } = setup();
    conn.connect();
    for (let i = 0; i < 8; i++) {
      socket(i).close();
      await vi.advanceTimersByTimeAsync(30_000);
    }
    expect(FakeSocket.all).toHaveLength(9);
    socket(8).close();
    await vi.advanceTimersByTimeAsync(29_999);
    expect(FakeSocket.all).toHaveLength(9);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeSocket.all).toHaveLength(10);
  });

  it("retries a first attempt that cannot connect, and says nothing of it", async () => {
    const { conn, socket, ready, onReady, onFailure, onClose } = setup();
    conn.connect();
    socket(0).close();
    await flush();
    expect(onFailure).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(1000);
    await ready(1);
    expect(onReady).toHaveBeenCalledOnce();
  });

  it("reports a failed rpc.hello, and tries again", async () => {
    const { conn, socket, ready, onFailure, onReady } = setup();
    conn.connect();
    socket(0).open();
    socket(0).fail(socket(0).sent[0]?.id, Code.internal, "boom");
    await flush();
    expect(onFailure).toHaveBeenCalledWith("boom");
    expect(socket(0).closed).toBe(true);
    await vi.advanceTimersByTimeAsync(1000);
    await ready(1);
    expect(onReady).toHaveBeenCalledOnce();
  });

  it("stops on a protocol mismatch", async () => {
    const { conn, socket, onFailure, onReady } = setup();
    conn.connect();
    socket(0).open();
    socket(0).fail(socket(0).sent[0]?.id, Code.protocolMismatch, "protocol 2");
    await flush();
    expect(onFailure).toHaveBeenCalledWith("protocol 2");
    expect(socket(0).closed).toBe(true);
    conn.connect();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(FakeSocket.all).toHaveLength(1);
    expect(onReady).not.toHaveBeenCalled();
  });

  it("stops on a protocol mismatch when it reconnects too", async () => {
    const { conn, socket, ready, onFailure } = setup();
    conn.connect();
    await ready(0);
    socket(0).close();
    await vi.advanceTimersByTimeAsync(1000);
    socket(1).open();
    socket(1).fail(socket(1).sent[0]?.id, Code.protocolMismatch, "protocol 2");
    await flush();
    expect(onFailure).toHaveBeenCalledWith("protocol 2");
    conn.connect();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(FakeSocket.all).toHaveLength(2);
  });
});
