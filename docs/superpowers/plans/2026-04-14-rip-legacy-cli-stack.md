# Rip Legacy CLI Stack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete `cmd/ma-player`, `cmd/test-sync`, and the entire `internal/` legacy player pipeline they depend on. Unify on the `pkg/sendspin.Player` + `sendspin-player` code path as the only player in the tree.

**Architecture:** `cmd/ma-player` and `cmd/test-sync` both call `internal/app.Player`, which wires together `internal/client`, `internal/audio`, `internal/player` (scheduler), `internal/artwork`, and `internal/sync` into a parallel player pipeline that predates `pkg/sendspin`. Since PR #26 migrated `internal/app.Player`'s output layer to `pkg/audio/output.Malgo`, `ma-player` is functionally equivalent to `sendspin-player` — both negotiate the same supported-formats list, both route 24-bit hi-res through malgo, both connect to Music Assistant cleanly. Empirically verified: `sendspin-player.exe` connects to Music Assistant and plays 192kHz/24-bit successfully (log evidence in `sendspin-player.log` from v1.2.0 verification). This plan removes the duplicate pipeline and its supporting packages, then deletes the `pkg/sync.SetGlobalClockSync`/`ServerMicrosNow` deprecation shims that only existed to keep the legacy readers working.

**Tech Stack:** Go 1.24+, existing `pkg/sendspin`, `pkg/sync`, `pkg/audio`, `pkg/protocol`, `pkg/discovery`.

**Context:**
- Resolves issue #28 (sync deprecation migration).
- Partially addresses issue #35 (release workflow only builds `ma-player` + `sendspin-player` Linux binaries — after this, only `sendspin-player` remains to ship).
- Does NOT fix issue #27 (unknown binary type 8), #29 (memory stats), #30 (clock offset display), #31 (duplicate Resampler), #32 (hello log spam), #33 (resonate-artwork cache dir), #34 (FLAC stub) — those are separate and unrelated to this cleanup.
- The `docs/superpowers/plans/2026-04-12-layered-architecture.md` plan is scoped to `pkg/sendspin` and does not overlap with this work.

**Out of scope:**
- Any refactor of `pkg/sendspin.Player` itself.
- Deletion of `internal/server/` — that package is still live and imported by `cmd/sendspin-server` and `pkg/sendspin/server.go`.
- Deletion of `internal/discovery/` — still imported by `internal/server`, `main.go`, and `pkg/sendspin`.
- Deletion of `internal/version/` — still imported by `main.go`.
- Deletion of `internal/ui/` — still imported by `main.go` for the TUI.

---

## Dependency Audit Summary

Reverse-import scan before writing this plan confirmed the following (all grep paths relative to repo root):

| Package | Non-legacy importers (survive) | Legacy importers (die with `internal/app`) |
|---|---|---|
| `internal/app` | (none) | `cmd/ma-player/main.go`, `cmd/test-sync/main.go` |
| `internal/audio` | (none) | `internal/app/player.go`, `internal/player/scheduler.go` |
| `internal/client` | (none) | `internal/app/player.go` |
| `internal/player` | (none) | `internal/app/player.go` |
| `internal/artwork` | (none) | `internal/app/player.go` |
| `internal/protocol` | (none — already orphaned) | (none) |
| `internal/sync` | `main.go` (Quality enum only), `internal/ui/model.go`, `internal/ui/model_test.go` | `internal/app/player.go`, `internal/player/scheduler.go` |
| `internal/ui` | `main.go` | `internal/app/player.go` |
| `internal/version` | `main.go` | `internal/app/player.go` |
| `internal/discovery` | `internal/server/server.go`, `pkg/sendspin/{client_dialer,client_dialer_test,client_discovery_integration_test,server}.go`, `main.go` | `internal/app/player.go` |
| `internal/server` | `cmd/sendspin-server/main.go`, `pkg/sendspin/server.go` | (none) |

