# Spec: Routines

A routine is work Porter does on a schedule: a schedule and a prompt,
written by the owner as a note in their vault. porterd reads routines,
runs each when it is due, and writes what it produces. This spec is the
note's format and how porterd runs it. The owner writes routines in
frostnotes, and porterd consumes them.

**Status: Porter's founding contract** (2026-10-19, Proposed). It binds
from [roadmap](../plans/0001-roadmap.md) Phase 3.

## Interface

### Where routines live

- A routine is a markdown file directly in `Porter/Routines/` of the vault
  Porter's notes token names. Files in subfolders are not routines.
- Its name is the file name without `.md`, for example `Morning brief`.
  Names are unique because file names are.
- Porter only reads `Porter/Routines/`. Its notes token excludes that
  folder from its writes
  ([ADR-0005](../adr/0005-memory-and-routines-live-in-the-vault.md)).

### The note

YAML frontmatter, then the prompt as the body.

| Field | Type | Required | Constraints |
| --- | --- | --- | --- |
| `schedule` | string | yes | Five cron fields (minute, hour, day of month, month, day of week), in `timezone` |
| `timezone` | string | no | An IANA zone; defaults to Porter's configured zone |
| `output` | string | no | A vault path template ending in `.md`, under `Porter/` and not under `Porter/Routines/`; placeholders below |
| `thinking` | bool | no | Whether the model thinks before answering; default `false` |
| `model` | string | no | A lodge inference alias Porter may use; default Porter's `routine_alias` (configuration), itself `local` unless set |
| `enabled` | bool | no | Default `true`; `false` keeps the routine without running it |

| Placeholder | Becomes |
| --- | --- |
| `{{date}}` | the run's date, `YYYY-MM-DD` |
| `{{week}}` | its ISO week, `YYYY-Www` |
| `{{weekday}}` | its weekday's English name |

The body is the prompt: 1 to 16,384 bytes of markdown, given to the model
as the owner's message for the run.

```markdown
---
schedule: "30 6 * * 1-5"
output: "Porter/Briefs/{{date}}.md"
---
Write my morning brief for today. Keep it to one phone screen, in this order:

1. **Today:** my events with times and places. Flag clashes, and name the
   first thing I need to be ready for.
2. **Waiting on me:** up to five threads where someone asked me something I
   haven't answered, VIPs first. One line each, with a link.
3. **Due:** tasks due today or overdue, and unchecked items in the last
   three days' daily notes.
4. **Yesterday:** two or three lines on what I wrote and what I sent.

Draft no replies. End with one line offering to draft replies to the
"Waiting on me" threads.
```

```markdown
---
schedule: "0 18 * * 0"
output: "Porter/Reviews/{{week}}.md"
thinking: true
---
Write my weekly review, about two screens, in this order:

1. **My open loops:** mail I haven't answered in over three days, overdue
   or undated tasks, and unchecked items in this week's daily notes.
2. **Others' open loops:** mail I sent with no reply in five days.
3. **This week:** the notes I created, as one list.
4. **Next week:** events that need preparation, and any double-booking.

Link every item.
```

### A run

1. **When:** porterd starts a run when the schedule matches the current
   minute in the routine's zone.
2. **Tools and rights:** the run gets the system prompt, `Porter/Memory.md`
   and the routine's body, but not the thread. It runs only the unattended
   tools of [ADR-0004](../adr/0004-porter-drafts-and-the-owner-sends.md):
   reads, mail drafts, and notes under `Porter/` outside `Porter/Routines/`.
   The other tools are offered only to propose. A call of one, such as
   flagging mail or appending to the daily note, does not run. It becomes
   an approval card, which runs only when the owner approves it. `remember`
   is not offered. Its inference requests are `background`.
3. **Output:** the model's final answer is the output. With `output` set,
   porterd writes it to that note, with frontmatter `routine: <name>` and
   `run: <the run's scheduled time, RFC 3339 in the routine's zone>`.
   Without `output`, the answer is the thread line itself, after the
   routine's name. In the output note, a link to a note is a wikilink, and
   any other lodge address is an absolute URL under Porter's `lodge_url`,
   so the note's links work in notes and in Obsidian.
4. **Reporting:** each run posts one line in the thread, linking to the
   output. Its cards are what the run did and proposes: drafts, notes
   written, and approvals. It also records an Activity entry.

## Rules

- A routine's prompt cannot widen what a run may do. porterd gives runs
  only the unattended tools, and the apps' scopes enforce the rest.
- porterd reads `Porter/Routines/` at start. After that, it lists the
  folder every minute and rereads each note whose modified time changed,
  so a new or edited routine needs no restart. notes pushes no changes to
  agents.
- An invalid routine never runs, even partly. That covers:
  - frontmatter that doesn't parse;
  - an unknown field, or a field with a bad value;
  - an `output` outside `Porter/`;
  - an empty body.
  Activity lists it with the reason, and the thread says so once per change.
- A routine never overlaps itself. When a run is still going at its next
  time, that time is skipped and noted in Activity.
- **Missed runs:** if porterd was down when a routine was due, it runs that
  routine once at start, if the missed time is less than 6 hours old. An
  older miss is noted, not run.
- **Run limits:** a run ends, as failed, after 10 minutes or 40 tool calls.
  A failed run writes no output note, and its thread line says why.
- **Overwriting:** an output note is replaced only if its frontmatter names
  the same routine. Otherwise porterd writes beside it, adding ` (2)`
  before `.md`, and so on.
- When the model's provider is unavailable, a run is retried every
  15 minutes for up to 2 hours, then fails.

## References

- Rationale: [ADR-0005](../adr/0005-memory-and-routines-live-in-the-vault.md), [ADR-0004](../adr/0004-porter-drafts-and-the-owner-sends.md)
- Context: [design/overview.md](../design/overview.md)
