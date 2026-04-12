# Layered Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `pkg/sendspin.Player` into a composable `Receiver` (connect/sync/decode/schedule) and a thin `Player` wrapper (output/volume/callback), enabling library consumers to get decoded audio without playback.

**Architecture:** Extract all connection, sync, decode, and scheduling goroutines from `player.go` into a new `receiver.go`. The `Receiver` type emits decoded `audio.Buffer` via a channel. `Player` becomes a thin wrapper that composes `Receiver` + `output.Output` + optional `ProcessCallback`. No breaking changes to the existing API.

**Tech Stack:** Go 1.24+, existing `pkg/protocol`, `pkg/sync`, `pkg/audio/decode`, `pkg/audio/output` packages.

**Spec:** `docs/2026-04-12-layered-architecture-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `pkg/sendspin/receiver.go` | CREATE | Receiver type, ReceiverConfig, all connection/decode/sync/schedule goroutines |
| `pkg/sendspin/receiver_test.go` | CREATE | Receiver unit tests |
| `pkg/sendspin/player.go` | MODIFY | Slim down to compose Receiver + Output + ProcessCallback |
| `pkg/sendspin/player_test.go` | CREATE | Player integration tests with mock output |
| `pkg/sync/clock.go` | MODIFY | Add deprecation warnings to global functions |
| `pkg/sendspin/scheduler.go` | MODIFY | Replace `sync.ServerMicrosNow()` with injected ClockSync method |

---

### Task 1: Add ServerMicrosNow Method to ClockSync

The scheduler currently calls the global `sync.ServerMicrosNow()`. Before moving code into Receiver, we need a non-global equivalent on the `ClockSync` instance so the Receiver's scheduler can use it without the global.

**Files:**
- Modify: `pkg/sync/clock.go`
- Test: `pkg/sync/clock_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/sync/clock_test.go`:

```go
func TestClockSync_ServerMicrosNow(t *testing.T) {
	cs := NewClockSync()

	// Before sync, should return roughly current Unix micros
	now1 := cs.ServerMicrosNow()
	unixNow := time.Now().UnixMicro()
	if abs64(now1-unixNow) > 1000000 { // within 1 second
		t.Errorf("before sync: expected ~%d, got %d", unixNow, now1)
	}

	// After sync, should return server-frame time
	cs.ProcessSyncResponse(1000, 500000, 500100, 1200)
	now2 := cs.ServerMicrosNow()
	if now2 == 0 {
		t.Error("after sync: got zero")
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/sync/ -run TestClockSync_ServerMicrosNow -v`
Expected: FAIL — `cs.ServerMicrosNow undefined`

- [ ] **Step 3: Write the implementation**

Add to `pkg/sync/clock.go` after the existing `ServerToLocalTime` method:

```go
// ServerMicrosNow returns current time in server's reference frame (us).
// This is the instance method equivalent of the deprecated package-level ServerMicrosNow().
func (cs *ClockSync) ServerMicrosNow() int64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.filter.Synced() {
		return time.Now().UnixMicro()
	}

	return cs.filter.ComputeServerTime(time.Now().UnixMicro())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/sync/ -run TestClockSync_ServerMicrosNow -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/sync/clock.go pkg/sync/clock_test.go
git commit -m "add ServerMicrosNow instance method to ClockSync"
```

---

### Task 2: Update Scheduler to Use Injected ClockSync

Replace the global `sync.ServerMicrosNow()` call in the scheduler with the injected `clockSync` instance method from Task 1.

**Files:**
- Modify: `pkg/sendspin/scheduler.go`

- [ ] **Step 1: Replace global call with instance method**

In `pkg/sendspin/scheduler.go`, line 74, change:

```go
serverNow := sync.ServerMicrosNow()
```

to:

```go
serverNow := s.clockSync.ServerMicrosNow()
```

- [ ] **Step 2: Run existing tests**

Run: `go test ./pkg/sendspin/ -v`
Expected: PASS (existing tests still work)

- [ ] **Step 3: Commit**

```bash
git add pkg/sendspin/scheduler.go
git commit -m "use injected ClockSync instance in scheduler instead of global"
```

---

### Task 3: Add Deprecation Warnings to Global Sync Functions

Mark the package-level `SetGlobalClockSync` and `ServerMicrosNow` as deprecated. They still work but log a warning on first use.

**Files:**
- Modify: `pkg/sync/clock.go`

- [ ] **Step 1: Add deprecation warnings**

In `pkg/sync/clock.go`, replace the existing global functions with:

```go
var (
	globalClockSync          *ClockSync
	globalDeprecationWarned  bool
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

// Deprecated: ServerMicrosNow returns current time in server's reference frame (us).
// Use ClockSync.ServerMicrosNow() on the instance from Receiver.ClockSync() instead.
func ServerMicrosNow() int64 {
	cs := globalClockSync
	if cs == nil {
		return time.Now().UnixMicro()
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.filter.Synced() {
		return time.Now().UnixMicro()
	}

	return cs.filter.ComputeServerTime(time.Now().UnixMicro())
}
```

- [ ] **Step 2: Run existing tests**

Run: `go test ./pkg/sync/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/sync/clock.go
git commit -m "deprecate global SetGlobalClockSync and ServerMicrosNow functions"
```

---

### Task 4: Create Receiver Type and Config

Create the `Receiver` type with its config struct and constructor. No goroutines yet — just the type, constructor, and stub methods.

**Files:**
- Create: `pkg/sendspin/receiver.go`
- Create: `pkg/sendspin/receiver_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/sendspin/receiver_test.go`:

```go
// ABOUTME: Tests for the Receiver type
// ABOUTME: Verifies Receiver creation, config defaults, and lifecycle
package sendspin

import (
	"testing"
)

func TestNewReceiver_Defaults(t *testing.T) {
	recv, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Receiver",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recv == nil {
		t.Fatal("expected non-nil receiver")
	}
	if recv.clockSync == nil {
		t.Error("expected clockSync to be initialized")
	}
	if recv.Output() == nil {
		t.Error("expected output channel to be non-nil")
	}
}

func TestNewReceiver_RequiresServerAddr(t *testing.T) {
	_, err := NewReceiver(ReceiverConfig{
		PlayerName: "Test",
	})
	if err == nil {
		t.Fatal("expected error for missing ServerAddr")
	}
}

func TestReceiver_ClockSyncIsOwnInstance(t *testing.T) {
	recv1, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Receiver 1",
	})
	recv2, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8928",
		PlayerName: "Receiver 2",
	})

	if recv1.ClockSync() == recv2.ClockSync() {
		t.Error("expected each receiver to have its own ClockSync instance")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/sendspin/ -run TestNewReceiver -v`
Expected: FAIL — `NewReceiver` undefined

- [ ] **Step 3: Write the Receiver type and constructor**

Create `pkg/sendspin/receiver.go`:

```go
// ABOUTME: Receiver handles connection, sync, decode, and scheduling
// ABOUTME: Emits decoded audio.Buffer via Output() channel for consumers
package sendspin

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
	"github.com/google/uuid"
)