**Key observation:** `pkg/sync.Quality` is byte-for-byte equivalent to `internal/sync.Quality` (same type, same three constants in the same iota order). `main.go`'s current `pkg/sync.Quality` → `internal/sync.Quality` conversion block (lines 207–216) is pure ceremony. Migrating the three callers of `internal/sync.Quality` (`internal/ui/model.go`, `internal/ui/model_test.go`, `main.go`) to `pkg/sync.Quality` allows `internal/sync/` to be deleted after `internal/app` and `internal/player` go.

---

## File Structure

**Delete entirely:**

| Path | Lines | Notes |
|---|---|---|
| `cmd/ma-player/main.go` | ~90 | Duplicate of root `main.go` via legacy stack |
| `cmd/test-sync/main.go` | ~50 | Clock-sync debug harness, unused |
| `internal/app/player.go` | 567 | Legacy orchestrator |
| `internal/app/player_test.go` | 229 | |
| `internal/audio/decoder.go` | 125 | Duplicate of `pkg/audio/decode` |
| `internal/audio/decoder_test.go` | 64 | |
| `internal/audio/types.go` | 59 | |
| `internal/client/websocket.go` | 378 | Duplicate of `pkg/protocol.Client` |
| `internal/client/websocket_test.go` | 24 | |
| `internal/player/scheduler.go` | 238 | Duplicate of `pkg/sendspin.Scheduler` |
| `internal/player/scheduler_test.go` | 44 | |
| `internal/artwork/downloader.go` | 108 | Not used by `pkg/sendspin.Player` today |
| `internal/artwork/downloader_test.go` | 278 | |
| `internal/sync/clock.go` | 139 | Duplicate of `pkg/sync/clock.go` |
| `internal/sync/clock_test.go` | (varies) | |
| `internal/protocol/messages.go` | 130 | Already orphaned (no non-internal importers) |
| `internal/protocol/messages_test.go` | 79 | |

**Modify:**

