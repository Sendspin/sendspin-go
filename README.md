# Sendspin Go

A complete Sendspin Protocol implementation in Go, featuring both server and player components for synchronized multi-room audio streaming.

**Key Highlights:**

- **Library-first design**: Use as a Go library or standalone CLI tools
- **Hi-res audio support**: Up to 192kHz/24-bit streaming
- **Multi-codec**: Opus, FLAC, MP3, PCM
- **Precise synchronization**: Microsecond-level multi-room sync
- **Easy to use**: Simple high-level APIs for common use cases
- **Flexible**: Low-level component APIs for custom implementations
- ~44mb of memory usage in Windows for sendspin-player

## Using as a Library

Install the library:

```bash
go get github.com/Sendspin/sendspin-go
```

### Quick Start - Player

```go
package main

import (
    "log"
    "github.com/Sendspin/sendspin-go/pkg/sendspin"
)

func main() {
    // Create and configure player
    player, err := sendspin.NewPlayer(sendspin.PlayerConfig{
        ServerAddr: "localhost:8927",
        PlayerName: "Living Room",
        Volume:     80,
        OnMetadata: func(meta sendspin.Metadata) {
            log.Printf("Playing: %s - %s", meta.Artist, meta.Title)
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // Connect and play
    if err := player.Connect(); err != nil {
        log.Fatal(err)
    }
    if err := player.Play(); err != nil {
        log.Fatal(err)
    }

    // Keep running
    select {}
}
```

### Quick Start - Server

```go
package main

import (
    "log"
    "github.com/Sendspin/sendspin-go/pkg/sendspin"
)

func main() {
    // Create test tone source (or use NewFileSource)
    source := sendspin.NewTestTone(192000, 2)

    // Create and start server
    server, err := sendspin.NewServer(sendspin.ServerConfig{
        Port:   8927,
        Name:   "My Server",
        Source: source,
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := server.Start(); err != nil {
        log.Fatal(err)
    }

    // Keep running
    select {}
}
```

### More Examples

See the [examples/](examples/) directory for more complete examples:

- **[basic-player/](examples/basic-player/)** - Simple player with status monitoring
- **[basic-server/](examples/basic-server/)** - Simple server with test tone
- **[custom-source/](examples/custom-source/)** - Custom audio source implementation

### API Documentation

- **High-level API**: `pkg/sendspin` - Player and Server with simple configuration
- **Audio processing**: `pkg/audio` - Format types, codecs, resampling, output
- **Protocol**: `pkg/protocol` - WebSocket client and message types
- **Clock sync**: `pkg/sync` - Precise timing synchronization
- **Discovery**: `pkg/discovery` - mDNS service discovery

Full API documentation: https://pkg.go.dev/github.com/Sendspin/sendspin-go

## Features

### Server

- Stream audio from multiple sources:
    - Local files (MP3, FLAC)
    - HTTP/HTTPS streams (direct MP3)
    - HLS streams (.m3u8 live radio)
    - Test tone generator (440Hz)
- Automatic resampling to 48kHz for Opus compatibility
- Multi-codec support (Opus @ 256kbps, PCM fallback)
- mDNS service advertisement for automatic discovery
- Real-time terminal UI showing connected clients
- WebSocket-based streaming with precise timestamps

### Player

- Automatic server discovery via mDNS
- Multi-codec support (Opus, FLAC, PCM)
- Precise clock synchronization for multi-room audio
- Interactive terminal UI with volume control
- Jitter buffer for smooth playback

## Installation

### Prerequisites

You'll need `pkg-config`, Opus libraries, and optionally `ffmpeg` for HLS streaming:

```bash
# macOS
brew install pkg-config opus opusfile ffmpeg

# Ubuntu/Debian
sudo apt-get install pkg-config libopus-dev libopusfile-dev ffmpeg

# Fedora
sudo dnf install pkg-config opus-devel opusfile-devel ffmpeg
```

**Windows (MSYS2):**

Install MSYS2 from https://www.msys2.org/, then in a **MSYS2 MinGW 64-bit** shell:

```bash
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-pkg-config \
          mingw-w64-x86_64-opus mingw-w64-x86_64-opusfile
```

All subsequent `go build`, `go test`, and `make` commands must be run from a shell with the MSYS2 MinGW 64-bit toolchain on PATH:

