#!/usr/bin/env bash
# ABOUTME: One-shot installer for sendspin-player on 64-bit Raspberry Pi OS.
# ABOUTME: Fetches the latest arm64 release tarball, installs systemd unit, starts daemon.
# shellcheck disable=SC2034 # Stub variables are set by functions and used in downstream phases.

set -euo pipefail

on_exit() {
    local rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        printf '\nIf install failed mid-way, re-running the script is safe — it is idempotent.\n' >&2
    fi
}
trap on_exit EXIT

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
stop_service() {
    if systemctl is-active --quiet "${BINARY_NAME}.service"; then
        log "Stopping running ${BINARY_NAME} service..."
        systemctl stop "${BINARY_NAME}.service"
    fi
}
install_binary() {
    local tarball_url tarball_name tmpdir
    tarball_name="${BINARY_NAME}-linux-arm64.tar.gz"
    tarball_url="${REPO_URL}/releases/download/${RESOLVED_TAG}/${tarball_name}"

    tmpdir="$(mktemp -d)"
    # shellcheck disable=SC2064
    trap "rm -rf '${tmpdir}'; on_exit" EXIT

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
install_unit() {
    local unit_url
    unit_url="${RAW_URL_BASE}/${RESOLVED_REF}/dist/systemd/${BINARY_NAME}.service"
    log "Installing systemd unit ${UNIT_PATH}..."
    curl -fSL "${unit_url}" -o "${UNIT_PATH}" \
        || die "Failed to download unit file from ${unit_url}"
    chmod 644 "${UNIT_PATH}"
}
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
