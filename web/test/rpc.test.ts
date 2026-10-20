import { describe, expect, it, vi } from "vitest";
import { Code, parseThreadEvent, RpcError } from "../src/api";
import { Connection, type Dial, type SocketEvents } from "../src/rpc";
import { card, entry, flush, turn } from "./fake";

class FakeSocket {
  sent: Record<string, unknown>[] = [];
  closed = false;
  constructor(
    readonly url: string,
    readonly on: SocketEvents,
  ) {}
  send(data: string): void {
    expect(data.endsWith("\n")).toBe(true);
    this.sent.push(JSON.parse(data) as Record<string, unknown>);
  }
  close(): void {
    this.closed = true;
    this.on.close();
  }
  reply(id: unknown, result: unknown): void {
    this.on.message(`${JSON.stringify({ jsonrpc: "2.0", id, result })}\n`);
  }
}

function setup() {
  const sockets: FakeSocket[] = [];
  const dial: Dial = (url, on) => {
    const s = new FakeSocket(url, on);
    sockets.push(s);
    return s;
  };
  const onReady = vi.fn();
  const onClose = vi.fn();
  const onNotification = vi.fn();
  const onFailure = vi.fn();
  const conn = new Connection({
    url: () => "wss://lodge.example/app/porter/api",
    client: "porter-web",
    dial,
    onReady,
    onClose,
    onNotification,
    onFailure,
    visible: () => false,
  });
  return { conn, sockets, onReady, onClose, onNotification, onFailure };
}

describe("Connection", () => {
  it("says rpc.hello first, then makes calls and passes notifications on", async () => {
    const { conn, sockets, onReady, onNotification } = setup();
    conn.connect();
    const s = sockets[0] as FakeSocket;
    expect(s.url).toBe("wss://lodge.example/app/porter/api");
    await expect(conn.call("thread.list")).rejects.toMatchObject({ code: Code.disconnected });
    s.on.open();
    expect(s.sent[0]).toMatchObject({ jsonrpc: "2.0", method: "rpc.hello", params: { protocol: 1, client: "porter-web" } });
    s.reply(s.sent[0]?.id, { protocol: 1, server: "porterd", caller: "session" });
    await flush();
    expect(onReady).toHaveBeenCalledOnce();

    const listed = conn.call("thread.list", { limit: 50 });
    expect(s.sent[1]).toMatchObject({ method: "thread.list", params: { limit: 50 } });
    // Two messages in one frame, a notification between.
    s.on.message(
      `${JSON.stringify({ jsonrpc: "2.0", method: "event", params: { event: "working", data: { working: true }, seq: 4 } })}\n${JSON.stringify({ jsonrpc: "2.0", id: s.sent[1]?.id, result: { turns: [], more: false } })}\n`,
    );
    await expect(listed).resolves.toEqual({ turns: [], more: false });
    expect(onNotification).toHaveBeenCalledWith("event", { event: "working", data: { working: true }, seq: 4 });

    const sent = conn.call("thread.send", { text: "hi" });
    s.on.message(`${JSON.stringify({ jsonrpc: "2.0", id: s.sent[2]?.id, error: { code: 1002, message: "busy" } })}\n`);
    await expect(sent).rejects.toEqual(new RpcError(Code.conflict, "busy"));
  });

  it("fails pending calls when the socket closes, and connects again when asked", async () => {
    const { conn, sockets, onClose, onReady } = setup();
    conn.connect();
    const s = sockets[0] as FakeSocket;
    s.on.open();
    s.reply(s.sent[0]?.id, { protocol: 1 });
    await flush();
    const pending = conn.call("thread.list");
    s.close();
    await expect(pending).rejects.toMatchObject({ code: Code.disconnected });
    expect(onClose).toHaveBeenCalledOnce();
    expect(conn.ready).toBe(false);
    conn.connect();
    expect(sockets).toHaveLength(2);
    const s2 = sockets[1] as FakeSocket;
    s2.on.open();
    s2.reply(s2.sent[0]?.id, { protocol: 1 });
    await flush();
    expect(onReady).toHaveBeenCalledTimes(2);
  });

  it("stops on a protocol mismatch", async () => {
    const { conn, sockets, onFailure } = setup();
    conn.connect();
    const s = sockets[0] as FakeSocket;
    s.on.open();
    s.on.message(`${JSON.stringify({ jsonrpc: "2.0", id: s.sent[0]?.id, error: { code: 1003, message: "protocol 2" } })}\n`);
    await flush();
    expect(onFailure).toHaveBeenCalledWith("protocol 2");
    expect(s.closed).toBe(true);
    conn.connect();
    expect(sockets).toHaveLength(1);
  });
});

describe("parseThreadEvent", () => {
  it("reads lodge's kit envelope, ignoring seq and unknown events", () => {
    const t = turn(5, { text: "Hi" });
    expect(parseThreadEvent({ event: "turn", data: t, seq: 9 })).toEqual({ type: "turn", turn: t });
    expect(parseThreadEvent({ event: "working", data: { working: false } })).toEqual({ type: "working", working: false });
    expect(parseThreadEvent({ event: "turn", data: { id: 6, role: "porter", at: "", done: false, text: "", cards: null } })).toEqual({
      type: "turn",
      turn: { id: 6, role: "porter", at: "", done: false, text: "", cards: [] },
    });
    expect(parseThreadEvent({ event: "routine", data: {} })).toBeNull();
    expect(parseThreadEvent({ event: "working", data: { working: "yes" } })).toBeNull();
    expect(parseThreadEvent({ type: "working", working: true })).toBeNull();
    expect(parseThreadEvent(null)).toBeNull();
  });

  it("reads an activity entry, giving it its array and string fields", () => {
    const e = entry(7, { cards: [card("a1", { kind: "approval", status: "pending" })], turn: 40 });
    expect(parseThreadEvent({ event: "activity", data: e, seq: 3 })).toEqual({ type: "activity", entry: e });
    const bare = { id: 8, kind: "missed", at: "", routine: "R", ok: false, cards: null };
    expect(parseThreadEvent({ event: "activity", data: bare })).toEqual({
      type: "activity",
      entry: { ...bare, text: "", cards: [] },
    });
  });

  it("returns null for an activity entry out of shape", () => {
    for (const data of [{}, { id: "7", kind: "run" }, { id: 7 }, { kind: "run" }, null, [entry(1)], "entry"]) {
      expect(parseThreadEvent({ event: "activity", data })).toBeNull();
    }
    expect(parseThreadEvent({ event: "activity" })).toBeNull();
  });
});