```bash
export PATH="/c/msys64/mingw64/bin:$PATH"
```

**Note:** `ffmpeg` is only required for HLS/m3u8 stream support. Local files and direct HTTP MP3 streams work without it.

### Build

Build both server and player:

```bash
make
```

Or build individually:

```bash
make server  # Builds sendspin-server
make player  # Builds sendspin-player
```

On Windows, both binaries are produced in the repo root as `sendspin-server.exe` and `sendspin-player.exe`. Run them from the same MSYS2 MinGW 64-bit shell (or from cmd/PowerShell once the MSYS2 runtime DLLs are on PATH).

## Usage

### Server

Start a server with the interactive TUI (default, plays 440Hz test tone):

```bash
./sendspin-server
```

Stream a local audio file:

```bash
./sendspin-server --audio /path/to/music.mp3
./sendspin-server --audio /path/to/album.flac
```

Stream from HTTP/HTTPS:

```bash
./sendspin-server --audio http://example.com/stream.mp3
```

Stream HLS/m3u8 (live radio):

```bash
./sendspin-server --audio "https://stream.radiofrance.fr/fip/fip.m3u8?id=radiofrance"
```

Run without TUI (streaming logs to stdout):

```bash
./sendspin-server --no-tui
```

#### Server Options

- `--port` - WebSocket server port (default: 8927)
- `--name` - Server friendly name (default: hostname-sendspin-server)
- `--audio` - Audio source to stream:
    - Local file path: `/path/to/music.mp3`, `/path/to/audio.flac`
    - HTTP stream: `http://example.com/stream.mp3`
    - HLS stream: `https://example.com/live.m3u8`
    - If not specified, plays 440Hz test tone
- `--log-file` - Log file path (default: sendspin-server.log)
- `--debug` - Enable debug logging
- `--no-mdns` - Disable mDNS advertisement (clients must connect manually)
- `--no-tui` - Disable TUI, use streaming logs instead

#### Server TUI

The server TUI shows:

- Server name and port
- Uptime
- Currently playing audio
- Connected clients with codec and state
- Press `q` or `Ctrl+C` to quit

### Player

Start a player (auto-discovers servers via mDNS):

```bash
./sendspin-player --name "Living Room"
```

Connect to a specific server manually:

```bash
./sendspin-player --server ws://192.168.1.100:8927 --name "Kitchen"
```

#### Player Options

- `--config` - Path to player.yaml config file. Default search: `$SENDSPIN_PLAYER_CONFIG`, `~/.config/sendspin/player.yaml`, `/etc/sendspin/player.yaml`.
- `--server` - Manual server WebSocket address (skips mDNS discovery)
- `--port` - Port for mDNS advertisement (default: 8927)
- `--name` - Player friendly name (default: hostname-sendspin-player)
- `--buffer-ms` - Jitter buffer size in milliseconds (default: 150)
- `--log-file` - Log file path (default: sendspin-player.log)
- `--client-id` - Override the persisted `client_id`. When set, the value is written to the config file and reused on subsequent launches.
- `--debug` - Enable debug logging

#### Configuration File (`player.yaml`)

Every CLI flag has a matching key in `player.yaml`. Keys use `snake_case` (`--buffer-ms` ↔ `buffer_ms`).

**Search order** (first existing file wins; missing is not an error):

1. `--config <path>` flag
2. `$SENDSPIN_PLAYER_CONFIG`
3. `~/.config/sendspin/player.yaml` (macOS: `~/Library/Application Support/sendspin/player.yaml`; Windows: `%AppData%\sendspin\player.yaml`)
4. `/etc/sendspin/player.yaml` (daemon/system-wide)

**Value precedence**, for every flag:

1. CLI flag if passed
2. Env var `SENDSPIN_PLAYER_<UPPER_SNAKE>` (e.g. `SENDSPIN_PLAYER_BUFFER_MS=200`)
3. Config file key
4. Built-in default

**Example `player.yaml`:**

```yaml
# Identity
name:       "Living Room"
client_id:  "aa:bb:cc:dd:ee:ff"    # auto-derived from MAC if unset

# Network
server: ""                          # empty = use mDNS
port:   8927

# Audio
buffer_ms:       150
static_delay_ms: 0
preferred_codec: ""                 # pcm (default), opus, flac
buffer_capacity: 1048576

# Device identity (shown in Music Assistant)
product_name: ""
manufacturer: ""

# Behavior
no_reconnect: false
daemon:       false
no_tui:       false
log_file:     "sendspin-player.log"
```

