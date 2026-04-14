# Layered Architecture Design: Receiver + Player Split

**Date:** 2026-04-12
**Status:** Approved
**Target version:** v1.2.0

## Motivation

The sendspin-go SDK tightly couples audio decoding, clock sync, scheduling, and playback into a single `Player` type. Consumers who want decoded audio bytes without playback (visualizers, DSP pipelines, custom output backends, embedded devices) cannot use the library without initializing an audio device.

The Sendspin project is standardizing a 3-layer architecture across SDKs (see sendspin-rs):

1. **Raw connection** — WebSocket transport and message types
2. **Data processing** — clock sync, decoding, scheduling
3. **Playback** — audio output to hardware

This design splits `pkg/sendspin.Player` into a `Receiver` (layers 1+2) and a slimmed `Player` (layer 3 + convenience wrapper), with no breaking changes.

## Design

### Receiver

The `Receiver` is the core new type. It handles: connect, handshake, clock sync, decode, and schedule. It emits time-stamped, decoded audio buffers via a channel.

```go
type ReceiverConfig struct {
    ServerAddr     string
    PlayerName     string
    BufferMs       int                                         // default: 500
    DeviceInfo     DeviceInfo
    DecoderFactory func(audio.Format) (decode.Decoder, error)  // nil = default
    OnMetadata     func(Metadata)
    OnStreamStart  func(audio.Format)
    OnStreamEnd    func()
    OnError        func(error)
}

type Receiver struct {
    client    *protocol.Client
    clockSync *sync.ClockSync     // own instance, not global
    scheduler *Scheduler
    decoder   decode.Decoder
    // ... context, state
}

func NewReceiver(config ReceiverConfig) (*Receiver, error)
func (r *Receiver) Connect() error
func (r *Receiver) Output() <-chan audio.Buffer   // closed when Receiver.Close() is called
func (r *Receiver) ClockSync() *sync.ClockSync
func (r *Receiver) Stats() ReceiverStats
func (r *Receiver) Close() error
```

The `Receiver` owns all goroutines currently in `Player`: `handleStreamStart`, `handleAudioChunks`, `clockSyncLoop`, `handleStreamClear`, `handleStreamEnd`, `handleServerState`, `handleGroupUpdates`, and `watchConnection`.

Each `Receiver` creates its own `sync.ClockSync` instance. It never touches the global.

### Player (Refactored)

`Player` becomes a thin wrapper composing `Receiver` + `output.Output` + optional hooks. The existing public API is fully preserved.

```go
type PlayerConfig struct {
    // Existing fields (unchanged)
    ServerAddr    string
    PlayerName    string
    Volume        int
    BufferMs      int
    DeviceInfo    DeviceInfo
    OnMetadata    func(Metadata)
    OnStateChange func(PlayerState)
    OnError       func(error)

    // New optional fields
    Output          output.Output                               // nil = auto-select
    DecoderFactory  func(audio.Format) (decode.Decoder, error)  // nil = default
    ProcessCallback func([]int32)                               // tap before output
}

type Player struct {
    receiver *Receiver
    output   output.Output
    config   PlayerConfig
    // ... state, context
}
```

`Player.Connect()` internally:

1. Creates a `Receiver` from its config fields.
2. Calls `receiver.Connect()`.
3. Sets `sync.SetGlobalClockSync(receiver.ClockSync())` for backward compat.
4. Starts a goroutine that reads from `receiver.Output()` and for each buffer:
   - Calls `config.ProcessCallback(buf.Samples)` if set.
   - Calls `output.Write(buf.Samples)`.

Output auto-selection (when `PlayerConfig.Output` is nil) uses the format from the `OnStreamStart` callback:

- `bitDepth <= 16` -> `output.NewOto()`
- `bitDepth > 16` -> `output.NewMalgo()`

`Player` retains: output lifecycle, volume/mute control, `ProcessCallback`, state change notifications.

### ProcessCallback

```go
type ProcessCallback func([]int32)
```

