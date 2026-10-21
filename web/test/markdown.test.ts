import { describe, expect, it, vi } from "vitest";
import { classifyHref, decodeEntities, markdownText, renderMarkdown } from "../src/markdown";

function render(md: string) {
  const open = vi.fn<(path: string) => void>();
  const div = document.createElement("div");
  div.append(renderMarkdown(md, { open }));
  return { div, open };
}

describe("renderMarkdown", () => {
  it("renders an image as a link and never as an img", () => {
    const { div } = render("Look: ![a *red* cat](https://example.com/cat.png)");
    expect(div.querySelector("img")).toBeNull();
    const a = div.querySelector("a");
    expect(a).not.toBeNull();
    expect(a?.getAttribute("href")).toBe("https://example.com/cat.png");
    expect(a?.textContent).toBe("image: a red cat (example.com)");
  });

  it("renders an image without alt, or one inside a link, without loading it", () => {
    const { div } = render("![](https://example.com/x.png) and [![logo](https://example.com/l.png)](https://example.org/)");
    expect(div.querySelector("img")).toBeNull();
    const links = [...div.querySelectorAll("a")];
    expect(links.map((a) => a.textContent)).toEqual(["image (example.com)", "image: logo (example.org)"]);
    expect(div.querySelector("a a")).toBeNull();
  });

  it("shows raw HTML, a script among it, as text", () => {
    const { div } = render(
      '<script>alert(1)</script>\n\nhi <b>bold</b> <img src=x onerror="alert(1)"> <iframe src="https://example.com"></iframe>',
    );
    expect(div.querySelector("script")).toBeNull();
    expect(div.querySelector("b")).toBeNull();
    expect(div.querySelector("img")).toBeNull();
    expect(div.querySelector("iframe")).toBeNull();
    expect(div.textContent).toContain("<script>alert(1)</script>");
    expect(div.textContent).toContain("<b>bold</b>");
    expect(div.textContent).toContain('<img src=x onerror="alert(1)">');
  });

  it("opens a lodge address with lodge.open", () => {
    const { div, open } = render("See [Plan](/notes/1/Projects/Plan.md).");
    const a = div.querySelector("a");
    expect(a?.getAttribute("href")).toBe("/notes/1/Projects/Plan.md");
    expect(a?.getAttribute("target")).toBeNull();
    expect(a?.textContent).toBe("Plan");
    const click = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
    a?.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    expect(open).toHaveBeenCalledWith("/notes/1/Projects/Plan.md");
  });

  it("opens an https link in a new tab and shows its real host", () => {
    const { div, open } = render("[the docs](https://docs.example.com/guide?a=1&amp;b=2)");
    const a = div.querySelector("a");
    expect(a?.getAttribute("href")).toBe("https://docs.example.com/guide?a=1&b=2");
    expect(a?.getAttribute("target")).toBe("_blank");
    expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
    expect(a?.textContent).toBe("the docs (docs.example.com)");
    // The browser opens it; keep happy-dom from following it.
    div.addEventListener("click", (e) => e.preventDefault());
    a?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    expect(open).not.toHaveBeenCalled();
  });

  it("leaves the host off when the text already is it", () => {
    const { div } = render("[example.com](https://example.com/x) <https://example.com/a/b>");
    expect([...div.querySelectorAll("a")].map((a) => a.textContent)).toEqual(["example.com", "https://example.com/a/b"]);
  });

  it("shows the real host when the text names another", () => {
    const { div } = render("[https://bank.example](https://evil.example/login)");
    expect(div.querySelector("a")?.textContent).toBe("https://bank.example (evil.example)");
  });

  it("renders a javascript: link, and any other scheme, as text", () => {
    const { div } = render(
      [
        "[click](javascript:alert(1))",
        "[enc](javascript&#58;alert(1))",
        "[case](JaVaScRiPt:alert(1))",
        "[data](data:text/html,hi)",
        "[plain](http://example.com/)",
        "[mail](mailto:a@example.com)",
        "[proto](//evil.example/x)",
        "[back](/\\evil.example)",
        "[rel](notes/x.md)",
      ].join(" "),
    );
    expect(div.querySelector("a")).toBeNull();
    expect(div.textContent).toBe("click enc case data plain mail proto back rel");
  });

  it("builds lists, code, quotes and tables", () => {
    const { div } = render(
      "# Title\n\n- [x] done\n- [ ] todo **now**\n\n3. three\n4. four\n\n```\n<b>code</b>\n```\n\n> quoted\n\n| a | b |\n|:-|-:|\n| 1 &amp; | `2` |",
    );
    expect(div.querySelector("h1")?.textContent).toBe("Title");
    const boxes = [...div.querySelectorAll<HTMLInputElement>("li.task input")];
    expect(boxes.map((b) => [b.checked, b.disabled])).toEqual([
      [true, true],
      [false, true],
    ]);
    expect(div.querySelector("ol")?.getAttribute("start")).toBe("3");
    expect(div.querySelector("pre code")?.textContent).toBe("<b>code</b>");
    expect(div.querySelector("blockquote")?.textContent).toBe("quoted");
    expect([...div.querySelectorAll("td")].map((td) => td.textContent)).toEqual(["1 &", "2"]);
    expect(div.querySelector("th")?.style.textAlign).toBe("left");
    expect(div.querySelector("b")).toBeNull();
  });

  it("keeps line breaks within a paragraph", () => {
    const { div } = render("one\ntwo");
    expect(div.querySelectorAll("p br")).toHaveLength(1);
  });
});

describe("classifyHref", () => {
  it("says where each destination goes", () => {
    expect(classifyHref("/mail/message/abc")).toEqual({ kind: "lodge", path: "/mail/message/abc" });
    expect(classifyHref("https://example.com/").kind).toBe("https");
    expect(classifyHref("//example.com/").kind).toBe("text");
    expect(classifyHref("/\\example.com").kind).toBe("text");
    expect(classifyHref("/notes/a\u0000b").kind).toBe("text");
    expect(classifyHref(" javascript:alert(1)").kind).toBe("text");
    expect(classifyHref("http://example.com").kind).toBe("text");
  });
});

describe("markdownText", () => {
  it("is the text without markup, on one line", () => {
    expect(markdownText("Your brief is ready: [Brief](/notes/1/Brief.md)\n\n- **one**\n- `two` ![map](https://example.com/m.png)")).toBe(
      "Your brief is ready: Brief one two map",
    );
  });
});

describe("decodeEntities", () => {
  it("resolves numeric and common named references, and leaves the rest", () => {
    expect(decodeEntities("a &amp; b &#169; &#x2014; &nbsp;&unknown; &constructor;")).toBe("a & b © —  &unknown; &constructor;");
  });
});
