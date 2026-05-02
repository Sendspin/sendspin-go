# Pi Quickstart Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `scripts/quickstart-pi.sh`, a self-contained `curl | sudo bash` installer that turns a fresh 64-bit Raspberry Pi OS Lite host into a running, mDNS-discoverable `sendspin-player` daemon using the prebuilt arm64 release tarball.

**Architecture:** A single Bash script under `set -euo pipefail` with phase-named functions, fetched verbatim from `raw.githubusercontent.com`. Reuses every artifact already in the repo: the arm64 release tarball, `dist/systemd/sendspin-player.service`, `dist/systemd/sendspin-player.env`, and `dist/config/player.example.yaml` — all pulled at the resolved release tag's ref. No code changes inside `pkg/` or `cmd/`. Per-task verification is `shellcheck`; final acceptance is a smoke run inside an `arm64v8/debian:bookworm` Docker container plus a manual Pi install.

**Tech Stack:** Bash 5.x, `curl`, `tar`, `apt-get`, `systemctl`, `shellcheck` (per-task verify), Docker with QEMU for arm64 emulation (final smoke test). No new runtime dependencies on the user's Pi beyond what `apt-get` installs.

**Spec:** [docs/superpowers/specs/2026-05-01-quickstart-pi-design.md](../specs/2026-05-01-quickstart-pi-design.md)

---

## File Structure

- **Create:** `scripts/quickstart-pi.sh` — the entire installer (single file, all phases as functions, `main()` at the bottom)
- **Modify:** `.github/workflows/ci.yml` — add a shellcheck step to the existing `lint` job
- **Modify:** `README.md` — add a "Quickstart on Raspberry Pi" section near the existing install instructions

The script lives in `scripts/` (a new directory) rather than the repo root because the root already hosts `install-deps.sh` (build-time deps, source-build users) — keeping the prebuilt-binary installer in `scripts/` makes the distinction explicit.

Per-task commits use the `feat(quickstart):` scope on functional additions, `chore(ci):` for the lint step, and `docs(readme):` for the README change.

**Prerequisite for the implementer:** install `shellcheck` locally (`brew install shellcheck` / `apt-get install shellcheck`). Every task ends with a `shellcheck` invocation.

---

## Task 1: Script skeleton with stubbed phases and `--help`

**Files:**
- Create: `scripts/quickstart-pi.sh`

- [ ] **Step 1: Create the directory and write the skeleton**

```bash
mkdir -p scripts
```

Write `scripts/quickstart-pi.sh`:

```bash
#!/usr/bin/env bash
# ABOUTME: One-shot installer for sendspin-player on 64-bit Raspberry Pi OS.
# ABOUTME: Fetches the latest arm64 release tarball, installs systemd unit, starts daemon.

set -euo pipefail

readonly REPO_OWNER="Sendspin"
readonly REPO_NAME="sendspin-go"
readonly REPO_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"
readonly RAW_URL_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}"
readonly BINARY_NAME="sendspin-player"
readonly INSTALL_PATH="/usr/local/bin/${BINARY_NAME}"
readonly UNIT_PATH="/etc/systemd/system/${BINARY_NAME}.service"
readonly ENV_PATH="/etc/default/${BINARY_NAME}"
readonly CONFIG_DIR="/etc/sendspin"
readonly CONFIG_PATH="${CONFIG_DIR}/player.yaml"

# Set by parse_args
ARG_NAME=""
ARG_DEVICE=""
ARG_VERSION=""
ARG_UNINSTALL=0

# Resolved by resolve_version
RESOLVED_TAG=""
RESOLVED_REF=""

usage() {
    cat <<EOF
Usage: quickstart-pi.sh [--name <s>] [--device <s>] [--version <tag>] [--uninstall]

Installs sendspin-player as a systemd daemon on 64-bit Raspberry Pi OS.

Options:
  --name <s>       Friendly player name (default: <hostname>-sendspin-player).
  --device <s>     Exact audio device name. Run 'sendspin-player --list-audio-devices' after
                   install to discover available names.
  --version <tag>  Pin to a specific release tag (e.g. v1.6.2). Default: latest.
  --uninstall      Stop the service and remove the binary and unit file. Config is preserved.
  -h, --help       Show this help.

Run with sudo:
  curl -fsSL ${RAW_URL_BASE}/main/scripts/quickstart-pi.sh | sudo bash
  curl -fsSL ${RAW_URL_BASE}/main/scripts/quickstart-pi.sh | sudo bash -s -- --name "Living Room"
EOF
}

log()  { printf '==> %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

parse_args()       { :; }
preflight()        { :; }
do_uninstall()     { :; }
install_apt_deps() { :; }
resolve_version()  { :; }
stop_service()     { :; }
install_binary()   { :; }
install_unit()     { :; }
install_env()      { :; }
install_config()   { :; }
start_and_verify() { :; }

main() {
    parse_args "$@"
    preflight
    if [[ "${ARG_UNINSTALL}" -eq 1 ]]; then
        do_uninstall
        exit 0
    fi
    install_apt_deps
    resolve_version
    stop_service
    install_binary
    install_unit
    install_env
    install_config
    start_and_verify
}

main "$@"
```

