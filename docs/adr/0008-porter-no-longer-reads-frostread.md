# 0008 — Porter no longer reads frostread

- **Status:** Accepted (Brian, 2026-11-14)
- **Date:** 2026-11-15

## Context

[frostread](https://github.com/frostyard/frostread) is retired on
2026-11-15: a general feed reader covers its use. Its service on lodge's
host is stopped, its package is removed, and lodge no longer lists the app
`read`. Its MCP endpoint `/mcp/read`, its 4 read tools and its
`/read/item/<id>` address are gone with it
([ADR-0007](0007-porter-reads-frostread-read-only.md), which ADR-0003
extended).

Porter kept that app in 4 places: the default `apps` list of
`/etc/porter/porter.json`, the `read` entry of the tool registry's `known`,
the allow-list `readOnlyApps` that made every other tool of `read` Never,
and the prompt and card code that named Read and `/read/item/<id>`.

## Decision

- **Porter drops the app `read`.** The default `apps` list is `notes` and
  `mail`.
- **The `read` special case goes with it:** ADR-0007's read-only
  allow-list `readOnlyApps`, the `read` entry of `known`, the
  `/read/item/<id>` card link in `resultURL`, and the prompt's lines about
  Read and reading.
- **An app id `read` in `/etc/porter/porter.json` is treated like any
  other app.** Porter knows no tool of it, so each tool is marked by its
  annotations: read-only is Read, anything else is Change. No line of
  Porter's code names it.

## Consequences

- Chat and briefs no longer cover feeds. A question about a feed has no
  tool behind it.
- A routine body that asks for a Reading section gets no reading tools, so
  the model says so or leaves the section out.
- A configuration that still lists `read` gets the existing "no token"
  warning in the log, or the app's own MCP error, as for any app that is
  unreachable. Porter starts either way.
- Porter is one token and one app smaller. A future reader app adds itself
  as one more app and one more token
  ([ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md)), and
  needs no revival of this code.

## Alternatives considered

- **Keep the code for a later reader:** rejected. It is dead code for an
  app that does not exist, and a new reader app gets its own decision, on
  its own tools and scopes.
- **Keep only the read-only guard (`readOnlyApps`):** rejected. It guards
  an app Porter no longer reaches, and no other app asks for it. Any later
  read-only app brings its own ADR and its own line.

## References

- Supersedes: [ADR-0007](0007-porter-reads-frostread-read-only.md)
- Shapes: [design/overview.md](../design/overview.md), [specs/routines.md](../specs/routines.md), [plans/0001-roadmap.md](../plans/0001-roadmap.md) (Phase 5)
- Builds on: [ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md), [ADR-0004](0004-porter-drafts-and-the-owner-sends.md)
