# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
This guidance is aimed at Claude Code but may also be suitable for other AI tooling, such as GitHub Copilot and OpenAI Codex.

## Project Overview

`sendspin-go` is the Go implementation of the [Sendspin Protocol](https://github.com/Sendspin/website/blob/main/src/spec.md) for synchronized multi-room audio streaming. It ships as a library (`pkg/`) plus two CLI binaries: `sendspin-player` (root `main.go`) and `sendspin-server` (`./cmd/sendspin-server`). The Python sibling is [`aiosendspin`](https://github.com/Sendspin/aiosendspin); the two implementations are wire-compatible and share the role-family vocabulary (`player`, `controller`, `metadata`, `artwork`, `visualizer`).

**Note**: If uncertain about how something in Sendspin is supposed to work, fetch and refer to the [protocol specification](https://github.com/Sendspin/website/blob/main/src/spec.md) for authoritative implementation details.

## Commands

Native deps: `libopus`, `libopusfile`, `libflac`. Use `./install-deps.sh` (handles brew/apt/dnf/pacman) or the per-OS commands in README.md. `ffmpeg` is only required for HLS/m3u8 server input.

```bash
make                            # Build sendspin-player + sendspin-server
make player                     # Build sendspin-player only
make server                     # Build sendspin-server only
make test                       # go test ./...
make test-coverage              # -race + HTML coverage report
make lint                       # golangci-lint --timeout=5m
make conformance                # Run protocol conformance suite (clones ../conformance on first run; needs uv)
go test ./pkg/sendspin -run TestPlayer_Connect -race -v   # Single test
```

Pre-commit (`.pre-commit-config.yaml`) runs gofmt, goimports, go-mod-tidy, golangci-lint, and `go test -race -v ./...` on every commit. Run pre-commit before pushing any commit to ensure it is valid.

## Architecture

Library-first. The public API in `pkg/` is what consumers import; the CLI binaries are thin wrappers. `internal/` is private to the module by Go's rules — only `pkg/sendspin` legitimately reaches into it.

### Public layers (`pkg/`)

- **`pkg/sendspin`** (`receiver.go`, `player.go`, `server.go`): High-level API. The library splits the player into a `Receiver` (connect, handshake, clock sync, decode, schedule — emits `<-chan audio.Buffer`) and a `Player` (a thin wrapper composing `Receiver` + `pkg/audio/output.Output`). Use `Receiver` directly for visualizers, DSP, or custom output backends — no audio device required. Each `Receiver`/`Player` owns its own `*sync.ClockSync`; the package-level `SetGlobalClockSync` / `ServerMicrosNow` shims are deprecated.

- **`pkg/audio`** (`types.go`, `resample.go`, plus `decode/`, `encode/`, `output/` subpackages): Core `Format` and `Buffer` types, sample conversion, linear resampler, codec encoders/decoders (PCM, Opus, FLAC). `output` ships a single `malgo` backend; the `oto` backend was removed in v1.2.

- **`pkg/protocol`** (`messages.go`, `client.go`, `server_conn.go`): Wire messages plus `Client` (player side) and `ServerConn` (CGO-free helpers for serving binary frames).

- **`pkg/sync`** (`clock.go`, `timefilter.go`): `ClockSync` plus `TimeFilter`, the 2D Kalman filter tracking offset and drift per the Sendspin time-filter spec. This is what makes hi-res multi-room sync actually work; do not regress it without re-running `make conformance`.

- **`pkg/discovery`** (`mdns.go`): mDNS browse/advertise.

### Server-side group / role model (`pkg/sendspin`)

Two-level architecture, mirroring `aiosendspin` so the wire behavior matches across implementations.

- **`Group`** (`group.go`): Owns the typed event bus for one playback group. Publishes `ClientJoinedEvent`, `ClientLeftEvent`, `ClientStateChangedEvent`, `GroupStateChangedEvent`, etc. `ClientJoinedEvent` carries a live `*ServerClient` (the connection is fully alive at publish); `ClientLeftEvent` intentionally drops the pointer because the client is mid-teardown.

- **`GroupRole`** (`group_role.go`): One implementation per role family, coordinating across all member roles in the group. Built-in implementations: `ControllerGroupRole` (`role_controller.go`), `MetadataGroupRole` (`role_metadata.go`), `PlayerGroupRole` (`role_player.go`). Add new server-side behavior by writing a new `GroupRole` and registering it via `activateRoles`, **not** by editing `server_dispatch.go` directly.

- **`ServerClient`** (`server_client.go`): Per-connection state with typed accessors (`State()`, `Volume()`, `Muted()`, `Codec()`). Message dispatch lives in `server_dispatch.go`, audio streaming in `server_stream.go`, and per-client send-ahead pacing in `buffer_tracker.go`.

### Audio Pipeline

```
Server: AudioSource → codec negotiation per client (Opus/FLAC/PCM)
                   → 20 ms chunks tagged monotonic-µs server timestamps
                   → sent ~500 ms ahead → WebSocket binary frames
Player: protocol.Client → pkg/sync (Kalman-mapped local time)
                       → Scheduler priority queue (200 ms startup buffer)
                       → pkg/audio/output.Malgo
```

Binary message-type IDs encode role bits 7–2 / slot bits 1–0 per spec. Binary messages use a 9-byte header (1B message type + 8B `timestamp_us`); audio chunk = type 4. Artwork has its own slot via `protocol.StreamStart.Artwork`.

### Configuration & Daemon Mode

`pkg/sendspin/config.go` provides `PlayerConfigFile` / `ServerConfigFile` plus `LoadPlayerConfig` / `LoadServerConfig` and `ApplyEnvAndFile`. Precedence is **CLI > env (`SENDSPIN_PLAYER_*` / `SENDSPIN_SERVER_*`) > YAML file > built-in default**. Default search paths: `$SENDSPIN_*_CONFIG`, `~/.config/sendspin/*.yaml`, `/etc/sendspin/*.yaml`. Both binaries accept `--config` and `--daemon` (the latter logs to stdout for journalctl). Annotated examples and systemd units live in `dist/config/` and `dist/systemd/`; `make install-{player,server}-daemon` installs them.

### Internal layout (`internal/`)

- **`internal/server`**: Audio engine, source decoders, Opus/FLAC encoders, resampler, server TUI. `pkg/sendspin.Server` is a façade over this.
- **`internal/ui`**: bubbletea models, hotkey/device-picker widgets shared by both binaries.
- **`internal/discovery`**: mDNS plumbing (TXT parsing, browse loops) used by `pkg/discovery`.
- **`internal/version`**: ldflags version target.

The legacy `internal/{app,artwork,audio,client,player,protocol,sync}` packages were deleted in the v1.2 "rip-legacy-cli" sweep. Do not reintroduce that layering.

## Code Style

- Go ≥1.24. Module path is `github.com/Sendspin/sendspin-go`.
- Every `.go` file starts with two `// ABOUTME:` header lines summarizing its purpose. Match this on new files.
- Linting: golangci-lint with gosimple, govet, ineffassign, unused, gofmt, goimports, misspell, errcheck enabled; staticcheck intentionally disabled.
- Conventional commits: `type(scope): subject` (`feat`, `fix`, `refactor`, `test`, `chore`, `docs`, `style`, `build`, `ci`).

## Testing

- Tests are co-located (`_test.go` next to source).
- Integration tests use the `_integration_test.go` suffix (e.g. `flac_integration_test.go`, `client_discovery_integration_test.go`).
- Wire-format / message-type / negotiation changes must keep `make conformance` green. The harness lives in `Sendspin/conformance` and is symlinked into this checkout on first run.
- Audio invariants — 20 ms chunks (50/s), microsecond timestamps, 9-byte binary header — are interop-critical. Don't change without running the conformance suite against `aiosendspin`.

## Contribution & AI Policy

This project follows the [Open Home Foundation AI Policy](https://github.com/music-assistant/.github/blob/main/AI_POLICY.md):

- **No autonomous agents.** PRs from autonomous agents will be closed.
- **Human-in-the-loop required.** All contributions must be reviewed and understood by the contributor before submission.
- **Disclose AI-generated text.** Quote it with `>` blocks and accompany it with your own commentary explaining relevance and implications.

PRs target `main`. Recent design notes worth consulting before touching the relevant area: `docs/2026-04-12-layered-architecture-design.md`, `docs/CLOCK_SYNC_ANALYSIS.md`, `docs/FORMAT_NEGOTIATION_FIX.md`, `docs/superpowers/plans/`.