- [ ] **Step 2: Make executable and run shellcheck**

```bash
chmod +x scripts/quickstart-pi.sh
shellcheck scripts/quickstart-pi.sh
```

Expected: clean exit 0, no warnings.

- [ ] **Step 3: Verify `--help` works in isolation**

```bash
bash scripts/quickstart-pi.sh --help 2>&1 | head -5
```

Note: this will currently fail because `parse_args` is a no-op stub — it falls through to `preflight` which is also a no-op, then continues to `install_apt_deps` etc. That's expected; we're just verifying the syntax parses. Use `bash -n scripts/quickstart-pi.sh` to confirm syntax-only:

```bash
bash -n scripts/quickstart-pi.sh
```

Expected: clean exit 0.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): script skeleton with stubbed phases"
```

---

## Task 2: Argument parser and `--help` handling

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `parse_args` stub)

- [ ] **Step 1: Replace the `parse_args` stub**

Find:
```bash
parse_args()       { :; }
```

Replace with:
```bash
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --name)
                [[ $# -ge 2 ]] || die "--name requires a value"
                ARG_NAME="$2"
                shift 2
                ;;
            --device)
                [[ $# -ge 2 ]] || die "--device requires a value"
                ARG_DEVICE="$2"
                shift 2
                ;;
            --version)
                [[ $# -ge 2 ]] || die "--version requires a value (e.g. v1.6.2)"
                ARG_VERSION="$2"
                shift 2
                ;;
            --uninstall)
                ARG_UNINSTALL=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                printf 'Unknown argument: %s\n\n' "$1" >&2
                usage >&2
                exit 2
                ;;
        esac
    done
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Smoke test the parser by hand**

```bash
bash scripts/quickstart-pi.sh --help                         # prints usage, exits 0
bash scripts/quickstart-pi.sh --bogus 2>&1 | head -3         # prints "Unknown argument" + usage, exits 2
echo "exit=$?"
```

Expected: `--help` exits 0; `--bogus` exits 2.

(Don't run it with valid args yet — preflight is still a stub and would let the script fall through to apt-get without arch-checking. That's the next task.)

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): argument parser with --name/--device/--version/--uninstall"
```

---

## Task 3: Pre-flight checks (root, arch, debian, systemd)

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `preflight` stub)

- [ ] **Step 1: Replace the `preflight` stub**

Find:
```bash
preflight()        { :; }
```

Replace with:
```bash
preflight() {
    if [[ "${EUID}" -ne 0 ]]; then
        die "Root required. Re-run with sudo:
  curl -fsSL ${RAW_URL_BASE}/main/scripts/quickstart-pi.sh | sudo bash"
    fi

    local arch
    arch="$(uname -m)"
    if [[ "${arch}" != "aarch64" ]]; then
        die "Unsupported architecture: ${arch}. This script supports 64-bit
Raspberry Pi OS only (aarch64). For Pi 3 / 4 / 5 / Zero 2 W, install
the 64-bit Pi OS image: https://www.raspberrypi.com/software/operating-systems/
Pi 1 / Zero (v1) / Zero W are not supported (32-bit ARMv6 only)."
    fi

    if [[ ! -f /etc/debian_version ]]; then
        die "Unsupported OS. This script targets Debian-based distros (Pi OS,
Raspberry Pi OS Lite). For other distros see the README install steps:
  ${REPO_URL}#installation"
    fi

    if ! command -v systemctl >/dev/null 2>&1; then
        die "systemctl not found. The quickstart installs sendspin-player as a
systemd service; non-systemd hosts must follow the manual install steps."
    fi
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Verify behavior on the dev machine**

```bash
bash scripts/quickstart-pi.sh --help     # prints usage, exits 0 (preflight not yet reached)
bash scripts/quickstart-pi.sh 2>&1 | head -3
```

Expected: the second command fails with "Root required" if you're not root, or with "Unsupported architecture" if you're root on a non-aarch64 dev machine. Either of those is correct — both prove preflight runs.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): preflight checks for root, aarch64, debian, systemd"
```

---

## Task 4: Uninstall short-circuit

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `do_uninstall` stub)

- [ ] **Step 1: Replace the `do_uninstall` stub**

Find:
```bash
do_uninstall()     { :; }
```

Replace with:
```bash
do_uninstall() {
    log "Uninstalling sendspin-player..."

    if systemctl list-unit-files "${BINARY_NAME}.service" >/dev/null 2>&1; then
        systemctl disable --now "${BINARY_NAME}.service" 2>/dev/null || true
    fi

    rm -f "${INSTALL_PATH}"
    rm -f "${UNIT_PATH}"
    systemctl daemon-reload

    log "Uninstall complete."
    log "Config preserved at ${CONFIG_DIR}/ and ${ENV_PATH}."
    log "Remove manually for a full purge:"
    log "  sudo rm -rf ${CONFIG_DIR} ${ENV_PATH}"
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): --uninstall removes binary and unit, preserves config"
```

---

## Task 5: Apt runtime deps

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `install_apt_deps` stub)

- [ ] **Step 1: Replace the `install_apt_deps` stub**

Find:
```bash
install_apt_deps() { :; }
```

Replace with:
```bash
install_apt_deps() {
    log "Installing runtime dependencies..."
    DEBIAN_FRONTEND=noninteractive apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        libopus0 \
        libopusfile0 \
        libflac12 \
        libasound2 \
        ca-certificates \
        curl \
        tar
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): install runtime deps via apt-get"
```

---

## Task 6: Version resolution

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `resolve_version` stub)

- [ ] **Step 1: Replace the `resolve_version` stub**

Find:
```bash
resolve_version()  { :; }
```

Replace with:
```bash
resolve_version() {
    if [[ -n "${ARG_VERSION}" ]]; then
        RESOLVED_TAG="${ARG_VERSION}"
        RESOLVED_REF="${ARG_VERSION}"
        log "Installing pinned version: ${RESOLVED_TAG}"
    else
        # Use GitHub's latest-release redirect: no API call, no JSON parsing.
        # The redirect target tells us the resolved tag.
        local redirect_url
        redirect_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
            "${REPO_URL}/releases/latest")" \
            || die "Failed to resolve latest release tag from ${REPO_URL}/releases/latest"
        RESOLVED_TAG="${redirect_url##*/}"
        # The dist/ files at "main" are forward-compatible enough for the
        # latest-tagged release; pin them to the same tag for consistency.
        RESOLVED_REF="${RESOLVED_TAG}"
        log "Installing latest release: ${RESOLVED_TAG}"
    fi
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): resolve version via GitHub latest-release redirect"
```

---

## Task 7: Stop service if running

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `stop_service` stub)

- [ ] **Step 1: Replace the `stop_service` stub**

Find:
```bash
stop_service()     { :; }
```

Replace with:
```bash
stop_service() {
    if systemctl is-active --quiet "${BINARY_NAME}.service"; then
        log "Stopping running ${BINARY_NAME} service..."
        systemctl stop "${BINARY_NAME}.service"
    fi
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): stop running service before binary swap"
```

---

## Task 8: Download, extract, and install the binary

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `install_binary` stub)

- [ ] **Step 1: Replace the `install_binary` stub**

Find:
```bash
install_binary()   { :; }
```

Replace with:
```bash
install_binary() {
    local tarball_url tarball_name tmpdir
    tarball_name="${BINARY_NAME}-linux-arm64.tar.gz"
    tarball_url="${REPO_URL}/releases/download/${RESOLVED_TAG}/${tarball_name}"

    tmpdir="$(mktemp -d)"
    # shellcheck disable=SC2064
    trap "rm -rf '${tmpdir}'" EXIT

    log "Downloading ${tarball_url}..."
    curl -fSL "${tarball_url}" -o "${tmpdir}/${tarball_name}" \
        || die "Failed to download release tarball from ${tarball_url}"

    log "Extracting..."
    tar -xzf "${tmpdir}/${tarball_name}" -C "${tmpdir}"

    if [[ ! -f "${tmpdir}/${BINARY_NAME}" ]]; then
        die "Tarball did not contain expected binary '${BINARY_NAME}'"
    fi

    log "Installing ${INSTALL_PATH}..."
    install -m 755 "${tmpdir}/${BINARY_NAME}" "${INSTALL_PATH}"
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean (the `SC2064` disable is for the trap quoting, which is intentional — we want `${tmpdir}` expanded at trap-set time, not trap-fire time).

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): download release tarball into tempdir, install binary"
```

---

## Task 9: Install the systemd unit

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `install_unit` stub)

- [ ] **Step 1: Replace the `install_unit` stub**

Find:
```bash
install_unit()     { :; }
```

Replace with:
```bash
install_unit() {
    local unit_url
    unit_url="${RAW_URL_BASE}/${RESOLVED_REF}/dist/systemd/${BINARY_NAME}.service"
    log "Installing systemd unit ${UNIT_PATH}..."
    curl -fSL "${unit_url}" -o "${UNIT_PATH}" \
        || die "Failed to download unit file from ${unit_url}"
    chmod 644 "${UNIT_PATH}"
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): install systemd unit from tagged ref"
```

---

## Task 10: Install the env file (with flag handling)

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `install_env` stub)

- [ ] **Step 1: Replace the `install_env` stub**

Find:
```bash
install_env()      { :; }
```

Replace with:
```bash
# shell_quote: wrap a string in single quotes, escaping any embedded single
# quotes via the standard '\'' pattern. Safe for arbitrary user input.
shell_quote() {
    local s="$1"
    s="${s//\'/\'\\\'\'}"
    printf "'%s'" "${s}"
}

