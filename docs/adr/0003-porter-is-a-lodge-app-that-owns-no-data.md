# 0003 — Porter is a lodge app that owns no data

- **Status:** Accepted (Brian, 2026-10-23)
- **Date:** 2026-10-19

## Context

The owner wants a personal assistant (not a coding agent) beside their mail,
calendar, tasks and notes. Those already run behind
[lodge](https://github.com/frostyard/lodge), frostyard's tailnet-only web
shell, as the apps `mail` (frostmail's maild) and `notes` (frostnotes'
notesd). Each serves agents an MCP endpoint, `/mcp/<id>`, whose token scope
the app enforces in its engine
([lodge ADR-0006](https://github.com/frostyard/lodge/blob/main/docs/adr/0006-agents-reach-each-app-through-its-own-mcp-endpoint.md)).
lodge also runs an inference layer that routes apps to models, the owner's
local model first
([lodge ADR-0011](https://github.com/frostyard/lodge/blob/main/docs/adr/0011-a-shared-inference-layer-routes-apps-to-models.md)).

On 2026-10-19 the local model ran a whole assistant task through lodge's
inference socket with OpenAI-style tools: search mail, read the thread,
draft a reply, then answer. It took four steps of about 2 s each. The owner
expects to talk to the assistant mostly in a chat panel beside the app they
are using. The next app after this one is a GitHub manager, and the
assistant will use it too.

## Decision

- **Porter is lodge's app `porter`.** Its daemon, `porterd`, runs as the
  system user `frostporter` on lodge's app socket, on lodge's kit, and its
  web bundle is served at `/app/porter/`. Its switcher key is `p`.
- **Its main surface is lodge's companion,** a chat panel beside the current
  app ([lodge ADR-0013](https://github.com/frostyard/lodge/blob/main/docs/adr/0013-the-companion-opens-beside-the-current-app.md)).
  Its manifest sets `companion: true`. The full app shows the same thread,
  with Activity and Approvals.
- **Porter owns none of the owner's data.** It reads and writes mail,
  calendar, tasks and notes only through each app's `/mcp/<id>`, with one
  token per app, issued by `lodgectl`, and scoped by the owner. It reaches
  them through lodge's local agent socket
  ([lodge ADR-0014](https://github.com/frostyard/lodge/blob/main/docs/adr/0014-agents-on-lodges-host-use-a-local-agent-socket.md)),
  so nothing leaves the host. Any later lodge app that serves MCP, the
  GitHub manager first, becomes one more token and one more tool source.
- **Porter thinks only through lodge's inference socket,** with lodge's
  aliases. Its chat is `interactive` and its routines are `background`, so
  lodge logs its use as app `porter` and holds every provider credential.
- **Porter never runs as the account that owns the data,** and it is not a
  coding agent. Agents that run commands stay outside lodge, with their own
  identities.

## Consequences

- Each app's engine enforces what Porter may reach, so a bug in Porter's
  prompt or tool code cannot widen its reach. Revoking a token cuts Porter
  off from that app at once.
- Porter depends on four other pieces of work:
  - lodge's companion and agent socket;
  - notes' folder scopes
    ([frostnotes ADR-0028](https://github.com/frostyard/frostnotes/blob/main/docs/adr/0028-an-agents-scope-can-limit-writes-to-folders.md));
  - mail's calendar and task tools
    ([frostmail plan 0012](https://github.com/frostyard/frostmail/blob/main/docs/plans/0012-calendar-tasks-and-contacts-for-agents.md));
  - for its GitHub tools, the GitHub manager.
- It has no store of mail or notes to keep in sync. Its own state is its
  conversation, the routine runs and the approvals.
- What it can do is bounded by the tools the apps offer. A capability
  missing from an app is that app's work, not Porter's.

## Alternatives considered

- **A chat panel inside each app:** that is many assistants with many
  contexts and duplicate code. One companion with each app's `context`
  gives "chat inside notes" without it.
- **Porter's own copies of mail and notes, or direct access to their
  databases:** that breaks "files are the truth" and the apps' scopes, and
  makes Porter a second engine to keep correct.
- **A tagged tailnet node of its own:** that is lodge ADR-0006's path for
  agents off the host. On lodge's host, it would put tsnet in Porter and
  send traffic out to the tailnet and back for nothing.

## References

- Shapes: [design/overview.md](../design/overview.md), [plans/0001-roadmap.md](../plans/0001-roadmap.md)
- Extended by: [ADR-0007](0007-porter-reads-frostread-read-only.md) (frostread, the third app, read-only)
- Builds on: frostyard/core [ADR-0057](https://github.com/frostyard/core/blob/main/docs/adr/0057-run-first-party-apps-behind-lodge.md), lodge [ADR-0006](https://github.com/frostyard/lodge/blob/main/docs/adr/0006-agents-reach-each-app-through-its-own-mcp-endpoint.md), [ADR-0011](https://github.com/frostyard/lodge/blob/main/docs/adr/0011-a-shared-inference-layer-routes-apps-to-models.md), [ADR-0013](https://github.com/frostyard/lodge/blob/main/docs/adr/0013-the-companion-opens-beside-the-current-app.md), [ADR-0014](https://github.com/frostyard/lodge/blob/main/docs/adr/0014-agents-on-lodges-host-use-a-local-agent-socket.md)
