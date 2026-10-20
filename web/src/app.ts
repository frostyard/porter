// Porter's chat: the thread, its cards, and the input with its context
// chip; in the full app, search and the Activity and Approvals tabs as well
// (design/overview.md, The UI; ADR-0006). It speaks to porterd only through
// a Transport, and to lodge only through Shell, so tests drive it with
// fakes.

import type { AppContext, Role } from "@frostyard/lodge-client";
import { ActivityView } from "./activity";
import {
  activityAPI,
  type Card,
  Code,
  type Context,
  errorCode,
  parseThreadEvent,
  type ThreadAPI,
  type Transport,
  type Turn,
  threadAPI,
} from "./api";
import { ApprovalsView } from "./approvals";
import { type CardHost, renderCard, sameCard } from "./cards";
import { el, icon, reconcile } from "./dom";
import { type LinkOpener, markdownText, renderMarkdown } from "./markdown";
import {
  applyEvent,
  busy,
  emptyThread,
  findCard,
  findTurn,
  newestPage,
  olderPage,
  patchCard,
  sendFailed,
  sendStarted,
  sendSucceeded,
  subscribed,
  type ThreadState,
  upsert,
} from "./thread";
import { dayKey, dayLabel, localDate } from "./time";

/** What the chat needs of lodge-client. */
export interface Shell {
  readonly role: Role;
  /** lodge.open: a lodge address, or an outside URL. */
  open(url: string): void;
}

export interface AppOptions {
  root: HTMLElement;
  shell: Shell;
  transport: Transport;
}

/** The newest page, and each older page scrolling up loads. */
const PAGE = 50;
/** The page size when loading back to a search result. */
const REVEAL_PAGE = 200;
/** thread.send's limit on the text. */
const MAX_SEND_BYTES = 16_384;
/** How close to the bottom, in pixels, still counts as at the bottom. */
const PIN_SLACK = 40;
/** How close to the top, in pixels, loads older turns. */
const LOAD_SLACK = 300;

/** The full app's tabs. */
type Tab = "thread" | "activity" | "approvals";

const TABS: readonly { tab: Tab; label: string }[] = [
  { tab: "thread", label: "Thread" },
  { tab: "activity", label: "Activity" },
  { tab: "approvals", label: "Approvals" },
];

interface Env {
  links: LinkOpener;
  cards: CardHost;
}

const ROLE_LABEL: Record<string, string> = { owner: "You", porter: "Porter", notice: "Porter" };

function contextLabel(c: Context): string {
  return `About: ${c.title || c.kind}`;
}

function contextLine(className: string, c: Context): HTMLElement {
  const line = el("div", className);
  line.append(el("span", "ctx-app", c.app), el("span", "ctx-title", contextLabel(c)));
  line.title = `${c.app}: ${c.title || c.ref}`;
  return line;
}

function sameContext(a: Context | undefined, b: Context | undefined): boolean {
  return a?.app === b?.app && a?.kind === b?.kind && a?.ref === b?.ref && a?.title === b?.title;
}

function workingDots(): HTMLElement {
  const w = el("div", "working");
  w.setAttribute("role", "status");
  w.setAttribute("aria-label", "Porter is working");
  w.append(el("span"), el("span"), el("span"));
  return w;
}

/** One turn's element, updated in place as the turn changes. */
class TurnView {
  readonly el: HTMLElement;
  #env: Env;
  #turn: Turn;
  #context: HTMLElement | null = null;
  #body: HTMLElement;
  #cards: HTMLElement | null = null;
  #cardEls = new Map<string, { card: Card; el: HTMLElement }>();
  #indicator: HTMLElement | null = null;
  #indicatorBusy: boolean | null = null;