install_env() {
    if [[ -n "${ARG_NAME}" || -n "${ARG_DEVICE}" ]]; then
        log "Writing ${ENV_PATH} with --name/--device from flags..."
        local opts=""
        if [[ -n "${ARG_NAME}" ]]; then
            opts+="--name $(shell_quote "${ARG_NAME}") "
        fi
        if [[ -n "${ARG_DEVICE}" ]]; then
            opts+="--audio-device $(shell_quote "${ARG_DEVICE}") "
        fi
        # Trim trailing space.
        opts="${opts% }"
        cat >"${ENV_PATH}" <<EOF
# /etc/default/sendspin-player
# Written by quickstart-pi.sh. Edit freely; re-running quickstart with
# --name/--device will overwrite this file.
SENDSPIN_PLAYER_OPTS="${opts}"
EOF
        chmod 644 "${ENV_PATH}"
        return
    fi

    if [[ ! -f "${ENV_PATH}" ]]; then
        local env_url
        env_url="${RAW_URL_BASE}/${RESOLVED_REF}/dist/systemd/${BINARY_NAME}.env"
        log "Installing example env file ${ENV_PATH}..."
        curl -fSL "${env_url}" -o "${ENV_PATH}" \
            || die "Failed to download env file from ${env_url}"
        chmod 644 "${ENV_PATH}"
    fi
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Sanity-check the quoting logic on the dev machine**

```bash
bash -c '
    shell_quote() { local s="$1"; s="${s//\'\''/\'\''\\\'\''\'\''}"; printf "%s%s%s" "'\''" "${s}" "'\''"; }
    shell_quote "Bob'\''s Living Room"
'
```

Expected output: `'Bob'\''s Living Room'` — i.e. the single quote inside the name is escaped via the standard `'\''` pattern. (You can also test this by writing the function to a tiny standalone script and sourcing it.)

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): install env file with shell-safe --name/--device handling"
```

---

## Task 11: Install YAML config (only if missing)

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `install_config` stub)

- [ ] **Step 1: Replace the `install_config` stub**

Find:
```bash
install_config()   { :; }
```

Replace with:
```bash
install_config() {
    if [[ -f "${CONFIG_PATH}" ]]; then
        log "Preserving existing ${CONFIG_PATH}"
        return
    fi
    local config_url
    config_url="${RAW_URL_BASE}/${RESOLVED_REF}/dist/config/player.example.yaml"
    log "Installing example config ${CONFIG_PATH}..."
    install -d -m 755 "${CONFIG_DIR}"
    curl -fSL "${config_url}" -o "${CONFIG_PATH}" \
        || die "Failed to download config from ${config_url}"
    chmod 644 "${CONFIG_PATH}"
}
```

- [ ] **Step 2: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): install YAML config only if missing"
```

