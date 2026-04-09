> [!IMPORTANT]
> This is a very early proof of concept of the Sendspin protocol. The protocol will likely change. This does work today (10/26), but may not work tomorrow.

# Sendspin Go

A complete Sendspin Protocol implementation in Go, featuring both server and player components for synchronized multi-room audio streaming.

**Key Highlights:**

- **Library-first design**: Use as a Go library or standalone CLI tools
- **Hi-res audio support**: Up to 192kHz/24-bit streaming
- **Multi-codec**: Opus, FLAC, MP3, PCM
- **Precise synchronization**: Microsecond-level multi-room sync
- **Easy to use**: Simple high-level APIs for common use cases
- **Flexible**: Low-level component APIs for custom implementations

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

- `--server` - Manual server WebSocket address (skips mDNS discovery)
- `--port` - Port for mDNS advertisement (default: 8927)
- `--name` - Player friendly name (default: hostname-sendspin-player)
- `--buffer-ms` - Jitter buffer size in milliseconds (default: 150)
- `--log-file` - Log file path (default: sendspin-player.log)
- `--debug` - Enable debug logging

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
- **`pkg/audio/output`**: PortAudio playback
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

## Contributing

Found a bug or have a feature request? Please check existing issues or create a new one:

**[View Issues](https://github.com/Sendspin/sendspin-go/issues)**

### Known Issues & Todo

**High Priority:**

- [ ] Verify hi-res audio (96kHz/192kHz) compatibility with Music Assistant and other Sendspin servers
- [ ] Test multi-room synchronization accuracy with 5+ players
- [ ] Audit protocol implementation for spec compliance as official spec evolves
- [ ] Performance profiling and optimization for CPU/memory usage
- [ ] Add comprehensive integration tests with real audio files

**Protocol & Compatibility:**

- [ ] Validate all message types match latest Sendspin Protocol spec
- [ ] Test with Music Assistant server
- [ ] Test with other Sendspin protocol implementations
- [ ] Document any protocol extensions or deviations
- [ ] Add protocol version negotiation

**Audio Quality:**

- [ ] Verify 24-bit audio pipeline maintains full bit depth
- [ ] Test sample rate conversion quality (FLAC 96kHz → Opus 48kHz)
- [ ] Add audio quality metrics and testing
- [ ] Support for gapless playback
- [ ] Volume curve optimization (currently linear)

**Features:**

- [ ] FLAC and MP3 decoder implementation (currently stubs)
- [ ] Visualizer role support (FFT spectrum data)
- [ ] Album artwork support (already in protocol)
- [ ] Player groups and zones
- [ ] Playlist/queue management
- [ ] Cross-fade between tracks

**Stability:**

- [ ] Reconnection handling and automatic retry
- [ ] Network error recovery
- [ ] Graceful degradation on clock sync loss
- [ ] Memory leak testing for long-running sessions
- [ ] Stress testing with many clients

**Developer Experience:**

- [ ] Add godoc examples for all public APIs
- [ ] CI/CD pipeline for automated testing
- [ ] Cross-platform testing (Linux/macOS/Windows)
- [ ] Docker containers for easy deployment
- [ ] Benchmarking suite

### Roadmap

**v0.9.x (Current - Library Stabilization)**

- Fix bugs discovered during testing
- Improve protocol compliance
- Add integration tests

**v1.0.0 (Stable Release)**

- Complete FLAC/MP3 decoder implementation
- Full Music Assistant compatibility verified
- 100% protocol spec compliance
- Production-ready stability

**v1.1.0 (Enhanced Features)**

- Album artwork support
- Gapless playback
- Advanced volume controls

**v2.0.0 (Advanced Multi-Room)**

- Player groups and zones
- Synchronized playback controls
- Playlist management

## Protocol

Implements the [Sendspin Protocol](https://github.com/Sendspin/spec) specification.

**Implementation Status:**

- ✅ WebSocket transport
- ✅ Client/Server handshake
- ✅ Clock synchronization (NTP-style)
- ✅ Audio streaming (binary frames)
- ✅ Metadata messages
- ✅ Control commands
- ✅ Multi-codec support (Opus, PCM)
- ⚠️ Visualizer role (planned)
- ⚠️ Album artwork (protocol support exists, not implemented)
