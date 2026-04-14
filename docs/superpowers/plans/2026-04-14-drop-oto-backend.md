# Drop Oto Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `github.com/ebitengine/oto/v3` dependency entirely by replacing both oto-using audio output implementations with `pkg/audio/output.Malgo`, leaving a single 24-bit-capable audio backend.

**Architecture:** Delete `pkg/audio/output/oto.go` (library backend used by `pkg/sendspin.Player`) and `internal/player/output.go` (CLI backend used by `internal/app.Player`). Simplify the `pkg/sendspin.Player` backend selector to always use malgo. Migrate `internal/app.Player` to construct an `output.Malgo` directly via the `pkg/audio/output.Output` interface, and track volume/mute state inside the app layer. Run `go mod tidy` to drop the oto dependency.

**Tech Stack:** Go 1.24+, `github.com/gen2brain/malgo` (miniaudio via CGo).

**Context:**
- This supersedes PR #25 (`feat/oto-float32-output`), which proposed switching oto to `FormatFloat32LE` as a defensive fix for issue #3. That PR is obsolete because the real fix is deletion, not format negotiation.
- The layered-architecture plan at `docs/superpowers/plans/2026-04-12-layered-architecture.md` is scoped to `pkg/sendspin` and does NOT touch `internal/app` or `internal/player` — so this plan does not conflict with it.
- After this plan: issue #3 closes (malgo handles 24-bit natively via `FormatS24`), PR #25 closes without merging, and the tree has exactly one audio backend.

**Out of scope:**
- Deleting other `internal/player/*` files (e.g. `scheduler.go`) — that belongs to the layered-architecture refactor, not this one.
- Touching `internal/client`, `internal/audio`, `internal/sync` — the legacy CLI pipeline stays functional with its internal packages; only the audio output layer swaps.
- Removing `malgo`'s `write16Bit` branch — it stays as defensive code for 16-bit sources.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `pkg/audio/output/volume.go` | CREATE | Shared `applyVolume` / `getVolumeMultiplier` (moved out of `oto.go` before `oto.go` is deleted) |
| `pkg/audio/output/volume_test.go` | CREATE | Tests for the moved helpers (ported from `internal/player/output_test.go`) |
| `pkg/audio/output/oto.go` | DELETE | Oto backend and its exported `NewOto` constructor |
| `pkg/audio/output/output_test.go` | MODIFY | Drop `TestOtoImplementsOutput` and `TestNewOto` |
| `pkg/audio/output/doc.go` | MODIFY | Update usage example if it references `NewOto` |
| `pkg/sendspin/player.go` | MODIFY | Collapse `onStreamStart` backend selector to always use `NewMalgo` |
| `internal/app/player.go` | MODIFY | Replace `*internal/player.Output` with `pkg/audio/output.Output`; add `volume`/`muted` fields; adapt call sites |
| `internal/player/output.go` | DELETE | The duplicate oto-based output used by legacy CLIs |
| `internal/player/output_test.go` | DELETE | Covered by the new `pkg/audio/output/volume_test.go` |
| `go.mod` / `go.sum` | MODIFY | `go mod tidy` removes `github.com/ebitengine/oto/v3` |

---

## Pre-flight: Branch setup

- [ ] **Step 0a: Start from clean main**

```bash
git checkout main
git pull --ff-only
git checkout -b feat/drop-oto
```

- [ ] **Step 0b: Verify baseline green before any changes**

```bash
export PATH="/c/msys64/mingw64/bin:$PATH"
go build ./...
go test ./... 2>&1 | tail -20
```
Expected: all packages pass. If anything is red before you start, stop and investigate — do not conflate an unrelated failure with this work.

---

### Task 1: Extract shared volume helpers into `pkg/audio/output/volume.go`

Both `oto.go` and `malgo.go` (in the same package) use package-level `applyVolume` and `getVolumeMultiplier`. They currently live in `oto.go`. We need to move them out before deleting `oto.go`, otherwise `malgo.go` loses its symbol.

