# sendspin-player: Raspberry Pi quickstart script

**Status:** Design approved 2026-05-01
**Tracks:** lower the on-ramp for Pi-as-player setups using prebuilt arm64 release tarballs

## Problem

Setting up a Raspberry Pi as a Sendspin player today requires the user to: install build-time deps via `install-deps.sh`, clone the repo, build with the CGo toolchain, then either run the binary by hand or wire up systemd through `make install-player-daemon`. None of that is friendly for a "stick a Pi behind the speakers and forget about it" workflow, and almost all of it is unnecessary now that the GitHub Releases pipeline ships ready-to-run `linux-arm64` tarballs.

A `curl | sudo bash` quickstart that takes a fresh 64-bit Raspberry Pi OS install to a running, mDNS-discoverable player in one command closes that gap.

## Goals

1. Single-command install: `curl -fsSL https://raw.githubusercontent.com/Sendspin/sendspin-go/main/scripts/quickstart-pi.sh | sudo bash` brings up the player on a fresh 64-bit Raspberry Pi OS — Lite is the recommended target (headless, no PulseAudio/PipeWire, lower scheduling jitter), full desktop also works. Bookworm or newer is required for the `libflac12` runtime package.
2. Idempotent re-run: invoking the script a second time upgrades the binary and refreshes the unit file in place.
3. Optional one-shot configuration via flags: `bash -s -- --name living-room --device "USB DAC"`.
4. Clean uninstall via `--uninstall`.
5. Pin a specific version via `--version v1.6.2` (default: latest).
6. Zero changes to existing build, daemon-install, or release flows.

## Non-goals

- **No 32-bit Pi support.** Releases ship `linux-arm64` only. The script detects `armv6l` / `armv7l` and exits with a clear pointer to 64-bit Raspberry Pi OS. Pi 1 / Zero (v1) / Zero W are excluded by hardware; Pi 2 is excluded by OS choice.
- **No non-Debian distros.** The script's package-install step targets `apt-get`. Other distros must follow the README's manual install steps.
- **No Debian < 12 (Bullseye and earlier).** The script hard-codes `libflac12`, which is Bookworm's package name. On older Debian / Pi OS, `apt-get install libflac12` fails loudly with "no installation candidate" — that error is acceptable as the user-facing rejection rather than adding fallback logic.
- **No checksum / signature verification.** HTTPS to `github.com` is the same trust boundary the `curl | bash` itself crosses; layering a second integrity check adds operational cost without materially changing the threat model. May reconsider if signed releases land later.
- **No interactive device picker.** `--device "<exact name>"` is the documented path. Users wanting a list run `sendspin-player --list-audio-devices` after install.
- **No support for running `sendspin-server` via this script.** Pi-as-server is a less common configuration; if it becomes one, a sibling script is the right move, not flag bloat in this one.
- **No purge of `/etc/sendspin/` on `--uninstall`.** Matches the existing `make uninstall-player-daemon` behavior; user state is precious.

## Approach

A single shell script at `scripts/quickstart-pi.sh`, fetched via raw GitHub URL and piped to `sudo bash`. Self-contained: every helper is inlined, no second fetch, no source tree dependency.

The script reuses every artifact already shipped in the repo:

- The `dist/systemd/sendspin-player.service` unit (already root-running with `ProtectSystem=strict` / `ProtectHome=read-only` hardening; no change).
- The `dist/systemd/sendspin-player.env` file (already wires `SENDSPIN_PLAYER_OPTS` into `ExecStart`).
- The `dist/config/player.example.yaml` file (already configured with sensible defaults; the daemon writes its own `client_id` back on first launch).

These are downloaded directly from `https://raw.githubusercontent.com/Sendspin/sendspin-go/<ref>/dist/...` at the resolved release tag (so a `--version v1.6.2` install gets the unit/config/env files from that tag, not from `main`).

The release tarball comes from GitHub's `/releases/latest/download/` redirect by default, or `/releases/download/<tag>/` when `--version` is set. No GitHub API call, no JSON parsing.

`/etc/default/sendspin-player` is touched only on first install OR when `--name` / `--device` are passed on this invocation. That makes "re-run with new flags" the documented reconfigure path; "re-run with no flags" is purely an upgrade.

## Flow

The script executes the following stages in order. Any non-zero exit aborts the run; idempotency is the recovery story.

1. **Argument parse.** `--name <s>`, `--device <s>`, `--version <tag>`, `--uninstall`, `-h|--help`. Unknown flags abort with usage.
2. **Pre-flight.**
   - Effective UID must be 0 (else: print sudo hint, exit).
   - `uname -m` must be `aarch64` (else: print 64-bit-OS hint, exit).
   - `/etc/debian_version` must exist (else: point at README manual steps, exit).
   - `command -v systemctl` must succeed.
