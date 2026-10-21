// The thread domain's wire types (schema/rpc/thread.yaml; api/thread.go is
// the Go side, and its JSON is this shape), and typed calls over a
// Transport.

/** Who a turn is from. */
export type TurnRole = "owner" | "porter" | "notice";

/** What a card shows. */
export type CardKind = "tool" | "capture";

/** Where a card stands. */
export type CardStatus = "done" | "failed" | "pending" | "cancelled";

/** What the current app reported as on screen. `kind` is "" when nothing is open. */
export interface Context {
  app: string;
  kind: string;
  ref: string;
  title: string;
}

/** One thing Porter did, or proposes, within a reply. */
export interface Card {
  id: string;
  kind: CardKind;
  status: CardStatus;
  app: string;
  tool: string;
  summary: string;
  /** A lodge address for what it touched. */
  url?: string;
  /** For a capture, the text to append. */
  text?: string;
  /** Why it failed. */
  detail?: string;
  /** The tool call's arguments as a JSON object, long strings shortened. */
  args?: string;
}

/** One turn of the thread. */
export interface Turn {
  id: number;
  /** RFC 3339, UTC. */
  at: string;
  role: TurnRole;
  /** Markdown. */
  text: string;
  context?: Context;
  cards: Card[];
  done: boolean;
}

/** A page of the thread, oldest first. */
export interface TurnList {
  turns: Turn[];
  more: boolean;
}

/** The owner's turn as stored, and the id Porter's reply will have. */
export interface Sent {
  turn: Turn;
  reply: number;
}

/**
 * An event on a subscribed connection, as the bundle handles it. On the
 * wire it is a notification of method "event" whose params are lodge's kit
 * envelope, `{event, data, seq?}` (see parseThreadEvent).
 */
export type ThreadEvent = { type: "turn"; turn: Turn } | { type: "working"; working: boolean };

/** JSON-RPC error codes (schema/rpc/errors.yaml), and -1 for a call made without a connection. */
export const Code = {
  disconnected: -1,
  methodNotFound: -32601,
  invalidParams: -32602,
  internal: -32603,
  notFound: 1001,
  conflict: 1002,
  protocolMismatch: 1003,
  notReady: 1004,
  unavailable: 1005,
} as const;

/** An error answer to a call. */
export class RpcError extends Error {
  readonly code: number;
  constructor(code: number, message: string) {
    super(message);
    this.name = "RpcError";
    this.code = code;
  }
}

/** errorCode returns an RpcError's code, or 0 for anything else. */
export function errorCode(err: unknown): number {
  return err instanceof RpcError ? err.code : 0;
}

/** Transport makes one JSON-RPC call; the socket, or a fake in tests. */
export interface Transport {
  call<T>(method: string, params?: object): Promise<T>;
}

/** ThreadAPI is the thread domain's methods. */
export interface ThreadAPI {
  list(p: { before?: number; limit?: number }): Promise<TurnList>;
  send(p: { text: string; context?: Context }): Promise<Sent>;
  search(p: { query: string; limit?: number }): Promise<TurnList>;
  confirm(p: { card: string; text?: string }): Promise<Turn>;
  cancel(p: { card: string }): Promise<void>;
  subscribe(): Promise<boolean>;
}

/** normalizeTurn gives a turn from the wire its array and string fields, whatever a nil slice encoded as. */
export function normalizeTurn(t: Turn): Turn {
  return { ...t, text: typeof t.text === "string" ? t.text : "", cards: Array.isArray(t.cards) ? t.cards : [] };
}

function normalizeList(l: TurnList): TurnList {
  return { turns: Array.isArray(l?.turns) ? l.turns.map(normalizeTurn) : [], more: l?.more === true };
}

export function threadAPI(t: Transport): ThreadAPI {
  return {
    list: async (p) => normalizeList(await t.call<TurnList>("thread.list", p)),
    send: async (p) => {
      const s = await t.call<Sent>("thread.send", p);
      return { turn: normalizeTurn(s.turn), reply: s.reply };
    },
    search: async (p) => normalizeList(await t.call<TurnList>("thread.search", p)),
    confirm: async (p) => normalizeTurn(await t.call<Turn>("thread.confirm", p)),
    cancel: async (p) => {
      await t.call<unknown>("thread.cancel", p);
    },
    subscribe: async () => (await t.call<boolean>("thread.subscribe", {})) === true,
  };
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * parseThreadEvent reads the params of an "event" notification, lodge's
 * kit envelope `{event, data, seq?}`: "turn" carries a Turn as data, and
 * "working" carries `{working}`. It ignores `seq`, and returns null for any
 * other event name, for forward compatibility, or for data out of shape.
 */
export function parseThreadEvent(params: unknown): ThreadEvent | null {
  if (!isObject(params) || !isObject(params.data)) return null;
  const data = params.data;
  switch (params.event) {
    case "turn":
      if (typeof data.id !== "number" || typeof data.role !== "string") return null;
      return { type: "turn", turn: normalizeTurn(data as unknown as Turn) };
    case "working":
      return typeof data.working === "boolean" ? { type: "working", working: data.working } : null;
  }
  return null;
}
