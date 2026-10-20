// The thread and activity domains' wire types (schema/rpc/thread.yaml and
// activity.yaml; api/thread.go and api/activity.go are the Go side, and
// their JSON is this shape), and typed calls over a Transport.

/** Who a turn is from. */
export type TurnRole = "owner" | "porter" | "notice";

/** What a card shows. An approval is an action a routine proposed: it runs only when the owner approves (confirm) it. */
export type CardKind = "tool" | "capture" | "approval";

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
  /** The tool call's arguments as a JSON object: whole for an approval, otherwise with long strings shortened. */
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
export type ThreadEvent =
  | { type: "turn"; turn: Turn }
  | { type: "working"; working: boolean }
  | { type: "activity"; entry: Entry };

/** What an Activity entry records. */
export type EntryKind = "run" | "skipped" | "missed" | "invalid";

/** One thing Porter did or noticed on its own. */
export interface Entry {
  id: number;
  /** RFC 3339, UTC. */
  at: string;
  kind: EntryKind;
  /** The routine's name, its file name without .md. */
  routine: string;
  /** For a run, whether it succeeded. */
  ok: boolean;
  /** One line: what happened, or why. */
  text: string;
  /** For a run, a lodge address of its output note. */
  url?: string;
  /** The thread turn that reported it. */
  turn?: number;
  /** For a run, what it did or proposes. */
  cards: Card[];
}

/** A page of Activity, newest first. */
export interface EntryList {
  entries: Entry[];
  more: boolean;
}

/** A routine as porterd last read it from Porter/Routines/. */
export interface Routine {
  name: string;
  path: string;
  url?: string;
  valid: boolean;
  /** Why it is invalid. */
  error?: string;
  schedule: string;
  timezone: string;
  output?: string;
  enabled: boolean;
  /** When it runs next, RFC 3339 in UTC. */
  next?: string;
  /** When it last ran, RFC 3339 in UTC. */
  last?: string;
  lastOk?: boolean;
}

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
  /** The turns holding a pending capture or approval, newest first. */
  pending(): Promise<TurnList>;
  subscribe(): Promise<boolean>;
}

/** ActivityAPI is the activity domain's methods. */
export interface ActivityAPI {
  list(p: { before?: number; limit?: number }): Promise<EntryList>;
  routines(): Promise<Routine[]>;
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
    pending: async () => normalizeList(await t.call<TurnList>("thread.pending", {})),
    subscribe: async () => (await t.call<boolean>("thread.subscribe", {})) === true,
  };
}

/** normalizeEntry gives an entry from the wire its array and string fields. */
export function normalizeEntry(e: Entry): Entry {
  return { ...e, text: typeof e.text === "string" ? e.text : "", cards: Array.isArray(e.cards) ? e.cards : [] };
}

export function activityAPI(t: Transport): ActivityAPI {
  return {
    list: async (p) => {
      const l = await t.call<EntryList>("activity.list", p);
      return { entries: Array.isArray(l?.entries) ? l.entries.map(normalizeEntry) : [], more: l?.more === true };
    },
    routines: async () => {
      const r = await t.call<Routine[]>("activity.routines", {});
      return Array.isArray(r) ? r : [];
    },
  };
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * parseThreadEvent reads the params of an "event" notification, lodge's
 * kit envelope `{event, data, seq?}`: "turn" carries a Turn as data,
 * "working" carries `{working}`, and "activity" an Entry. It ignores `seq`, and returns null for any
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
    case "activity":
      if (typeof data.id !== "number" || typeof data.kind !== "string") return null;
      return { type: "activity", entry: normalizeEntry(data as unknown as Entry) };
  }
  return null;
}
