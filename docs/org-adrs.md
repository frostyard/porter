# Organization ADRs binding porter

Decisions from [frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr) that this repository follows:

- [ADR-0002](https://github.com/frostyard/core/blob/main/docs/adr/0002-agent-portable-instruction-surface.md), [ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md): `AGENTS.md` is canonical. `CLAUDE.md`, `GEMINI.md` and `.github/copilot-instructions.md` are symlinks to it.
- [ADR-0021](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md): CI actions are pinned to full commit SHAs, with least-privilege permissions.
- [ADR-0023](https://github.com/frostyard/core/blob/main/docs/adr/0023-verified-pinned-downloads.md): downloads are version-pinned and checksum-verified.
- [ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md): one `docs/` tree in the four-category shape.
- [ADR-0038](https://github.com/frostyard/core/blob/main/docs/adr/0038-scope-the-test-name-filter-to-chairlift.md): `make ci` is the canonical gate, and tests run without a name filter. It supersedes ADR-0022, so porter has no `TestI` reservation.
- [ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md), [ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md): tools are pinned in `mise.toml` with `mise.lock`, and the gates are `make verify`, `check` and `ci`.
- [ADR-0057](https://github.com/frostyard/core/blob/main/docs/adr/0057-run-first-party-apps-behind-lodge.md): frostyard's first-party apps run behind lodge. Porter is such an app.

The scaffold does not yet have CI, `mise.toml`, or the `verify` and `ci`
gates. Phase 2 of the [roadmap](plans/0001-roadmap.md) adds them, from lodge's.
