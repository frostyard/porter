import type { Role } from "@frostyard/lodge-client";
import { describe, expect, it, vi } from "vitest";
import { Code, RpcError } from "../src/api";
import { App, snippet } from "../src/app";
import { FakeTransport, flush, turn } from "./fake";

function make(role: Role = "companion", transport = new FakeTransport()) {
  let next = 100;
  transport
    .on("thread.subscribe", () => false)
    .on("thread.list", () => ({ turns: [turn(1, { role: "notice", text: "Your brief for Monday is ready." })], more: false }))
    .on("thread.send", (p) => {
      next += 2;
      return { turn: turn(next - 1, { role: "owner", text: String(p.text), context: p.context as never }), reply: next };
    });
  const root = document.createElement("div");
  document.body.replaceChildren(root);
  const open = vi.fn<(url: string) => void>();
  const app = new App({ root, transport, shell: { role, open } });
  const input = root.querySelector("textarea.input") as HTMLTextAreaElement;
  const type = (text: string) => {
    input.value = text;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  };
  const enter = (shiftKey = false) =>
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", shiftKey, bubbles: true, cancelable: true }));
  return { app, root, transport, input, type, enter, open };
}

const plan = { app: "notes", kind: "note", ref: "notes/1/Projects/Plan.md", title: "Plan" };

describe("the input", () => {
  it("sends on Enter, and not on Shift+Enter", async () => {
    const { app, transport, type, enter, input } = make();
    await app.connected();
    type("first line");
    enter(true);
    expect(transport.paramsOf("thread.send")).toEqual([]);
    enter();
    await flush();
    expect(transport.paramsOf("thread.send")).toStrictEqual([{ text: "first line" }]);
    expect(input.value).toBe("");
  });

  it("shows an owner's turn right-aligned with its context above it", async () => {
    const { app, root, type, enter } = make();
    await app.connected();
    app.context(plan);
    type("What's left here?");
    enter();
    await flush();
    const owner = root.querySelector(".turn.owner") as HTMLElement;
    expect(owner.querySelector(".bubble")?.textContent).toBe("What's left here?");
    expect(owner.querySelector(".turn-context")?.textContent).toBe("notesAbout: Plan");
  });

  it("sends the chip's context, not once dismissed, and again after a new context", async () => {
    const { app, root, transport, type, enter } = make();
    await app.connected();
    app.context(plan);
    const chip = root.querySelector(".chip") as HTMLElement;
    expect(chip.hidden).toBe(false);
    expect(chip.textContent).toContain("About: Plan");
    expect(chip.textContent).toContain("notes");

    type("one");
    enter();
    await flush();
    app.event({ event: "working", data: { working: false } });
    expect(transport.paramsOf("thread.send")[0]).toStrictEqual({ text: "one", context: plan });

    (chip.querySelector("button") as HTMLButtonElement).click();
    expect(chip.hidden).toBe(true);
    type("two");
    enter();
    await flush();
    app.event({ event: "working", data: { working: false } });
    expect(transport.paramsOf("thread.send")[1]).toStrictEqual({ text: "two" });

    // The same context again keeps it dismissed; a new one brings it back.
    app.context(plan);
    expect(chip.hidden).toBe(true);
    const mail = { app: "mail", kind: "message", ref: "<m1@example.com>", title: "Trip" };
    app.context(mail);
    expect(chip.hidden).toBe(false);
    type("three");
    enter();
    await flush();
    expect(transport.paramsOf("thread.send")[2]).toStrictEqual({ text: "three", context: mail });
  });

  it("shows no chip when nothing is open", async () => {
    const { app, root } = make();
    app.context({ app: "mail", kind: null, ref: "", title: "" });
    expect((root.querySelector(".chip") as HTMLElement).hidden).toBe(true);
  });

  it("does not send while a reply is in progress, and says so gently on a conflict", async () => {
    const { app, root, transport, type, enter, input } = make();
    await app.connected();
    app.event({ event: "working", data: { working: true } });
    type("wait");
    expect((root.querySelector("button.send") as HTMLButtonElement).disabled).toBe(true);
    enter();
    await flush();
    expect(transport.paramsOf("thread.send")).toEqual([]);
    expect(root.querySelector(".note")?.textContent).toMatch(/still replying/);

    app.event({ event: "working", data: { working: false } });
    expect((root.querySelector(".note") as HTMLElement).hidden).toBe(true);
    transport.on("thread.send", () => {
      throw new RpcError(Code.conflict, "a reply is in progress");
    });
    enter();
    await flush();
    expect(root.querySelector(".note")?.textContent).toMatch(/still replying/);
    expect(input.value).toBe("wait");
    expect(app.state.working).toBe(true);
  });

  it("shows working dots while Porter's reply is not done", async () => {
    const { app, root, type, enter } = make();
    await app.connected();
    type("hi");
    enter();
    await flush();
    expect(root.querySelector(".turn.pending .working")).not.toBeNull();
    app.event({ event: "turn", data: turn(102, { text: "Hel", done: false }) });
    expect(root.querySelector(".turn.pending")).toBeNull();
    expect(root.querySelector('.turn.porter[data-id="102"] .working')).not.toBeNull();
    app.event({ event: "turn", data: turn(102, { text: "Hello.", done: true }) });
    app.event({ event: "working", data: { working: false } });
    expect(root.querySelector(".working")).toBeNull();
    expect(root.querySelector('.turn.porter[data-id="102"]')?.textContent).toBe("Hello.");
  });

  it("ignores events it does not know", async () => {
    const { app } = make();
    await app.connected();
    const before = app.state;
    app.event({ event: "approval", data: { id: 1 } });
    app.event({ type: "turn", turn: turn(9) });
    expect(app.state).toBe(before);
  });
});

