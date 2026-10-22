import { describe, expect, it } from "vitest";
import { parseThreadEvent } from "../src/api";
import { card, entry, turn } from "./fake";

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
