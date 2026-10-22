# Plan: Roadmap

What Porter delivers, in order: what it needs from lodge and the apps, then
chat in the companion, then routines, then reading from frostread, then the
GitHub manager. It is
lodge's roadmap Phase 5 (the assistant) carried into a build, and it waits
on that phase's lodge work.

Status (2026-10-22):
- Phase 0 is done.
- The founding documents (ADRs 0003 to 0006, the design doc and
  [specs/routines.md](../specs/routines.md)) are Proposed.
- Phase 1 is built and checked on lodge's host (2026-10-19).
- Phases 2 and 3 are built and merged to `main` (porter#1 and porter#2,
  2026-10-21). Their done-when checks are still open.
- Phase 5 (reading from frostread) is built before Phase 4, merged
  (porter#7) and deployed (2026-10-21,
  [ADR-0007](../adr/0007-porter-reads-frostread-read-only.md), Accepted).
  Its done-when is met: a brief run by hand lists frostread items, and
  chat answers "what is new in my feeds" from Read (OWNER-REPORTED,
  Brian, 2026-10-21).
- `main` at `d3db41b` is deployed on lodge's host (2026-10-21). See
  [Deployments](#deployments). The later merges have no deploy record:
  porter#10 to porter#13 pin `@frostyard/lodge-ui` `ui/v0.5.0` and move the
  web bundle to lodge-client's `rpc` subpath at `client/v0.3.2`.

## Deployments

Porter runs on lodge's host, the Incus VM `frostnotes-hub`, as lodge's
`porter` app.

| Date | Version | What | Checked live |
|------|---------|------|--------------|
| 2026-10-20 | web `7405fdf`, porterd `d8b1e1b` | Phases 2 and 3 from their branches | (not recorded) |
| 2026-10-21 | `73eead0` | `main` after porter#5: the service-bell icon (frostmail ADR-0015's frame), the look from `@frostyard/lodge-ui` `ui/v0.1.0`, times in the owner's zone, links into the vault, the `routine_alias` default | `lodgectl status` and `lodgectl apps` show porter `73eead0` up; the journal since the restart has no warning or error; the icon shows in the rail and on the companion button; 1 chat turn; a link in a brief opens the note, and a `/mail/message/…` link in a brief opens the message |
| 2026-10-21 | `d3db41b` | `main` after porter#7: Porter reads frostread, read-only (ADR-0007); a routine knows its last success. `read` added to `/etc/porter/porter.json`; token `porter-read` (scope `read`, agent `frostporter`). Rollback archive `/root/porter-rollback-73eead0-20261021T155413Z.tar.gz` | `lodgectl apps` shows porter `d3db41b` up, with 3 apps; no warning in the journal since the restart; a temporary routine with the Morning brief's body and no `output` ran by hand (15:57 UTC, 0 actions), and its Reading section listed The Go Blog's new item with a `/read/item/<id>` link and the unread count; the temporary routine was then deleted |

How the host differs from `make lodge-package`'s layout:
- porterd is `/opt/lodge/bin/porterd`, with the version before it kept as
  `porterd.prev`. stage.sh stages `/usr/local/bin/porterd`.
- The units are in `/etc/systemd/system/` and the sysusers.d entry in
  `/etc/sysusers.d/`. Their `ExecStart` is `/opt/lodge/bin/porterd`; they
  are otherwise the same as stage.sh's.
- Thus do not extract the staged tarball onto `/`: porterd would not
  change. Extract it to a directory, copy `usr/local/bin/porterd` to
  `/opt/lodge/bin/porterd`, replace `/usr/share/lodge/apps/porter/` with
  the staged one (directories 0755, files 0644, owned by root), then
  `systemctl daemon-reload`, `systemctl restart frostporter.service` and
  `lodgectl reload`.
- `/etc/porter/porter.json` has `lodge_url` set to lodge's own tailnet node
  (from `lodgectl status`), not to the host's name.

## Phase 0 — The scaffold (done)

- lodge's new-lodge-app skill rendered `porter` (user `frostporter`, key
  `p`) on lodge's kit `v0.1.0` and `@frostyard/lodge-client` `client/v0.1.0`.
