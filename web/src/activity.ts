// The full app's Activity: the routines porterd knows, then what Porter
// did or noticed on its own, newest first (docs/design/overview.md, The
// UI). Entries arrive in pages and live as activity events. Their cards are
// read-only here, and follow turn events for the turn that reported them.

import { type ActivityAPI, Code, type Entry, type EntryList, errorCode, type Routine, type Turn } from "./api";
import { type CardHost, lodgeLink, renderCard, sameCard } from "./cards";
import { el, reconcile } from "./dom";
import { timeEl } from "./time";

export interface ActivityState {
  /** The loaded entries, newest first. */
  readonly entries: readonly Entry[];
  /** Whether older entries exist after the last. */
  readonly more: boolean;
  /** Whether the first page has arrived. */
  readonly loaded: boolean;
}

export const emptyActivity: ActivityState = { entries: [], more: false, loaded: false };

/** merge adds entries, replacing those with the same id, newest first. */
function merge(entries: readonly Entry[], incoming: readonly Entry[]): Entry[] {
  const byID = new Map<number, Entry>();
  for (const e of entries) byID.set(e.id, e);
  for (const e of incoming) byID.set(e.id, e);
  return [...byID.values()].sort((a, b) => b.id - a.id);
}

/**
 * newestEntries takes the newest page, on every (re)connect. When the page
 * does not reach back to the entries already shown and older ones exist,
 * it replaces them rather than leave a gap.
 */
export function newestEntries(s: ActivityState, page: EntryList): ActivityState {
  const oldest = page.entries.reduce((m, e) => Math.min(m, e.id), Number.POSITIVE_INFINITY);
  const newest = s.entries[0];
  if (newest === undefined || (page.more && oldest > newest.id)) {
    return { entries: merge([], page.entries), more: page.more, loaded: true };
  }
  const last = s.entries.at(-1) as Entry;
  return { entries: merge(s.entries, page.entries), more: last.id < oldest ? s.more : page.more, loaded: true };
}

/** olderEntries adds a page from before the last entry. */
export function olderEntries(s: ActivityState, page: EntryList): ActivityState {
  return { ...s, entries: merge(s.entries, page.entries), more: page.more };
}

/** addEntry adds an activity event's entry. */
export function addEntry(s: ActivityState, e: Entry): ActivityState {
  return { ...s, entries: merge(s.entries, [e]) };
}

/** entryTurn brings the cards of entries a turn reported up to date with it. */
export function entryTurn(s: ActivityState, t: Turn): ActivityState {
  let changed = false;
  const entries = s.entries.map((e) => {
    if (e.turn !== t.id) return e;
    let cardsChanged = false;
    const cards = e.cards.map((c) => {
      const next = t.cards.find((x) => x.id === c.id);
      if (next === undefined || sameCard(next, c)) return c;
      cardsChanged = true;
      return next;
    });
    if (!cardsChanged) return e;
    changed = true;
    return { ...e, cards };
  });
  return changed ? { ...s, entries } : s;
}

/** The page size of Activity. */
const PAGE = 50;

const KIND_LABEL: Record<string, string> = { skipped: "Skipped", missed: "Missed", invalid: "Invalid" };

function kindLabel(e: Entry): string {
  if (e.kind === "run") return e.ok ? "Ran" : "Failed";
  return KIND_LABEL[e.kind] ?? e.kind;
}

/** setNote shows a view's one-line note, or hides it when text is empty. */
function setNote(note: HTMLElement, text: string, error: boolean): void {
  note.textContent = text;
  note.className = error ? "view-note error" : "view-note muted";
  note.hidden = text === "";
}

function message(err: unknown): string {
  return err instanceof Error && err.message ? err.message : String(err);
}

export class ActivityView {
  readonly el: HTMLElement;
  #api: ActivityAPI;
  #cards: CardHost;
  #state: ActivityState = emptyActivity;
  #routines: readonly Routine[] | null = null;
  #routinesSeq = 0;
  #routinesError: string | null = null;
  #entriesError: string | null = null;
  #loadingMore = false;
  #entryEls = new Map<number, { entry: Entry; el: HTMLElement }>();

  #routinesEl: HTMLElement;
  #routinesNote: HTMLElement;
  #entriesEl: HTMLElement;
  #entriesNote: HTMLElement;
  #moreEl: HTMLElement;

  constructor(api: ActivityAPI, cards: CardHost) {
    this.#api = api;
    this.#cards = cards;
    this.el = el("section", "view activity");
    const inner = el("div", "view-inner");
    this.#routinesNote = el("div", "view-note muted", "Loading routines…");
    this.#routinesEl = el("div", "routines");
    this.#entriesNote = el("div", "view-note muted", "Loading activity…");
    this.#entriesEl = el("div", "entries");
    this.#entriesEl.setAttribute("aria-label", "Activity, newest first");
    this.#moreEl = el("div", "view-more");
    inner.append(
      el("h2", "view-title", "Routines"),
      this.#routinesNote,
      this.#routinesEl,
      el("h2", "view-title", "Activity"),
      this.#entriesNote,
      this.#entriesEl,
      this.#moreEl,
    );
    this.el.append(inner);
  }