describe("ask", () => {
  it("fills the input and sends at once, with that context", async () => {
    const { app, transport, input } = make();
    await app.connected();
    app.context(plan);
    app.ask("notes", "What is due here?");
    await flush();
    expect(transport.paramsOf("thread.send")).toStrictEqual([{ text: "What is due here?", context: plan }]);
    expect(input.value).toBe("");
  });

  it("waits for the connection when it comes first", async () => {
    const { app, transport, input } = make();
    app.context(plan);
    app.ask("notes", "Summarize this");
    expect(input.value).toBe("Summarize this");
    expect(transport.paramsOf("thread.send")).toEqual([]);
    await app.connected();
    await flush();
    expect(transport.paramsOf("thread.send")).toStrictEqual([{ text: "Summarize this", context: plan }]);
  });
});

describe("the layout by role", () => {
  it("has no header in the companion", () => {
    const { root } = make("companion");
    expect(root.querySelector("header")).toBeNull();
    expect(root.querySelector("input[type=search]")).toBeNull();
    expect(root.querySelector(".tabs")).toBeNull();
  });

  it("searches in the full app, and scrolls to a result, loading older turns", async () => {
    const transport = new FakeTransport();
    const { app, root } = make("app", transport);
    transport.on("thread.list", (p) => {
      if (p.before === undefined) return { turns: [turn(300), turn(301)], more: true };
      return { turns: [turn(42, { text: "The trip to Lisbon" }), turn(43)], more: false };
    });
    transport.on("thread.search", () => ({ turns: [turn(42, { text: "The trip to Lisbon" })], more: false }));
    await app.connected();
    const search = root.querySelector("header input[type=search]") as HTMLInputElement;
    search.value = "Lisbon";
    search.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flush();
    expect(transport.paramsOf("thread.search")).toStrictEqual([{ query: "Lisbon", limit: 20 }]);
    const result = root.querySelector(".results .result") as HTMLButtonElement;
    expect(result.textContent).toContain("The trip to Lisbon");
    result.click();
    await vi.waitFor(() => expect(root.querySelector('[data-id="42"]')?.classList.contains("flash")).toBe(true));
    expect(transport.paramsOf("thread.list").at(-1)).toStrictEqual({ before: 300, limit: 200 });
    expect(app.state.turns.map((t) => t.id)).toEqual([42, 43, 300, 301]);
  });
});

describe("snippet", () => {
  it("cuts long text around the first term", () => {
    const text = `${"a ".repeat(100)}needle ${"b ".repeat(100)}`;
    const s = snippet(text, "needle", 60);
    expect(s).toContain("needle");
    expect(s.startsWith("…")).toBe(true);
    expect(snippet("short\n text", "x")).toBe("short text");
  });
});
