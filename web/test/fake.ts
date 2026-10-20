// Fakes for the bundle's tests: a transport that records calls and answers
// from handlers, and made-up turns, Activity entries and routines.
import { type Card, Code, type Entry, type Routine, RpcError, type Transport, type Turn } from "../src/api";

export type Handler = (params: Record<string, unknown>) => unknown;

export class FakeTransport implements Transport {
  readonly calls: { method: string; params: Record<string, unknown> }[] = [];
  readonly handlers = new Map<string, Handler>();

  on(method: string, h: Handler): this {
    this.handlers.set(method, h);
    return this;
  }

  async call<T>(method: string, params: object = {}): Promise<T> {
    const p = { ...params } as Record<string, unknown>;
    this.calls.push({ method, params: p });
    const h = this.handlers.get(method);
    if (h === undefined) throw new RpcError(Code.methodNotFound, `no ${method}`);
    return (await h(p)) as T;
  }

  /** paramsOf returns the params of every call to method, in order. */
  paramsOf(method: string): Record<string, unknown>[] {
    return this.calls.filter((c) => c.method === method).map((c) => c.params);
  }
}

export function turn(id: number, patch: Partial<Turn> = {}): Turn {
  return { id, at: "2026-10-19T09:00:00Z", role: "porter", text: "", cards: [], done: true, ...patch };
}

export function card(id: string, patch: Partial<Card> = {}): Card {
  return { id, kind: "tool", status: "done", app: "notes", tool: "read_note", summary: "Read Plan.md", ...patch };
}

export function entry(id: number, patch: Partial<Entry> = {}): Entry {
  return {
    id,
    at: "2026-10-19T06:30:00Z",
    kind: "run",
    routine: "Morning brief",
    ok: true,
    text: "Wrote the brief.",
    cards: [],
    ...patch,
  };
}

export function routine(name: string, patch: Partial<Routine> = {}): Routine {
  return {
    name,
    path: `Porter/Routines/${name}.md`,
    url: `/notes/1/Porter/Routines/${name}.md`,
    valid: true,
    schedule: "30 6 * * 1-5",
    timezone: "Europe/Lisbon",
    enabled: true,
    ...patch,
  };
}

/** deferred is a promise to settle by hand, for a call held in flight. */
export function deferred<T>(): { promise: Promise<T>; resolve(v: T): void; reject(e: unknown): void } {
  let resolve: (v: T) => void = () => {};
  let reject: (e: unknown) => void = () => {};
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** flush lets pending promise callbacks and zero-delay timers run. */
export function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}