---

## Task 12: Reload, enable, start, health-check, and success message

**Files:**
- Modify: `scripts/quickstart-pi.sh` (replace `start_and_verify` stub)

- [ ] **Step 1: Replace the `start_and_verify` stub**

Find:
```bash
start_and_verify() { :; }
```

Replace with:
```bash
start_and_verify() {
    log "Reloading systemd and starting service..."
    systemctl daemon-reload
    systemctl enable --now "${BINARY_NAME}.service"

    sleep 2

    if ! systemctl is-active --quiet "${BINARY_NAME}.service"; then
        warn "Service failed to come up. Recent logs:"
        journalctl -u "${BINARY_NAME}.service" --no-pager -n 20 || true
        die "${BINARY_NAME} service is not active. See logs above."
    fi

    log ""
    log "sendspin-player ${RESOLVED_TAG} installed and running."
    log "  Binary:   ${INSTALL_PATH}"
    log "  Config:   ${CONFIG_PATH}"
    log "  Env:      ${ENV_PATH}"
    log "  Logs:     journalctl -u ${BINARY_NAME} -f"
    log "  Devices:  ${BINARY_NAME} --list-audio-devices"
}
```

- [ ] **Step 2: Replace the failure-hint trap**

Find:
```bash
set -euo pipefail
```

