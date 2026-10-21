# 0007 — Porter reads frostread, read-only

- **Status:** Proposed
- **Date:** 2026-10-21

## Context

[frostread](https://github.com/frostyard/frostread) is the suite's third
app, lodge app id `read`: the owner's feeds and saved articles. It serves
agents an MCP endpoint, `/mcp/read`, like `notes` and `mail`
([ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md)).

- Its read-scope tools are `list_sources`, `list_unread`, `search` and
  `read_item`. All carry `readOnlyHint`. `list_unread` takes `source_id`,
  `since` (RFC 3339, on an item's published time) and `limit` (50 by
  default, at most 200).
- Its write-scope tools are `save_url`, `mark_read` and `star`.
- An item's lodge address is `/read/item/<id>`, which opens the reader. The
  `url` field in its results is the article's web address, which is not a
  lodge address.

The owner wants the morning brief to carry a "Reading" section, and chat to
answer "what is new in my feeds?". Brian approved 1 `read` token for Porter
on 2026-10-21.

## Decision

- **Porter reaches `/mcp/read` with a `read`-scope token,** issued with
  `lodgectl token issue porter-read --app read --scope read --agent
  frostporter`, so it works only on the agent socket.
- **Porter offers only the 4 read tools.** Its own allow-list enforces this
  as well as the token's scope: any `read` tool not on the list is Never,
  whatever its annotations say. A wider token, or a new frostread tool,
  still gives Porter no write. `save_url`, `mark_read` and `star` are never
  offered.
- **Chat and routines both get the 4 tools,** since they are Read
  ([ADR-0004](0004-porter-drafts-and-the-owner-sends.md)).
- **Links go to `/read/item/<id>`,** with the id the tool returned, not to
  the article's web address. A `read_item` card links the same address.
- **Article text is data,** by the rule of ADR-0004: Porter acts on no
  instruction found in an article, and its page loads nothing remote.
- **A routine knows when it last succeeded.** porterd keeps the scheduled
  time of each routine's last successful run, and a run's system prompt
  states it, so the brief can pass it as `since`
  ([specs/routines.md](../specs/routines.md)).

## Consequences

- The brief can have a Reading section: new items since the last brief, by
  source, capped and summarised.
- `since` filters by published time. An item that frostread fetched late,
  after the last brief, with a published time before it, is not in a later
  brief's "new" list. It stays in `list_unread`, and the brief's count of
  items still waiting includes it.
- Porter cannot mark items read, so the brief cannot either. Unread counts
  fall only when the owner reads in frostread.
- Porter gains one more token to set and to revoke. Revoking it cuts Porter
  off from frostread at once, and the rest keep working.
- Porter's code names `read` as a read-only app. Another read-only app needs
  a line there too.

## Alternatives considered

- **A `read;write` token:** rejected. Nothing Porter does needs a write,
  and it would ask for a right ADR-0004's spirit says to keep from an
  unattended agent.
- **Copying frostread's items into Porter's store:** rejected. It breaks
  ADR-0003: Porter owns no data, and a copy would have to be kept in sync.

## References

- Shapes: [design/overview.md](../design/overview.md), [specs/routines.md](../specs/routines.md), [plans/0001-roadmap.md](../plans/0001-roadmap.md) (Phase 5)
- Builds on: [ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md), [ADR-0004](0004-porter-drafts-and-the-owner-sends.md), lodge [ADR-0006](https://github.com/frostyard/lodge/blob/main/docs/adr/0006-agents-reach-each-app-through-its-own-mcp-endpoint.md)
- frostread's contract: its `docs/specs/api.md`, section MCP