// ReceiverConfig configures a Receiver
type ReceiverConfig struct {
	// ServerAddr is the server address (host:port)
	ServerAddr string

	// PlayerName is the display name for this client
	PlayerName string

	// BufferMs is the playback buffer size in milliseconds (default: 500)
	BufferMs int

	// DeviceInfo provides device identification
	DeviceInfo DeviceInfo

	// DecoderFactory overrides the default decoder selection.
	// When nil, the default codec switch (PCM, Opus, FLAC) is used.
	DecoderFactory func(audio.Format) (decode.Decoder, error)

	// OnMetadata is called when metadata is received
	OnMetadata func(Metadata)

	// OnStreamStart is called when a stream starts with its format
	OnStreamStart func(audio.Format)

	// OnStreamEnd is called when the stream ends
	OnStreamEnd func()

	// OnError is called when errors occur
	OnError func(error)
}

// ReceiverStats contains receiver pipeline statistics
type ReceiverStats struct {
	Received    int64
	Played      int64
	Dropped     int64
	BufferDepth int
	SyncRTT     int64
	SyncQuality sync.Quality
}

// Receiver handles connection, clock sync, decoding, and scheduling.
// It emits decoded, time-stamped audio buffers via the Output() channel.
type Receiver struct {
	config ReceiverConfig

	// Components
	client    *protocol.Client
	clockSync *sync.ClockSync
	scheduler *Scheduler
	decoder   decode.Decoder
	format    audio.Format

	// Output channel for decoded buffers
	output chan audio.Buffer

	// Lifecycle
	ctx             context.Context
	cancel          context.CancelFunc
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc
	serverAddr      string
	connected       bool
}

