# Sendspin Go

## Project Overview
Synchronized multi-room audio streaming protocol implementation in Go. Provides both a library (`pkg/`) and CLI tools (player at root `main.go`, server at `cmd/sendspin-server`).

## Build & Test
```bash
make              # Build player + server
make player       # Build sendspin-player only
make server       # Build sendspin-server only
make test         # Run all tests
make test-verbose # Verbose test output
make test-coverage # Coverage report
make lint         # Run golangci-lint
```

## Architecture
- **pkg/** — Public library API (sendspin, audio, protocol, sync, discovery)
- **internal/** — CLI-specific implementations (server, ui, version, discovery)
- **cmd/** — Additional CLI entry points (sendspin-server)
- Root `main.go` — Player CLI entry point

### Key layers
1. `pkg/sendspin` — High-level Player/Server API
2. `pkg/audio` — Codec encode/decode, resampling, output
3. `pkg/protocol` — WebSocket client + message types
4. `pkg/sync` — Clock synchronization
5. `pkg/discovery` — mDNS discovery

## Conventions
- Go 1.24+, modules at `github.com/Sendspin/sendspin-go`
- `ABOUTME:` comments at top of files describe purpose
- Linting: golangci-lint with errcheck and staticcheck disabled
- Tests: `_test.go` co-located with source
- Audio chunks are 20ms at 50Hz; timestamps in microseconds
- Protocol uses WebSocket with JSON control messages and binary audio frames