- **Done when:** `make check` passes, kittest's socket rules included.
  Done 2026-10-19.

## Phase 1 — What Porter needs from the others (done 2026-10-19, in their repositories)

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
    [plan 0007](https://github.com/frostyard/frostnotes/blob/main/docs/plans/0007-folder-scopes-for-agents.md)).
- **mail:**
  - calendar, task and contact tools;
  - the "awaiting reply" queries
    ([frostmail plan 0012](https://github.com/frostyard/frostmail/blob/main/docs/plans/0012-calendar-tasks-and-contacts-for-agents.md)).
- **Done when:** on lodge's development build, an agent on the agent
  socket with a notes token scoped as Porter's appends to today's daily
  note, is refused writing `Porter/Routines/`, and lists today's events
  through mail's MCP.
- **Done (2026-10-19), on lodge's own host rather than the development
  build.** lodge bab1ed7, notes dd604aa and mail 424e2a7, on the test vault
  and showcase mail.
  - The agent ran as user `frostporter` on `/run/lodge/agent.sock`, with
    notes `vault=1;write=Porter/;except=Porter/Routines/;daily` and mail
    `account=*;write` tokens.
  - It appended to today's daily note (`/notes/1/2026-10-19.md`) and wrote
    `Porter/Phase 1 check.md`.
  - Writing `Porter/Routines/Planted.md`, or a note outside `Porter/`,
    answered invalidParams naming the writable folders.
  - It listed the day's three events with their clash, nine tasks, and
    "waiting on you" with a Message-ID and a `/mail/message/…` link. Mail
    offered no send tool.
  - The same mail token over the tailnet route answered 401, and a user
    not in `lodge-agents` could not open the socket.

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

**Built 2026-10-20** on porter branch `phase3/routines`:
- the scheduler (`internal/routine`);
- approval cards for what a routine may not do;
- `thread.pending` and the `activity` domain;
- the full app's Activity and Approvals tabs.

What remains is the done-when evidence: a week of briefs, and a review.

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

## Phase 5 — Reading from frostread

Built before Phase 4, since frostread exists and the GitHub manager does
not ([ADR-0007](../adr/0007-porter-reads-frostread-read-only.md)).

- **The token:** one `read`-scope token, issued with
  `lodgectl token issue porter-read --app read --scope read --agent
  frostporter` and set with `porterd token set read`. Brian approved it on
  2026-10-21. `read` joins the default apps.
- **The tools:** `list_sources`, `list_unread`, `search` and `read_item`,
  by Porter's own allow-list as well as the token's scope. `save_url`,
  `mark_read` and `star` are never offered. A `read_item` card links
  `/read/item/<id>`.
- **The brief's Reading section:** new items since the last brief, by
  source, capped and summarised
  ([specs/routines.md](../specs/routines.md)).
- **The last-run time:** porterd keeps the scheduled time of each routine's
  last successful run, and the run's system prompt gives it, so "since the
  last brief" has a meaning.
- **Done when,** on lodge's host: a brief run by hand lists frostread items
  by source with links to `/read/item/<id>`, and chat answers "what is new
  in my feeds?" from Read.
- **The brief half is met** (2026-10-21, `d3db41b`; see
  [Deployments](#deployments)).
- **Done when is met** (OWNER-REPORTED, Brian, 2026-10-21): Brian asked
  Porter's chat "what is new in my feeds" on lodge's host, at `d3db41b`,
  and reported that it worked well.

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
- Decisions: [ADR-0003](../adr/0003-porter-is-a-lodge-app-that-owns-no-data.md), [ADR-0004](../adr/0004-porter-drafts-and-the-owner-sends.md), [ADR-0005](../adr/0005-memory-and-routines-live-in-the-vault.md), [ADR-0006](../adr/0006-one-continuous-thread.md), [ADR-0007](../adr/0007-porter-reads-frostread-read-only.md)
- Related: lodge's [roadmap](https://github.com/frostyard/lodge/blob/main/docs/plans/0001-roadmap.md), Phase 5
