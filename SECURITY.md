# Security policy

Porter is a lodge app: the `porterd` daemon on lodge's app socket, and its
web bundle, which lodge serves. It holds an MCP token per app in
`/var/lib/frostporter/tokens/` (mode 0600), and it reaches the apps only
through lodge's agent socket. No token carries a send or post right: Porter
drafts, and the owner sends
([ADR-0004](docs/adr/0004-porter-drafts-and-the-owner-sends.md)). It reaches
mail, calendar, tasks and notes. It passes other people's words to the
model as data, never as instructions. It read frostread, read-only, until
that app was retired on 2026-11-15
([ADR-0008](docs/adr/0008-porter-no-longer-reads-frostread.md)).
Treat any report that touches the tokens, the tool classification (what a
tool may do), the routines' unattended tools, the approval cards, prompt
injection through tool results, or the sockets with care.

## Supported versions

Porter is pre-release. Fixes go to `main` and ship in the next release.
Only the latest `vX.Y.Z` release gets fixes, and only its
`frostyard-porterd` `.deb`; there are no backports to older releases.

## Reporting a vulnerability

Report it privately, through GitHub's private vulnerability reporting for
this repository: <https://github.com/frostyard/porter/security>
("Report a vulnerability").

Do not open a public issue for a vulnerability. Never post tokens, host
names, tailnet names, mail or notes.

## What to include

- the version: the manifest's `version` in `lodgectl apps`, or the commit;
- where it runs: an installed `.deb`, the sysext `porterd`, or a
  `make lodge-package` stage;
- the impact, and what you expected instead;
- the steps to reproduce it, with made-up data (`alice@example.com`,
  `Plan.md`).