**Files:**
- Create: `pkg/audio/output/volume.go`
- Create: `pkg/audio/output/volume_test.go`
- Modify: `pkg/audio/output/oto.go` (temporary — helpers deleted from here)

- [ ] **Step 1: Create `volume.go` with the helpers**

Write `pkg/audio/output/volume.go`:

```go
// ABOUTME: Volume and mute helpers shared by audio output backends
// ABOUTME: Extracted from oto.go during the oto removal cleanup
package output

import "github.com/Sendspin/sendspin-go/pkg/audio"

// applyVolume applies volume and mute to samples with clipping protection.
// Samples are expected to be in the int32 24-bit range.
func applyVolume(samples []int32, volume int, muted bool) []int32 {
	multiplier := getVolumeMultiplier(volume, muted)

	result := make([]int32, len(samples))
	for i, sample := range samples {
		scaled := int64(float64(sample) * multiplier)

		if scaled > audio.Max24Bit {
			scaled = audio.Max24Bit
		} else if scaled < audio.Min24Bit {
			scaled = audio.Min24Bit
		}

		result[i] = int32(scaled)
	}

	return result
}

// getVolumeMultiplier returns the float multiplier for a given volume/mute state.
func getVolumeMultiplier(volume int, muted bool) float64 {
	if muted {
		return 0.0
	}
	return float64(volume) / 100.0
}
```

- [ ] **Step 2: Delete the helpers from `oto.go`**

Open `pkg/audio/output/oto.go` and remove the `applyVolume` and `getVolumeMultiplier` functions (currently around lines 172-202). Leave the rest of the file alone — it will be deleted entirely in Task 4.

Also remove the now-unused `github.com/Sendspin/sendspin-go/pkg/audio` import from `oto.go` only if it was the last user of that package within `oto.go` — it isn't, because `SampleToInt16` is still called in `Write`. Leave the import alone.

- [ ] **Step 3: Create `volume_test.go` with ported tests**

Write `pkg/audio/output/volume_test.go`:

```go
// ABOUTME: Tests for shared volume/mute helpers
package output

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestVolumeMultiplier(t *testing.T) {
	tests := []struct {
		volume   int
		muted    bool
		expected float64
	}{
		{100, false, 1.0},
		{50, false, 0.5},
		{0, false, 0.0},
		{80, true, 0.0}, // muted overrides volume
	}

	for _, tt := range tests {
		result := getVolumeMultiplier(tt.volume, tt.muted)
		if result != tt.expected {
			t.Errorf("volume=%d muted=%v: expected %f, got %f",
				tt.volume, tt.muted, tt.expected, result)
		}
	}
}

func TestApplyVolume_HalfScale(t *testing.T) {
	samples := []int32{1000 << 8, -1000 << 8, 500 << 8, -500 << 8}

	result := applyVolume(samples, 50, false)

	if result[0] != int32(500<<8) {
		t.Errorf("sample 0: expected %d, got %d", 500<<8, result[0])
	}
	if result[1] != int32(-500<<8) {
		t.Errorf("sample 1: expected %d, got %d", -500<<8, result[1])
	}
}

func TestApplyVolume_Muted(t *testing.T) {
	samples := []int32{audio.Max24Bit, audio.Min24Bit, 1 << 20}

	result := applyVolume(samples, 100, true)

	for i, got := range result {
		if got != 0 {
			t.Errorf("sample %d: expected 0 when muted, got %d", i, got)
		}
	}
}

func TestApplyVolume_Clamps24Bit(t *testing.T) {
	// Volume > 100 is not expected from callers, but the clamping must still
	// prevent overflow past the 24-bit range.
	samples := []int32{audio.Max24Bit, audio.Min24Bit}

	result := applyVolume(samples, 100, false)

	if result[0] != audio.Max24Bit {
		t.Errorf("max sample: expected %d, got %d", audio.Max24Bit, result[0])
	}
	if result[1] != audio.Min24Bit {
		t.Errorf("min sample: expected %d, got %d", audio.Min24Bit, result[1])
	}
}
```

