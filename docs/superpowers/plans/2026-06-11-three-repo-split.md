# Plan: Split sendspin-go into three repos (SDK / player CLI / server CLI)

**Status:** In progress (Phase 0 → Phase 1)
**Owner:** Chris
**Created:** 2026-06-11

## Goal

Break the single `github.com/Sendspin/sendspin-go` module into three repos:

1. **SDK** — `github.com/Sendspin/sendspin-go` (keep the path) — the importable library: shared protocol/audio/sync/discovery core **plus** the high-level client (`Receiver`/`Player`) **and** server (`Server`/`Group`/roles) APIs, plus `examples/`.
2. **Player CLI** — `github.com/Sendspin/sendspin-player` — thin binary: root `main.go` + `internal/ui` (player TUI) + `internal/version`.
3. **Server CLI** — `github.com/Sendspin/sendspin-server` — thin binary: `cmd/sendspin-server` + the server TUI.

## Decision: one SDK with both client + server (not a client-only SDK)

The SDK keeps **both** halves of the library; only the CLI binaries are peeled into their own repos. Rationale comes from how the sibling implementations are organized (verified 2026-06-11):

| Impl | Module shape | SDK contains | CLI |
|------|--------------|--------------|-----|
| aiosendspin (Python) | one library package | client **+** server | separate repo `sendspin-cli` (depends on the package) |
| sendspin-rs (Rust) | one crate `sendspin` (not a workspace), library-first | client + controller + `listener` | examples only (no in-repo binary) |
| sendspin-cpp (C++) | one library (`include/sendspin` + `src`), ESP-IDF | client-only roles | examples only |
| sendspin-go (today) | one repo, library + 2 bundled binaries + full server | both | bundled |

Takeaways:
- **No sibling splits its library into separate client-lib vs server-lib modules.** Each is one library-first module.
- The only thing anyone peels into a separate repo is the **CLI** — the `aiosendspin` + `sendspin-cli` pattern, which is exactly the target here.
- `aiosendspin` is the direct analog (full client + server in one package) and keeps both together.

A client-only SDK would make sendspin-go the only impl that fragments its library and would force the conformance adapter (and `Server`/roles) into the server-CLI repo, inverting the dependency. Rejected.

One place sendspin-go goes finer than the ecosystem: **two** CLI repos (player + server) rather than a single `sendspin-cli`. Justified because Go has two genuinely distinct heavy binaries.

## Target dependency direction (must stay acyclic)

```
sendspin-player ─┐
                 ├─► sendspin-go (SDK)
sendspin-server ─┘        pkg/sendspin ─► {pkg/protocol, pkg/audio*, pkg/sync, pkg/discovery}
                          (protocol/audio/sync/discovery have no internal-module deps)
```

## Current-state analysis

### `pkg/sendspin` file classification (the package to decompose)

| File | Class | Notes |
|------|-------|-------|
| receiver.go, player.go, scheduler.go, client_id.go | **CLIENT** | core player engine, client_id resolution |
| server.go, group.go, group_role.go, role_player.go, role_metadata.go, role_controller.go, server_client.go, server_dispatch.go, server_stream.go, buffer_tracker.go, client_dialer.go | **SERVER** | `client_dialer.go` is mis-named — it is *server-initiated* dialing |
| config.go | **SHARED → split** | holds both `PlayerConfig*` and `ServerConfig*`; overlay machinery (`ApplyEnvAndFile`, `Write*Key`, `atomicWriteFile`) is shared |
| source.go | **SHARED contract / SERVER impls** | `AudioSource` interface = server-SDK contract; `TestToneSource` is a server source; `NewFileSource` is a dead stub |
| doc.go | doc | split/duplicate per resulting package |

Cross-cutting snags inside the package:
- **`ChunkDurationMs`** (defined in `server.go`) is read by the **client** `scheduler.go` — an interop-critical 20 ms invariant that must move to a shared home.
- `containsRole` (defined in `player.go`) is used by `receiver.go`.
- `Default{SampleRate,Channels,BitDepth}` (in `server.go`) are read by server files and `source.go`.

### The `internal/` relocation problem

