// Porter's web bundle, in a frame of lodge's shell: the companion beside
// the current app, or Porter's full app. It connects to the shell with
// lodge-client, then to porterd's API at /app/porter/api.
import { connectLodge } from "@frostyard/lodge-client";
import { App } from "./app";
import { Connection } from "./connection";

const lodge = await connectLodge({ app: "porter" });

// Register for the shell's messages before anything else runs: lodge-client
// replays nothing, and the companion's first context and ask follow init.
const root = document.getElementById("app") as HTMLElement;
const visible = () => document.visibilityState === "visible";
let app: App | null = null;
const conn = new Connection({
  url: () => lodge.apiURL(),
  client: "porter-web",
  visible,
  onReady: () => void app?.connected(),
  onClose: () => app?.disconnected(),
  onFailure: (message) => app?.failed(`Porter can't connect: ${message}`),
  onEvent: (params) => app?.event(params),
});
app = new App({ root, transport: conn, shell: { role: lodge.role, open: (url) => lodge.open(url) } });

lodge.on("context", (c) => app?.context(c));
lodge.on("ask", ({ app: id, prompt }) => app?.ask(id, prompt));
lodge.on("visible", ({ visible: shown }) => {
  if (!shown) return;
  if (visible()) conn.connect();
  if (lodge.role === "companion") app?.shown();
});

// Follow the shell's theme rather than prefers-color-scheme.
document.documentElement.dataset.theme = lodge.theme;
lodge.on("theme", ({ scheme }) => {
  document.documentElement.dataset.theme = scheme;
});

// Report the route the frame opened at, and follow the shell's navigation.
// In the companion's frame, lodge-client keeps routes in memory.
lodge.route(lodge.history.location.pathname, "", { replace: true });
lodge.on("navigate", ({ route }) => lodge.route(route, "", { replace: true }));

conn.connect();

// lodge-client pings every 30 s while the page is visible, so lodge keeps the
// session (it closes one after 90 s without a frame). Connect when the page is
// visible again.
document.addEventListener("visibilitychange", () => {
  if (visible()) conn.connect();
});