| Path | Change |
|---|---|
| `internal/ui/model.go` | Replace `internal/sync` import with `pkg/sync`; type of `syncQuality` and `StatusMsg.SyncQuality` unchanged in name, changes in import source |
| `internal/ui/model_test.go` | Same replacement |
| `main.go` | Drop `internalsync "github.com/Sendspin/sendspin-go/internal/sync"` import; delete the Quality conversion block at ~lines 207–216; pass `stats.SyncQuality` directly to `ui.StatusMsg` |
| `pkg/sync/clock.go` | Delete package-level deprecated `SetGlobalClockSync`, `ServerMicrosNow`, and the `globalClockSync` / `globalDeprecationWarned` package variables |
| `pkg/sendspin/player.go` | Delete the `sync.SetGlobalClockSync(recv.ClockSync())` compatibility-shim call (currently at line 157 per PR #26 final state) |
| `.github/workflows/release.yml` | Remove `ma-player` from build matrix if present |
| `Makefile` | Remove any `ma-player` or `test-sync` targets if present |

Total deletion: ~2500–2600 lines across ~17 files, plus small modifications to 5 surviving files.

---

## Pre-flight: Branch setup

- [ ] **Step 0a: Start from clean main**

```bash
git checkout main
git pull --ff-only
git worktree add -b feat/rip-legacy-cli ../sendspin-go-worktrees/rip-legacy-cli origin/main
cd ../sendspin-go-worktrees/rip-legacy-cli
```

- [ ] **Step 0b: Verify baseline green before any changes**

```bash
export PATH="/c/msys64/mingw64/bin:$PATH"
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: all packages pass. If anything is red before you start, stop and investigate.

- [ ] **Step 0c: Commit the plan doc on the feature branch**

```bash
# If the plan file is on main, pull it in. If the plan lives elsewhere, cp or cherry-pick.
# Assuming the plan file is already at docs/superpowers/plans/2026-04-14-rip-legacy-cli-stack.md on main:
git log --oneline main -- docs/superpowers/plans/2026-04-14-rip-legacy-cli-stack.md
# If the file exists, it's already on the branch via the fresh origin/main base.
# If not, commit it explicitly:
# git add docs/superpowers/plans/2026-04-14-rip-legacy-cli-stack.md
# git commit -m "docs: add rip-legacy-cli implementation plan"
```

---

### Task 1: Migrate `internal/ui` to `pkg/sync.Quality`

`internal/ui/model.go` and its test file both import `internal/sync` for the `Quality` enum. Switch them to `pkg/sync`, which has the same type and the same constants (`QualityGood`, `QualityDegraded`, `QualityLost`). After this task, `internal/ui` no longer imports `internal/sync`.

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`

- [ ] **Step 1: Update the import in `internal/ui/model.go`**

Find the line:

```go
"github.com/Sendspin/sendspin-go/internal/sync"
```

Replace with:

```go
"github.com/Sendspin/sendspin-go/pkg/sync"
```

The package name used in the file is `sync` in both cases (no alias needed). All existing references like `sync.Quality`, `sync.QualityGood`, `sync.QualityDegraded`, `sync.QualityLost` continue to resolve correctly because `pkg/sync` has the same identifiers.

- [ ] **Step 2: Update the import in `internal/ui/model_test.go`**

Same replacement. If the test file uses `sync.Quality*` constants they all resolve against the new package identically.

- [ ] **Step 3: Verify `internal/ui` builds and tests pass**

```bash
go build ./internal/ui/
go test ./internal/ui/
```
Expected: clean build, all tests pass.

- [ ] **Step 4: Verify `main.go` still builds**

```bash
go build .
```
Expected: clean. `main.go` still converts `pkg/sync.Quality` → `internal/sync.Quality` in its own code at this point; that conversion keeps working because `internal/sync` is still alive.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "refactor(internal/ui): use pkg/sync.Quality instead of internal/sync.Quality

pkg/sync.Quality is byte-for-byte equivalent to internal/sync.Quality
(same type, same three constants in the same iota order). Switching
the TUI's only internal/sync consumer is step 1 of deleting the
legacy internal/ stack."
```

---

### Task 2: Drop the `internal/sync` conversion layer in `main.go`

`main.go` currently imports both `pkg/sync` (as `sync`) and `internal/sync` (as `internalsync`), and explicitly converts `pkg/sync.Quality` to `internal/sync.Quality` before constructing a `ui.StatusMsg`. After Task 1, `ui.StatusMsg.SyncQuality` is typed as `pkg/sync.Quality`, so the conversion is dead ceremony. Remove it and drop the `internal/sync` import.

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Remove the `internalsync` import**

Find in the import block:

```go
internalsync "github.com/Sendspin/sendspin-go/internal/sync"
```

Delete that line.

- [ ] **Step 2: Remove the Quality conversion block**

Find in `statsUpdateLoop` (currently around lines 207–216):

```go
		// Convert pkg/sync.Quality to internal/sync.Quality
		var syncQuality internalsync.Quality
		switch stats.SyncQuality {
		case 0: // QualityGood
			syncQuality = internalsync.QualityGood
		case 1: // QualityDegraded
			syncQuality = internalsync.QualityDegraded
		case 2: // QualityLost
			syncQuality = internalsync.QualityLost
		}
```

Delete the entire block.

Then find the `ui.StatusMsg` construction that uses the `syncQuality` local variable (a few lines below the deleted block):

```go
		updateTUI(ui.StatusMsg{
			Received:    stats.Received,
			Played:      stats.Played,
			Dropped:     stats.Dropped,
			BufferDepth: stats.BufferDepth,
			SyncRTT:     stats.SyncRTT,
			SyncQuality: syncQuality,
			Goroutines:  runtime.NumGoroutine(),
			MemAlloc:    0,
			MemSys:      0,
		})
```

Change the `SyncQuality: syncQuality,` line to:

```go
			SyncQuality: stats.SyncQuality,
```

(Passing `pkg/sync.Quality` directly, now that `ui.StatusMsg.SyncQuality` is typed as `pkg/sync.Quality` after Task 1.)

- [ ] **Step 3: Verify the root binary builds**

```bash
go build .
```
Expected: clean build, produces `sendspin-player` (or `sendspin-player.exe` on Windows). If you see `undefined: internalsync`, a reference was missed — grep `internalsync` in `main.go` and remove any remaining hits.

- [ ] **Step 4: Verify full build and tests**

```bash
go build ./...
go test ./... 2>&1 | tail -25
```
Expected: all packages still pass. `internal/sync` is still present and still imported by `internal/app/player.go` and `internal/player/scheduler.go` — those die in a later task.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "refactor(main): drop internal/sync conversion layer

ui.StatusMsg.SyncQuality is now pkg/sync.Quality (see prior commit),
so the pkg/sync -> internal/sync conversion block is pure ceremony.
Pass stats.SyncQuality through directly."
```

---

### Task 3: Delete `cmd/ma-player` and `cmd/test-sync`

Both CLIs import `internal/app`, which is the only non-legacy consumer of that package. Deleting them orphans `internal/app` for deletion in the next task.

**Files:**
- Delete: `cmd/ma-player/main.go`
- Delete: `cmd/test-sync/main.go`

- [ ] **Step 1: Confirm these are the only importers of `internal/app`**

```bash
grep -rln "\"github.com/Sendspin/sendspin-go/internal/app\"" --include='*.go' .
```
Expected output (exactly):
```
./cmd/ma-player/main.go
./cmd/test-sync/main.go
```
If there are other hits, STOP and report as BLOCKED — the scope was assumed wrong.

- [ ] **Step 2: Delete the CLI directories**

```bash
git rm -r cmd/ma-player cmd/test-sync
```

- [ ] **Step 3: Check the Makefile for references**

```bash
grep -n "ma-player\|test-sync" Makefile
```

If any targets or recipes name `ma-player` or `test-sync`, delete those lines. The existing `build`, `server`, `player`, `test`, `lint`, `clean`, `install` targets should be unaffected. Example: if you see

```makefile
ma-player:
	go build -o ma-player ./cmd/ma-player
```

delete the target and any references to it in the phony list or the top-level `all:`/`build:` dependency lists.

- [ ] **Step 4: Check the release workflow for references**

```bash
grep -n "ma-player\|test-sync" .github/workflows/release.yml
```

If any build steps name these binaries, delete them. This partially addresses issue #35 (release workflow gap) by removing the entries that will no longer exist.

- [ ] **Step 5: Verify the full module still builds**

```bash
go build ./...
```
Expected: clean. Nothing compiles against `cmd/ma-player` or `cmd/test-sync` (they were standalone `package main` binaries), so the rest of the tree builds fine.

- [ ] **Step 6: Verify `internal/app` now has no importers**

```bash
grep -rln "\"github.com/Sendspin/sendspin-go/internal/app\"" --include='*.go' .
```
Expected output: empty (no hits).

- [ ] **Step 7: Commit**

```bash
git add -A cmd/ Makefile .github/workflows/release.yml
git commit -m "refactor: delete cmd/ma-player and cmd/test-sync

ma-player duplicated sendspin-player's functionality via the legacy
internal/app stack; verified empirically that sendspin-player.exe
handles the Music Assistant case cleanly, including 192kHz/24-bit
PCM hi-res streaming (v1.2.0 verification logs). test-sync was a
one-off clock-sync debug harness with no remaining users. Removing
both is step 1 of deleting the duplicate internal/ pipeline."
```

---

### Task 4: Delete `internal/app`

After Task 3, no caller remains. `internal/app/player.go` and its test file both die. This orphans `internal/audio`, `internal/client`, `internal/player`, `internal/artwork`, `internal/version`, and `internal/discovery` of their legacy consumer — though some of those packages (version, discovery) still have non-legacy consumers and survive.

**Files:**
- Delete: `internal/app/player.go`
- Delete: `internal/app/player_test.go`

- [ ] **Step 1: Confirm `internal/app` still has zero importers**

```bash
grep -rln "\"github.com/Sendspin/sendspin-go/internal/app\"" --include='*.go' .
```
Expected output: empty.

- [ ] **Step 2: Delete the package**

```bash
git rm -r internal/app
```

- [ ] **Step 3: Verify build and test**

```bash
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: all surviving packages build and pass. `internal/audio`, `internal/client`, `internal/player`, `internal/artwork` will still build independently — their tests continue to pass within their own packages because they don't depend on `internal/app`. The one surprise that could happen is if any package depends on `internal/app` in a way the grep missed (e.g., via test helpers or a build-tagged file). If the build fails, STOP and report.

- [ ] **Step 4: Commit**

```bash
git add -A internal/app/
git commit -m "refactor(internal/app): delete legacy player orchestrator

Only callers were cmd/ma-player and cmd/test-sync, both deleted in
the previous commit. This removes ~800 lines of duplicate pipeline
wiring that predated pkg/sendspin."
```

---

### Task 5: Delete `internal/player`

`internal/player/scheduler.go` was only used by `internal/app`. After Task 4, it has no importers. This is the last non-test consumer of `internal/audio` and (along with the earlier `internal/ui`/`main.go` migration) of `internal/sync`.

**Files:**
- Delete: `internal/player/scheduler.go`
- Delete: `internal/player/scheduler_test.go`

- [ ] **Step 1: Confirm `internal/player` has zero importers**

```bash
grep -rln "\"github.com/Sendspin/sendspin-go/internal/player\"" --include='*.go' .
```
Expected output: empty.

- [ ] **Step 2: Delete the package**

```bash
git rm -r internal/player
```

- [ ] **Step 3: Verify build and test**

```bash
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add -A internal/player/
git commit -m "refactor(internal/player): delete legacy scheduler

The only caller, internal/app, was deleted in the previous commit.
pkg/sendspin has its own Scheduler that serves the modern player
pipeline."
```

---

### Task 6: Delete the remaining orphaned `internal/` packages

After Tasks 4 and 5, the following packages have no importers anywhere in the tree:

- `internal/audio/` (decoder.go, decoder_test.go, types.go)
- `internal/client/` (websocket.go, websocket_test.go)
- `internal/artwork/` (downloader.go, downloader_test.go)
- `internal/sync/` (clock.go, clock_test.go)
- `internal/protocol/` (messages.go, messages_test.go) — already orphaned before this PR; deleting it now finishes the sweep

**Files:**
- Delete: `internal/audio/` (directory)
- Delete: `internal/client/` (directory)
- Delete: `internal/artwork/` (directory)
- Delete: `internal/sync/` (directory)
- Delete: `internal/protocol/` (directory)

- [ ] **Step 1: Confirm every target package has zero importers**

```bash
for pkg in audio client artwork sync protocol; do
  echo "=== internal/$pkg ==="
  grep -rln "\"github.com/Sendspin/sendspin-go/internal/$pkg\"" --include='*.go' . || echo "(clean)"
done
```
Expected: all five should print `(clean)`. If any one still has importers, STOP and report — earlier tasks missed something.

- [ ] **Step 2: Delete the five packages**

```bash
git rm -r internal/audio internal/client internal/artwork internal/sync internal/protocol
```

- [ ] **Step 3: Verify build and test**

```bash
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: all surviving packages pass. The only remaining `internal/` packages should now be:

```bash
ls internal/
# expected: discovery  server  ui  version
```

- [ ] **Step 4: Commit**

```bash
git add -A internal/
git commit -m "refactor: delete orphaned internal/ packages

After removing internal/app and internal/player, the following
packages have no remaining importers in the tree:

  - internal/audio      (duplicate of pkg/audio/decode)
  - internal/client     (duplicate of pkg/protocol.Client)
  - internal/artwork    (unused by pkg/sendspin.Player)
  - internal/sync       (duplicate of pkg/sync)
  - internal/protocol   (already orphaned pre-PR)

Surviving internal/ packages: discovery, server, ui, version."
```

---

### Task 7: Delete the `pkg/sync` deprecation shims

With `internal/player/scheduler.go` and `internal/app/player.go` deleted, nothing in the tree reads from the package-level `sync.ServerMicrosNow()` or writes to the global via `sync.SetGlobalClockSync()`. The compatibility shim at `pkg/sendspin/player.go` (line ~157 per PR #26 final state) also becomes dead — it was only there so the `internal/` readers could see the `ClockSync` via the global.

This resolves issue #28 (sync deprecation migration) in full.

**Files:**
- Modify: `pkg/sync/clock.go`
- Modify: `pkg/sendspin/player.go`

- [ ] **Step 1: Confirm no caller of `sync.ServerMicrosNow()` or `sync.SetGlobalClockSync`**

```bash
grep -rn "sync\.ServerMicrosNow\|sync\.SetGlobalClockSync" --include='*.go' . | grep -v "_test.go" | grep -v "pkg/sync/clock.go"
```
Expected: no hits in production code. Test files may still reference the deprecated symbols if the `pkg/sync` package's own tests exercise them — those will need updating in Step 3 if so.

- [ ] **Step 2: Delete the deprecated package-level functions from `pkg/sync/clock.go`**

In `pkg/sync/clock.go`, find and delete these three sections:

```go
// Deprecated: ServerMicrosNow returns current time in server's reference frame (us).
// Use ClockSync.ServerMicrosNow() on the instance from Receiver.ClockSync() instead.
func ServerMicrosNow() int64 {
	cs := globalClockSync
	// ... (entire function body)
}
```

```go
var (
	globalClockSync         *ClockSync
	globalDeprecationWarned bool
)

// Deprecated: SetGlobalClockSync sets the global clock sync instance.
// Use Receiver.ClockSync() instead for new code.
func SetGlobalClockSync(cs *ClockSync) {
	if !globalDeprecationWarned {
		log.Printf("Warning: SetGlobalClockSync is deprecated, use Receiver.ClockSync() instead")
		globalDeprecationWarned = true
	}
	globalClockSync = cs
}
```

Delete both declarations (`var (...)` block and both functions) entirely. Do NOT touch the instance method `(cs *ClockSync) ServerMicrosNow()` — that one stays; it's the replacement API.

After the delete, check whether `pkg/sync/clock.go` still imports `"log"`. If `log.Printf` is no longer called anywhere in the file, `go build` will flag the unused import — remove it.

- [ ] **Step 3: Update `pkg/sync/clock_test.go` if it references the deleted symbols**

```bash
grep -n "ServerMicrosNow\b\|SetGlobalClockSync\|globalClockSync" pkg/sync/clock_test.go
```

If the test file references the package-level `ServerMicrosNow()` (no receiver) or `SetGlobalClockSync`, those tests need to migrate to the instance method `cs.ServerMicrosNow()` or be deleted. In particular, if there's a `TestServerMicrosNow` (package-level) AND a `TestClockSync_ServerMicrosNow` (instance method), keep the latter and delete the former.

If `pkg/sync/clock_test.go` has no references to the deleted symbols, skip this step.

- [ ] **Step 4: Delete the compat shim from `pkg/sendspin/player.go`**

In `pkg/sendspin/player.go`, find the line (currently around line 157, inside `Connect()`):

```go
	// Backward compat: set global clock sync
	sync.SetGlobalClockSync(recv.ClockSync())
```

Delete both lines (the comment and the call).

After the delete, check whether `pkg/sendspin/player.go` still uses the `"github.com/Sendspin/sendspin-go/pkg/sync"` import for anything. If `sync.` no longer appears in the file, remove the import. If it appears for other types (e.g., `sync.Quality`), leave the import alone.

- [ ] **Step 5: Verify build and test**

```bash
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: all packages pass. Particularly `pkg/sync`, `pkg/sendspin`, and `main.go` all still build cleanly.

- [ ] **Step 6: Grep to confirm the deprecation warning string is gone**

```bash
grep -rn "SetGlobalClockSync is deprecated" --include='*.go' .
```
Expected: no hits. The deprecation warning no longer exists because the function that emits it no longer exists.

- [ ] **Step 7: Commit**

```bash
git add pkg/sync/clock.go pkg/sync/clock_test.go pkg/sendspin/player.go
git commit -m "refactor(pkg/sync): delete deprecated global clock-sync shims

The only callers of sync.ServerMicrosNow() (package-level) and
sync.SetGlobalClockSync() lived in the internal/ legacy stack,
which was deleted in earlier commits. The instance method
(*ClockSync).ServerMicrosNow() is now the only way to read
server-frame time.

Resolves #28."
```

---

### Task 8: Final sweep and `go mod tidy`

Dropping the `internal/` packages may have orphaned module dependencies that were only pulled in by the legacy stack. Run `go mod tidy` to clean `go.mod` and `go.sum`.

**Files:**
- Modify: `go.mod`, `go.sum` (if anything changed)

- [ ] **Step 1: Run `go mod tidy`**

```bash
go mod tidy
```

- [ ] **Step 2: Review the diff**

```bash
git diff go.mod go.sum
```

If `go.mod` / `go.sum` are unchanged, skip steps 3 and 4. If they are changed, read the diff — you should only see REMOVALS (a dependency that `internal/client` or `internal/audio` was the only user of). Any ADDITION is suspicious; investigate before committing.

Candidate dependencies that may disappear:
- Anything the legacy `internal/client/websocket.go` used that `pkg/protocol/client.go` doesn't
- Anything `internal/audio/decoder.go` used that `pkg/audio/decode/` doesn't

- [ ] **Step 3: Verify final build and full test suite**

```bash
go build ./...
go test -count=1 ./... 2>&1 | tail -25
```
Expected: clean + all packages pass.

- [ ] **Step 4: Commit (skip if `go mod tidy` was a no-op)**

```bash
git add go.mod go.sum
git commit -m "chore(deps): go mod tidy after legacy stack removal"
```

- [ ] **Step 5: Final structural check**

```bash
ls internal/
# expected: discovery  server  ui  version

find cmd -type d
# expected: cmd  cmd/sendspin-server
```

- [ ] **Step 6: Sanity: grep for any string references to deleted binaries**

```bash
grep -rn "ma-player\|test-sync" --include='*.md' --include='*.yml' --include='*.sh' --include='Makefile' .
```

Review every hit. Historical notes in `CHANGELOG.md`, `docs/PHASE1_IMPLEMENTATION.md`, and frozen plan docs under `docs/superpowers/plans/` may legitimately mention them — leave those alone. Current-state references (instructions, scripts, CI configs) need cleanup. Commit any doc updates as a separate trailing commit if needed:

```bash
git add <any updated docs>
git commit -m "docs: remove stale references to deleted ma-player/test-sync"
```

---

### Task 9: Open the PR

- [ ] **Step 1: Push the feature branch**

```bash
git push -u origin feat/rip-legacy-cli
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --title "refactor: delete legacy internal/ CLI stack (ma-player, test-sync, internal/app and deps)" --body "$(cat <<'EOF'
## Summary

Delete \`cmd/ma-player\`, \`cmd/test-sync\`, and the entire \`internal/\` legacy player pipeline they depended on:

- \`cmd/ma-player/\`, \`cmd/test-sync/\`
- \`internal/app/\`, \`internal/audio/\`, \`internal/client/\`, \`internal/player/\`, \`internal/artwork/\`, \`internal/sync/\`, \`internal/protocol/\`

Surviving \`internal/\` packages: \`discovery\`, \`server\`, \`ui\`, \`version\`.

Also removes the \`pkg/sync.SetGlobalClockSync\` / \`ServerMicrosNow\` package-level deprecation shims (and the compat-shim call in \`pkg/sendspin.Player.Connect\`), now that no readers remain. Resolves #28.

## Why

\`ma-player\` duplicated \`sendspin-player\`'s functionality via the legacy \`internal/\` stack. After PR #26 migrated \`internal/app.Player\`'s output layer to \`pkg/audio/output.Malgo\`, the only difference between the two binaries was that one went through \`pkg/sendspin.Receiver\` and the other went through \`internal/app.Player\` → \`internal/client\` → \`internal/player.Scheduler\`. Both negotiate the same hi-res-first format list, both route 24-bit PCM through malgo, both connect to Music Assistant. Empirically verified: \`sendspin-player.exe\` was tested end-to-end against a real Music Assistant server at 192kHz/24-bit during the v1.2.0 verification pass — log evidence in \`sendspin-player.log\`.

\`test-sync\` was a one-off clock-sync debugging CLI from the earlier "make server sync work" era; nobody runs it today. It's in git history if anyone needs it back.

## Scope

- **In:** delete the two CLIs and the seven orphaned \`internal/\` packages; migrate \`internal/ui\` and \`main.go\` off \`internal/sync.Quality\` onto \`pkg/sync.Quality\`; delete the \`pkg/sync\` deprecation shims.
- **Out:** \`internal/server\` (still used by \`cmd/sendspin-server\` and \`pkg/sendspin/server.go\`), \`internal/discovery\`, \`internal/ui\`, \`internal/version\` — all still have non-legacy consumers.

## Stats

~2500–2600 lines deleted across ~17 files. No new functionality, no behavioural change to \`sendspin-player\` or \`sendspin-server\`.

## Resolves

- #28 — deprecated \`sync.SetGlobalClockSync\` / \`sync.ServerMicrosNow\` shims removed along with their last readers.
- Partially addresses #35 — release workflow now only needs to ship \`sendspin-player\` and \`sendspin-server\` (ma-player gone).

## Test plan

- [x] \`go build ./...\` clean
- [x] \`go test -count=1 ./...\` green on every surviving package
- [ ] **Manual playback verification** — run \`./sendspin-player.exe --server <addr>\` against a real server and confirm audio plays, volume/mute work, TUI shows expected state. The expected behavior is identical to v1.2.0 since \`sendspin-player\` was untouched by this PR.

## Rollback

\`git revert\` the PR merge commit. All deletions are in one linear chain; a revert restores the full legacy stack with no partial state.
EOF
)"
```

---

## Verification Checklist (before merging the PR)

- [ ] `go build ./...` clean on a fresh checkout
- [ ] `go test -count=1 ./...` green
- [ ] `ls internal/` shows exactly `discovery  server  ui  version`
- [ ] `ls cmd/` shows exactly `sendspin-server`
- [ ] `grep -rn "internal/app\|internal/client\|internal/player\|internal/audio\|internal/artwork\|internal/sync\|internal/protocol" --include='*.go' .` returns no hits
- [ ] `grep -rn "SetGlobalClockSync\|package-level ServerMicrosNow" --include='*.go' .` returns no hits
- [ ] `./sendspin-player` plays audio end-to-end against a real server (manual)
- [ ] `./sendspin-server` accepts a client and streams audio (manual)

## Rollback Plan

If anything turns out to depend on a deleted symbol and can't be trivially replaced:

1. `git revert` the PR merge commit. The full chain was designed as one revertable unit.
2. `go mod tidy` restores any dependencies that got pruned.
3. The legacy stack returns to life with no partial state.

The most likely sources of trouble, in order of probability:

1. An external consumer of the library that imports `internal/` (shouldn't happen — `internal/` packages are not importable from outside the module, by Go spec).
2. A build-tagged or generated file that references one of the deleted packages (extremely unlikely in this repo; grep confirmed no such files).
3. A script in `scripts/` or a CI workflow that invokes `ma-player` or `test-sync` binaries directly (Task 3 Step 4 sweeps for these).

None of these are expected to actually bite.
