# sendspin-server: YAML config file + daemon mode

**Status:** Design approved 2026-04-20
**Tracks:** parity with `sendspin-player` config (closes equivalent of #40 for the server side)

## Problem

`sendspin-server` has no config-file support and no `--daemon` flag. To run it
under systemd today, an operator has to stuff every option into `ExecStart` or
an env file, and logging fights `journalctl` because the binary always opens
`sendspin-server.log`. The player solved this with a three-layer precedence
model (CLI > env > YAML > default), `WriteStringKey` for comment-preserving
round-trips, and a `--daemon` flag that logs to stdout only. The server should
get the same treatment.

## Goals

1. YAML config file at `/etc/sendspin/server.yaml` or `~/.config/sendspin/server.yaml` with the same search-order contract as `player.yaml`.
2. `SENDSPIN_SERVER_*` env var overlay with the same precedence rules as the player.
3. A `--daemon` flag that logs to stdout only (journalctl-friendly) and suppresses both the TUI and the log file.
4. `systemctl enable --now sendspin-server` works after `make install-daemon`.
5. Zero behavior change for users who don't opt in to any of the above.

## Non-goals

- **No write-back.** The server has no analog of the player's `client_id`; mDNS rediscovery handles instance identity at the network layer. `WriteStringKey` stays unused by the server (and stays generic in case future features need it).
- **No `-stream-logs` alias.** The player has it for historical reasons; the server starts clean without the redundant alias.
- **No unified `ConfigFile` struct.** Each binary keeps its own typed struct. Only the machinery (search paths, flag overlay) is shared.

## Approach

Refactor `pkg/sendspin/config.go` so the generic machinery takes a
`(envPrefix, map[string]string)` pair instead of a typed `*PlayerConfigFile`,
then add a parallel `ServerConfigFile` / `LoadServerConfig` /
`DefaultServerConfigPath` surface alongside the player's. The player's
`asStringMap()` already produces exactly the map the refactored function
needs, so the call-site change is one line.

No public API outside `pkg/sendspin` uses the old `ApplyEnvAndFile` signature
(verified by grep: only `main.go`, `config.go`, and `config_test.go`). The
technically-breaking signature change is contained.

## Architecture

### `pkg/sendspin/config.go` — refactor

Change:

```go
// Before
func ApplyEnvAndFile(fs *flag.FlagSet, setByUser map[string]bool, cfg *PlayerConfigFile) error

// After
func ApplyEnvAndFile(fs *flag.FlagSet, setByUser map[string]bool, envPrefix string, fileValues map[string]string) error
```

New unexported helpers:

- `loadYAMLConfig(searchPaths []string, out any) (string, error)` — opens the first existing file, unmarshals into `out`. Missing files are not an error (`(nil, "", nil)` equivalent).
- `userConfigPath(relative string) (string, error)` — wraps `os.UserConfigDir() + "/sendspin/" + relative`. Used by both `DefaultPlayerConfigPath` and `DefaultServerConfigPath`.

Existing public surface (`WriteStringKey`, `topLevelMapping`, `setOrAppendStringKey`, `atomicWriteFile`) is already generic and unchanged.

### `pkg/sendspin/config.go` — new server surface

```go
const ServerEnvPrefix = "SENDSPIN_SERVER_"

type ServerConfigFile struct {
    Name            string `yaml:"name,omitempty"`
    Port            *int   `yaml:"port,omitempty"`
    LogFile         string `yaml:"log_file,omitempty"`
    Debug           *bool  `yaml:"debug,omitempty"`
    NoMDNS          *bool  `yaml:"no_mdns,omitempty"`
    NoTUI           *bool  `yaml:"no_tui,omitempty"`
    Audio           string `yaml:"audio,omitempty"`
    DiscoverClients *bool  `yaml:"discover_clients,omitempty"`
    Daemon          *bool  `yaml:"daemon,omitempty"`
}

func LoadServerConfig(explicitPath string) (*ServerConfigFile, string, error)
func DefaultServerConfigPath() (string, error)
func (c *ServerConfigFile) asStringMap() map[string]string
```

`LoadServerConfig` search order (first existing wins; missing is not an error):

1. `explicitPath` if non-empty
2. `$SENDSPIN_SERVER_CONFIG`
3. `<UserConfigDir>/sendspin/server.yaml`
4. `/etc/sendspin/server.yaml`

### Player call-site update

```go
// main.go (player)
if err := sendspin.ApplyEnvAndFile(
    flag.CommandLine, setByUser, sendspin.PlayerEnvPrefix, cfg.asStringMap(),
); err != nil { ... }
```

`cfg.asStringMap()` on a nil `*PlayerConfigFile` must keep returning an empty map (existing behavior — confirmed in `TestApplyEnvAndFile_NilConfigStillHonorsEnv`). Preserve that contract.

### `cmd/sendspin-server/main.go` — new flags & wiring

Add:

```go
daemon     = flag.Bool("daemon", false, "Daemon mode: log to stdout only (journalctl-friendly), no TUI, no log file")
configPath = flag.String("config", "", "Path to server.yaml config file. Default search: $SENDSPIN_SERVER_CONFIG, ~/.config/sendspin/server.yaml, /etc/sendspin/server.yaml.")
```

After `flag.Parse()`:

```go
setByUser := map[string]bool{"config": true}
flag.Visit(func(f *flag.Flag) { setByUser[f.Name] = true })

cfg, _, err := sendspin.LoadServerConfig(*configPath)
if err != nil { log.Fatalf("config: %v", err) }
if err := sendspin.ApplyEnvAndFile(
    flag.CommandLine, setByUser, sendspin.ServerEnvPrefix, cfg.asStringMap(),
); err != nil { log.Fatalf("config overlay: %v", err) }
```

### Daemon mode

- `useTUI := !(*noTUI || *daemon)`
- Logging branch:
  - `-daemon` → `log.SetOutput(os.Stdout)`; skip opening `*logFile`.
  - TUI on → log to file only (unchanged).
  - Neither → `io.MultiWriter(os.Stdout, f)` (unchanged).
- Non-TUI startup banner logs `"Starting Sendspin Server: %s (port %d)"` and, when `-daemon`, adds `"Daemon mode: logging to stdout only"`.

### Distribution artifacts

**`dist/systemd/sendspin-server.service`** — shape mirrors `sendspin-player.service`:

```ini
[Unit]
Description=Sendspin Server
Documentation=https://github.com/Sendspin/sendspin-go
After=network-online.target sound.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sendspin-server --daemon
Restart=on-failure
RestartSec=5
EnvironmentFile=-/etc/default/sendspin-server
ExecStart=
ExecStart=/usr/local/bin/sendspin-server --daemon $SENDSPIN_SERVER_OPTS

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

`ProtectHome=read-only` (not `yes`) so `-audio /home/user/Music/...` still works.

**`dist/systemd/sendspin-server.env`** — operator-editable env file installed to `/etc/default/sendspin-server`:

```sh
# sendspin-server: extra CLI flags appended to ExecStart
# Prefer /etc/sendspin/server.yaml for structured config; use this for
# ad-hoc overrides or keys that aren't (yet) in the YAML.
# SENDSPIN_SERVER_OPTS="--debug --discover-clients"
SENDSPIN_SERVER_OPTS=""
```

**`dist/config/server.example.yaml`** — annotated starter, every key commented out so the file is a no-op until edited:

```yaml
# sendspin-server configuration
#
# Search order (first existing file wins; missing is not an error):
#   1. --config <path>
#   2. $SENDSPIN_SERVER_CONFIG
#   3. ~/.config/sendspin/server.yaml           (user install)
#   4. /etc/sendspin/server.yaml                (daemon / system-wide)
#
# Per-value precedence for every key below:
#   CLI flag > SENDSPIN_SERVER_<UPPER_SNAKE> env > this file > built-in default

# --- Identity ---
# name: "Living Room Server"

# --- Network ---
# port: 8927
# no_mdns: false
# discover_clients: false

# --- Audio source ---
# Local file path, HTTP URL, or HLS URL. Empty = built-in test tone.
# audio: "/srv/music/radio.m3u8"

# --- Logging / runtime ---
# daemon: false
# no_tui: false
# log_file: "sendspin-server.log"
# debug: false
```

### Makefile

Split `install-daemon` / `uninstall-daemon` into leaf + aggregate targets:

- `install-player-daemon` — existing player install logic, renamed.
- `install-server-daemon` — new; installs binary to `/usr/local/bin`, unit file to `/etc/systemd/system`, env file to `/etc/default/sendspin-server`, example YAML to `/etc/sendspin/server.yaml`. Each of the two editable files is installed only if absent (guard pattern from the player).
- `install-daemon` — aggregate: `install-player-daemon install-server-daemon`.
- `uninstall-server-daemon` — stops/disables unit, removes binary and unit file, leaves `/etc/default/sendspin-server` and `/etc/sendspin/server.yaml` intact.
- `uninstall-daemon` — aggregate.

`clean` already removes `sendspin-server`; no change.

## Flag → YAML key mapping

| Flag | YAML key | Type | Default |
|---|---|---|---|
| `-port` | `port` | `*int` | 8927 |
| `-name` | `name` | `string` | `<hostname>-sendspin-server` |
| `-log-file` | `log_file` | `string` | `sendspin-server.log` |
| `-debug` | `debug` | `*bool` | false |
| `-no-mdns` | `no_mdns` | `*bool` | false |
| `-no-tui` | `no_tui` | `*bool` | false |
| `-audio` | `audio` | `string` | *(empty → test tone)* |
| `-discover-clients` | `discover_clients` | `*bool` | false |
| `-daemon` | `daemon` | `*bool` | false |
| `-config` | *(not in YAML — circular)* | `string` | *(empty)* |

## Precedence

For every key: **CLI flag > `SENDSPIN_SERVER_<UPPER_SNAKE>` env > `server.yaml` > built-in default**.

Matches the player exactly. Enforced by the shared `ApplyEnvAndFile` helper.

## Error handling

- Invalid YAML → `log.Fatalf("config: %v", err)` at startup.
- Invalid env var (e.g., `SENDSPIN_SERVER_PORT=abc`) → fatal; error message includes the offending flag name. Behavior inherited from the shared helper.
- Missing config file → silent no-op; all keys fall through to flag defaults. This is the documented contract.
- `-audio` validation stays in `server.NewAudioSource`; config loading only carries the string.
- `-daemon` combined with TUI flags → daemon wins, no warning. Matches player behavior.

## Testing

**Existing player tests** (`pkg/sendspin/config_test.go`) — each `ApplyEnvAndFile(..., cfg)` call-site must be rewritten to `ApplyEnvAndFile(..., PlayerEnvPrefix, cfg.asStringMap())` to match the new signature. `TestApplyEnvAndFile_NilConfigStillHonorsEnv` passes a nil map instead of a nil struct (a nil `map[string]string` iterates as empty, so the env-only path behaves identically). No assertion changes; the precedence tests then exercise the shared code path for both binaries.

**New server tests** — structural coverage only:

- `TestLoadServerConfig_ExplicitPathWithAllKeys` — round-trip every `ServerConfigFile` field through YAML.
- `TestLoadServerConfig_MissingFileIsNotAnError` — contract symmetry.
- `TestLoadServerConfig_EnvPathHonored` — `SENDSPIN_SERVER_CONFIG` picked up.
- `TestApplyEnvAndFile_ServerEnvPrefix` — confirms the generalized `envPrefix` parameter routes `SENDSPIN_SERVER_*` correctly.

No new `WriteStringKey` tests (server doesn't use write-back).

## Manual verification (part of the plan's completion step)

1. `sendspin-server` with no config file → unchanged behavior.
2. `sendspin-server --config /tmp/s.yaml` with `port: 9000` → binds 9000.
3. `SENDSPIN_SERVER_PORT=9001 sendspin-server --config /tmp/s.yaml` → binds 9001 (env beats file).
4. `sendspin-server --port 9002 --config /tmp/s.yaml` → binds 9002 (CLI beats env + file).
5. `sendspin-server --daemon` → no TUI, stdout logging with timestamps, no `sendspin-server.log` created.
6. Linux box: `make install-daemon && systemctl enable --now sendspin-server && journalctl -u sendspin-server -f` → clean startup.

## Out-of-scope (future work)

- `server_id` / persistent identity for the server.
- A unified `pkg/sendspin/configfile` sub-package if a third binary ever joins (YAGNI until then).
- README deep-dive on daemon operation — this work adds a one-line pointer only.
