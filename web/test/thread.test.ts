import { describe, expect, it } from "vitest";
import {
  applyEvent,
  busy,
  emptyThread,
  findCard,
  newestPage,
  olderPage,
  patchCard,
  sendFailed,
  sendStarted,
  sendSucceeded,
  subscribed,
} from "../src/thread";
import { card, entry, turn } from "./fake";

const ids = (s: { turns: readonly { id: number }[] }) => s.turns.map((t) => t.id);

describe("the turn reducer", () => {
  it("replaces a turn with the same id", () => {
    let s = newestPage(emptyThread, { turns: [turn(1, { role: "owner", text: "hi" }), turn(2, { done: false })], more: false });
    const growing = turn(2, { done: false, cards: [card("c1")] });
    s = applyEvent(s, { type: "turn", turn: growing });
    expect(ids(s)).toEqual([1, 2]);
    expect(s.turns[1]).toBe(growing);
    s = applyEvent(s, { type: "turn", turn: turn(2, { text: "Done.", cards: [card("c1")], done: true }) });
    expect(ids(s)).toEqual([1, 2]);
    expect(s.turns[1]?.text).toBe("Done.");
    expect(s.turns[1]?.done).toBe(true);
  });

  it("keeps turns in id order, whatever order they arrive in", () => {
    let s = newestPage(emptyThread, { turns: [turn(10), turn(12)], more: true });
    s = applyEvent(s, { type: "turn", turn: turn(15) });
    s = applyEvent(s, { type: "turn", turn: turn(11) });
    s = applyEvent(s, { type: "turn", turn: turn(13) });
    expect(ids(s)).toEqual([10, 11, 12, 13, 15]);
    s = olderPage(s, { turns: [turn(4), turn(7)], more: false });
    expect(ids(s)).toEqual([4, 7, 10, 11, 12, 13, 15]);
    expect(s.more).toBe(false);
  });

  it("leaves unchanged turns as the same objects", () => {
    const first = turn(1);
    let s = newestPage(emptyThread, { turns: [first, turn(2)], more: false });
    s = applyEvent(s, { type: "turn", turn: turn(2, { text: "new" }) });
    expect(s.turns[0]).toBe(first);
  });

  it("follows the working flag", () => {
    let s = subscribed(emptyThread, false);
    expect(busy(s)).toBe(false);
    s = applyEvent(s, { type: "working", working: true });
    expect(s.working).toBe(true);
    expect(busy(s)).toBe(true);
    s = applyEvent(s, { type: "working", working: false });
    expect(busy(s)).toBe(false);
    expect(busy(subscribed(emptyThread, true))).toBe(true);
  });

  it("stays busy from a send until its reply is done", () => {
    let s = newestPage(subscribed(emptyThread, false), { turns: [turn(1)], more: false });
    s = sendStarted(s);
    expect(busy(s)).toBe(true);
    s = sendSucceeded(s, { turn: turn(2, { role: "owner", text: "hi" }), reply: 3 });
    expect(s.sending).toBe(false);
    expect(s.awaiting).toBe(3);
    expect(busy(s)).toBe(true);
    s = applyEvent(s, { type: "turn", turn: turn(3, { done: false }) });
    expect(busy(s)).toBe(true);
    s = applyEvent(s, { type: "turn", turn: turn(3, { text: "Hello.", done: true }) });
    expect(busy(s)).toBe(false);
    expect(ids(s)).toEqual([1, 2, 3]);
  });

  it("is not left busy when the reply ended before the send's answer", () => {
    let s = sendStarted(subscribed(emptyThread, false));
    s = applyEvent(s, { type: "working", working: true });
    s = applyEvent(s, { type: "working", working: false });
    s = sendSucceeded(s, { turn: turn(2, { role: "owner" }), reply: 3 });
    expect(busy(s)).toBe(false);
  });

  it("is busy after a conflict until porterd says the reply ended", () => {
    let s = sendFailed(sendStarted(subscribed(emptyThread, false)), true);
    expect(busy(s)).toBe(true);
    s = applyEvent(s, { type: "working", working: false });
    expect(busy(s)).toBe(false);
    expect(busy(sendFailed(sendStarted(emptyThread), false))).toBe(false);
  });

  it("ignores an event for a turn before the loaded window while older turns are unloaded", () => {
    let s = newestPage(emptyThread, { turns: [turn(50), turn(51)], more: true });
    s = applyEvent(s, { type: "turn", turn: turn(3) });
    expect(ids(s)).toEqual([50, 51]);
  });

  it("replaces the shown turns when a reconnect's newest page leaves a gap", () => {
    let s = newestPage(emptyThread, { turns: [turn(1), turn(2)], more: false });
    s = newestPage(s, { turns: [turn(80), turn(81)], more: true });
    expect(ids(s)).toEqual([80, 81]);
    expect(s.more).toBe(true);
  });

  it("merges a reconnect's newest page that reaches the shown turns", () => {
    let s = newestPage(emptyThread, { turns: [turn(5), turn(6)], more: true });
    s = olderPage(s, { turns: [turn(3), turn(4)], more: true });
    s = newestPage(s, { turns: [turn(5), turn(6), turn(7)], more: true });
    expect(ids(s)).toEqual([3, 4, 5, 6, 7]);
    expect(s.more).toBe(true);
  });

  it("keeps a shown turn's completion over a page read before it", () => {
    let s = newestPage(emptyThread, { turns: [turn(1, { done: false })], more: false });
    s = applyEvent(s, { type: "turn", turn: turn(1, { text: "Done.", done: true }) });
    s = newestPage(s, { turns: [turn(1, { done: false })], more: false });
    expect(s.turns[0]?.done).toBe(true);
  });

  it("patches a card in whichever turn holds it", () => {
    let s = newestPage(emptyThread, {
      turns: [turn(1, { cards: [card("t1"), card("c1", { kind: "capture", status: "pending", text: "x" })] })],
      more: false,
    });
    const before = s.turns[0];
    s = patchCard(s, "c1", { status: "done", text: "y" });
    expect(s.turns[0]).not.toBe(before);
    expect(s.turns[0]?.cards[1]).toMatchObject({ id: "c1", status: "done", text: "y" });
    expect(s.turns[0]?.cards[0]).toBe(before?.cards[0]);
    expect(patchCard(s, "nope", { status: "done" })).toBe(s);
  });

  it("leaves the thread as it is for an activity entry", () => {
    const s = newestPage(emptyThread, { turns: [turn(1)], more: false });
    expect(applyEvent(s, { type: "activity", entry: entry(1, { turn: 1 }) })).toBe(s);
  });

  it("finds a card in whichever turn holds it", () => {
    const turns = [turn(1, { cards: [card("t1")] }), turn(2, { cards: [card("a1")] })];
    const s = newestPage(emptyThread, { turns, more: false });
    expect(findCard(s, "a1")).toBe(s.turns[1]?.cards[0]);
    expect(findCard(s, "nope")).toBeUndefined();
  });
});