  /** load reads the routines and the newest entries, on every (re)connect. */
  async load(): Promise<void> {
    await Promise.all([this.refreshRoutines(), this.#loadNewest()]);
  }

  /** refreshRoutines reads the routines again, when an entry arrives or the view opens. */
  async refreshRoutines(): Promise<void> {
    const seq = ++this.#routinesSeq;
    try {
      const routines = await this.#api.routines();
      if (seq !== this.#routinesSeq) return;
      this.#routines = routines;
      this.#routinesError = null;
    } catch (err) {
      if (seq !== this.#routinesSeq || errorCode(err) === Code.disconnected) return;
      this.#routinesError = `Couldn't load the routines: ${message(err)}`;
    }
    this.#renderRoutines();
  }

  /** entry takes an activity event's entry. */
  entry(e: Entry): void {
    this.#state = addEntry(this.#state, e);
    this.#renderEntries();
  }

  /** turn takes a turn event, for the entries it reported. */
  turn(t: Turn): void {
    const next = entryTurn(this.#state, t);
    if (next === this.#state) return;
    this.#state = next;
    this.#renderEntries();
  }

  async #loadNewest(): Promise<void> {
    try {
      this.#state = newestEntries(this.#state, await this.#api.list({ limit: PAGE }));
      this.#entriesError = null;
    } catch (err) {
      if (errorCode(err) === Code.disconnected) return;
      this.#entriesError = `Couldn't load the activity: ${message(err)}`;
    }
    this.#renderEntries();
  }

  async #loadMore(): Promise<void> {
    const last = this.#state.entries.at(-1);
    if (this.#loadingMore || !this.#state.more || last === undefined) return;
    this.#loadingMore = true;
    this.#renderEntries();
    try {
      this.#state = olderEntries(this.#state, await this.#api.list({ before: last.id, limit: PAGE }));
      this.#entriesError = null;
    } catch (err) {
      this.#entriesError = `Couldn't load older activity: ${message(err)}`;
    } finally {
      this.#loadingMore = false;
    }
    this.#renderEntries();
  }

  // ---- rendering ----

  #renderRoutines(): void {
    const routines = this.#routines ?? [];
    let note = "";
    if (this.#routinesError !== null) note = this.#routinesError;
    else if (this.#routines === null) note = "Loading routines…";
    else if (routines.length === 0) note = "No routines yet. Each note in Porter/Routines/ is one.";
    setNote(this.#routinesNote, note, this.#routinesError !== null);
    this.#routinesEl.replaceChildren(...routines.map((r) => this.#routine(r)));
  }

  #routine(r: Routine): HTMLElement {
    const root = el("div", `routine${r.valid ? "" : " invalid"}${r.enabled ? "" : " off"}`);
    root.dataset.name = r.name;
    const head = el("div", "routine-head");
    const open = (url: string) => this.#cards.open(url);
    head.append((r.url ? lodgeLink(r.url, open, "routine-name", r.name) : null) ?? el("span", "routine-name", r.name));
    if (!r.valid) head.append(el("span", "badge danger", "Invalid"));
    else head.append(el("span", r.enabled ? "badge ok" : "badge", r.enabled ? "On" : "Off"));
    root.append(head);
    if (!r.valid) root.append(el("div", "routine-error", r.error || "This routine is invalid, so it does not run."));

    const meta = el("div", "routine-meta");
    if (r.schedule) meta.append(el("code", "routine-schedule", r.schedule));
    if (r.timezone) meta.append(el("span", "routine-zone", r.timezone));
    if (meta.childElementCount > 0) root.append(meta);

    const times = el("div", "routine-times");
    if (r.next) {
      const next = el("span", "routine-next");
      next.append(el("span", "label", "Next "), timeEl(r.next));
      times.append(next);
    }
    if (r.last) {
      const last = el("span", "routine-last");
      last.append(el("span", "label", "Last "), timeEl(r.last));
      if (r.lastOk !== undefined) {
        last.append(" ", el("span", r.lastOk ? "run-ok" : "run-failed", r.lastOk ? "ok" : "failed"));
      }
      times.append(last);
    }
    if (times.childElementCount > 0) root.append(times);
    return root;
  }

  #renderEntries(): void {
    const s = this.#state;
    const next = new Map<number, { entry: Entry; el: HTMLElement }>();
    const nodes: HTMLElement[] = [];
    for (const e of s.entries) {
      const prev = this.#entryEls.get(e.id);
      const node = prev !== undefined && prev.entry === e ? prev.el : this.#entry(e);
      next.set(e.id, { entry: e, el: node });
      nodes.push(node);
    }
    this.#entryEls = next;
    reconcile(this.#entriesEl, nodes);

    let note = "";
    if (!s.loaded && this.#entriesError === null) note = "Loading activity…";
    else if (s.loaded && s.entries.length === 0) note = "Nothing yet. Routine runs appear here.";
    setNote(this.#entriesNote, note, false);

    this.#moreEl.replaceChildren();
    if (this.#entriesError !== null) this.#moreEl.append(el("div", "view-note error", this.#entriesError));
    if (s.more) {
      const more = el("button", "link", this.#loadingMore ? "Loading…" : "Load older activity");
      more.type = "button";
      more.disabled = this.#loadingMore;
      more.addEventListener("click", () => void this.#loadMore());
      this.#moreEl.append(more);
    }
  }

  #entry(e: Entry): HTMLElement {
    const failed = (e.kind === "run" && !e.ok) || e.kind === "invalid";
    const root = el("article", `entry ${e.kind}${failed ? " failed" : ""}`);
    root.dataset.id = String(e.id);
    const head = el("div", "entry-head");
    head.append(
      el("span", "entry-kind", kindLabel(e)),
      el("span", "entry-routine", e.routine),
      timeEl(e.at, "entry-time"),
    );
    root.append(head);
    if (e.text) root.append(el("div", "entry-text", e.text));
    const link = e.url ? lodgeLink(e.url, (url) => this.#cards.open(url), "entry-link", "Open the note") : null;
    if (link !== null) root.append(link);
    if (e.cards.length > 0) {
      const cards = el("div", "cards");
      for (const c of e.cards) cards.append(renderCard(c, this.#cards, undefined, true));
      root.append(cards);
    }
    return root;
  }
}