3. **Uninstall short-circuit.** If `--uninstall` is set:
   - `systemctl disable --now sendspin-player` (tolerate "no such unit" cleanly).
   - Remove `/usr/local/bin/sendspin-player` and `/etc/systemd/system/sendspin-player.service`.
   - `systemctl daemon-reload`.
   - Print note: "Config preserved at `/etc/sendspin/` and `/etc/default/sendspin-player`. Remove manually for a full purge."
   - Exit 0.
4. **Install runtime deps.** `apt-get update && apt-get install -y libopus0 libopusfile0 libflac12 libasound2 ca-certificates curl tar`. `libasound2` is the ALSA userspace library miniaudio links against; it is usually present on Pi OS Lite but not guaranteed on a truly minimal image, so we install it explicitly.
5. **Resolve version.** Default URL: `https://github.com/Sendspin/sendspin-go/releases/latest/download/sendspin-player-linux-arm64.tar.gz`. With `--version`: `https://github.com/Sendspin/sendspin-go/releases/download/<tag>/sendspin-player-linux-arm64.tar.gz`. Same logic produces the `dist/...` raw URL ref: `main` for default, `<tag>` for pinned.
6. **Stop service if running.** `systemctl is-active --quiet sendspin-player && systemctl stop sendspin-player`.
7. **Download + extract.** `curl -fSL <url> -o $TMP/sp.tar.gz`, `tar -xzf $TMP/sp.tar.gz -C $TMP`, `install -m 755 $TMP/sendspin-player /usr/local/bin/sendspin-player`. `$TMP` is `mktemp -d` and removed via `EXIT` trap.
8. **Install systemd unit.** `curl -fSL <raw-dist-url>/systemd/sendspin-player.service -o /etc/systemd/system/sendspin-player.service`. Always overwritten.
9. **Install env file (conditional).**
   - If `/etc/default/sendspin-player` does not exist, fetch `dist/systemd/sendspin-player.env` from raw GitHub.
   - If `--name` or `--device` were passed, write `SENDSPIN_PLAYER_OPTS="--name <n> --audio-device <d>"` to `/etc/default/sendspin-player` (overwriting). Each flag value is wrapped in single quotes with embedded single quotes escaped via the standard `'\''` pattern, so names like `Bob's Living Room` survive intact. Only the flags actually passed are emitted.
   - Otherwise (file exists, no flags), leave it alone.
10. **Install YAML config (conditional).** If `/etc/sendspin/player.yaml` does not exist, `mkdir -p /etc/sendspin && curl -fSL <raw-dist-url>/config/player.example.yaml -o /etc/sendspin/player.yaml`. Otherwise leave it alone (preserves any `client_id` the daemon has already written back).
11. **Reload + enable + start.** `systemctl daemon-reload && systemctl enable --now sendspin-player`.
12. **Health check.** Sleep 2s; check `systemctl is-active sendspin-player`. If not active, print last 20 lines of `journalctl -u sendspin-player --no-pager` and exit non-zero.
13. **Success message.** Print resolved version, config file path, env file path, and `journalctl -u sendspin-player -f` for live logs.

## Error handling

- `set -euo pipefail` at the top of the script.
- Every download uses `curl -fSL` so a 404 (e.g. typo'd `--version`) exits cleanly with the curl error.
- `EXIT` trap removes the staging tempdir and, on non-zero status, prints a one-line "If install failed mid-way, re-run the script — it's idempotent" hint.
- The binary is `install -m 755`'d only after the tarball extracts cleanly. A failed download leaves `/usr/local/bin/sendspin-player` untouched.
- Pre-flight failures print the *specific* missing requirement, never a generic "system not supported".

## Testing

- **Local smoke**: run in `arm64v8/debian:bookworm` (without systemd — verify deps install, binary lands, files in place; `systemctl` steps will fail loudly which is expected without an init system).
- **Real Pi**: run the actual `curl | bash` on a fresh 64-bit Raspberry Pi OS install, confirm the player appears in mDNS and Music Assistant.
- **Idempotency**: run twice on the Pi, confirm second invocation completes cleanly and the service stays up.
- **Reconfigure**: run with `--name` after a vanilla install, confirm `/etc/default/sendspin-player` is rewritten and the service picks up the new name.
- **Arch rejection**: run on `armv7l` (real Pi 2 or `qemu-user-static`), confirm clean error message naming the supported set.
- **Uninstall**: `bash quickstart-pi.sh --uninstall`, confirm service stopped/disabled, binary and unit removed, config preserved.
- **CI**: add `shellcheck scripts/quickstart-pi.sh` to the existing GitHub Actions lint workflow. No new workflow.

## Open questions

None at design time. Flag any post-implementation surprises in the PR.