Add immediately after it:
```bash
on_exit() {
    local rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        printf '\nIf install failed mid-way, re-running the script is safe — it is idempotent.\n' >&2
    fi
}
trap on_exit EXIT
```

(Note: this `EXIT` trap coexists with the `install_binary` tempdir-cleanup trap. The tempdir trap is set inside that function and replaces this one only for the duration of `install_binary`. To avoid the conflict, change `install_binary`'s trap to be additive — see step 3.)

- [ ] **Step 3: Make `install_binary`'s trap additive**

In `install_binary`, replace:
```bash
    # shellcheck disable=SC2064
    trap "rm -rf '${tmpdir}'" EXIT
```

With:
```bash
    # shellcheck disable=SC2064
    trap "rm -rf '${tmpdir}'; on_exit" EXIT
```

This preserves both behaviors: tempdir cleanup AND the failure hint.

- [ ] **Step 4: Run shellcheck**

```bash
shellcheck scripts/quickstart-pi.sh
```

Expected: clean.

- [ ] **Step 5: Run a syntax check**

```bash
bash -n scripts/quickstart-pi.sh
```

Expected: clean exit 0.

- [ ] **Step 6: Commit**

```bash
git add scripts/quickstart-pi.sh
git commit -m "feat(quickstart): start/enable service, health-check, success message"
```

---

## Task 13: Add shellcheck to CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add shellcheck install + step to the lint job**

Open `.github/workflows/ci.yml`. Find the `lint` job's "Install dependencies" step:

```yaml
    - name: Install dependencies
      run: |
        sudo apt-get update
        sudo apt-get install -y libopus-dev libopusfile-dev libasound2-dev
```

Replace with:

```yaml
    - name: Install dependencies
      run: |
        sudo apt-get update
        sudo apt-get install -y libopus-dev libopusfile-dev libasound2-dev shellcheck
```

Then, immediately before the `- name: golangci-lint` step in the same job, add:

```yaml
    - name: Shellcheck
      run: shellcheck scripts/*.sh install-deps.sh
```

(`install-deps.sh` is included so we get coverage on the existing script too — since shellcheck is now available in CI, there's no reason to leave it unchecked.)

- [ ] **Step 2: Validate the YAML locally**

```bash
python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" \
    && echo "yaml ok"
```

Expected: `yaml ok`. (If `python` isn't available, use `yq . .github/workflows/ci.yml` or `cat .github/workflows/ci.yml` and eyeball indentation.)

- [ ] **Step 3: Run shellcheck locally on both scripts to confirm both pass**

```bash
shellcheck scripts/quickstart-pi.sh install-deps.sh
```

Expected: clean exit 0. (If `install-deps.sh` produces warnings that aren't introduced by this PR, fix them in a separate commit before merging this task — don't bundle pre-existing fixes into the quickstart change.)

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "chore(ci): run shellcheck on shell scripts in lint job"
```

---

## Task 14: README quickstart section

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a "Quickstart on Raspberry Pi" section**

Open `README.md`. Find the existing install instructions (search for the "Install the library" or build-from-source section near the top). Immediately *before* the build-from-source instructions, add:

```markdown
## Quickstart on Raspberry Pi

For a 64-bit Raspberry Pi OS (Lite is recommended; Bookworm or newer required):

```bash
curl -fsSL https://raw.githubusercontent.com/Sendspin/sendspin-go/main/scripts/quickstart-pi.sh | sudo bash
```

The script installs runtime dependencies, downloads the latest `sendspin-player-linux-arm64` release tarball, and registers the player as a systemd service. Add flags after `--` to pre-configure the player without editing files afterwards:

```bash
curl -fsSL https://raw.githubusercontent.com/Sendspin/sendspin-go/main/scripts/quickstart-pi.sh \
  | sudo bash -s -- --name "Living Room" --device "USB Audio Device"
```

Pin to a specific release with `--version v1.6.2`. Remove the player with `--uninstall` (config in `/etc/sendspin/` is preserved). Supported on Pi 3 / 4 / 5 / Zero 2 W; not supported on 32-bit-only hardware (Pi 1 / Zero v1 / Zero W).

After install:

- View live logs: `journalctl -u sendspin-player -f`
- Discover device names: `sendspin-player --list-audio-devices`
- Edit config: `sudo nano /etc/sendspin/player.yaml`

```

(Adjust the surrounding heading levels if the README uses `###` for that depth — match what's already there.)

- [ ] **Step 2: Eyeball the rendered output**

```bash
git diff README.md | head -40
```

Confirm the markdown reads cleanly and the surrounding headings still flow.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): add Pi quickstart section"
```

---

## Task 15: Smoke test in arm64 Docker

**Files:** none (verification-only)

This task verifies the script works end-to-end without needing a physical Pi. It uses Docker's QEMU-based arm64 emulation to simulate the target environment. The smoke test is intentionally not a CI check — it's a developer-machine acceptance step before merge. Systemd flows can't be exercised in a default Docker container, so we cover everything *up to* the systemd-reload step and stop there.

- [ ] **Step 1: Ensure Docker buildx + qemu-static are available**

```bash
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
```

(One-time setup on the dev machine. Idempotent.)

- [ ] **Step 2: Run an interactive arm64 Bookworm container**

```bash
docker run --rm -it --platform linux/arm64 \
    -v "$(pwd)/scripts/quickstart-pi.sh:/tmp/quickstart-pi.sh:ro" \
    arm64v8/debian:bookworm bash
```

- [ ] **Step 3: Inside the container, install systemctl shim and run the script**

In the container:
```bash
apt-get update -qq && apt-get install -y -qq curl ca-certificates
# Provide a no-op systemctl so the script's preflight passes; we'll skip
# the actual systemd interaction in the next step.
cat >/usr/local/bin/systemctl <<'EOF'
#!/bin/bash
echo "systemctl-shim: $*" >&2
case "$1" in
    is-active) exit 1 ;;            # pretend service is not active
    list-unit-files) exit 0 ;;
    *) exit 0 ;;
