// The thread as the bundle holds it: turns in id order, and whether a reply
// is in progress. Every function returns a new state and leaves turns it
// does not change as the same objects, so the view can skip them.

import type { Card, Sent, ThreadEvent, Turn, TurnList } from "./api";

export interface ThreadState {
  /** The loaded turns, by increasing id. */
  readonly turns: readonly Turn[];
  /** Whether older turns exist before the first. */
  readonly more: boolean;
  /** Whether the first page has arrived. */
  readonly loaded: boolean;
  /** Whether porterd says a reply is in progress (subscribe and working events). */
  readonly working: boolean;
  /** Whether a thread.send is in flight. */
  readonly sending: boolean;
  /** The id of the reply to this client's last send, until it is done. */
  readonly awaiting: number | null;
  /** A working:false event arrived while a send was in flight. */
  readonly idleWhileSending: boolean;
}

export const emptyThread: ThreadState = {
  turns: [],
  more: false,
  loaded: false,
  working: false,
  sending: false,
  awaiting: null,
  idleWhileSending: false,
};

/** busy reports whether a reply is in progress, so the owner cannot send. */
export function busy(s: ThreadState): boolean {
  return s.sending || s.working || s.awaiting !== null;
}

/** indexOf returns the index of turn id, or -(insertion point) - 1. */
function indexOf(turns: readonly Turn[], id: number): number {
  let lo = 0;
  let hi = turns.length - 1;
  while (lo <= hi) {
    const mid = (lo + hi) >>> 1;
    const t = turns[mid] as Turn;
    if (t.id === id) return mid;
    if (t.id < id) lo = mid + 1;
    else hi = mid - 1;
  }
  return -lo - 1;
}

/** findTurn returns the loaded turn with id, if any. */
export function findTurn(s: ThreadState, id: number): Turn | undefined {
  const i = indexOf(s.turns, id);
  return i >= 0 ? s.turns[i] : undefined;
}

/** settle clears `awaiting` once the awaited reply is done. */
function settle(s: ThreadState): ThreadState {
  if (s.awaiting === null) return s;
  return findTurn(s, s.awaiting)?.done ? { ...s, awaiting: null } : s;
}

type Pick = (current: Turn, incoming: Turn) => Turn;

const latest: Pick = (_current, incoming) => incoming;

/**
 * fresher picks between a turn already shown and the same turn from a
 * page. A page can be read before an event that already arrived, so a
 * page's copy never takes back what the shown copy has: completion, or a
 * capture's outcome.
 */
const fresher: Pick = (current, incoming) => {
  if (current.done && !incoming.done) return current;
  for (const c of current.cards) {
    if (c.status === "pending") continue;
    if (incoming.cards.some((i) => i.id === c.id && i.status === "pending")) return current;
  }
  return incoming;
};

function merge(turns: readonly Turn[], incoming: readonly Turn[], pick: Pick): Turn[] {
  const out = [...turns];
  for (const t of incoming) {
    const i = indexOf(out, t.id);
    if (i >= 0) out[i] = pick(out[i] as Turn, t);
    else out.splice(-i - 1, 0, t);
  }
  return out;
}

/** upsert adds turns, or replaces those with the same id, keeping id order. */
export function upsert(s: ThreadState, turns: readonly Turn[]): ThreadState {
  if (turns.length === 0) return s;
  return settle({ ...s, turns: merge(s.turns, turns, latest) });
}

/**
 * newestPage takes the newest page, on the first load and on every
 * reconnect. When the page does not reach back to the turns already shown
 * and older ones exist, it replaces them rather than leave a gap.
 */
export function newestPage(s: ThreadState, page: TurnList): ThreadState {
  const first = page.turns.reduce((m, t) => Math.min(m, t.id), Number.POSITIVE_INFINITY);
  const last = s.turns.at(-1);
  if (s.turns.length === 0 || (page.more && last !== undefined && first > last.id)) {
    return settle({ ...s, turns: merge([], page.turns, latest), more: page.more, loaded: true });
  }
  const oldest = s.turns[0] as Turn;
  const more = oldest.id < first ? s.more : page.more;
  return settle({ ...s, turns: merge(s.turns, page.turns, fresher), more, loaded: true });
}

/** olderPage adds a page from before the first turn. */
export function olderPage(s: ThreadState, page: TurnList): ThreadState {
  return settle({ ...s, turns: merge(s.turns, page.turns, fresher), more: page.more });
}

/** applyEvent applies a subscribed connection's event. An activity entry leaves the thread as it is. */
export function applyEvent(s: ThreadState, e: ThreadEvent): ThreadState {
  if (e.type === "activity") return s;
  if (e.type === "working") {
    if (e.working) return { ...s, working: true };
    return { ...s, working: false, awaiting: null, idleWhileSending: s.sending || s.idleWhileSending };
  }
  // A turn before the loaded window, with older turns unloaded, waits until
  // its page is loaded: showing it alone would hide the gap.
  const oldest = s.turns[0];
  if (s.more && oldest !== undefined && e.turn.id < oldest.id) return s;
  return upsert(s, [e.turn]);
}

/** subscribed takes thread.subscribe's answer, on every (re)connect. */
export function subscribed(s: ThreadState, working: boolean): ThreadState {
  return { ...s, working, sending: false, awaiting: null, idleWhileSending: false };
}

/** sendStarted marks a thread.send in flight. */
export function sendStarted(s: ThreadState): ThreadState {
  return { ...s, sending: true, idleWhileSending: false };
}

/** sendSucceeded takes thread.send's answer and waits for the reply it names. */
export function sendSucceeded(s: ThreadState, sent: Sent): ThreadState {
  const next = upsert({ ...s, sending: false }, [sent.turn]);
  const ended = s.idleWhileSending || findTurn(next, sent.reply)?.done === true;
  return { ...next, awaiting: ended ? null : sent.reply, idleWhileSending: false };
}

/** sendFailed ends a thread.send that failed; a conflict means a reply is in progress. */
export function sendFailed(s: ThreadState, conflict: boolean): ThreadState {
  const working = conflict && !s.idleWhileSending ? true : s.working;
  return { ...s, sending: false, idleWhileSending: false, working };
}

/** findCard returns the card with id from whichever loaded turn holds it. */
export function findCard(s: ThreadState, cardID: string): Card | undefined {
  for (const t of s.turns) {
    const c = t.cards.find((x) => x.id === cardID);
    if (c !== undefined) return c;
  }
  return undefined;
}

/** patchCard changes one card in whichever loaded turn holds it. */
export function patchCard(s: ThreadState, cardID: string, patch: Partial<Omit<Card, "id">>): ThreadState {
  const i = s.turns.findIndex((t) => t.cards.some((c) => c.id === cardID));
  if (i < 0) return s;
  const t = s.turns[i] as Turn;
  const turns = [...s.turns];
  turns[i] = { ...t, cards: t.cards.map((c) => (c.id === cardID ? { ...c, ...patch } : c)) };
  return { ...s, turns };
}
