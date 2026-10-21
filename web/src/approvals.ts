// The full app's Approvals: every capture and approval waiting for the
// owner, newest first, each acted on exactly as in the thread. It reads
// thread.pending on every (re)connect and follows turn events, so a card
// that stops waiting leaves and a new one appears.

import { type Card, Code, errorCode, type ThreadAPI, type Turn, type TurnList } from "./api";
import { type CardHost, renderCard, sameCard } from "./cards";
import { el, reconcile } from "./dom";
import { timeEl } from "./time";

export interface PendingState {
  /** The turns holding a waiting card, newest first. */
  readonly turns: readonly Turn[];
  /** Whether thread.pending has answered. */
  readonly loaded: boolean;
}

export const emptyPending: PendingState = { turns: [], loaded: false };

/** waiting reports whether a card waits for the owner: a pending capture or approval. */
export function waiting(c: Card): boolean {
  return c.status === "pending" && (c.kind === "capture" || c.kind === "approval");
}

/** put adds or replaces a turn while it holds a waiting card, and drops it otherwise. */
function put(turns: readonly Turn[], t: Turn): Turn[] {
  const out = turns.filter((x) => x.id !== t.id);
  if (t.cards.some(waiting)) {
    out.push(t);
    out.sort((a, b) => b.id - a.id);
  }
  return out;
}

/** pendingPage takes thread.pending's answer. */
export function pendingPage(page: TurnList): PendingState {
  return { turns: page.turns.reduce<Turn[]>(put, []), loaded: true };
}

/** pendingTurn takes a turn event: the turn joins, changes or leaves. */
export function pendingTurn(s: PendingState, t: Turn): PendingState {
  if (!s.turns.some((x) => x.id === t.id) && !t.cards.some(waiting)) return s;
  return { ...s, turns: put(s.turns, t) };
}

/** settleCard marks a card the owner acted on, which then leaves. */
export function settleCard(s: PendingState, cardID: string, patch: Partial<Omit<Card, "id">>): PendingState {
  const t = s.turns.find((x) => x.cards.some((c) => c.id === cardID));
  if (t === undefined) return s;
  return pendingTurn(s, { ...t, cards: t.cards.map((c) => (c.id === cardID ? { ...c, ...patch } : c)) });
}

/** pendingCount is how many cards wait. */
export function pendingCount(s: PendingState): number {
  return s.turns.reduce((n, t) => n + t.cards.filter(waiting).length, 0);
}

function message(err: unknown): string {
  return err instanceof Error && err.message ? err.message : String(err);
}

interface Item {
  turn: Turn;
  el: HTMLElement;
  cardsEl: HTMLElement;
  cards: Map<string, { card: Card; el: HTMLElement }>;
}

export class ApprovalsView {
  readonly el: HTMLElement;
  #api: ThreadAPI;
  #cards: CardHost;
  #onChange: () => void;
  #state: PendingState = emptyPending;
  #error: string | null = null;
  #seq = 0;
  /** Turn events while thread.pending is in flight, applied over its answer. */
  #buffer: Turn[] | null = null;
  #items = new Map<number, Item>();

  #listEl: HTMLElement;
  #noteEl: HTMLElement;

  constructor(api: ThreadAPI, cards: CardHost, onChange: () => void) {
    this.#api = api;
    this.#cards = cards;
    this.#onChange = onChange;
    this.el = el("section", "view approvals");
    const inner = el("div", "view-inner");
    this.#noteEl = el("div", "view-note muted", "Loading…");
    this.#listEl = el("div", "pending-list");
    this.#listEl.setAttribute("aria-label", "Waiting for you, newest first");
    inner.append(el("h2", "view-title", "Waiting for you"), this.#noteEl, this.#listEl);
    this.el.append(inner);
  }

  /** How many cards wait, for the tab's badge. */
  get count(): number {
    return pendingCount(this.#state);
  }

  /** load reads thread.pending, on every (re)connect. */
  async load(): Promise<void> {
    const seq = ++this.#seq;
    const buffer: Turn[] = [];
    this.#buffer = buffer;
    try {
      const page = await this.#api.pending();
      if (seq !== this.#seq) return;
      this.#state = buffer.reduce(pendingTurn, pendingPage(page));
      this.#error = null;
    } catch (err) {
      if (seq !== this.#seq || errorCode(err) === Code.disconnected) return;
      this.#error = `Couldn't load what's waiting: ${message(err)}`;
    } finally {
      if (seq === this.#seq) this.#buffer = null;
    }
    this.#render();
  }

  /** turn takes a turn event. */
  turn(t: Turn): void {
    this.#buffer?.push(t);
    this.#set(pendingTurn(this.#state, t));
  }

  /** settle drops a card the owner acted on, without waiting for its turn event. */
  settle(cardID: string, patch: Partial<Omit<Card, "id">>): void {
    this.#set(settleCard(this.#state, cardID, patch));
  }

  #set(next: PendingState): void {
    if (next === this.#state) return;
    this.#state = next;
    this.#render();
  }

  #render(): void {
    const s = this.#state;
    const next = new Map<number, Item>();
    const nodes: HTMLElement[] = [];
    for (const t of s.turns) {
      const item = this.#items.get(t.id) ?? this.#item(t);
      this.#setCards(item, t);
      next.set(t.id, item);
      nodes.push(item.el);
    }
    this.#items = next;
    reconcile(this.#listEl, nodes);

    let note = "";
    if (this.#error !== null) note = this.#error;
    else if (!s.loaded) note = "Loading…";
    else if (s.turns.length === 0) note = "Nothing is waiting for you.";
    this.#noteEl.textContent = note;
    this.#noteEl.className = this.#error === null ? "view-note muted" : "view-note error";
    this.#noteEl.hidden = note === "";
    this.#onChange();
  }

  #item(t: Turn): Item {
    const root = el("div", "pending-item");
    root.dataset.turn = String(t.id);
    const cardsEl = el("div", "cards");
    root.append(timeEl(t.at, "pending-time"), cardsEl);
    return { turn: t, el: root, cardsEl, cards: new Map() };
  }

  /** setCards shows a turn's waiting cards, keeping each element (and a capture's edit) while it renders alike. */
  #setCards(item: Item, t: Turn): void {
    item.turn = t;
    const next = new Map<string, { card: Card; el: HTMLElement }>();
    const nodes: HTMLElement[] = [];
    for (const card of t.cards.filter(waiting)) {
      const prev = item.cards.get(card.id);
      const e = prev && sameCard(prev.card, card) ? prev.el : renderCard(card, this.#cards, prev?.el);
      next.set(card.id, { card, el: e });
      nodes.push(e);
    }
    item.cards = next;
    reconcile(item.cardsEl, nodes);
  }
}