esac
EOF
chmod +x /usr/local/bin/systemctl
bash /tmp/quickstart-pi.sh --version v1.6.2 --name "Smoke Test"
```

Expected: the script downloads the v1.6.2 tarball, installs the binary at `/usr/local/bin/sendspin-player`, writes the unit file, writes `/etc/default/sendspin-player` with `SENDSPIN_PLAYER_OPTS="--name 'Smoke Test'"`, and writes `/etc/sendspin/player.yaml`. Final "service failed to come up" warning is *expected* — the systemctl shim returns non-zero on `is-active`, which surfaces the health-check failure path. That confirms the health check works.

- [ ] **Step 4: Inside the container, sanity-check the installed artifacts**

```bash
ls -la /usr/local/bin/sendspin-player /etc/systemd/system/sendspin-player.service \
       /etc/default/sendspin-player /etc/sendspin/player.yaml
cat /etc/default/sendspin-player
/usr/local/bin/sendspin-player --version
```

Expected: all four files present; env file shows `--name 'Smoke Test'`; binary prints v1.6.2.

- [ ] **Step 5: Run idempotency check**

In the same container:
```bash
bash /tmp/quickstart-pi.sh --version v1.6.2
```

Expected: clean run, "Preserving existing /etc/sendspin/player.yaml" message, env file *not* rewritten (still shows the prior `Smoke Test` name).

- [ ] **Step 6: Run uninstall**

```bash
bash /tmp/quickstart-pi.sh --uninstall
ls /usr/local/bin/sendspin-player /etc/systemd/system/sendspin-player.service 2>&1
ls /etc/sendspin/player.yaml /etc/default/sendspin-player
```

Expected: binary and unit gone; config dir and env file preserved.

- [ ] **Step 7: Final pass — real Pi**

On a fresh 64-bit Pi OS Lite install:

```bash
curl -fsSL https://raw.githubusercontent.com/Sendspin/sendspin-go/main/scripts/quickstart-pi.sh | sudo bash
```

Verify: the player appears in mDNS / Music Assistant within ~10 seconds, audio plays cleanly, `journalctl -u sendspin-player -f` shows the expected "connected to server" / chunk-decode logs.

- [ ] **Step 8: No commit needed for this task** — it's verification-only. If anything failed in steps 3–7, file fixes as additional commits and re-run from step 3.