// NewReceiver creates a new Receiver with the given configuration
func NewReceiver(config ReceiverConfig) (*Receiver, error) {
	if config.ServerAddr == "" {
		return nil, fmt.Errorf("ServerAddr is required")
	}

	if config.BufferMs == 0 {
		config.BufferMs = 500
	}
	if config.DeviceInfo.ProductName == "" {
		config.DeviceInfo.ProductName = "Sendspin Player"
	}
	if config.DeviceInfo.Manufacturer == "" {
		config.DeviceInfo.Manufacturer = "Sendspin"
	}
	if config.DeviceInfo.SoftwareVersion == "" {
		config.DeviceInfo.SoftwareVersion = "1.0.0"
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Receiver{
		config:     config,
		clockSync:  sync.NewClockSync(),
		output:     make(chan audio.Buffer, 10),
		ctx:        ctx,
		cancel:     cancel,
		serverAddr: config.ServerAddr,
	}, nil
}

// Output returns the channel of decoded, scheduled audio buffers.
// The channel is closed when Close() is called.
func (r *Receiver) Output() <-chan audio.Buffer {
	return r.output
}

// ClockSync returns the clock synchronization instance for this Receiver.
func (r *Receiver) ClockSync() *sync.ClockSync {
	return r.clockSync
}

// Stats returns pipeline statistics
func (r *Receiver) Stats() ReceiverStats {
	stats := ReceiverStats{}

	if r.scheduler != nil {
		s := r.scheduler.Stats()
		stats.Received = s.Received
		stats.Played = s.Played
		stats.Dropped = s.Dropped
		stats.BufferDepth = r.scheduler.BufferDepth()
	}

	if r.clockSync != nil {
		rtt, quality := r.clockSync.GetStats()
		stats.SyncRTT = rtt
		stats.SyncQuality = quality
	}

	return stats
}

// Close tears down the Receiver and closes the Output channel.
func (r *Receiver) Close() error {
	r.cancel()

	if r.client != nil {
		r.client.SendGoodbye("shutdown")
		r.client.Close()
	}

	if r.schedulerCancel != nil {
		r.schedulerCancel()
	}
	if r.scheduler != nil {
		r.scheduler.Stop()
	}

	if r.decoder != nil {
		r.decoder.Close()
	}

	close(r.output)
	r.connected = false

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/sendspin/ -run TestNewReceiver -v && go test ./pkg/sendspin/ -run TestReceiver -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/receiver.go pkg/sendspin/receiver_test.go
git commit -m "add Receiver type with config, constructor, and lifecycle methods"
```

---

### Task 5: Move Connection and Sync Goroutines to Receiver

Move `Connect`, `performInitialSync`, `clockSyncLoop`, and `watchConnection` from `player.go` into `receiver.go`.

**Files:**
- Modify: `pkg/sendspin/receiver.go`
- Modify: `pkg/sendspin/receiver_test.go`

- [ ] **Step 1: Write a test for Connect returning error on bad address**

Add to `pkg/sendspin/receiver_test.go`:

```go
func TestReceiver_Connect_BadAddress(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:99999",
		PlayerName: "Test",
	})

	err := recv.Connect()
	if err == nil {
		t.Fatal("expected connection error for bad address")
	}
}
```

- [ ] **Step 2: Add Connect method to Receiver**

Add to `pkg/sendspin/receiver.go`:

```go
// Connect establishes connection to the server, performs initial clock sync,
// and starts all pipeline goroutines.
func (r *Receiver) Connect() error {
	clientID := uuid.New().String()

	clientConfig := protocol.Config{
		ServerAddr: r.serverAddr,
		ClientID:   clientID,
		Name:       r.config.PlayerName,
		Version:    1,
		DeviceInfo: protocol.DeviceInfo{
			ProductName:     r.config.DeviceInfo.ProductName,
			Manufacturer:    r.config.DeviceInfo.Manufacturer,
			SoftwareVersion: r.config.DeviceInfo.SoftwareVersion,
		},
		PlayerV1Support: protocol.PlayerV1Support{
			SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 176400, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 96000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 88200, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
				{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
			},
			BufferCapacity:    1048576,
			SupportedCommands: []string{"volume", "mute"},
		},
		ArtworkV1Support: &protocol.ArtworkV1Support{
			Channels: []protocol.ArtworkChannel{
				{Source: "album", Format: "jpeg", MediaWidth: 600, MediaHeight: 600},
			},
		},
		VisualizerV1Support: &protocol.VisualizerV1Support{
			BufferCapacity: 1048576,
		},
	}

	r.client = protocol.NewClient(clientConfig)

	if err := r.client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	log.Printf("Connected to server: %s", r.serverAddr)
	r.connected = true

	if err := r.performInitialSync(); err != nil {
		log.Printf("Initial clock sync failed: %v", err)
	}

	go r.watchConnection()
	go r.handleStreamStart()
	go r.handleStreamClear()
	go r.handleStreamEnd()
	go r.handleAudioChunks()
	go r.handleServerState()
	go r.handleGroupUpdates()
	go r.clockSyncLoop()

	return nil
}

