# Packaging

Porter installs on lodge's host as the `frostyard-porterd` `.deb`, whose
files are all under `/usr`, so a sysext can carry it (core ADR-0058), and it
runs as lodge's `porter` app. `packaging/lodge/stage.sh` stages the same tree
by hand with `make lodge-package`.

| File | Installs as |
| --- | --- |
| `lodge/app.json` | `/usr/share/lodge/apps/porter/app.json`: lodge's manifest for the app, with the `version` that `stage.sh` writes |
| `lodge/stage.sh` | the staged tree: `porterd` as `/usr/bin/porterd`, and under `/usr/share/lodge/apps/porter/` the manifest and the web bundle, when `build/web` holds one. It writes only under `usr/` |
| `lodge/frostporter.service` | `/usr/lib/systemd/system/frostporter.service`: the daemon, as user `frostporter` |
| `lodge/frostporter.socket` | `/usr/lib/systemd/system/frostporter.socket`: the socket lodge reaches the app's API on |
| `lodge/sysusers.d-frostporter.conf` | `/usr/lib/sysusers.d/frostporter.conf`: the system user `frostporter`, in the groups `lodge-apps` and `lodge-agents` |
| `nfpm.yaml`, `deb-check.py` | the `.deb`: `make deb` builds it with nFPM, and `make deb-check` reads the archive's exact member names |

## Update

These steps are for lodge's host, which is hand-installed: it has no apt
source and no sysext for porterd (2026-11-03). An update installs a
release's `.deb`, `frostyard-porterd_X.Y.Z_amd64.deb` (see
[Release](#release)), by hand with `dpkg -i`. Since v0.2.0 (2026-11-03) the
host has the package's layout: porterd is `/usr/bin/porterd`, and the units
and the sysusers.d entry are the package's, in `/usr/lib/`. The package has
no maintainer scripts: it starts, stops and reloads nothing, so the steps
below do that.

1. Get the package and check it, on a computer with `gh`. Continue only when
   the hash is the release asset's digest and the attestation's workflow is
   `release.yml` at `refs/tags/vX.Y.Z`. Then copy the package to the host's
   `/root`, and keep it there: it is the next roll back.

   ```sh
   gh release download vX.Y.Z -R frostyard/porter -p 'frostyard-porterd_*_amd64.deb'
   sha256sum frostyard-porterd_X.Y.Z_amd64.deb
   gh release view vX.Y.Z -R frostyard/porter --json assets --jq '.assets[].digest'
   gh attestation verify frostyard-porterd_X.Y.Z_amd64.deb -R frostyard/porter
   ```

2. On the host, stop Porter. While it is stopped, lodge shows `porter` down
   and routines do not run.

   ```sh
   sudo systemctl stop frostporter.socket frostporter.service
   ```

3. Archive what a roll back needs. Keep the archive root-only, by its mode
   as well as its directory, because `/var/lib/frostporter/tokens/` holds the
   app tokens.

   ```sh
   sudo sh -c 'umask 077; tar -czpf /root/porter-rollback-<old version>-<UTC time>.tar.gz \
     /usr/bin/porterd /usr/share/lodge/apps/porter \
     /usr/lib/systemd/system/frostporter.socket \
     /usr/lib/systemd/system/frostporter.service \
     /usr/lib/sysusers.d/frostporter.conf /etc/porter /var/lib/frostporter'
   ```

4. Install, reload and start:

   ```sh
   sudo dpkg -i /root/frostyard-porterd_X.Y.Z_amd64.deb
   sudo systemctl daemon-reload
   sudo systemctl start frostporter.socket frostporter.service
   sudo lodgectl reload
   ```

5. Check. `sudo lodgectl apps` must show `porter` up, at the package's
   version. `journalctl -u frostporter.service` since the start must show no
   warning and no error.

The move to the package, once (v0.2.0 on lodge's host, 2026-11-03): before
it, porterd was `/opt/lodge/bin/porterd`, with full units in
`/etc/systemd/system/` and the sysusers.d entry in `/etc/sysusers.d/`. Those
copies shadow the package's, so the move archived them with step 3's archive
(their paths), then ran `systemctl disable frostporter.socket`, removed
them, `/opt/lodge/bin/porterd`, `porterd.prev` and
`/usr/share/lodge/apps/porter/`, then ran `dpkg -i`,
`systemctl daemon-reload`, `systemctl enable frostporter.socket` and the
start of step 4. It did not run `lodgectl reload`, so lodge showed the old
version until a later reload. Only one porterd can start now.

## Roll back

1. Stop Porter, as in step 2 of Update.
2. Put the earlier release back with its `.deb`, which step 1 of Update kept
   in `/root`. `dpkg` accepts the older version:

   ```sh
   sudo dpkg -i /root/frostyard-porterd_OLD_amd64.deb
   ```

   To go back to `d3db41b`, the version before the package, use its archive
   from the move, `/root/porter-rollback-d3db41b-20261103T152041Z.tar.gz`:
   `sudo dpkg -r frostyard-porterd`, remove `/usr/share/lodge/apps/porter/`,
   then restore the binary, the bundle, the units, their enable link and the
   sysusers.d entry, but not the state or the configuration:

   ```sh
   sudo tar -xzpf ARCHIVE -C / --exclude=var/lib/frostporter --exclude=etc/porter
   ```

3. Start, reload and check, as in steps 4 (without `dpkg -i`) and 5 of
   Update.

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
4. Start and check, as in steps 4 (without `dpkg -i`) and 5 of Update.

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
[`.github/workflows/release.yml`](../.github/workflows/release.yml): it builds
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