- [ ] **Step 4: Verify tests compile and pass**

```bash
go test ./pkg/audio/output/ -run 'TestVolumeMultiplier|TestApplyVolume' -v
```
Expected: all four test functions PASS.

- [ ] **Step 5: Verify the full output package still builds and passes**

```bash
go test ./pkg/audio/output/
```
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add pkg/audio/output/volume.go pkg/audio/output/volume_test.go pkg/audio/output/oto.go
git commit -m "refactor(audio/output): move volume helpers out of oto.go

Preparatory step for removing the oto backend. Extracts applyVolume
and getVolumeMultiplier into a shared volume.go file so malgo.go
retains access to them after oto.go is deleted. Ports the volume
tests from internal/player/output_test.go (which will be deleted
in a later step) to the new package location."
```

---

### Task 2: Simplify `pkg/sendspin.Player` backend selector

Remove the `BitDepth <= 16 ? oto : malgo` switch in `onStreamStart` — always use malgo. Also update the `PlayerConfig.Output` doc comment that mentions auto-selection.

**Files:**
- Modify: `pkg/sendspin/player.go` (lines 43-44 doc comment; lines 167-176 selector)

- [ ] **Step 1: Update `PlayerConfig.Output` doc comment**

In `pkg/sendspin/player.go`, find:

```go
// Output overrides the default audio output backend.
// When nil, auto-selects oto (16-bit) or malgo (24-bit) based on stream format.
Output output.Output
```

Replace with:

```go
// Output overrides the default audio output backend.
// When nil, a malgo-backed output is created on stream start.
Output output.Output
```

- [ ] **Step 2: Simplify `onStreamStart`**

Find the block at roughly lines 167-176:

```go
func (p *Player) onStreamStart(format audio.Format) {
	if p.output == nil {
		if format.BitDepth <= 16 {
			p.output = output.NewOto()
			log.Printf("Using oto backend for %d-bit audio", format.BitDepth)
		} else {
			p.output = output.NewMalgo()
			log.Printf("Using malgo backend for %d-bit audio", format.BitDepth)
		}
	}
```

Replace with:

```go
func (p *Player) onStreamStart(format audio.Format) {
	if p.output == nil {
		p.output = output.NewMalgo()
	}
```

- [ ] **Step 3: Verify `pkg/sendspin` still builds**

```bash
go build ./pkg/sendspin/...
go test ./pkg/sendspin/ 2>&1 | tail -20
```
Expected: clean build, all tests pass. Any test that mocked the `Output` interface continues to work because it passes `PlayerConfig.Output` directly; the selector branch is unreachable by tests.

- [ ] **Step 4: Commit**

```bash
git add pkg/sendspin/player.go
git commit -m "refactor(sendspin): always use malgo backend in Player

Collapses the bit-depth-based oto/malgo selector to a single malgo
construction. malgo handles 16/24/32-bit natively and is the only
backend that will remain after oto is removed."
```

---

### Task 3: Delete `pkg/audio/output/oto.go` and update references

**Files:**
- Delete: `pkg/audio/output/oto.go`
- Modify: `pkg/audio/output/output_test.go` (drop `TestOtoImplementsOutput`, `TestNewOto`)
- Modify: `pkg/audio/output/doc.go` (update usage example if it references `NewOto`)

- [ ] **Step 1: Delete `oto.go`**

```bash
git rm pkg/audio/output/oto.go
```

- [ ] **Step 2: Update `output_test.go`**

Current contents should leave only the Malgo assertion. Replace the file body with:

```go
// ABOUTME: Audio output interface tests
// ABOUTME: Verifies Output interface implementation
package output

import "testing"

func TestMalgoImplementsOutput(t *testing.T) {
	var _ Output = (*Malgo)(nil)
}
```

- [ ] **Step 3: Check `doc.go` for oto references**

```bash
grep -n -i "oto\|NewOto" pkg/audio/output/doc.go
```
If any line mentions `NewOto` or the oto library, edit the example to use `NewMalgo` instead. Leave unrelated content alone.

- [ ] **Step 4: Verify package builds**

```bash
go build ./pkg/audio/output/
go test ./pkg/audio/output/
```
Expected: no references to `Oto` remaining, tests pass.

- [ ] **Step 5: Verify dependent packages still build**

```bash
go build ./pkg/sendspin/...
```
Expected: clean build. If `pkg/sendspin/player.go` still imports or calls `output.NewOto`, Task 2 was not applied correctly — go back and fix.

- [ ] **Step 6: Commit**

```bash
git add -A pkg/audio/output/
git commit -m "refactor(audio/output): delete oto backend

malgo is now the only audio output backend in pkg/audio/output.
The oto backend had one fixed int16 output format and could not
be reinitialized; malgo supports 16/24/32-bit natively and handles
format changes cleanly."
```

---

### Task 4: Migrate `internal/app.Player` off `internal/player.Output`

Replace the `*player.Output` field with a `pkg/audio/output.Output` interface value (satisfied by `*output.Malgo`). Track volume and mute state on the `Player` struct itself because the `output.Output` interface does not expose `GetVolume`/`IsMuted`. Adapt the `Initialize(format)` / `Play(buf)` call sites to the interface's `Open(sr,ch,bd)` / `Write([]int32)` shape.

**Files:**
- Modify: `internal/app/player.go`

- [ ] **Step 1: Add the new import and swap the struct field**

In `internal/app/player.go`, add the import (alongside the existing `github.com/Sendspin/sendspin-go/internal/player`):

```go
"github.com/Sendspin/sendspin-go/pkg/audio/output"
```

Change the `Player` struct field:

```go
output          *player.Output
```
to:

```go
output          output.Output
volume          int
muted           bool
```

- [ ] **Step 2: Update the `New()` constructor**

Find:

```go
return &Player{
    config:      config,
    clockSync:   clockSync,
    output:      player.NewOutput(),
    artwork:     artworkDL,
    ctx:         ctx,
    cancel:      cancel,
    playerState: "idle", // Start in idle state
}
```

Change to:

```go
return &Player{
    config:      config,
    clockSync:   clockSync,
    output:      output.NewMalgo(),
    volume:      100,
    artwork:     artworkDL,
    ctx:         ctx,
    cancel:      cancel,
    playerState: "idle", // Start in idle state
}
```

- [ ] **Step 3: Update `handleStreamStart` to call `Open(sampleRate, channels, bitDepth)`**

Find the block inside `handleStreamStart` that currently reads:

```go
format := audio.Format{
    Codec:      start.Player.Codec,
    SampleRate: start.Player.SampleRate,
    Channels:   start.Player.Channels,
    BitDepth:   start.Player.BitDepth,
}

// Initialize decoder
decoder, err := audio.NewDecoder(format)
if err != nil {
    log.Printf("Failed to create decoder: %v", err)
    continue
}
p.decoder = decoder

// Initialize output
if err := p.output.Initialize(format); err != nil {
    log.Printf("Failed to initialize output: %v", err)
    continue
}
```

Replace the `p.output.Initialize(format)` call with:

```go
if err := p.output.Open(start.Player.SampleRate, start.Player.Channels, start.Player.BitDepth); err != nil {
    log.Printf("Failed to initialize output: %v", err)
    continue
}

// Apply any pre-stream volume/mute state to the fresh device
p.output.SetVolume(p.volume)
p.output.SetMuted(p.muted)
```

Leave the `format := audio.Format{...}` declaration alone — `audio.NewDecoder(format)` above still uses it. Only the `p.output.Initialize(format)` call is being replaced.

- [ ] **Step 4: Update `handleScheduledAudio` to call `Write([]int32)`**

Find:

```go
func (p *Player) handleScheduledAudio(ctx context.Context) {
    for {
        select {
        case buf := <-p.scheduler.Output():
            if err := p.output.Play(buf); err != nil {
                log.Printf("Playback error: %v", err)
            }
```

Replace the playback call:

```go
func (p *Player) handleScheduledAudio(ctx context.Context) {
    for {
        select {
        case buf := <-p.scheduler.Output():
            if err := p.output.Write(buf.Samples); err != nil {
                log.Printf("Playback error: %v", err)
            }
```

- [ ] **Step 5: Update `handleControls` to read volume/mute from `p`, not `p.output`**

Find:

```go
case "volume":
    p.output.SetVolume(cmd.Volume)
    p.client.SendState(protocol.ClientState{
        State:  p.playerState,
        Volume: cmd.Volume,
        Muted:  p.output.IsMuted(),
    })

case "mute":
    p.output.SetMuted(cmd.Mute)
    p.client.SendState(protocol.ClientState{
        State:  p.playerState,
        Volume: p.output.GetVolume(),
        Muted:  cmd.Mute,
    })
```

Replace with:

```go
case "volume":
    p.volume = cmd.Volume
    p.output.SetVolume(cmd.Volume)
    p.client.SendState(protocol.ClientState{
        State:  p.playerState,
        Volume: p.volume,
        Muted:  p.muted,
    })

case "mute":
    p.muted = cmd.Mute
    p.output.SetMuted(cmd.Mute)
    p.client.SendState(protocol.ClientState{
        State:  p.playerState,
        Volume: p.volume,
        Muted:  p.muted,
    })
```

- [ ] **Step 6: Update `handleVolumeControl`**

Find:

```go
// Apply to output
if p.output != nil {
    p.output.SetVolume(vol.Volume)
    p.output.SetMuted(vol.Muted)
}
```

Replace with:

```go
if p.output != nil {
    p.volume = vol.Volume
    p.muted = vol.Muted
    p.output.SetVolume(vol.Volume)
    p.output.SetMuted(vol.Muted)
}
```

- [ ] **Step 7: Update `Stop()` — `output.Close()` now returns an error**

Find:

```go
if p.output != nil {
    p.output.Close()
}
```

Replace with:

```go
if p.output != nil {
    if err := p.output.Close(); err != nil {
        log.Printf("Warning: output close error: %v", err)
    }
}
```

- [ ] **Step 8: Build `internal/app` and the two CLIs that depend on it**

```bash
go build ./internal/app/
go build ./cmd/ma-player/
go build ./cmd/test-sync/
```
Expected: all three clean. If any step fails, read the error — most likely a missed `p.output.IsMuted()` / `p.output.GetVolume()` call site or a stray `player.Output` reference. Grep to find any remaining:

```bash
grep -n "player\.Output\|player\.NewOutput\|output\.GetVolume\|output\.IsMuted" internal/app/player.go
```
Should return no hits.

- [ ] **Step 9: Run the full test suite**

```bash
go test ./... 2>&1 | tail -25
```
Expected: all packages pass.

- [ ] **Step 10: Commit**

```bash
git add internal/app/player.go
git commit -m "refactor(internal/app): use pkg/audio/output.Malgo directly

Replaces the internal/player.Output field with the public
output.Output interface, constructed as output.NewMalgo(). Volume
and mute are now tracked on Player itself since the interface does
not expose getters. This removes the last caller of
internal/player.Output and clears the way for deleting that file."
```

---

### Task 5: Delete `internal/player/output.go` and its test

At this point nothing references the legacy output. The scheduler files in the same package stay — they are still used by `internal/app.Player`.

**Files:**
- Delete: `internal/player/output.go`
- Delete: `internal/player/output_test.go`

- [ ] **Step 1: Confirm no remaining callers**

```bash
grep -rn "player\.Output\|player\.NewOutput" internal/ cmd/
```
Expected: no hits. If anything matches, resolve it before deleting.

- [ ] **Step 2: Delete the files**

```bash
git rm internal/player/output.go internal/player/output_test.go
```

- [ ] **Step 3: Verify `internal/player` still builds (scheduler remains)**

```bash
go build ./internal/player/
go test ./internal/player/
```
Expected: clean. The `scheduler.go` and `scheduler_test.go` files are unaffected.

- [ ] **Step 4: Full build and test**

```bash
go build ./...
go test ./... 2>&1 | tail -25
```
Expected: all packages pass.

- [ ] **Step 5: Commit**

```bash
git add -A internal/player/
git commit -m "refactor(internal/player): delete legacy oto-based Output

The only caller, internal/app.Player, was migrated to
pkg/audio/output.Malgo in the previous commit. The scheduler in
the same package remains and is still used."
```

---

### Task 6: Drop the oto dependency from `go.mod`

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Run `go mod tidy`**

```bash
go mod tidy
```

- [ ] **Step 2: Verify oto is gone from `go.mod`**

```bash
grep -n oto go.mod
```
Expected: no match. If `go.mod` still shows the `github.com/ebitengine/oto/v3` line, something is still importing it — grep the tree:

```bash
grep -rn "ebitengine/oto" --include='*.go'
```
Resolve any hits before proceeding. Do NOT hand-edit `go.mod` to force the dependency out.

- [ ] **Step 3: Verify build and test still green**

```bash
go build ./...
go test ./... 2>&1 | tail -25
```
Expected: all packages pass. A dependency removal that breaks a build means the package was still being pulled in transitively — investigate.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): drop github.com/ebitengine/oto/v3

No longer imported after both oto-using audio output
implementations were replaced with pkg/audio/output.Malgo."
```

---

### Task 7: Sweep docs, Makefile, install-deps for stale oto references

**Files (check each; modify only if stale):**
- `README.md`
- `CHANGELOG.md`
- `CLAUDE.md`
- `install-deps.sh`
- `Makefile`
- `docs/hires-audio-verification.md`
- `docs/MA_HIRES_ISSUE.md`
- `docs/FORMAT_NEGOTIATION_FIX.md`
- `examples/README.md`
- `pkg/audio/output/doc.go`

- [ ] **Step 1: Grep for any remaining oto mentions**

```bash
grep -rn -i "\boto\b\|ebitengine/oto\|NewOto" \
  --include='*.md' --include='*.sh' --include='Makefile' \
  --include='*.go'
```

- [ ] **Step 2: For each hit, decide**

- **Stale instructions / setup steps / API examples** → update to malgo.
- **Historical notes / CHANGELOG entries / resolved-issue docs** → leave alone. They accurately describe history.
- **`CLAUDE.md` project guidance** → update if it mentions oto as part of the current architecture.

Edit in place with `Edit` or manual editor. Do not rewrite history in docs files.

- [ ] **Step 3: Commit (skip if nothing changed)**

```bash
git add -A
git diff --cached --stat
git commit -m "docs: remove stale oto references after backend deletion"
```

---

### Task 8: Close loose ends — PR #25, issue #3, and open the new PR

- [ ] **Step 1: Push the feature branch**

```bash
git push -u origin feat/drop-oto
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --title "refactor(audio): drop oto backend, standardize on malgo" --body "$(cat <<'EOF'
## Summary
- Deletes both oto-using audio output implementations (`pkg/audio/output/oto.go` and `internal/player/output.go`).
- Migrates `internal/app.Player` to construct `pkg/audio/output.Malgo` directly via the `Output` interface, tracking volume/mute on the app struct.
- Collapses `pkg/sendspin.Player`'s backend selector to always use malgo.
- Removes the `github.com/ebitengine/oto/v3` dependency from `go.mod`.
- Closes #3 (true 24-bit output — malgo's `FormatS24` path handles this natively).

## Why
`pkg/audio/output/malgo.go` already supports 16/24/32-bit output via miniaudio and was used for hi-res sources. oto was a lossy fallback path that existed only because malgo landed later and nobody removed the original backend. Having two audio stacks meant two volume/mute code paths, two sets of tests, and a bit-depth-based selector that obscured which backend was actually in use.

## Scope
- In: both oto implementations, the `pkg/sendspin` selector, the `internal/app` migration, the dep removal, doc sweep.
- Out: `internal/player/scheduler.go` (still used by `internal/app`), `internal/client` / `internal/sync` (unchanged), the `malgo.write16Bit` branch (kept as defensive fallback).

## Supersedes
- PR #25 (`feat/oto-float32-output`) — that PR proposed switching oto to float32 as a defensive fix. Obsolete: we're deleting oto instead.

## Test plan
- [x] `go build ./...` clean
- [x] `go test ./...` green
- [x] New `pkg/audio/output/volume_test.go` covers the ported helpers
- [ ] **Manual playback verification required**: needs the main player binary and at least one legacy CLI (`ma-player` or `test-sync`) to be run against a real server on each platform that matters. I cannot run interactive audio from the dev environment.
EOF
)"
```

- [ ] **Step 3: Close PR #25**

```bash
gh pr close 25 --comment "Superseded by the 'drop oto' work. The float32 fix was defensive against a backend we are now deleting entirely — see the newer PR for context."
```

- [ ] **Step 4: Close issue #3**

```bash
gh issue close 3 --repo Sendspin/sendspin-go --comment "$(cat <<'EOF'
Closing — resolved by removing the oto backend entirely.

Hi-res sources were already being routed to \`pkg/audio/output/malgo.go\`, which uses miniaudio's native \`FormatS24\` / \`FormatS32\` paths (see \`malgo.go:148-154\` and the \`write24Bit\` / \`write32Bit\` callbacks). The only lossy code in the tree was the oto backend, which was used as a fallback for \`BitDepth <= 16\` sources (where there was nothing to lose). There was also a duplicate oto-based output in \`internal/player/output.go\` used by \`cmd/ma-player\` and \`cmd/test-sync\`.

The newer PR deletes both oto implementations, makes malgo the only backend, and drops the \`github.com/ebitengine/oto/v3\` dependency. The 24-bit path is now the only path.

Note: as with any of the originally proposed solutions, whether 24-bit samples actually reach the DAC still depends on the OS audio stack (WASAPI/CoreAudio/ALSA mixer configuration). That portion is out of our control.
EOF
)"
```

- [ ] **Step 5: Delete the stale `feat/oto-float32-output` branch**

```bash
git push origin --delete feat/oto-float32-output
git branch -D feat/oto-float32-output 2>/dev/null || true
```

---

## Verification Checklist (before merging the PR)

- [ ] `go build ./...` clean on a fresh checkout
- [ ] `go test ./...` green
- [ ] `grep -rn "ebitengine/oto\|NewOto\|FormatSignedInt16LE" --include='*.go'` returns nothing
- [ ] `go.mod` no longer lists `github.com/ebitengine/oto/v3`
- [ ] Main player (`main.go`) plays a hi-res source end-to-end (manual)
- [ ] At least one legacy CLI (`ma-player` or `test-sync`) connects and plays (manual)

## Rollback Plan

If malgo turns out to fail on a platform we care about and we need oto back:

1. `git revert` the PR from this plan (a single revert commit undoes everything including the dep removal).
2. `go mod tidy` reintroduces the oto dependency from the restored imports.
3. The `internal/app.Player` migration reverts alongside the deletions, so `cmd/ma-player` and `cmd/test-sync` return to their previous audio path.

The whole plan is designed as one merge-unit revertable in one step.