Called with decoded samples (24-bit range `int32`) before every `output.Write`. Runs on the audio consumption goroutine. Consumers must not block — same constraints as the Rust SDK's callback.

Use cases: VU meters, visualization overlays, audio monitoring alongside playback.

For consumers who want audio WITHOUT playback, use `Receiver` directly.

### ClockSync Changes

- `Receiver` creates and owns its own `sync.ClockSync` instance.
- `Player` still calls `sync.SetGlobalClockSync()` after connect for backward compat.
- `sync.SetGlobalClockSync()` and `sync.ServerMicrosNow()` are deprecated (log warning on first use).
- Multiple `Receiver` instances can coexist in one process, each with independent clock sync.

## Consumer Use Cases

### Visualizer (raw PCM, no playback)

```go
recv, _ := sendspin.NewReceiver(sendspin.ReceiverConfig{
    ServerAddr: "192.168.1.50:8927",
    PlayerName: "My Visualizer",
})
recv.Connect()

for buf := range recv.Output() {
    visualizer.ProcessAudio(buf.Samples)
}
```

### Standard player (unchanged)

```go
player, _ := sendspin.NewPlayer(sendspin.PlayerConfig{
    ServerAddr: "192.168.1.50:8927",
    PlayerName: "Living Room",
})
player.Connect()
defer player.Close()
```

### Player with VU meter

```go
player, _ := sendspin.NewPlayer(sendspin.PlayerConfig{
    ServerAddr: "192.168.1.50:8927",
    PlayerName: "Living Room",
    ProcessCallback: func(samples []int32) {
        vuMeter.Update(samples)
    },
})
player.Connect()
```

### Custom output backend

```go
player, _ := sendspin.NewPlayer(sendspin.PlayerConfig{
    ServerAddr: "192.168.1.50:8927",
    PlayerName: "Custom Device",
    Output:     myALSAOutput,
})
player.Connect()
```

## File Structure

No new packages. `Receiver` lives in `pkg/sendspin` alongside `Player`.

```
pkg/sendspin/
    receiver.go      NEW — Receiver type, all connection/decode/sync goroutines
    player.go        SLIMMED — composes Receiver + Output, volume/mute, ProcessCallback
    scheduler.go     UNCHANGED
    server.go        UNCHANGED
    source.go        UNCHANGED

pkg/sync/
    clock.go         MINOR — deprecation warnings on global functions

All other packages UNCHANGED.
```

## API Changes Summary

### New

| Symbol | Purpose |
|--------|---------|
| `sendspin.ReceiverConfig` | Config for data-only client |
| `sendspin.NewReceiver()` | Create a Receiver |
| `Receiver.Connect()` | Connect and start pipeline |
| `Receiver.Output()` | Channel of decoded `audio.Buffer` |
| `Receiver.ClockSync()` | Access clock sync instance |
| `Receiver.Stats()` | Pipeline statistics |
| `Receiver.Close()` | Tear down |
| `PlayerConfig.Output` | Inject custom output backend |
| `PlayerConfig.DecoderFactory` | Inject custom decoder |
| `PlayerConfig.ProcessCallback` | Tap audio before output |

### Deprecated

| Symbol | Replacement |
|--------|-------------|
| `sync.SetGlobalClockSync()` | Use `Receiver.ClockSync()` |
| `sync.ServerMicrosNow()` | Use `receiver.ClockSync().ServerToLocalTime()` |

### Breaking Changes

None.

## Testing

- Receiver unit tests: connect with mock server, verify buffers arrive on Output channel with correct timestamps.
- Player integration tests: verify existing behavior is preserved when refactored to use Receiver internally.
- ProcessCallback test: verify callback fires with correct samples before output.Write.
- Multi-receiver test: two Receivers to different mock servers, verify independent clock sync.
- DecoderFactory test: inject a custom decoder, verify it's used instead of the default.
- Output injection test: inject a mock output, verify Write is called with decoded samples.