Go forbids importing another module's `internal/`. After the split, anything under `internal/` reachable from a different repo must be **promoted to `pkg/`**:

| internal pkg | Cross-boundary consumer | Destination |
|--------------|------------------------|-------------|
| `internal/server` encoders (`OpusEncoder`, `FLACEncoder`, `New*Encoder`) | `pkg/sendspin` server | promote to `pkg/audio/encode` (currently orphaned — see below) |
| `internal/server` source decoders (`NewAudioSource`, `MP3Source`, `FLACSource`) | server CLI + `pkg/sendspin` | promote to a public `pkg/audio/source` |
| `internal/discovery` (`Manager`, `Config`, `ClientInfo`) | `pkg/sendspin` server + player CLI | promote to `pkg/discovery` (currently orphaned — see below) |
| `internal/ui` | player CLI only | stays `internal/` in the player repo |
| server TUI (`internal/server/tui*.go`) | server CLI only | stays `internal/` in the server repo |
| `internal/version` | both CLIs | trivial duplicate, or a tiny `pkg/version` |

### Dead / duplicated code to remove (forcing-function cleanup)

- **Legacy `internal/server.{Server, AudioEngine, Client, Config, New, NewAudioEngine, CreateAudioChunk}`** — a complete *second* server, **zero callers outside its own package** (verified). The live server is `pkg/sendspin.Server`. **Delete.**
- **`pkg/discovery`** — orphaned (imported by nobody), diverged from `internal/discovery`; `Browse()` has a `Timeout: 3` (3 **nanoseconds**) bug and no nil-`AddrV4` guard. **Delete**, then promote the maintained `internal/discovery` here.
- **`pkg/audio/encode`** — orphaned (imported by nobody), diverged from the real encoders (no `SetBitrate`, no FLAC, `[]int32` vs `[]int16`); README advertises it. **Delete or re-home** the real encoders into it.
- `internal/server` `ResampledSource` + `resampler.go` (a dead duplicate of `pkg/audio.Resampler`), `NewFileSource` stub, stale "Resonate" godoc in `pkg/{protocol,discovery}/doc.go`.

### Code-review bugs to fix along the way (not blockers for the split, but co-located)

High: reconnect leaks the dropped `Receiver` (no `Close()`); concurrent `WriteJSON` on the websocket (`sendJSON` uses `RLock`); codec-negotiation race (client streamed before async codec set); decoder leak on mid-session format change; `Player.state`/`receiver` data race; `convertToInt16` truncates instead of clamping.
Medium: `OnMetadata` invoked under `metadataMu`; `server/state`/`group/update` block 100 ms instead of `ctx.Done()`; droppable lifecycle events; goroutine leak if `net.Listen` fails; resampler tail dropped; `PlayerState.Volume/Muted` `omitempty`.

## Phased plan

Each phase is independently shippable and keeps `make test` + `make conformance` green. The principle: **do all decomposition inside the current single module first**, where the full test + conformance suites still validate everything, before extracting any module.

### Phase 0 — Decisions + dead-code removal (this PR)
- [x] Lock the SDK-scope decision (one SDK, client+server). *(this doc)*
- [x] **Delete the dead legacy `internal/server` server** (`server.go`, `audio_engine.go`, `tui_update.go`, `audio_engine_test.go`); relocate the two constants the surviving sources need.
- [x] Delete orphaned `pkg/discovery` (had a 3-ns browse-timeout bug) and `pkg/audio/encode`; drop their README references.
- [x] Remove dead `ResampledSource` + `internal/server/resampler.go`; fix the stale "Resonate" godoc in `pkg/protocol/doc.go`.
- [ ] Remove the `NewFileSource` stub — folded into Phase 1 (handled with the `source.go` split).

### Phase 1 — Decompose `pkg/sendspin` by file, in place ✅
- [x] Split `config.go` → `config_common.go` / `config_player.go` / `config_server.go`.
- [x] Split `source.go` (→ `source.go` interface + `source_testtone.go`); removed the dead `NewFileSource` stub.
- [x] Hoist `ChunkDurationMs`, `Default{SampleRate,Channels,BitDepth}`, `ProtocolVersion`, `AudioChunkMessageType`, `BufferAheadMs` to a neutral `constants.go`; moved `containsRole` next to its only user (`receiver.go`).
- Gate: `make test` green locally (12→ same after the file moves); `make conformance` runs in CI. **No public API change** — all symbols kept the same package + names, so the conformance adapter and external importers are unaffected.

