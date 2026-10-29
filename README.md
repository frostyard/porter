# Porter

The owner's personal assistant inside
[lodge](https://github.com/frostyard/lodge). You talk to it in a chat panel
beside whatever app you are in. It reads your mail, calendar, tasks and
notes through their MCP endpoints, drafts but never sends, keeps its memory
and its scheduled prompts (routines) as notes in your vault, and thinks
through lodge's inference layer. Pre-release: chat, routines and reading
from frostread are built and run on lodge's host. The design is in
[docs/](docs/README.md).

lodge's `porter` app: the `porterd` daemon, on the app socket lodge
reaches it through, and its web bundle, which lodge serves at
`/app/porter/`. It follows lodge's
[app contract](https://github.com/frostyard/lodge/blob/main/docs/specs/app-contract.md),
on [lodge's kit](https://github.com/frostyard/lodge/tree/main/kit) and
[`@frostyard/lodge-client`](https://github.com/frostyard/lodge/tree/main/client).

| Path | What |
| --- | --- |
| `cmd/porterd` | the daemon, with the contract's socket rules as tests (kittest) |
| `api`, `internal/rpcserver` | its JSON-RPC API, on `kit/jsonrpc` |
| `schema/rpc` | the API's contract; `api/rpc.go` follows it by hand |
| `internal/…` | the thread store, the tool registry, the agent loop and the routine scheduler (see `AGENTS.md`'s map) |
| `web` | the companion and the full app, on `@frostyard/lodge-client` and `@frostyard/lodge-ui` |
| `packaging/lodge` | the manifest, the socket and service units, the system user, and `stage.sh` |

- `make check` formats, vets and tests the daemon.
- `make web-build` builds the bundle in an nsl machine.
- `make lodge-package` stages everything under `build/lodge-root` (or `LODGE_DESTDIR`).

## Update

These steps are for lodge's host today, which is hand-installed. It differs
from the layout `make lodge-package` stages: porterd is
`/opt/lodge/bin/porterd`, and the units are in `/etc/systemd/system/`. Do not
extract the staged tree onto `/`.

1. Get the new code with `git pull --ff-only origin main`. Build the web
   bundle with `make web-build` (in an nsl machine), then stage with
   `make lodge-package`. The tree is `build/lodge-root`. porterd is
   `usr/bin/porterd` there, or `usr/local/bin/porterd` in earlier commits.
2. On the host, stop Porter. While it is stopped, lodge shows `porter` down
   and routines do not run.

   ```sh
   sudo systemctl stop frostporter.socket frostporter.service
   ```

3. Archive what a roll back needs. Keep the archive root-only, by its mode
   as well as its directory, because `/var/lib/frostporter/tokens/` holds the
   app tokens.

   ```sh
   sudo sh -c 'umask 077; tar -czpf /root/porter-rollback-<old commit>-<UTC time>.tar.gz \
     /opt/lodge/bin/porterd /usr/share/lodge/apps/porter \
     /etc/systemd/system/frostporter.socket \
     /etc/systemd/system/frostporter.service \
     /etc/sysusers.d/frostporter.conf /etc/porter /var/lib/frostporter'
   ```

4. Keep the old binary, then copy in the new one. Both files must stay owned
   by root.

   ```sh
   sudo mv /opt/lodge/bin/porterd /opt/lodge/bin/porterd.prev
   sudo install -m755 <staged porterd> /opt/lodge/bin/porterd
   ```

5. Replace `/usr/share/lodge/apps/porter/` with the staged one. Use `cp -r`,
   not `-a`, so that root owns the files. The `chmod` sets directories to 0755
   and files to 0644.

   ```sh
   sudo rm -rf /usr/share/lodge/apps/porter
   sudo cp -r build/lodge-root/usr/share/lodge/apps/porter /usr/share/lodge/apps/
   sudo chmod -R u=rwX,go=rX /usr/share/lodge/apps/porter
   ```
6. Start and reload:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl start frostporter.socket frostporter.service
   sudo lodgectl reload
   ```

7. Check. `sudo lodgectl apps` must show `porter` up, at the version of the
   manifest that `make lodge-package` wrote. `journalctl -u frostporter.service`
   since the start must show no warning and no error.

These steps keep the host's units in `/etc/systemd/system/`. If a new commit
changes `packaging/lodge/frostporter.service` or `frostporter.socket` in a
line other than `ExecStart=`, copy that change by hand. Keep
`ExecStart=/opt/lodge/bin/porterd`.

## Roll back

1. Stop Porter, as in step 2 of Update.
2. Put the old binary back: `porterd.prev`, or `porterd` from the archive,
   to `/opt/lodge/bin/porterd`.
3. Remove `/usr/share/lodge/apps/porter/`, then restore from the archive.
   This command restores the binary and the bundle, and the units, but not
   the state or the configuration:

   ```sh
   sudo tar -xzpf ARCHIVE -C / --exclude=var/lib/frostporter --exclude=etc/porter
   ```

   You can also build the earlier commit and stage it.
4. Start, reload and check, as in steps 6 and 7 of Update.

The thread database, `/var/lib/frostporter/thread.db`, has no schema version.
A newer porterd only adds the column `routines.last_success`, and an older
porterd still opens the database with it. Today a roll back does not need a
restore of `/var/lib/frostporter`. A later migration can change this.

Restore `/var/lib/frostporter` only if `journalctl -u frostporter.service`
shows a thread database error at start. These errors follow `porterd: ` in
the journal, and begin with `open thread database:`, `thread schema:`,
`thread recovery:`, `routines columns:`, `add routines.last_success:` or
`fill routines.last_success:`. A start can fail for other reasons, for example a bad
`/etc/porter/porter.json`, and a restore does not help then. To restore:

1. Stop `frostporter.socket` and `frostporter.service`.
2. Keep the current state aside:
   `sudo mv /var/lib/frostporter /var/lib/frostporter.new`.
3. Extract it: `sudo tar -xzpf ARCHIVE -C / var/lib/frostporter`.
4. Start and check, as in steps 6 and 7 of Update.

The restore loses the thread turns, Activity, approvals and routine history
after the archive, and any token changes. It does not undo what Porter
already did in other apps, such as drafts and notes.

Configuration is in `/etc/porter/porter.json`. Memory and routines live in
your vault, not on the host.

### After the sysexts

The sysext `porterd` is built and published for Snosi bootc images (Floe).
[snosi#1169](https://github.com/frostyard/snosi/pull/1169) added the first 2
suite sysexts (notesd, maild).
[snosi#1173](https://github.com/frostyard/snosi/pull/1173) added lodge, readd
and porterd. [snosi#1175](https://github.com/frostyard/snosi/pull/1175) pinned
readd 0.2.0 and porterd 0.1.1 (lodge stays 0.1.0). Updex downloads them from
`https://repository.frostyard.org/ext/porterd/` and checks the signature. The
full steps are on [the site page](https://frostyard.org/docs/sysexts/porterd/).

lodge's own host stays hand-installed (core Plan 0010 Phase 4 is deferred,
2026-10-23), so the steps above stay valid for that host.

The state in `/var/lib/frostporter` and the configuration in `/etc/porter` are
not in the sysext. A sysext roll back does not undo a change to `thread.db`,
so back up `/var/lib/frostporter` before you update. The units are upheld
(`Upholds=`), so a plain `systemctl stop` does not hold. For a backup or a
restore, mask the units, then unmask and start them. Keep the archive
root-only:

```sh
sudo systemctl mask --now frostporter.socket frostporter.service
sudo sh -c 'umask 077; tar -czpf /root/porter-state-$(date -u +%Y%m%dT%H%MZ).tar.gz /var/lib/frostporter'
sudo systemctl unmask frostporter.socket frostporter.service
sudo systemctl start frostporter.socket frostporter.service
```

On a Snosi image, update with:

```sh
sudo updex features update --component=porterd
```

An interactive update does a live refresh. With Updex v2.1.3 and later
([ADR-0019](https://github.com/frostyard/updex/blob/main/docs/adr/0019-apply-merged-sysusers-and-tmpfiles-on-a-live-refresh.md)),
the new files merge at once, `systemd-sysusers` and `systemd-tmpfiles --create`
run, and systemd reloads its unit files. Updex does not restart units: the
running daemon keeps its old binary until you restart it. No reboot is
necessary. Restart Porter, then let lodge read the new manifest:

```sh
sudo systemctl restart frostporter.socket frostporter.service
sudo lodgectl reload
sudo lodgectl apps
```

`lodgectl apps` lists `porter` up at the new version. lodge polls every 15
seconds, so if it shows `down` at once, run it again after 15 seconds.

Only these cases wait for the next boot: `--no-refresh`, the automatic update
timer, or output that says `refresh skipped`. Then reboot. With an Updex older
than v2.1.3, run `sudo systemd-sysusers`, `sudo systemd-tmpfiles --create` and
`sudo systemctl daemon-reload` first (or reboot). `updex --version` shows the
version.

Roll back with `sudo updex features rollback porterd`. It takes effect at the
next boot. Add `--now` to apply it at once, then restart and reload as
above.

For when to restore, and how, see [Roll back](#roll-back). On a Snosi image,
mask the units there instead of a plain stop.

## Release

A release is a tag, and `make bump` is the only way to make one
([core ADR-0012](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md)).
It checks the pinned `svu`, that you are on `main` at `origin/main` with a
clean tree, and that `make check` passes, then tags `svu next` and pushes
the tag. The tag starts
[`.github/workflows/release.yml`](.github/workflows/release.yml): it builds
`frostyard-porterd_X.Y.Z_amd64.deb` with `make deb`, attests its provenance, and
attaches it to the GitHub release.

`make deb` builds the same package on a developer's machine, after the web
bundle: `make web-build` (nsl), then `make deb`. It writes
`dist/frostyard-porterd_0.0.0-<commit>-snapshot_amd64.deb`, and `make deb-check`
(`packaging/deb-check.py`) reads the archive's exact member names. It fails unless
the control fields are right (`Depends: systemd`, no maintainer scripts), and
the package holds only root-owned, non-writable directories and regular files (no links)
under `/usr`, with the binary (mode 0755), the units, the sysusers.d entry and the web bundle's `index.html`, and a manifest whose `version` is the package's
version. `internal/packagecheck` feeds it bad packages, and each must fail.
CI builds a snapshot on every pull request (job `deb`).

Verify a downloaded package:

```sh
gh attestation verify frostyard-porterd_X.Y.Z_amd64.deb --repo frostyard/porter
```

Snosi pins each release's `.deb` in a reviewed checksum PR, then builds and
publishes the sysext `porterd`
([snosi#1173](https://github.com/frostyard/snosi/pull/1173)). See
[After the sysexts](#after-the-sysexts).

## Limits

What Porter does not do:

- It never sends mail or posts anything. It drafts, and you send. No token
  carries a send right
  ([ADR-0004](docs/adr/0004-porter-drafts-and-the-owner-sends.md)).
- It writes notes only in `Porter/`, and appends to the daily note. It never
  writes `Porter/Routines/`. It writes memory only when you ask
  ([ADR-0005](docs/adr/0005-memory-and-routines-live-in-the-vault.md)).
- Routines get only the unattended tools: reads, drafts, and notes in
  `Porter/` outside `Porter/Routines/`. Other actions become approval cards.
- It reads frostread read-only
  ([ADR-0007](docs/adr/0007-porter-reads-frostread-read-only.md)). It writes
  no calendar events.
- It owns none of your data. It reaches the apps only through their MCP
  endpoints, with lodge tokens
  ([ADR-0003](docs/adr/0003-porter-is-a-lodge-app-that-owns-no-data.md)). It
  runs only as lodge's `porter` app. The GitHub manager is not built.

What can go wrong:

- The model can be wrong. Read a draft before you send it.
- Text from other people (mail, notes, articles) reaches the model as data,
  not as instructions. This lowers the risk. The lack of a send right limits
  the harm. It does not make an attack impossible.
- An app with no token only warns in the log, and Porter leaves it out. Its
  tools are then missing.
- An app that is down loses its tools until it is back. Porter leaves them
  out, and its prompt tells the model to say so if you ask for them.
- Without lodge's inference, chat says that the model is unavailable, and
  routines retry.
- A reply stops at 40 tool calls. A routine run that passes 40 fails.
- porterd refuses unknown keys in `/etc/porter/porter.json`, and does not
  start.
- Revoking an app's token in lodge cuts Porter off from that app at once.