  constructor(turn: Turn, env: Env, isBusy: boolean) {
    this.#env = env;
    this.#turn = turn;
    this.el = el(turn.role === "notice" ? "div" : "article", `turn ${turn.role}`);
    this.el.dataset.id = String(turn.id);
    const d = localDate(turn.at);
    if (d !== null) this.el.title = d.toLocaleString();
    this.#body = el("div", turn.role === "owner" ? "bubble" : "md");
    if (turn.role === "owner") this.#setContext(turn.context);
    this.el.append(this.#body);
    this.#setText(turn.text);
    // Porter's replies and notices (a routine's run) carry cards; the owner's turns never do.
    if (turn.role !== "owner") {
      this.#cards = el("div", "cards");
      this.el.append(this.#cards);
      this.#setCards(turn.cards);
    }
    this.#setIndicator(isBusy);
  }

  update(turn: Turn, isBusy: boolean): void {
    const old = this.#turn;
    this.#turn = turn;
    if (turn !== old) {
      if (turn.text !== old.text) this.#setText(turn.text);
      if (turn.role === "owner" && !sameContext(turn.context, old.context)) this.#setContext(turn.context);
      if (turn.cards !== old.cards) this.#setCards(turn.cards);
    }
    this.#setIndicator(isBusy);
  }

  #setContext(c: Context | undefined): void {
    const next = c?.kind ? contextLine("turn-context", c) : null;
    if (this.#context !== null) this.#context.remove();
    this.#context = next;
    if (next !== null) this.el.prepend(next);
  }

  #setText(text: string): void {
    if (this.#turn.role === "owner") {
      this.#body.textContent = text;
      return;
    }
    this.#body.replaceChildren(renderMarkdown(text, this.#env.links));
    this.#body.hidden = text === "";
  }

  #setCards(cards: readonly Card[]): void {
    if (this.#cards === null) return;
    const next = new Map<string, { card: Card; el: HTMLElement }>();
    const nodes: HTMLElement[] = [];
    for (const card of cards) {
      const prev = this.#cardEls.get(card.id);
      const e = prev && sameCard(prev.card, card) ? prev.el : renderCard(card, this.#env.cards, prev?.el);
      next.set(card.id, { card, el: e });
      nodes.push(e);
    }
    this.#cardEls = next;
    reconcile(this.#cards, nodes);
    this.#cards.hidden = nodes.length === 0;
    this.el.classList.toggle("with-cards", nodes.length > 0);
  }

  /** While Porter's turn is not done: working dots, or a note that it stopped. */
  #setIndicator(isBusy: boolean): void {
    const show = this.#turn.role === "porter" && !this.#turn.done;
    if (!show) {
      this.#indicator?.remove();
      this.#indicator = null;
      this.#indicatorBusy = null;
      return;
    }
    if (this.#indicator !== null && this.#indicatorBusy === isBusy) return;
    const next = isBusy ? workingDots() : el("div", "unfinished", "Porter stopped before finishing this reply.");
    if (this.#indicator !== null) this.#indicator.replaceWith(next);
    else this.el.append(next);
    this.#indicator = next;
    this.#indicatorBusy = isBusy;
  }
}

type NoteKind = "busy" | "error" | "info";

export class App {
  #shell: Shell;
  #api: ThreadAPI;
  #env: Env;
  #state: ThreadState = emptyThread;
  #ready = false;
  #everReady = false;
  #failure: string | null = null;
  #context: Context | null = null;
  #dismissed = false;
  #pendingAsk: { app: string; prompt: string } | null = null;
  #pinned = true;
  #loadingOlder: Promise<boolean> | null = null;
  #views = new Map<number, TurnView>();
  #dividers = new Map<string, HTMLElement>();
  #note: { kind: NoteKind; text: string } | null = null;
  #searchSeq = 0;
  #searchTimer: ReturnType<typeof setTimeout> | undefined;
  #tab: Tab = "thread";
  #threadScroll = 0;
  #activity: ActivityView | null = null;
  #approvals: ApprovalsView | null = null;

  #wrapEl: HTMLElement;
  #threadEl: HTMLElement;
  #listEl: HTMLElement;
  #topEl: HTMLElement;
  #emptyEl: HTMLElement;
  #placeholder: HTMLElement;
  #jumpEl: HTMLButtonElement;
  #statusEl: HTMLElement;
  #chipEl: HTMLElement;
  #inputEl: HTMLTextAreaElement;
  #sendEl: HTMLButtonElement;
  #noteEl: HTMLElement;
  #composerEl: HTMLElement;
  #resultsEl: HTMLElement | null = null;
  #searchEl: HTMLInputElement | null = null;
  #tabEls = new Map<Tab, HTMLButtonElement>();
  #badgeEl: HTMLElement | null = null;
  #barStatusEl: HTMLElement | null = null;

  constructor(o: AppOptions) {
    this.#shell = o.shell;
    this.#api = threadAPI(o.transport);
    this.#env = {
      links: { open: (path) => this.#shell.open(path) },
      cards: {
        open: (url) => this.#shell.open(url),
        confirm: (card, text) => this.#confirm(card, text),
        cancel: (card) => this.#cancel(card),
      },
    };

    const root = o.root;
    root.classList.add("chat", `role-${o.shell.role}`);
    root.replaceChildren();

    const wrap = el("div", "thread-wrap");
    this.#wrapEl = wrap;
    if (o.shell.role === "app") {
      this.#activity = new ActivityView(activityAPI(o.transport), this.#env.cards);
      this.#approvals = new ApprovalsView(this.#api, this.#env.cards, () => this.#renderBadge());
      root.append(this.#buildHeader(wrap));
    }

    this.#threadEl = el("div", "thread");
    this.#topEl = el("div", "thread-top");
    this.#listEl = el("div", "turns");
    this.#listEl.setAttribute("role", "log");
    this.#listEl.setAttribute("aria-label", "Your thread with Porter");
    this.#emptyEl = el("div", "empty", "Ask Porter about what's on screen, or anything else.");
    this.#emptyEl.hidden = true;
    this.#placeholder = el("div", "turn porter pending");
    this.#placeholder.append(workingDots());
    this.#threadEl.append(this.#topEl, this.#emptyEl, this.#listEl);
    this.#threadEl.addEventListener("scroll", () => this.#onScroll(), { passive: true });

    this.#jumpEl = el("button", "jump");
    this.#jumpEl.type = "button";
    this.#jumpEl.setAttribute("aria-label", "Jump to the latest");
    this.#jumpEl.title = "Jump to the latest";
    this.#jumpEl.append(icon(["M12 5v14", "M6 13l6 6 6-6"]));
    this.#jumpEl.hidden = true;
    this.#jumpEl.addEventListener("click", () => {
      this.#pinned = true;
      this.#scrollToBottom();
      this.#jumpEl.hidden = true;
    });
    wrap.append(this.#threadEl, this.#jumpEl);
    root.append(wrap);
    if (this.#activity !== null && this.#approvals !== null) {
      root.append(this.#panel(this.#activity.el, "activity"), this.#panel(this.#approvals.el, "approvals"));
      this.#panel(wrap, "thread");
    }

    const composer = el("form", "composer");
    this.#composerEl = composer;
    composer.setAttribute("aria-label", "Message Porter");
    this.#statusEl = el("div", "status");
    this.#statusEl.setAttribute("role", "status");
    this.#chipEl = el("div", "chip");
    this.#chipEl.hidden = true;
    const row = el("div", "row");
    this.#inputEl = el("textarea", "input");
    this.#inputEl.rows = 1;
    this.#inputEl.setAttribute("aria-label", "Message Porter");
    this.#inputEl.setAttribute("enterkeyhint", "send");
    this.#inputEl.addEventListener("keydown", (e) => this.#onKey(e));
    this.#inputEl.addEventListener("input", () => {
      this.#autosize();
      if (this.#note?.kind !== "busy") this.#setNote(null);
      this.#renderComposer();
    });
    this.#sendEl = el("button", "send");
    this.#sendEl.type = "submit";
    this.#sendEl.setAttribute("aria-label", "Send");
    this.#sendEl.title = "Send";
    this.#sendEl.append(icon(["M12 19V5", "M6 11l6-6 6 6"]));
    row.append(this.#inputEl, this.#sendEl);
    this.#noteEl = el("div", "note");
    this.#noteEl.setAttribute("aria-live", "polite");
    composer.append(this.#statusEl, this.#chipEl, row, this.#noteEl);
    composer.addEventListener("submit", (e) => {
      e.preventDefault();
      this.#submit();
    });
    root.append(composer);

    if (typeof ResizeObserver !== "undefined") {
      new ResizeObserver(() => {
        if (this.#pinned) this.#scrollToBottom();
      }).observe(this.#threadEl);
    }
    if (this.#activity !== null) this.#select("thread");
    this.#render();
  }

  /** The thread as it stands, for tests. */
  get state(): ThreadState {
    return this.#state;
  }

  /**
   * connected runs after rpc.hello: subscribe, then load the newest page
   * and, in the full app, Activity and Approvals.
   */
  async connected(): Promise<void> {
    this.#ready = true;
    this.#everReady = true;
    this.#failure = null;
    this.#render();
    let views: Promise<unknown> = Promise.resolve();
    try {
      const working = await this.#api.subscribe();
      this.#state = subscribed(this.#state, working);
      // After subscribing, so no event falls between a view's read and its events.
      views = Promise.all([this.#activity?.load(), this.#approvals?.load()]);
      const firstLoad = !this.#state.loaded;
      const page = await this.#api.list({ limit: PAGE });
      this.#state = newestPage(this.#state, page);
      if (firstLoad) this.#pinned = true;
      this.#render();
      await this.#fill();
    } catch (err) {
      if (errorCode(err) !== Code.disconnected) this.#setNote({ kind: "error", text: `Couldn't load the thread: ${message(err)}` });
      return;
    }
    const ask = this.#pendingAsk;
    this.#pendingAsk = null;
    if (ask !== null && this.#inputEl.value === ask.prompt) this.ask(ask.app, ask.prompt);
    await views;
  }

  /** disconnected runs when the socket closes. */
  disconnected(): void {
    this.#ready = false;
    this.#render();
  }

  /** failed shows why rpc.hello failed. */
  failed(text: string): void {
    this.#failure = text;
    this.#render();
  }

  /** event takes an "event" notification's params. */
  event(params: unknown): void {
    const e = parseThreadEvent(params);
    if (e === null) return;
    if (e.type === "activity") {
      // A run may change a routine's last and next runs.
      this.#activity?.entry(e.entry);
      void this.#activity?.refreshRoutines();
      return;
    }
    if (e.type === "turn") {
      this.#approvals?.turn(e.turn);
      this.#activity?.turn(e.turn);
    }
    this.#state = applyEvent(this.#state, e);
    this.#render();
  }

  /** context takes the current app's context (lodge's companion context). */
  context(c: AppContext): void {
    const next: Context | null = c.kind === null ? null : { app: c.app, kind: c.kind, ref: c.ref, title: c.title };
    const prev = this.#context;
    const same = prev !== null && next !== null && prev.app === next.app && prev.kind === next.kind && prev.ref === next.ref;
    if (!same) this.#dismissed = false;
    this.#context = next;
    this.#renderChip();
  }

  /** ask fills the input with an app's "Ask about this" and sends it with that app's context. */
  ask(app: string, prompt: string): void {
    this.#inputEl.value = prompt;
    this.#autosize();
    const ctx = this.#context !== null && this.#context.app === app ? this.#context : undefined;
    if (ctx !== undefined) {
      this.#dismissed = false;
      this.#renderChip();
    }
    if (!this.#ready || !this.#state.loaded) {
      this.#pendingAsk = { app, prompt };
      this.#renderComposer();
      return;
    }
    void this.#send(prompt, ctx);
  }

  /** shown runs when the companion opens. */
  shown(): void {
    if (this.#pinned) this.#scrollToBottom();
    const fine = typeof matchMedia === "function" && matchMedia("(pointer: fine)").matches;
    if (fine) this.#inputEl.focus({ preventScroll: true });
  }

  /** reveal scrolls to a turn, loading older pages until it is loaded. */
  async reveal(id: number): Promise<void> {
    for (let i = 0; i < 100; i++) {
      const first = this.#state.turns[0];
      if (findTurn(this.#state, id) || !this.#state.more || first === undefined || first.id < id) break;
      if (!(await this.#loadOlder(REVEAL_PAGE))) break;
    }
    const v = this.#views.get(id);
    if (v === undefined) {
      this.#setNote({ kind: "info", text: "That message isn't in the thread any more." });
      return;
    }
    this.#pinned = false;
    v.el.scrollIntoView?.({ block: "center" });
    v.el.classList.add("flash");
    setTimeout(() => v.el.classList.remove("flash"), 1600);
  }

  // ---- the composer ----

  #onKey(e: KeyboardEvent): void {
    if (e.key !== "Enter" || e.shiftKey || e.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    this.#submit();
  }

  #submit(): void {
    const text = this.#inputEl.value;
    if (text.trim() === "") return;
    void this.#send(text, this.#context !== null && !this.#dismissed ? this.#context : undefined);
  }

  async #send(raw: string, context: Context | undefined): Promise<void> {
    const text = raw.trim();
    if (text === "") return;
    if (!this.#ready) {
      this.#setNote({ kind: "error", text: "Not connected yet. Your message is still here." });
      return;
    }
    if (busy(this.#state)) {
      this.#setNote({ kind: "busy", text: "Porter is still replying. Send this when it's done." });
      return;
    }
    if (new TextEncoder().encode(text).length > MAX_SEND_BYTES) {
      this.#setNote({ kind: "error", text: "That message is too long for Porter (16 KB at most)." });
      return;
    }
    this.#state = sendStarted(this.#state);
    this.#setNote(null);
    this.#pinned = true;
    this.#render();
    try {
      const sent = await this.#api.send(context !== undefined ? { text, context: { ...context } } : { text });
      this.#state = sendSucceeded(this.#state, sent);
      if (this.#inputEl.value === raw) {
        this.#inputEl.value = "";
        this.#autosize();
      }
    } catch (err) {
      const conflict = errorCode(err) === Code.conflict;
      this.#state = sendFailed(this.#state, conflict);
      this.#setNote(
        conflict
          ? { kind: "busy", text: "Porter is still replying. Your message is still here; send it when the reply is done." }
          : { kind: "error", text: errorCode(err) === Code.disconnected ? "Not connected. Your message is still here." : `Couldn't send: ${message(err)}` },
      );
    }
    this.#render();
  }

  #autosize(): void {
    const t = this.#inputEl;
    t.style.height = "auto";
    if (t.scrollHeight > 0) t.style.height = `${t.scrollHeight}px`;
  }

  #setNote(note: { kind: NoteKind; text: string } | null): void {
    this.#note = note;
    this.#noteEl.textContent = note?.text ?? "";
    this.#noteEl.className = note === null ? "note" : `note ${note.kind}`;
    this.#noteEl.hidden = note === null;
  }

  // ---- captures and approvals ----

  async #confirm(card: Card, text: string | undefined): Promise<void> {
    const notice = await this.#api.confirm(text === undefined ? { card: card.id } : { card: card.id, text });
    this.#settled(card.id, text === undefined ? { status: "done" } : { status: "done", text });
    this.#state = upsert(this.#state, [notice]);
    this.#render();
  }

  async #cancel(card: Card): Promise<void> {
    await this.#api.cancel({ card: card.id });
    this.#settled(card.id, { status: "cancelled" });
    this.#render();
  }

  /**
   * settled marks a card the owner acted on. The card's turn event usually
   * comes first and may say the action failed, so only a card still
   * pending changes.
   */
  #settled(cardID: string, patch: Partial<Omit<Card, "id">>): void {
    if (findCard(this.#state, cardID)?.status === "pending") this.#state = patchCard(this.#state, cardID, patch);
    this.#approvals?.settle(cardID, patch);
  }

  // ---- scrolling ----

  #onScroll(): void {
    const t = this.#threadEl;
    this.#pinned = t.scrollHeight - t.scrollTop - t.clientHeight < PIN_SLACK;
    this.#jumpEl.hidden = this.#pinned;
    if (t.scrollTop < LOAD_SLACK) void this.#loadOlder();
  }

  #scrollToBottom(): void {
    this.#threadEl.scrollTop = this.#threadEl.scrollHeight;
  }

  /** fill loads older pages while the thread does not yet scroll. */
  async #fill(): Promise<void> {
    for (let i = 0; i < 4; i++) {
      const t = this.#threadEl;
      if (!this.#state.more || t.clientHeight === 0 || t.scrollHeight > t.clientHeight + PIN_SLACK) return;
      if (!(await this.#loadOlder())) return;
    }
  }

  #loadOlder(limit = PAGE): Promise<boolean> {
    if (this.#loadingOlder !== null) return this.#loadingOlder;
    const first = this.#state.turns[0];
    if (!this.#ready || !this.#state.more || first === undefined) return Promise.resolve(false);
    const run = async (): Promise<boolean> => {
      try {
        const page = await this.#api.list({ before: first.id, limit });
        const t = this.#threadEl;
        const height = t.scrollHeight;
        const top = t.scrollTop;
        this.#state = olderPage(this.#state, page);
        this.#render();
        // Keep the turns on screen where they were.
        if (!this.#pinned) t.scrollTop = top + (t.scrollHeight - height);
        return true;
      } catch {
        return false;
      } finally {
        this.#loadingOlder = null;
        this.#renderTop();
      }
    };
    this.#loadingOlder = run();
    this.#renderTop();
    return this.#loadingOlder;
  }

  // ---- tabs (the full app) ----

  #panel(view: HTMLElement, tab: Tab): HTMLElement {
    view.id = `porter-${tab}`;
    view.setAttribute("role", "tabpanel");
    view.setAttribute("aria-labelledby", `porter-tab-${tab}`);
    return view;
  }

  #buildTabs(): HTMLElement {
    const tabs = el("div", "tabs");
    tabs.setAttribute("role", "tablist");
    tabs.setAttribute("aria-label", "Porter");
    for (const { tab, label } of TABS) {
      const b = el("button", "tab", label);
      b.type = "button";
      b.id = `porter-tab-${tab}`;
      b.dataset.tab = tab;
      b.setAttribute("role", "tab");
      b.setAttribute("aria-controls", `porter-${tab}`);
      b.addEventListener("click", () => this.#select(tab));
      if (tab === "approvals") {
        this.#badgeEl = el("span", "tab-count");
        this.#badgeEl.hidden = true;
        b.append(this.#badgeEl);
      }
      this.#tabEls.set(tab, b);
      tabs.append(b);
    }
    // Arrow keys move between tabs (WAI-ARIA's tabs pattern).
    tabs.addEventListener("keydown", (e) => {
      const order = TABS.map((t) => t.tab);
      const i = order.indexOf(this.#tab);
      const to: Record<string, number> = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: order.length - 1 };
      const j = to[e.key];
      if (j === undefined) return;
      const next = order[(j + order.length) % order.length] as Tab;
      e.preventDefault();
      this.#select(next);
      this.#tabEls.get(next)?.focus();
    });
    return tabs;
  }

  /** select shows a tab: the thread with its input and search, Activity, or Approvals. */
  #select(tab: Tab): void {
    const leaving = this.#tab;
    if (leaving === "thread" && tab !== "thread") this.#threadScroll = this.#threadEl.scrollTop;
    this.#tab = tab;
    for (const [t, b] of this.#tabEls) {
      b.setAttribute("aria-selected", String(t === tab));
      b.tabIndex = t === tab ? 0 : -1;
    }
    const thread = tab === "thread";
    this.#wrapEl.hidden = !thread;
    this.#composerEl.hidden = !thread;
    if (this.#searchEl !== null) this.#searchEl.hidden = !thread;
    if (this.#activity !== null) this.#activity.el.hidden = tab !== "activity";
    if (this.#approvals !== null) this.#approvals.el.hidden = tab !== "approvals";
    if (thread && leaving !== "thread") {
      // A hidden element can lose its scroll position.
      if (this.#pinned) this.#scrollToBottom();
      else this.#threadEl.scrollTop = this.#threadScroll;
    }
    if (tab === "activity" && leaving !== "activity" && this.#ready) void this.#activity?.refreshRoutines();
    this.#renderComposer();
  }

  #renderBadge(): void {
    const badge = this.#badgeEl;
    if (badge === null) return;
    const n = this.#approvals?.count ?? 0;
    badge.textContent = n === 0 ? "" : String(n);
    badge.hidden = n === 0;
    this.#tabEls.get("approvals")?.setAttribute("aria-label", n === 0 ? "Approvals" : `Approvals, ${n} waiting`);
  }

  // ---- search (the full app) ----

  #buildHeader(wrap: HTMLElement): HTMLElement {
    const header = el("header", "bar");
    const search = el("input", "search");
    search.type = "search";
    search.placeholder = "Search the thread";
    search.setAttribute("aria-label", "Search the thread");
    search.setAttribute("enterkeyhint", "search");
    search.autocomplete = "off";
    const results = el("div", "results");
    results.setAttribute("aria-label", "Search results");
    results.hidden = true;
    search.addEventListener("input", () => {
      clearTimeout(this.#searchTimer);
      this.#searchTimer = setTimeout(() => void this.#search(search.value), 250);
    });
    search.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        clearTimeout(this.#searchTimer);
        void this.#search(search.value);
      } else if (e.key === "Escape") {
        search.value = "";
        void this.#search("");
      }
    });
    this.#barStatusEl = el("div", "bar-status");
    this.#barStatusEl.setAttribute("role", "status");
    this.#barStatusEl.hidden = true;
    header.append(this.#buildTabs(), search, this.#barStatusEl);
    wrap.append(results);
    this.#resultsEl = results;
    this.#searchEl = search;
    return header;
  }

  async #search(query: string): Promise<void> {
    const results = this.#resultsEl;
    if (results === null) return;
    const seq = ++this.#searchSeq;
    const q = query.trim();
    if (q === "") {
      results.hidden = true;
      results.replaceChildren();
      return;
    }
    try {
      const found = await this.#api.search({ query: q, limit: 20 });
      if (seq !== this.#searchSeq) return;
      this.#showResults(found.turns, q);
    } catch (err) {
      if (seq !== this.#searchSeq) return;
      results.replaceChildren(el("div", "results-empty", `Search failed: ${message(err)}`));
      results.hidden = false;
    }
  }

  #showResults(turns: readonly Turn[], query: string): void {
    const results = this.#resultsEl as HTMLElement;
    results.replaceChildren();
    if (turns.length === 0) results.append(el("div", "results-empty", "No matches."));
    for (const t of turns) {
      const b = el("button", "result");
      b.type = "button";
      const d = localDate(t.at);
      const when = d === null ? "" : ` · ${d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })}`;
      b.append(el("span", "result-meta", `${ROLE_LABEL[t.role] ?? t.role}${when}`), el("span", "result-text", snippet(t.role === "owner" ? t.text : markdownText(t.text), query)));
      b.addEventListener("click", () => {
        results.hidden = true;
        void this.reveal(t.id);
      });
      results.append(b);
    }
    results.hidden = false;
  }

  // ---- rendering ----

  #render(): void {
    const s = this.#state;
    const isBusy = busy(s);
    const nodes: Node[] = [];
    const seen = new Set<number>();
    const days = new Set<string>();
    let day = "";
    for (const t of s.turns) {
      const key = dayKey(t.at);
      if (key !== day && key !== "") {
        nodes.push(this.#divider(key, t.at));
        days.add(key);
        day = key;
      }
      let v = this.#views.get(t.id);
      if (v === undefined) {
        v = new TurnView(t, this.#env, isBusy);
        this.#views.set(t.id, v);
      } else {
        v.update(t, isBusy);
      }
      seen.add(t.id);
      nodes.push(v.el);
    }
    for (const id of [...this.#views.keys()]) if (!seen.has(id)) this.#views.delete(id);
    for (const key of [...this.#dividers.keys()]) if (!days.has(key)) this.#dividers.delete(key);
    const replying = s.turns.some((t) => t.role === "porter" && !t.done && (s.awaiting === null || t.id === s.awaiting));
    if (isBusy && !replying) nodes.push(this.#placeholder);
    reconcile(this.#listEl, nodes);
    this.#emptyEl.hidden = !(s.loaded && s.turns.length === 0 && !isBusy);
    this.#renderTop();
    this.#renderComposer();
    this.#renderChip();
    if (this.#pinned) this.#scrollToBottom();
    this.#jumpEl.hidden = this.#pinned;
  }

  #divider(key: string, at: string): HTMLElement {
    let d = this.#dividers.get(key);
    if (d === undefined) {
      d = el("div", "day");
      d.setAttribute("role", "separator");
      this.#dividers.set(key, d);
    }
    const label = dayLabel(at);
    if (d.textContent !== label) d.textContent = label;
    return d;
  }

  #renderTop(): void {
    const s = this.#state;
    this.#topEl.replaceChildren();
    if (!s.more) return;
    if (this.#loadingOlder !== null) {
      this.#topEl.append(el("span", "muted", "Loading earlier messages…"));
      return;
    }
    const more = el("button", "link", "Load earlier messages");
    more.type = "button";
    more.addEventListener("click", () => void this.#loadOlder());
    this.#topEl.append(more);
  }

  #renderComposer(): void {
    const isBusy = busy(this.#state);
    this.#sendEl.disabled = !this.#ready || isBusy || this.#inputEl.value.trim() === "";
    this.#inputEl.placeholder = !this.#ready ? "Connecting…" : isBusy ? "Porter is replying…" : "Message Porter";
    if (!isBusy && this.#note?.kind === "busy") this.#setNote(null);
    const status = this.#ready
      ? ""
      : (this.#failure ?? (this.#everReady ? "Reconnecting to Porter…" : "Connecting to Porter…"));
    this.#statusEl.textContent = status;
    this.#statusEl.hidden = status === "";
    // Off the thread, the input (and its status line) is hidden: say it in the bar.
    if (this.#barStatusEl !== null) {
      const bar = this.#tab === "thread" ? "" : status;
      this.#barStatusEl.textContent = bar;
      this.#barStatusEl.hidden = bar === "";
    }
    if (this.#note === null) this.#noteEl.hidden = true;
  }

  #renderChip(): void {
    const c = this.#context;
    const chip = this.#chipEl;
    if (c === null || this.#dismissed) {
      chip.hidden = true;
      chip.replaceChildren();
      return;
    }
    const label = contextLabel(c);
    if (!chip.hidden && chip.dataset.key === `${c.app}\n${label}`) return;
    chip.dataset.key = `${c.app}\n${label}`;
    const close = el("button", "chip-close");
    close.type = "button";
    close.setAttribute("aria-label", `Don't send "${c.title || c.kind}" with your message`);
    close.title = "Don't send this with your message";
    close.append(icon(["M6 6l12 12", "M18 6L6 18"]));
    close.addEventListener("click", () => {
      this.#dismissed = true;
      this.#renderChip();
      this.#inputEl.focus({ preventScroll: true });
    });
    chip.replaceChildren(el("span", "ctx-app", c.app), el("span", "ctx-title", label), close);
    chip.title = `${c.app}: ${c.title || c.ref}`;
    chip.hidden = false;
  }
}

function message(err: unknown): string {
  return err instanceof Error && err.message ? err.message : String(err);
}

/** snippet is a short line of a turn's text, around the first query term it finds. */
export function snippet(text: string, query: string, max = 160): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= max) return flat;
  const lower = flat.toLowerCase();
  let at = -1;
  for (const term of query.toLowerCase().split(/\s+/)) {
    if (term === "") continue;
    const i = lower.indexOf(term);
    if (i >= 0 && (at < 0 || i < at)) at = i;
  }
  const start = Math.max(0, Math.min(at < 0 ? 0 : at - 40, flat.length - max));
  return `${start > 0 ? "…" : ""}${flat.slice(start, start + max)}${start + max < flat.length ? "…" : ""}`;
}