#### Player Identity (`client_id`)

The player sends a stable `client_id` so controllers like Music Assistant recognize it as the same player across restarts. Resolution order:

1. `--client-id` flag (when set, also persisted to the config file as `client_id`)
2. `client_id` key in the loaded `player.yaml`
3. MAC address of the primary network interface (`xx:xx:xx:xx:xx:xx`)
4. Freshly generated UUID (written to `player.yaml` as `client_id` and reused next launch)

Removing `client_id` from `player.yaml` causes the next launch to re-derive, which the server will see as a new player.

Running multiple players on one host:

```bash
./sendspin-player --name "Kitchen" --config ~/.config/sendspin/kitchen.yaml &
./sendspin-player --name "Bedroom" --config ~/.config/sendspin/bedroom.yaml &
```

Each config file holds its own `client_id`, so the two instances register as two distinct players.

#### Player TUI

The player TUI shows:

- Player name
- Server connection status
- Current audio title/artist
- Codec and sample rate
- Buffer depth
- Clock sync statistics (offset, RTT, drift)
- Playback statistics (received, played, dropped)
- Volume control (Up/Down arrows or +/- keys)
- Press `m` to mute/unmute
- Press `q` or `Ctrl+C` to quit

## Architecture

Sendspin Go is built with a **library-first architecture**, providing three layers of APIs:

### 1. High-Level API (`pkg/sendspin`)

Simple Player and Server types for common use cases:

- **Player**: Connect, play, control volume, get stats
- **Server**: Stream from AudioSource, manage clients
- **AudioSource**: Interface for custom audio sources

### 2. Component APIs

Lower-level building blocks for custom implementations:

- **`pkg/audio`**: Format types, sample conversions, Buffer
- **`pkg/audio/decode`**: PCM, Opus, FLAC, MP3 decoders
- **`pkg/audio/encode`**: PCM, Opus encoders
- **`pkg/audio/resample`**: Sample rate conversion
- **`pkg/audio/output`**: Audio playback via malgo (miniaudio); 16/24/32-bit native
- **`pkg/protocol`**: WebSocket client, message types
- **`pkg/sync`**: Clock synchronization with drift compensation
- **`pkg/discovery`**: mDNS service discovery

### 3. CLI Tools

Thin wrappers around the library APIs:

- **`cmd/sendspin-server`**: Full-featured server with TUI
- **`cmd/sendspin-player`**: Full-featured player with TUI (main.go at root)

### Server Pipeline

The server streams audio in 20ms chunks with microsecond timestamps. Audio is buffered 500ms ahead to allow for network jitter and clock synchronization.

**Processing flow:**

1. Audio source (file decoder or test tone generator)
2. Per-client codec negotiation (Opus or PCM)
3. Timestamp generation using monotonic clock
4. WebSocket binary message streaming

### Player Pipeline

The player uses a sophisticated scheduling system to ensure perfectly synchronized playback across multiple rooms.

**Processing flow:**

1. WebSocket client receives timestamped audio chunks
2. Clock sync system converts server timestamps to local time
3. Priority queue scheduler with startup buffering (200ms)
4. Persistent audio player with streaming I/O pipe
5. Software volume control and mixing

### Clock Synchronization

The player uses a simple, robust clock synchronization system:

- Calculates server loop origin on first sync
- Direct time base matching (no drift prediction)
- Continuous RTT measurement for quality monitoring
- Microsecond precision timestamps
- 500ms startup buffer matches server's lead time

## Example: Multi-Room Setup

Terminal 1 - Start the server:

```bash
./sendspin-server --audio ~/Music/favorite-album.mp3
```

Terminal 2 - Living room player:

```bash
./sendspin-player --name "Living Room"
```

Terminal 3 - Kitchen player:

```bash
./sendspin-player --name "Kitchen"
```

Both players will discover the server via mDNS and start playing in perfect sync.

## Development

Run tests:

```bash
make test
```

Clean binaries:

```bash
make clean
```

Install to GOPATH/bin:

```bash
make install
```

### Protocol conformance

The [Sendspin protocol conformance suite](https://github.com/Sendspin/conformance) runs real network scenarios between adapter binaries and compares outputs against canonical hashes. sendspin-go has a first-class adapter and is tested on every PR via the `Conformance` GitHub Actions workflow.

Run the same suite locally:

```bash
make conformance
```

This clones `Sendspin/conformance` into `../conformance` (sibling directory) and the `aiosendspin` reference peer on first run, installs the harness with `uv`, and runs `scripts/run_all.py` with this checkout pinned via the `CONFORMANCE_REPO_SENDSPIN_GO` environment variable. Requires [uv](https://docs.astral.sh/uv/getting-started/installation/) and Python 3.12+.

The published conformance report for the `main` branch is at https://sendspin.github.io/conformance/.

## Contributing

Found a bug or have a feature request? Please check existing issues or create a new one:

**[View Issues](https://github.com/Sendspin/sendspin-go/issues)**

### Recently Shipped

**v1.2.0** — drop the oto backend and unify on malgo for true 24-bit output (see [#3](https://github.com/Sendspin/sendspin-go/issues/3) and [#26](https://github.com/Sendspin/sendspin-go/pull/26))

**v1.1.0** — server-initiated client discovery, Kalman clock filter, code-path audit

### Known Issues & Todo

**Protocol & compatibility:**

- [ ] Validate all message types match latest Sendspin Protocol spec
- [ ] Test with additional Sendspin-compatible servers beyond Music Assistant
- [ ] Document protocol extensions or deviations
- [ ] Explicit protocol-version negotiation (versioned roles like `player@v1` exist; a numeric version handshake does not)

**Audio:**

- [ ] Test sample rate conversion quality (FLAC 96kHz → Opus 48kHz)
- [ ] Real FLAC streaming decoder (currently a stub — see [#34](https://github.com/Sendspin/sendspin-go/issues/34))
- [ ] Gapless playback
- [ ] Volume curve optimization (currently linear)
- [ ] Visualizer role support (FFT spectrum data)

**Stability:**

- [ ] Reconnection handling and automatic retry
- [ ] Graceful degradation on clock sync loss
- [ ] Memory leak testing for long-running sessions
- [ ] Stress testing with many clients and multi-room sync accuracy with 5+ players

**Features:**

- [ ] Album artwork end-to-end (downloader exists; not fully wired to TUI surfaces)
- [ ] Player groups and zones
- [ ] Playlist/queue management
- [ ] Cross-fade between tracks

**Developer experience:**

- [ ] Godoc examples for all public APIs
- [ ] Automated cross-platform test matrix (CI runs Linux only today)
- [ ] Docker containers for easy deployment
- [ ] Benchmarking suite
- [ ] Clean up pre-existing tech debt surfaced by [PR #26](https://github.com/Sendspin/sendspin-go/pull/26): see issues [#27–#34](https://github.com/Sendspin/sendspin-go/issues)

### Roadmap

**Released**

- **v1.2.0** — oto backend removed, malgo is the only audio output, true 24-bit pipeline end-to-end
- **v1.1.0** — server-initiated client discovery, Kalman time filter, protocol audit fixes
- **v1.0.0** — initial stable release, Music Assistant compatibility, precise multi-room sync

**Planned**

- **v2.0.0 (Advanced Multi-Room)** — player groups and zones, synchronized playback controls, playlist management

## Protocol

Implements the [Sendspin Protocol](https://github.com/Sendspin/spec) specification.

**Implementation Status:**

- ✅ WebSocket transport
- ✅ Client/Server handshake with versioned role negotiation (`player@v1`, `metadata@v1`)
- ✅ Clock synchronization (NTP-style, two-dimensional Kalman filter on offset + drift)
- ✅ Audio streaming (binary frames, microsecond timestamps)
- ✅ Metadata messages (via `server/state`)
- ✅ Control commands (volume, mute)
- ✅ Multi-codec support (Opus with server-side resampling, 24-bit PCM)
- ✅ True 24-bit audio output via malgo (v1.2.0)
- ✅ Server-initiated client discovery (v1.1.0)
- ⚠️ Album artwork — downloader exists, not fully wired through to all TUI surfaces
- ⚠️ Visualizer role (planned)