### Phase 2 — Promote cross-boundary `internal/` to public `pkg/` (still one module)
- [ ] Promote `internal/discovery` `Manager`/types → `pkg/discovery`; repoint `server.go`/`client_dialer.go`.
- [ ] Promote encoders → `pkg/audio/encode`; repoint `server.go`/`server_client.go`/`role_player.go`. Verify both `make test` and `make BUILDTAGS= test` (the opus.Stream path).
- [ ] Promote source decoders → `pkg/audio/source`; leave server TUI in `internal/`.
- Gate (critical checkpoint): SDK has **zero cross-boundary `internal/` imports**; `make test` + `make conformance` green.

### Phase 3 — Extract the two CLI modules
- [ ] Create `sendspin-server` repo: `cmd/sendspin-server` + server TUI; `go.mod` requiring tagged SDK (`replace` only for local dev, CI-guarded).
- [ ] Create `sendspin-player` repo: root `main.go` + `internal/ui` + `internal/version`.
- [ ] SDK repo: drop `main.go` + `cmd/`; keep module path. Split the release pipeline; conformance stays on the SDK.

## Cross-cutting concerns

- **Module paths:** SDK keeps `github.com/Sendspin/sendspin-go` (zero churn for Music Assistant + the conformance adapter). CLIs get new paths and `require` a tagged SDK version. Add a CI guard that fails a CLI release if a `replace` pointing outside the module survives.
- **Conformance harness** (external `Sendspin/conformance` repo): its Go adapter imports `pkg/{protocol,sync,sendspin}` and calls `NewServerClientFromConn`, `CreateAudioChunk`, `ServerConn` — all SDK symbols. Keep the adapter pointed at the SDK repo. The **file-only** decomposition (Phase 1) preserves these import paths exactly, so the adapter doesn't break; a package-level split would require lockstep adapter updates or re-export shims (argument for file-only first).
- **`examples/`** (`basic-player`, `basic-server`, `custom-source`) import only `pkg/sendspin` → stay in the SDK repo (they document the library and compile in SDK CI).
- **Versioning:** three independent semver streams; SDK leads, CLIs pin a minimum SDK version.
- **opus build tags:** the promoted encoders link `gopkg.in/hraban/opus.v2`; every repo's Makefile/CI/goreleaser must keep `GOFLAGS=-tags=nolibopusfile` (plus the `BUILDTAGS=` override job in the SDK).

## Risks / open questions

1. Confirm nothing external (outside this repo + the conformance adapter) imports the soon-to-be-promoted/deleted packages before deleting.
2. `pkg/audio/encode` and `pkg/discovery` already exist but are orphaned/divergent — promoting *into* them means replacing their contents, not merging.
3. `ChunkDurationMs` straddles client/server and is interop-critical (50 chunks/s) — getting its shared home wrong silently breaks timing.
4. File-only vs package-level split of `pkg/sendspin`: recommend **file-only first** (zero import-path churn, conformance-safe); an optional `client`/`server` sub-package split can follow behind a major version.

## Progress log

- 2026-06-11: Plan written. Phase 0 complete (in one PR): removed the dead legacy `internal/server` server, the orphaned `pkg/discovery` and `pkg/audio/encode` packages, dead `ResampledSource`/`internal/server/resampler.go`, and fixed stale protocol godoc. `NewFileSource` stub deferred to Phase 1. Net ~1.4 KLOC of dead code removed.
- 2026-06-11: Phase 1 complete — decomposed `pkg/sendspin` by file (in place, same package, no public API change): hoisted shared constants to `constants.go`, split `config.go` into common/player/server, split `source.go` and removed the `NewFileSource` stub, moved `containsRole` to `receiver.go`. Sets up the eventual client/server package split as a mechanical move.