func (r *Receiver) watchConnection() {
	select {
	case <-r.client.Done():
		log.Printf("Server connection lost, shutting down receiver")
		r.connected = false
		r.notifyError(fmt.Errorf("server connection lost"))
		r.cancel()
	case <-r.ctx.Done():
		return
	}
}

func (r *Receiver) performInitialSync() error {
	log.Printf("Performing initial clock synchronization...")

	for i := 0; i < 5; i++ {
		t1 := time.Now().UnixMicro()
		r.client.SendTimeSync(t1)

		select {
		case resp := <-r.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			r.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)
		case <-time.After(500 * time.Millisecond):
			log.Printf("Initial sync round %d timeout", i+1)
		}

		time.Sleep(100 * time.Millisecond)
	}

	rtt, quality := r.clockSync.GetStats()
	log.Printf("Initial clock sync complete: rtt=%dus, quality=%v", rtt, quality)

	return nil
}

func (r *Receiver) clockSyncLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for {
				select {
				case <-r.client.TimeSyncResp:
					log.Printf("Discarded stale time sync response")
				default:
					goto sendRequest
				}
			}

		sendRequest:
			t1 := time.Now().UnixMicro()
			r.client.SendTimeSync(t1)

		case resp := <-r.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			r.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)

		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) notifyError(err error) {
	if r.config.OnError != nil {
		r.config.OnError(err)
	} else {
		log.Printf("Receiver error: %v", err)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/sendspin/ -run TestReceiver -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/sendspin/receiver.go pkg/sendspin/receiver_test.go
git commit -m "add Connect, clock sync, and connection watch to Receiver"
```

---

### Task 6: Move Stream and Audio Goroutines to Receiver

Move `handleStreamStart`, `handleStreamClear`, `handleStreamEnd`, `handleAudioChunks`, `handleServerState`, and `handleGroupUpdates` from `player.go` into `receiver.go`. The key change: instead of writing to `output.Write()`, `handleScheduledAudio` now writes to `r.output` channel.

**Files:**
- Modify: `pkg/sendspin/receiver.go`

- [ ] **Step 1: Add stream and audio handling methods to Receiver**

Add to `pkg/sendspin/receiver.go`:

```go
func (r *Receiver) handleStreamStart() {
	for {
		select {
		case start := <-r.client.StreamStart:
			if start.Player == nil {
				log.Printf("Received stream/start with no player info")
				continue
			}

			log.Printf("Stream starting: %s %dHz %dch %dbit",
				start.Player.Codec, start.Player.SampleRate, start.Player.Channels, start.Player.BitDepth)

			format := audio.Format{
				Codec:      start.Player.Codec,
				SampleRate: start.Player.SampleRate,
				Channels:   start.Player.Channels,
				BitDepth:   start.Player.BitDepth,
			}

			// Create decoder
			var decoder decode.Decoder
			var err error

			if r.config.DecoderFactory != nil {
				decoder, err = r.config.DecoderFactory(format)
			} else {
				decoder, err = r.defaultDecoder(format)
			}

			if err != nil {
				r.notifyError(fmt.Errorf("failed to create decoder: %w", err))
				continue
			}
			r.decoder = decoder
			r.format = format

			// Notify consumer of stream start
			if r.config.OnStreamStart != nil {
				r.config.OnStreamStart(format)
			}

			// Stop any existing scheduler goroutines
			if r.schedulerCancel != nil {
				r.schedulerCancel()
			}
			if r.scheduler != nil {
				r.scheduler.Stop()
			}

			r.schedulerCtx, r.schedulerCancel = context.WithCancel(r.ctx)
			r.scheduler = NewScheduler(r.clockSync, r.config.BufferMs)
			go r.scheduler.Run()
			go r.pumpSchedulerOutput(r.schedulerCtx)

		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) defaultDecoder(format audio.Format) (decode.Decoder, error) {
	switch format.Codec {
	case "pcm":
		return decode.NewPCM(format)
	case "opus":
		return decode.NewOpus(format)
	case "flac":
		return decode.NewFLAC(format)
	default:
		return nil, fmt.Errorf("unsupported codec: %s", format.Codec)
	}
}

func (r *Receiver) handleAudioChunks() {
	for {
		select {
		case chunk := <-r.client.AudioChunks:
			if r.decoder == nil || r.scheduler == nil {
				continue
			}

			pcm, err := r.decoder.Decode(chunk.Data)
			if err != nil {
				r.notifyError(fmt.Errorf("decode error: %w", err))
				continue
			}

			buf := audio.Buffer{
				Timestamp: chunk.Timestamp,
				Samples:   pcm,
				Format:    r.format,
			}
			r.scheduler.Schedule(buf)

		case <-r.ctx.Done():
			return
		}
	}
}

// pumpSchedulerOutput reads from the scheduler and forwards to the output channel
func (r *Receiver) pumpSchedulerOutput(ctx context.Context) {
	for {
		select {
		case buf := <-r.scheduler.Output():
			select {
			case r.output <- buf:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleStreamClear() {
	for {
		select {
		case clear := <-r.client.StreamClear:
			log.Printf("Stream clear received for roles: %v", clear.Roles)
			if len(clear.Roles) == 0 || containsRole(clear.Roles, "player") {
				if r.scheduler != nil {
					r.scheduler.Clear()
				}
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleStreamEnd() {
	for {
		select {
		case end := <-r.client.StreamEnd:
			log.Printf("Stream end received for roles: %v", end.Roles)
			if len(end.Roles) == 0 || containsRole(end.Roles, "player") {
				if r.config.OnStreamEnd != nil {
					r.config.OnStreamEnd()
				}
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleServerState() {
	for {
		select {
		case state := <-r.client.ServerState:
			if state.Metadata != nil && r.config.OnMetadata != nil {
				meta := state.Metadata
				r.config.OnMetadata(Metadata{
					Title:       derefString(meta.Title),
					Artist:      derefString(meta.Artist),
					Album:       derefString(meta.Album),
					AlbumArtist: derefString(meta.AlbumArtist),
					ArtworkURL:  derefString(meta.ArtworkURL),
					Track:       derefInt(meta.Track),
					Year:        derefInt(meta.Year),
					Duration:    getDurationSeconds(meta.Progress),
				})
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleGroupUpdates() {
	for {
		select {
		case update := <-r.client.GroupUpdate:
			if update.PlaybackState != nil {
				log.Printf("Group playback state: %s", *update.PlaybackState)
			}
			if update.GroupID != nil {
				log.Printf("Joined group: %s", *update.GroupID)
			}
		case <-r.ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/sendspin/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add pkg/sendspin/receiver.go
git commit -m "move stream handling and audio pipeline goroutines to Receiver"
```

---

### Task 7: Refactor Player to Compose Receiver

Rewrite `player.go` to compose a `Receiver` internally. Remove all the goroutines that moved to `receiver.go`. Add `ProcessCallback` support. Preserve the entire existing public API.

**Files:**
- Modify: `pkg/sendspin/player.go`
- Create: `pkg/sendspin/player_test.go`

- [ ] **Step 1: Write tests for the refactored Player**

Create `pkg/sendspin/player_test.go`:

```go
// ABOUTME: Tests for refactored Player composing Receiver
// ABOUTME: Verifies backward-compatible API and new ProcessCallback
package sendspin

import (
	"testing"
)

func TestNewPlayer_Defaults(t *testing.T) {
	player, err := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Player",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if player == nil {
		t.Fatal("expected non-nil player")
	}
	if player.receiver != nil {
		t.Error("receiver should be nil before Connect")
	}
}

func TestNewPlayer_ProcessCallbackStored(t *testing.T) {
	called := false
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Player",
		ProcessCallback: func(samples []int32) {
			called = true
		},
	})
	if player.config.ProcessCallback == nil {
		t.Error("expected ProcessCallback to be stored in config")
	}
}

func TestPlayer_StatusBeforeConnect(t *testing.T) {
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Player",
		Volume:     80,
	})

	status := player.Status()
	if status.Volume != 80 {
		t.Errorf("expected volume 80, got %d", status.Volume)
	}
	if status.Connected {
		t.Error("expected not connected before Connect()")
	}
	if status.State != "idle" {
		t.Errorf("expected state idle, got %s", status.State)
	}
}
```

- [ ] **Step 2: Rewrite player.go**

Replace the contents of `pkg/sendspin/player.go` with:

```go
// ABOUTME: High-level Player API for Sendspin streaming
// ABOUTME: Composes Receiver + audio output with optional ProcessCallback
package sendspin

import (
	"context"
	"fmt"
	"log"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/output"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
)

// PlayerConfig holds player configuration
type PlayerConfig struct {
	// ServerAddr is the server address (host:port)
	ServerAddr string

	// PlayerName is the display name for this player
	PlayerName string

	// Volume is the initial volume (0-100)
	Volume int

	// BufferMs is the playback buffer size in milliseconds (default: 500)
	BufferMs int

	// DeviceInfo provides device identification
	DeviceInfo DeviceInfo

	// OnMetadata is called when metadata is received
	OnMetadata func(Metadata)

	// OnStateChange is called when playback state changes
	OnStateChange func(PlayerState)

	// OnError is called when errors occur
	OnError func(error)

	// Output overrides the default audio output backend.
	// When nil, auto-selects oto (16-bit) or malgo (24-bit) based on stream format.
	Output output.Output

	// DecoderFactory overrides the default decoder selection.
	// When nil, the default codec switch (PCM, Opus, FLAC) is used.
	DecoderFactory func(audio.Format) (decode.Decoder, error)

	// ProcessCallback is called with decoded samples before they are written to output.
	// Must not block. Runs on the audio consumption goroutine.
	ProcessCallback func([]int32)
}

// Player provides high-level audio playback from Sendspin servers.
// It composes a Receiver (connect/sync/decode/schedule) with an audio output backend.
type Player struct {
	config   PlayerConfig
	receiver *Receiver
	output   output.Output
	state    PlayerState
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewPlayer creates a new player with the given configuration
func NewPlayer(config PlayerConfig) (*Player, error) {
	if config.Volume == 0 {
		config.Volume = 100
	}
	if config.BufferMs == 0 {
		config.BufferMs = 500
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Player{
		config: config,
		output: config.Output, // may be nil — auto-selected on stream start
		ctx:    ctx,
		cancel: cancel,
		state: PlayerState{
			State:     "idle",
			Volume:    config.Volume,
			Muted:     false,
			Connected: false,
		},
	}, nil
}

// Connect establishes connection to the server and starts playback
func (p *Player) Connect() error {
	recv, err := NewReceiver(ReceiverConfig{
		ServerAddr:     p.config.ServerAddr,
		PlayerName:     p.config.PlayerName,
		BufferMs:       p.config.BufferMs,
		DeviceInfo:     p.config.DeviceInfo,
		DecoderFactory: p.config.DecoderFactory,
		OnMetadata:     p.config.OnMetadata,
		OnStreamStart:  p.onStreamStart,
		OnStreamEnd:    p.onStreamEnd,
		OnError:        p.config.OnError,
	})
	if err != nil {
		return err
	}

	p.receiver = recv

	if err := recv.Connect(); err != nil {
		return err
	}

	// Backward compat: set global clock sync
	sync.SetGlobalClockSync(recv.ClockSync())

	p.state.Connected = true
	p.notifyStateChange()

	// Start consuming decoded buffers
	go p.consumeAudio()

	return nil
}

func (p *Player) onStreamStart(format audio.Format) {
	// Auto-select output if not provided
	if p.output == nil {
		if format.BitDepth <= 16 {
			p.output = output.NewOto()
			log.Printf("Using oto backend for %d-bit audio", format.BitDepth)
		} else {
			p.output = output.NewMalgo()
			log.Printf("Using malgo backend for %d-bit audio", format.BitDepth)
		}
	}

	if err := p.output.Open(format.SampleRate, format.Channels, format.BitDepth); err != nil {
		p.notifyError(fmt.Errorf("failed to initialize output: %w", err))
		return
	}

	// Apply current volume
	p.output.SetVolume(p.state.Volume)
	p.output.SetMuted(p.state.Muted)

	p.state.Codec = format.Codec
	p.state.SampleRate = format.SampleRate
	p.state.Channels = format.Channels
	p.state.BitDepth = format.BitDepth
	p.state.State = "playing"
	p.notifyStateChange()
}

func (p *Player) onStreamEnd() {
	p.state.State = "idle"
	p.notifyStateChange()
}

func (p *Player) consumeAudio() {
	for {
		select {
		case buf, ok := <-p.receiver.Output():
			if !ok {
				return // channel closed
			}

			if p.config.ProcessCallback != nil {
				p.config.ProcessCallback(buf.Samples)
			}

			if p.output != nil {
				if err := p.output.Write(buf.Samples); err != nil {
					p.notifyError(fmt.Errorf("playback error: %w", err))
				}
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// Play starts or resumes playback
func (p *Player) Play() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "playing"
	p.notifyStateChange()
	return p.sendState()
}

// Pause pauses playback
func (p *Player) Pause() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "paused"
	p.notifyStateChange()
	return p.sendState()
}

// Stop stops playback
func (p *Player) Stop() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "idle"
	p.notifyStateChange()
	return p.sendState()
}

// SetVolume sets the volume (0-100)
func (p *Player) SetVolume(volume int) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	p.state.Volume = volume

	if p.output != nil {
		p.output.SetVolume(volume)
	}

	if p.receiver != nil && p.state.Connected {
		p.sendState()
	}

	p.notifyStateChange()
	return nil
}

// Mute sets the mute state
func (p *Player) Mute(muted bool) error {
	p.state.Muted = muted

	if p.output != nil {
		p.output.SetMuted(muted)
	}

	if p.receiver != nil && p.state.Connected {
		p.sendState()
	}

	p.notifyStateChange()
	return nil
}

// Status returns the current player state
func (p *Player) Status() PlayerState {
	return p.state
}

// Stats returns playback statistics
func (p *Player) Stats() PlayerStats {
	stats := PlayerStats{}

	if p.receiver != nil {
		rs := p.receiver.Stats()
		stats.Received = rs.Received
		stats.Played = rs.Played
		stats.Dropped = rs.Dropped
		stats.BufferDepth = rs.BufferDepth
		stats.SyncRTT = rs.SyncRTT
		stats.SyncQuality = rs.SyncQuality
	}

	return stats
}

// Close closes the player and releases all resources
func (p *Player) Close() error {
	p.cancel()

	if p.receiver != nil {
		p.receiver.Close()
	}

	if p.output != nil {
		p.output.Close()
	}

	p.state.Connected = false
	p.state.State = "idle"
	p.notifyStateChange()

	return nil
}

func (p *Player) sendState() error {
	if p.receiver == nil || p.receiver.client == nil {
		return nil
	}
	return p.receiver.client.SendState(protocol.PlayerState{
		State:  "synchronized",
		Volume: p.state.Volume,
		Muted:  p.state.Muted,
	})
}

func (p *Player) notifyStateChange() {
	if p.config.OnStateChange != nil {
		p.config.OnStateChange(p.state)
	}
}

func (p *Player) notifyError(err error) {
	if p.config.OnError != nil {
		p.config.OnError(err)
	} else {
		log.Printf("Player error: %v", err)
	}
}
```

- [ ] **Step 3: Add missing import for decode package**

The `PlayerConfig.DecoderFactory` references `decode.Decoder`. Add this import alias at the top of `player.go`:

```go
import (
	// ... existing imports
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
)
```

Note: The `decode` import is needed for the `DecoderFactory` field type in `PlayerConfig`. If Go complains about an unused import (because `decode.Decoder` is only in the function signature type), use a blank import or reference it in a type alias. In practice, `func(audio.Format) (decode.Decoder, error)` will require the import.

- [ ] **Step 4: Run all tests**

Run: `go test ./pkg/sendspin/ -v`
Expected: PASS — new Player tests pass, existing code compiles

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/player.go pkg/sendspin/player_test.go
git commit -m "refactor Player to compose Receiver internally

Player now delegates connection, sync, decode, and scheduling to
Receiver. Adds Output, DecoderFactory, and ProcessCallback config
fields. Existing API fully preserved."
```

---

### Task 8: Remove Dead Code from player.go

After the refactor, verify there is no leftover dead code from the original player.go (old goroutine methods, duplicate helper functions, unused imports).

**Files:**
- Modify: `pkg/sendspin/player.go`

- [ ] **Step 1: Check for duplicate helper functions**

The helper functions `derefString`, `derefInt`, `getDurationSeconds`, `containsRole` are used by receiver.go now. They should exist in exactly one file. If they're defined in both `player.go` and `receiver.go`, remove them from `player.go` since the receiver uses them.

Check: `grep -n "func derefString\|func derefInt\|func getDurationSeconds\|func containsRole" pkg/sendspin/*.go`

Remove any duplicates from `player.go`.

- [ ] **Step 2: Check for unused imports**

Run: `go build ./pkg/sendspin/`
Expected: No errors. If there are unused import errors, remove them.

Common removals from `player.go`:
- `"time"` (no longer used — sync loops moved to receiver)
- `"github.com/google/uuid"` (moved to receiver)
- `"github.com/Sendspin/sendspin-go/pkg/sync"` (only needed if `SetGlobalClockSync` is called — it is, so keep it)

- [ ] **Step 3: Run full test suite**

Run: `go test ./pkg/... -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/sendspin/player.go
git commit -m "remove dead code from player.go after Receiver extraction"
```

---

### Task 9: Update main.go CLI Entry Points

Update the root `main.go` (player CLI) to verify it still works with the refactored Player. No behavioral changes expected — this is a verification task.

**Files:**
- Review: `main.go`

- [ ] **Step 1: Verify main.go compiles**

Run: `go build -o sendspin-player .`
Expected: No errors

- [ ] **Step 2: Verify server CLI compiles**

Run: `go build -o sendspin-server ./cmd/sendspin-server/`
Expected: No errors

- [ ] **Step 3: Verify all packages compile**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 4: Run full test suite**

Run: `go test ./... 2>&1 | tail -30`
Expected: All tests pass (some packages may skip if they need hardware)

- [ ] **Step 5: Commit (only if changes were needed)**

```bash
git add -A
git commit -m "fix any compilation issues in CLI entry points after refactor"
```

---

### Task 10: Integration Smoke Test

Verify the refactored Player and new Receiver work end-to-end by adding an integration test that exercises the Receiver independently.

**Files:**
- Modify: `pkg/sendspin/receiver_test.go`

- [ ] **Step 1: Add Receiver lifecycle test**

Add to `pkg/sendspin/receiver_test.go`:

```go
func TestReceiver_CloseBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	// Should not panic
	err := recv.Close()
	if err != nil {
		t.Fatalf("unexpected error closing unconnected receiver: %v", err)
	}
}

func TestReceiver_StatsBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	stats := recv.Stats()
	if stats.Received != 0 || stats.Played != 0 || stats.Dropped != 0 {
		t.Error("expected zero stats before connect")
	}
}

func TestReceiver_OnStreamStartCallback(t *testing.T) {
	var receivedFormat audio.Format
	_, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		OnStreamStart: func(f audio.Format) {
			receivedFormat = f
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Callback is stored but not invoked until stream starts
	if receivedFormat.Codec != "" {
		t.Error("expected empty format before stream start")
	}
}

func TestReceiver_CustomDecoderFactory(t *testing.T) {
	factoryCalled := false
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		DecoderFactory: func(f audio.Format) (decode.Decoder, error) {
			factoryCalled = true
			return nil, fmt.Errorf("test decoder")
		},
	})

	if recv.config.DecoderFactory == nil {
		t.Error("expected DecoderFactory to be stored")
	}
	// Factory is stored but not invoked until stream starts
	if factoryCalled {
		t.Error("factory should not be called before connect")
	}
}
```

- [ ] **Step 2: Add missing imports to test file**

Ensure `receiver_test.go` imports:

```go
import (
	"fmt"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
)
```

- [ ] **Step 3: Run all tests**

Run: `go test ./pkg/sendspin/ -v`
Expected: PASS

- [ ] **Step 4: Run full project test suite**

Run: `go test ./...`
Expected: All packages pass

- [ ] **Step 5: Commit and tag**

```bash
git add pkg/sendspin/receiver_test.go
git commit -m "add integration tests for Receiver lifecycle and callbacks"
```
