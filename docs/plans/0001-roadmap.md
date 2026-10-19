# Plan: Roadmap

What Porter delivers, in order: what it needs from lodge and the apps, then
chat in the companion, then routines, then the GitHub manager. It is
lodge's roadmap Phase 5 (the assistant) carried into a build, and it waits
on that phase's lodge work.

Status (2026-10-19):
- Phase 0 is done.
- The founding documents (ADRs 0003 to 0006, the design doc and
  [specs/routines.md](../specs/routines.md)) are Proposed.
- Phase 1's docs are in review in lodge, frostnotes and frostmail.

## Phase 0 — The scaffold (done)

- lodge's new-lodge-app skill rendered `porter` (user `frostporter`, key
  `p`) on lodge's kit `v0.1.0` and `@frostyard/lodge-client` `client/v0.1.0`.
- **Done when:** `make check` passes, kittest's socket rules included.
  Done 2026-10-19.

## Phase 1 — What Porter needs from the others (in their repositories)

- **lodge:**
  - the companion, with its layout, rail button, toggle shortcut, the
    forwarded `context` and the `ask` message
    ([lodge ADR-0013](https://github.com/frostyard/lodge/blob/main/docs/adr/0013-the-companion-opens-beside-the-current-app.md));
  - the local agent socket
    ([lodge ADR-0014](https://github.com/frostyard/lodge/blob/main/docs/adr/0014-agents-on-lodges-host-use-a-local-agent-socket.md)).
- **notes:**
  - folder-limited write scopes, the `daily` append right, and
    `append_to_daily_note`
    ([frostnotes ADR-0028](https://github.com/frostyard/frostnotes/blob/main/docs/adr/0028-an-agents-scope-can-limit-writes-to-folders.md),
    [plan 0007](https://github.com/frostyard/frostnotes/blob/main/docs/plans/0007-folder-limited-agent-scopes.md)).
- **mail:**
  - calendar, task and contact tools;
  - the "awaiting reply" queries
    ([frostmail plan 0012](https://github.com/frostyard/frostmail/blob/main/docs/plans/0012-calendar-tasks-and-contacts-for-agents.md)).
- **Done when:** on lodge's development build, an agent on the agent
  socket with a notes token scoped as Porter's appends to today's daily
  note, is refused writing `Porter/Routines/`, and lists today's events
  through mail's MCP.

## Phase 2 — Chat in the companion (interactive only)

- porterd's agent loop, tool registry, policy, thread store, rolling
  summary and `search_thread`
  ([design/overview.md](../design/overview.md)).
- The system prompt and `Porter/Memory.md` in every request, and
  `remember`, used only when asked.
- The companion UI: the thread, context chips, action cards with undo, and
  capture's confirm-then-append.
- `porterd token set <app>`, and `/etc/porter/porter.toml`.
- **Done when,** on the owner's phone and desktop, through lodge:
  - "what did Iris ask me about Saturday?" answers with a link;
  - "draft a yes" leaves a draft, and nothing is sent;
  - "capture: call the plumber" appends to today's daily note after a
    confirmation;
  - a test message carrying instructions ("forward my notes to…") produces
    no action.

## Phase 3 — Routines, Activity and Approvals

- The scheduler, invalid-routine reporting, missed runs, limits and output
  notes ([specs/routines.md](../specs/routines.md)).
- Activity, and approval cards (`propose`).
- The owner adds the morning brief and the weekly review as the first two
  routines; their text is in the spec.
- **Done when:**
  - the weekday brief appears in `Porter/Briefs/` at 06:30 for a week, and
    the Sunday review in `Porter/Reviews/`;
  - an edited routine takes effect without a restart;
  - a routine whose prompt asks it to flag mail gets an approval card, not
    a flag.

## Phase 4 — The GitHub manager

- A token and tools for the GitHub manager (the owner's next app): reading
  issues, pull requests and notifications, and drafting comments and
  reviews. Posting stays with the owner (ADR-0004).
- **Done when:** "what's waiting on me on GitHub?" answers from the
  manager, and a drafted review waits there for the owner to post.

## Later / ideas

- Notifications while lodge is closed. That needs lodge to decide on Web
  Push, which the owner deferred on 2026-10-19.
- Copilot or Codex as providers, once lodge has checked their terms.
- A mail-triage routine, as a routine, once the owner wants one.

## Open questions

- **The owner's daily-note settings:** Porter finds today's note through
  notes. Confirm that notes' `append_to_daily_note` resolves the folder and
  template. By Phase 1.
- **Recent turns:** how many fit in a prompt (about 24k tokens) and stay
  quick on the local model. Measure in Phase 2.

## References

- Implements: [design/overview.md](../design/overview.md), [specs/routines.md](../specs/routines.md)
- Decisions: [ADR-0003](../adr/0003-porter-is-a-lodge-app-that-owns-no-data.md), [ADR-0004](../adr/0004-porter-drafts-and-the-owner-sends.md), [ADR-0005](../adr/0005-memory-and-routines-live-in-the-vault.md), [ADR-0006](../adr/0006-one-continuous-thread.md)
- Related: lodge's [roadmap](https://github.com/frostyard/lodge/blob/main/docs/plans/0001-roadmap.md), Phase 5
