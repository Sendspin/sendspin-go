# sendspin-server YAML config + --daemon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `sendspin-server` a YAML config file, `SENDSPIN_SERVER_*` env overlay, and a `--daemon` flag, plus the distribution artifacts needed to run it under systemd. Parity with `sendspin-player`.

**Architecture:** Refactor `pkg/sendspin/config.go` so the generic machinery (flag overlay, YAML load, config-dir lookup) takes `(envPrefix, map[string]string)` instead of a typed `*PlayerConfigFile`. Add a parallel `ServerConfigFile` / `LoadServerConfig` surface using the shared helpers. Wire the server CLI to load it. Ship `sendspin-server.service`, `sendspin-server.env`, and `server.example.yaml`; split `install-daemon` into per-binary leaf targets.

**Tech Stack:** Go 1.24+, `gopkg.in/yaml.v3`, Go stdlib `flag` package, systemd unit files, GNU Make.

**Spec:** `docs/superpowers/specs/2026-04-20-server-config-yaml-design.md`

**Environment note (from `memory/reference_cgo_toolchain.md`):** on Windows/MSYS2, prepend `/c/msys64/mingw64/bin` to `PATH` before any full-module build or test (`make`, `go build ./...`, `go test ./...`). The `pkg/sendspin` tests alone usually work without it — scope test runs narrowly unless told otherwise.

**Flaky test:** `TestServerStartStop` has been flaky on port 8929 on Chris's box (`memory/project_test_port_8929.md`) — not a regression. If it fails in the final verification step, skip it and move on.

---

## File Map

**Modify:**
- `pkg/sendspin/config.go` — refactor `ApplyEnvAndFile` signature; extract helpers; add server surface.
- `pkg/sendspin/config_test.go` — update existing call-sites for the new signature; add server tests.
- `main.go` (player, repo root) — single-line call-site update.
- `cmd/sendspin-server/main.go` — add `--config` and `--daemon` flags, wire config loading, daemon-mode logging branch.
- `Makefile` — split install/uninstall targets.
- `README.md` — add Server config-file section + daemon pointer.

**Create:**
- `dist/systemd/sendspin-server.service`
- `dist/systemd/sendspin-server.env`
- `dist/config/server.example.yaml`

---

### Task 1: Refactor `ApplyEnvAndFile` to take `(envPrefix, fileValues)` instead of a typed struct

**Files:**
- Modify: `pkg/sendspin/config.go:105-127`
- Modify: `pkg/sendspin/config_test.go:151, 186, 204, 219`
- Modify: `main.go:68` (player repo-root)

This is a pure signature refactor — no new behavior. All changes land in one commit because they have to compile together.

- [ ] **Step 1: Update the function signature and body**

Replace the existing `ApplyEnvAndFile` in `pkg/sendspin/config.go` with:

```go
// ApplyEnvAndFile overlays <envPrefix> env vars and YAML config-file values
// into the given FlagSet, but only for flags the user did NOT set on the CLI.
// Precedence: CLI (untouched here) > env > file > flag default.
//
// envPrefix is the namespace for env-var lookups (e.g. "SENDSPIN_PLAYER_").
// fileValues is the flat flag-key → value map the caller derives from its
// typed config struct (see PlayerConfigFile.asStringMap and
// ServerConfigFile.asStringMap). A nil map is treated as empty.
//
// setByUser is typically built with flag.Visit before calling this.
func ApplyEnvAndFile(fs *flag.FlagSet, setByUser map[string]bool, envPrefix string, fileValues map[string]string) error {
	var firstErr error
	fs.VisitAll(func(f *flag.Flag) {
		if firstErr != nil || setByUser[f.Name] {
			return
		}
		envKey := envPrefix + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if val, ok := os.LookupEnv(envKey); ok {
			if err := fs.Set(f.Name, val); err != nil {
				firstErr = fmt.Errorf("env %s -> -%s: %w", envKey, f.Name, err)
			}
			return
		}
		configKey := strings.ReplaceAll(f.Name, "-", "_")
		if val, ok := fileValues[configKey]; ok {
			if err := fs.Set(f.Name, val); err != nil {
				firstErr = fmt.Errorf("config %s -> -%s: %w", configKey, f.Name, err)
			}
		}
	})
	return firstErr
}
```

- [ ] **Step 2: Update player call-site in `main.go`**

Find the line (around `main.go:68`):

```go
if err := sendspin.ApplyEnvAndFile(flag.CommandLine, setByUser, cfg); err != nil {
```

Replace with:

```go
if err := sendspin.ApplyEnvAndFile(flag.CommandLine, setByUser, sendspin.PlayerEnvPrefix, cfg.asStringMap()); err != nil {
```

Wait — `asStringMap` is unexported and lives in package `sendspin`. From `main.go` (package `main`), we can't call it directly. We need either (a) export it, or (b) have the player pass the raw struct through a small exported wrapper.

Choose (a) — rename `asStringMap` to `AsStringMap` on `*PlayerConfigFile` and (in a later task) on `*ServerConfigFile`. Make this rename part of Step 1: update the method definition in `config.go:132` and every reference in `config_test.go`.

So replace the block above with two edits:

In `pkg/sendspin/config.go:132`:

```go
// Before
func (c *PlayerConfigFile) asStringMap() map[string]string {

// After
func (c *PlayerConfigFile) AsStringMap() map[string]string {
```

In `main.go` (player):

```go
if err := sendspin.ApplyEnvAndFile(flag.CommandLine, setByUser, sendspin.PlayerEnvPrefix, cfg.AsStringMap()); err != nil {
```

- [ ] **Step 3: Update the four existing test call-sites in `config_test.go`**

Line 151 (`TestApplyEnvAndFile_FileFillsUnsetFlags`):

```go
// Before
if err := ApplyEnvAndFile(fs, setByUser, cfg); err != nil {

// After
if err := ApplyEnvAndFile(fs, setByUser, PlayerEnvPrefix, cfg.AsStringMap()); err != nil {
```

Line 186 (`TestApplyEnvAndFile_EnvBeatsFile`):

```go
// Before
if err := ApplyEnvAndFile(fs, map[string]bool{}, cfg); err != nil {

// After
if err := ApplyEnvAndFile(fs, map[string]bool{}, PlayerEnvPrefix, cfg.AsStringMap()); err != nil {
```

Line 204 (`TestApplyEnvAndFile_NilConfigStillHonorsEnv`):

```go
// Before
if err := ApplyEnvAndFile(fs, map[string]bool{}, nil); err != nil {

// After
if err := ApplyEnvAndFile(fs, map[string]bool{}, PlayerEnvPrefix, nil); err != nil {
```

Line 219 (`TestApplyEnvAndFile_InvalidEnvReturnsError`):

```go
// Before
err := ApplyEnvAndFile(fs, map[string]bool{}, nil)

// After
err := ApplyEnvAndFile(fs, map[string]bool{}, PlayerEnvPrefix, nil)
```

- [ ] **Step 4: Run the package tests and verify they still pass**

```bash
go test ./pkg/sendspin/ -run 'TestLoadPlayerConfig|TestApplyEnvAndFile' -v
```

Expected: all 5 tests PASS (`TestLoadPlayerConfig_ExplicitPathWithAllKeys`, `..._MissingFileIsNotAnError`, `..._InvalidYAMLIsAnError`, `..._EnvPathHonored`, and the four `TestApplyEnvAndFile_*` plus `TestWriteStringKey_*`).

If cgo build errors appear (e.g., miniaudio), prepend `/c/msys64/mingw64/bin` to `PATH` per the memory note and retry.

- [ ] **Step 5: Build the player to catch any lingering call-site issues**

```bash
go build -o /dev/null .
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/sendspin/config.go pkg/sendspin/config_test.go main.go
git commit -m "refactor(sendspin): ApplyEnvAndFile takes envPrefix+map, not typed struct

Prepares the way for a second consumer (sendspin-server) to share the
same flag-overlay machinery. Player call-site passes cfg.AsStringMap()
and sendspin.PlayerEnvPrefix explicitly. asStringMap renamed to
AsStringMap so callers outside pkg/sendspin can use it.

No behavior change. All existing tests pass unchanged except for
signature updates at four call-sites."
```

---

### Task 2: Extract `loadYAMLConfig` helper

**Files:**
- Modify: `pkg/sendspin/config.go`

Pull the generic "search paths, open first existing, YAML-unmarshal into `out`" logic out of `LoadPlayerConfig` so the upcoming `LoadServerConfig` can call it too. Pure refactor.

- [ ] **Step 1: Add the helper function**

Add above `LoadPlayerConfig`:

```go
// loadYAMLConfig walks searchPaths, and for the first one that exists opens
// the file and unmarshals into out. It returns the path that was loaded, or
// empty if no candidate existed. A missing file is not an error; I/O or
// parse errors are returned as-is with the offending path attached.
func loadYAMLConfig(searchPaths []string, out any) (string, error) {
	for _, candidate := range searchPaths {
		if candidate == "" {
			continue
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return candidate, fmt.Errorf("read %s: %w", candidate, err)
		}
		if err := yaml.Unmarshal(data, out); err != nil {
			return candidate, fmt.Errorf("parse %s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", nil
}
```

- [ ] **Step 2: Refactor `LoadPlayerConfig` to use it**

Replace the body:

```go
func LoadPlayerConfig(explicitPath string) (*PlayerConfigFile, string, error) {
	var cfg PlayerConfigFile
	used, err := loadYAMLConfig(playerConfigSearchPaths(explicitPath), &cfg)
	if err != nil {
		return nil, used, err
	}
	if used == "" {
		return nil, "", nil
	}
	return &cfg, used, nil
}
```

- [ ] **Step 3: Run the package tests**

```bash
go test ./pkg/sendspin/ -run 'TestLoadPlayerConfig' -v
```

Expected: all `TestLoadPlayerConfig_*` tests PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/sendspin/config.go
git commit -m "refactor(sendspin): extract loadYAMLConfig helper

Prepares for a second consumer. No behavior change."
```

---

### Task 3: Extract `userConfigPath` helper

**Files:**
- Modify: `pkg/sendspin/config.go`

- [ ] **Step 1: Add the helper**

Add above `DefaultPlayerConfigPath`:

```go
// userConfigPath returns <UserConfigDir>/sendspin/<relative>. Matches the
// canonical path layout for both player.yaml and server.yaml.
func userConfigPath(relative string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "sendspin", relative), nil
}
```

- [ ] **Step 2: Refactor `DefaultPlayerConfigPath` and `playerConfigSearchPaths`**

Replace `DefaultPlayerConfigPath`:

```go
func DefaultPlayerConfigPath() (string, error) {
	return userConfigPath("player.yaml")
}
```

In `playerConfigSearchPaths`, replace the `os.UserConfigDir()` block with:

```go
if p, err := userConfigPath("player.yaml"); err == nil {
	paths = append(paths, p)
}
```

- [ ] **Step 3: Run the package tests**

```bash
go test ./pkg/sendspin/ -v
```

Expected: all tests in the package PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/sendspin/config.go
git commit -m "refactor(sendspin): extract userConfigPath helper

Shared between DefaultPlayerConfigPath and playerConfigSearchPaths;
ready for the server's analogous use. No behavior change."
```

---

### Task 4: Add `ServerConfigFile` + `LoadServerConfig` with TDD tests

**Files:**
- Modify: `pkg/sendspin/config.go` — add server surface.
- Create: `pkg/sendspin/config_server_test.go` — new test file, keeps server tests adjacent to player tests but separately readable.

TDD: write the failing tests first, then the implementation.

- [ ] **Step 1: Write the failing tests**

Create `pkg/sendspin/config_server_test.go`:

```go
// ABOUTME: Tests for YAML config loading and env prefix routing for sendspin-server
package sendspin

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConfig_ExplicitPathWithAllKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	body := `# Test server
name: "Living Room Server"
port: 9000
log_file: "custom-server.log"
debug: true
no_mdns: true
no_tui: true
audio: "/srv/music/radio.m3u8"
discover_clients: true
daemon: true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, used, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if used != path {
		t.Errorf("used = %q, want %q", used, path)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	if cfg.Name != "Living Room Server" {
		t.Errorf("name = %q", cfg.Name)
	}
	if cfg.Port == nil || *cfg.Port != 9000 {
		t.Errorf("port = %v, want 9000", cfg.Port)
	}
	if cfg.LogFile != "custom-server.log" {
		t.Errorf("log_file = %q", cfg.LogFile)
	}
	if cfg.Debug == nil || !*cfg.Debug {
		t.Errorf("debug = %v, want true", cfg.Debug)
	}
	if cfg.NoMDNS == nil || !*cfg.NoMDNS {
		t.Errorf("no_mdns = %v, want true", cfg.NoMDNS)
	}
	if cfg.NoTUI == nil || !*cfg.NoTUI {
		t.Errorf("no_tui = %v, want true", cfg.NoTUI)
	}
	if cfg.Audio != "/srv/music/radio.m3u8" {
		t.Errorf("audio = %q", cfg.Audio)
	}
	if cfg.DiscoverClients == nil || !*cfg.DiscoverClients {
		t.Errorf("discover_clients = %v, want true", cfg.DiscoverClients)
	}
	if cfg.Daemon == nil || !*cfg.Daemon {
		t.Errorf("daemon = %v, want true", cfg.Daemon)
	}
}

func TestLoadServerConfig_MissingFileIsNotAnError(t *testing.T) {
	cfg, used, err := LoadServerConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg != nil || used != "" {
		t.Errorf("expected nil/empty for missing file, got cfg=%v path=%q", cfg, used)
	}
}

func TestLoadServerConfig_EnvPathHonored(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "from-env-server.yaml")
	if err := os.WriteFile(envPath, []byte("name: EnvServer\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("SENDSPIN_SERVER_CONFIG", envPath)

	cfg, used, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if used != envPath {
		t.Errorf("used = %q, want %q", used, envPath)
	}
	if cfg.Name != "EnvServer" {
		t.Errorf("name = %q", cfg.Name)
	}
}

// TestApplyEnvAndFile_ServerEnvPrefix confirms the generalized envPrefix
// parameter routes SENDSPIN_SERVER_* correctly. Precedence rules themselves
// are already covered by the player tests; this is pure plumbing.
func TestApplyEnvAndFile_ServerEnvPrefix(t *testing.T) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	port := fs.Int("port", 8927, "port")
	audio := fs.String("audio", "", "audio source")
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Setenv("SENDSPIN_SERVER_PORT", "9999")
	t.Setenv("SENDSPIN_SERVER_AUDIO", "/srv/env.flac")

	if err := ApplyEnvAndFile(fs, map[string]bool{}, ServerEnvPrefix, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *port != 9999 {
		t.Errorf("port = %d, want 9999", *port)
	}
	if *audio != "/srv/env.flac" {
		t.Errorf("audio = %q", *audio)
	}
}
```

- [ ] **Step 2: Run the tests — confirm they fail to compile**

```bash
go test ./pkg/sendspin/ -run 'TestLoadServerConfig|TestApplyEnvAndFile_ServerEnvPrefix' -v
```

Expected: compile error — `undefined: LoadServerConfig`, `undefined: ServerEnvPrefix`, `undefined: ServerConfigFile` (fields).

- [ ] **Step 3: Add the server surface to `config.go`**

Append to `pkg/sendspin/config.go`:

```go
// ServerEnvPrefix is the namespace for environment overrides of server
// config values. Env key = ServerEnvPrefix + upper-snake(flag name).
// Example: "-no-mdns" -> SENDSPIN_SERVER_NO_MDNS.
const ServerEnvPrefix = "SENDSPIN_SERVER_"

// ServerConfigFile mirrors the server's CLI flags. Fields with "zero" values
// that could reasonably be meaningful (bool) are pointers so absence in
// the YAML can be distinguished from an explicit false.
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

// LoadServerConfig searches for a server.yaml and returns its parsed contents
// along with the path that was loaded (empty if none was found).
//
// Search order (first existing wins):
//  1. explicitPath if non-empty
//  2. $SENDSPIN_SERVER_CONFIG if set
//  3. $XDG_CONFIG_HOME or OS equivalent + /sendspin/server.yaml
//  4. /etc/sendspin/server.yaml
//
// A missing file is not an error; the caller gets (nil, "", nil).
func LoadServerConfig(explicitPath string) (*ServerConfigFile, string, error) {
	var cfg ServerConfigFile
	used, err := loadYAMLConfig(serverConfigSearchPaths(explicitPath), &cfg)
	if err != nil {
		return nil, used, err
	}
	if used == "" {
		return nil, "", nil
	}
	return &cfg, used, nil
}

// DefaultServerConfigPath returns the canonical user-level server.yaml path
// for this OS.
func DefaultServerConfigPath() (string, error) {
	return userConfigPath("server.yaml")
}

func serverConfigSearchPaths(explicit string) []string {
	paths := make([]string, 0, 4)
	if explicit != "" {
		paths = append(paths, explicit)
	}
	if env := os.Getenv("SENDSPIN_SERVER_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	if p, err := userConfigPath("server.yaml"); err == nil {
		paths = append(paths, p)
	}
	paths = append(paths, "/etc/sendspin/server.yaml")
	return paths
}

// AsStringMap returns only the keys the user actually set in the YAML, as
// strings suitable for flag.Set. Absent keys are omitted so the overlay
// correctly falls through to the flag default.
func (c *ServerConfigFile) AsStringMap() map[string]string {
	m := make(map[string]string)
	if c == nil {
		return m
	}
	if c.Name != "" {
		m["name"] = c.Name
	}
	if c.Port != nil {
		m["port"] = strconv.Itoa(*c.Port)
	}
	if c.LogFile != "" {
		m["log_file"] = c.LogFile
	}
	if c.Debug != nil {
		m["debug"] = strconv.FormatBool(*c.Debug)
	}
	if c.NoMDNS != nil {
		m["no_mdns"] = strconv.FormatBool(*c.NoMDNS)
	}
	if c.NoTUI != nil {
		m["no_tui"] = strconv.FormatBool(*c.NoTUI)
	}
	if c.Audio != "" {
		m["audio"] = c.Audio
	}
	if c.DiscoverClients != nil {
		m["discover_clients"] = strconv.FormatBool(*c.DiscoverClients)
	}
	if c.Daemon != nil {
		m["daemon"] = strconv.FormatBool(*c.Daemon)
	}
	return m
}
```

- [ ] **Step 4: Run the server tests — confirm they pass**

```bash
go test ./pkg/sendspin/ -run 'TestLoadServerConfig|TestApplyEnvAndFile_ServerEnvPrefix' -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Run the full package test suite to confirm no regressions**

```bash
go test ./pkg/sendspin/ -v
```

Expected: every test in the package PASSES.

- [ ] **Step 6: Commit**

```bash
git add pkg/sendspin/config.go pkg/sendspin/config_server_test.go
git commit -m "feat(sendspin): ServerConfigFile + LoadServerConfig

Mirrors LoadPlayerConfig: CLI > env > ~/.config/sendspin/server.yaml >
/etc/sendspin/server.yaml. Pointer bools so absence in YAML is distinct
from explicit false. AsStringMap() feeds the shared ApplyEnvAndFile
helper with SENDSPIN_SERVER_ as the env prefix."
```

---

### Task 5: Wire `--config` and `--daemon` into `cmd/sendspin-server/main.go`

**Files:**
- Modify: `cmd/sendspin-server/main.go`

This is the single user-visible change to the server binary. Touches flag declarations, logging setup, and the config-overlay call site.

- [ ] **Step 1: Add the two new flag declarations**

In the `var (...)` block at the top, add (order matches the player's convention — `config` first, `daemon` near `no-tui`):

```go
var (
	port            = flag.Int("port", 8927, "WebSocket server port")
	name            = flag.String("name", "", "Server friendly name (default: hostname-sendspin-server)")
	logFile         = flag.String("log-file", "sendspin-server.log", "Log file path")
	debug           = flag.Bool("debug", false, "Enable debug logging")
	noMDNS          = flag.Bool("no-mdns", false, "Disable mDNS advertisement")
	noTUI           = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	audioFile       = flag.String("audio", "", "Audio source to stream (MP3, FLAC, HTTP URL, HLS). Default: test tone")
	discoverClients = flag.Bool("discover-clients", false, "Enable server-initiated discovery: browse _sendspin._tcp and dial out to clients")
	daemon          = flag.Bool("daemon", false, "Daemon mode: log to stdout only (journalctl-friendly), no TUI, no log file")
	configPath      = flag.String("config", "", "Path to server.yaml config file. Default search: $SENDSPIN_SERVER_CONFIG, ~/.config/sendspin/server.yaml, /etc/sendspin/server.yaml.")
)
```

- [ ] **Step 2: Wire config loading after `flag.Parse()`**

Replace the opening of `main()` (current lines 30-33) with:

```go
func main() {
	flag.Parse()

	// Overlay YAML file and SENDSPIN_SERVER_* env vars onto flag vars for
	// anything the user didn't set on the CLI. --config is excluded because
	// putting it in the config file would be circular.
	setByUser := map[string]bool{"config": true}
	flag.Visit(func(f *flag.Flag) { setByUser[f.Name] = true })

	cfg, _, err := sendspin.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := sendspin.ApplyEnvAndFile(flag.CommandLine, setByUser, sendspin.ServerEnvPrefix, cfg.AsStringMap()); err != nil {
		log.Fatalf("config overlay: %v", err)
	}

	useTUI := !(*noTUI || *daemon)
```

(`cfg.AsStringMap()` is safe on a nil `cfg` — returns an empty map.)

- [ ] **Step 3: Replace the logging-setup block with a daemon-aware branch**

Current block (around lines 35-46) opens the log file unconditionally. Replace with:

```go
	if *daemon {
		// Daemon mode: log to stdout only. systemd/journalctl captures stdout
		// and adds its own timestamps, so we keep ours for grep-ability.
		log.SetOutput(os.Stdout)
	} else {
		f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		defer f.Close()

		if useTUI {
			// Log to file only when TUI is running; otherwise the log would stomp the TUI
			log.SetOutput(f)
		} else {
			log.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}
```

- [ ] **Step 4: Add the daemon-mode banner line**

Find the existing non-TUI startup log (around line 57-59):

```go
	if !useTUI {
		log.Printf("Starting Sendspin Server: %s on port %d", serverName, *port)
	}
```

Replace with:

```go
	if !useTUI {
		log.Printf("Starting Sendspin Server: %s on port %d", serverName, *port)
		if *daemon {
			log.Printf("Daemon mode: logging to stdout only")
		}
	}
```

- [ ] **Step 5: Build the server binary**

```bash
make server
```

Expected: builds cleanly. If cgo errors appear, prepend `/c/msys64/mingw64/bin` to `PATH` first.

- [ ] **Step 6: Smoke test 1 — default (no config, no daemon)**

```bash
./sendspin-server --no-tui
```

Expected: "Starting Sendspin Server: ... on port 8927" in stdout; `sendspin-server.log` appears in cwd; mDNS advertisement starts. Ctrl+C to exit.

- [ ] **Step 7: Smoke test 2 — config file overrides port**

Create `/tmp/s.yaml` (or equivalent) with:
```yaml
port: 9000
```

Run:
```bash
./sendspin-server --no-tui --config /tmp/s.yaml
```

Expected: banner says `port 9000`. Ctrl+C.

- [ ] **Step 8: Smoke test 3 — env beats file**

```bash
SENDSPIN_SERVER_PORT=9001 ./sendspin-server --no-tui --config /tmp/s.yaml
```

Expected: banner says `port 9001`.

- [ ] **Step 9: Smoke test 4 — CLI beats env + file**

```bash
SENDSPIN_SERVER_PORT=9001 ./sendspin-server --no-tui --port 9002 --config /tmp/s.yaml
```

Expected: banner says `port 9002`.

- [ ] **Step 10: Smoke test 5 — daemon mode**

Delete any stale `sendspin-server.log` in cwd first. Then:

```bash
./sendspin-server --daemon
```

Expected: logs to stdout (no TUI), banner includes "Daemon mode: logging to stdout only", no `sendspin-server.log` file created. Ctrl+C to exit, then:

```bash
ls sendspin-server.log 2>&1 || echo "confirmed: no log file created"
```

- [ ] **Step 11: Commit**

```bash
git add cmd/sendspin-server/main.go
git commit -m "feat(server): --config and --daemon flags

Matches the player's config surface: YAML search (CLI > env >
~/.config/sendspin/server.yaml > /etc/sendspin/server.yaml), env
overlay via SENDSPIN_SERVER_*, and --daemon (stdout-only logging,
no TUI, no log file) for journalctl-friendly operation under systemd."
```

---

### Task 6: Create `dist/systemd/sendspin-server.service`

**Files:**
- Create: `dist/systemd/sendspin-server.service`

- [ ] **Step 1: Write the unit file**

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
# Pass CLI flags via SENDSPIN_SERVER_OPTS in the environment file.
# ExecStart is overridden below to append them.
ExecStart=
ExecStart=/usr/local/bin/sendspin-server --daemon $SENDSPIN_SERVER_OPTS

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Commit**

```bash
git add dist/systemd/sendspin-server.service
git commit -m "feat(dist): systemd unit for sendspin-server

Mirrors sendspin-player.service. ProtectHome=read-only (not =yes) so
operators can use --audio /home/user/Music/... under systemd without
fighting the sandbox."
```

---

### Task 7: Create `dist/systemd/sendspin-server.env`

**Files:**
- Create: `dist/systemd/sendspin-server.env`

- [ ] **Step 1: Write the env file**

```sh
# sendspin-server: extra CLI flags appended to ExecStart
# Prefer /etc/sendspin/server.yaml for structured config; use this for
# ad-hoc overrides or keys that aren't (yet) in the YAML.
# SENDSPIN_SERVER_OPTS="--debug --discover-clients"
SENDSPIN_SERVER_OPTS=""
```

- [ ] **Step 2: Commit**

```bash
git add dist/systemd/sendspin-server.env
git commit -m "feat(dist): /etc/default/sendspin-server starter"
```

---

### Task 8: Create `dist/config/server.example.yaml`

**Files:**
- Create: `dist/config/server.example.yaml`

- [ ] **Step 1: Write the annotated example**

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
#
# Every key is optional. Uncomment and adjust as needed.

# ---------------------------------------------------------------------------
# Identity
# ---------------------------------------------------------------------------

# Friendly name shown in Music Assistant and via mDNS.
# Default: <hostname>-sendspin-server
# name: "Living Room Server"

# ---------------------------------------------------------------------------
# Network
# ---------------------------------------------------------------------------

# WebSocket server port.
# port: 8927

# Disable mDNS advertisement. Clients must then connect manually.
# no_mdns: false

# Enable server-initiated discovery: browse _sendspin._tcp and dial out
# to clients. Useful for players that can't initiate connections.
# discover_clients: false

# ---------------------------------------------------------------------------
# Audio source
# ---------------------------------------------------------------------------

# Local file path, HTTP URL, or HLS URL. Empty = built-in test tone.
# Examples:
#   audio: "/srv/music/ambient.flac"
#   audio: "http://example.com/stream.mp3"
#   audio: "https://stream.radiofrance.fr/fip/fip.m3u8"
# audio: ""

# ---------------------------------------------------------------------------
# Runtime / logging
# ---------------------------------------------------------------------------

# Daemon mode: log to stdout only (journalctl-friendly), no TUI, no log
# file. Recommended when running under systemd.
# daemon: false

# Disable the interactive terminal UI, stream logs to stdout instead.
# no_tui: false

# Log file path. Ignored when daemon mode is on.
# log_file: "sendspin-server.log"

# Enable verbose debug logging.
# debug: false
```

- [ ] **Step 2: Commit**

```bash
git add dist/config/server.example.yaml
git commit -m "docs(server): annotated server.yaml starter"
```

---

### Task 9: Split Makefile `install-daemon` / `uninstall-daemon` into leaves + aggregates

**Files:**
- Modify: `Makefile` (lines ~1-6 phony list, and the block at 85-117)

The existing `install-daemon` target (line 85) installs the player only. Rewrite so that `install-daemon` becomes an aggregate of per-binary leaf targets, and a matching `install-server-daemon` target installs the new artifacts.

- [ ] **Step 1: Update the `.PHONY` declaration**

At the top of the Makefile (around line 5), add the new target names:

```make
.PHONY: all clean test test-verbose test-coverage test-race lint help \
	build-all build-linux build-darwin help conformance \
	install-daemon uninstall-daemon \
	install-player-daemon uninstall-player-daemon \
	install-server-daemon uninstall-server-daemon
```

(Exact existing variable names may differ; preserve the existing list and append the new targets.)

- [ ] **Step 2: Rename the existing `install-daemon` body to `install-player-daemon`**

Replace the current `install-daemon: player` stanza (lines 85-107) with:

```make
# Install sendspin-player as a systemd daemon
install-player-daemon: player
	@echo "Installing sendspin-player daemon..."
	install -m 755 sendspin-player /usr/local/bin/sendspin-player
	install -m 644 dist/systemd/sendspin-player.service /etc/systemd/system/sendspin-player.service
	@if [ ! -f /etc/default/sendspin-player ]; then \
		install -m 644 dist/systemd/sendspin-player.env /etc/default/sendspin-player; \
		echo "Created /etc/default/sendspin-player — edit this file to configure."; \
	else \
		echo "/etc/default/sendspin-player already exists, not overwriting."; \
	fi
	@if [ ! -f /etc/sendspin/player.yaml ]; then \
		install -d -m 755 /etc/sendspin; \
		install -m 644 dist/config/player.example.yaml /etc/sendspin/player.yaml; \
		echo "Created /etc/sendspin/player.yaml — edit this file to configure."; \
	else \
		echo "/etc/sendspin/player.yaml already exists, not overwriting."; \
	fi
	systemctl daemon-reload
	@echo ""
	@echo "Installed. To start:"
	@echo "  sudo systemctl enable --now sendspin-player"
	@echo ""
	@echo "Configure via /etc/sendspin/player.yaml (preferred) or /etc/default/sendspin-player"
```

- [ ] **Step 3: Add the new `install-server-daemon` target**

Append directly below:

```make
# Install sendspin-server as a systemd daemon
install-server-daemon: server
	@echo "Installing sendspin-server daemon..."
	install -m 755 sendspin-server /usr/local/bin/sendspin-server
	install -m 644 dist/systemd/sendspin-server.service /etc/systemd/system/sendspin-server.service
	@if [ ! -f /etc/default/sendspin-server ]; then \
		install -m 644 dist/systemd/sendspin-server.env /etc/default/sendspin-server; \
		echo "Created /etc/default/sendspin-server — edit this file to configure."; \
	else \
		echo "/etc/default/sendspin-server already exists, not overwriting."; \
	fi
	@if [ ! -f /etc/sendspin/server.yaml ]; then \
		install -d -m 755 /etc/sendspin; \
		install -m 644 dist/config/server.example.yaml /etc/sendspin/server.yaml; \
		echo "Created /etc/sendspin/server.yaml — edit this file to configure."; \
	else \
		echo "/etc/sendspin/server.yaml already exists, not overwriting."; \
	fi
	systemctl daemon-reload
	@echo ""
	@echo "Installed. To start:"
	@echo "  sudo systemctl enable --now sendspin-server"
	@echo ""
	@echo "Configure via /etc/sendspin/server.yaml (preferred) or /etc/default/sendspin-server"

# Aggregate: install both binaries as systemd daemons
install-daemon: install-player-daemon install-server-daemon
```

- [ ] **Step 4: Rename the existing `uninstall-daemon` body to `uninstall-player-daemon` and add the server counterpart + aggregate**

Replace the current `uninstall-daemon` stanza (lines 110-117) with:

```make
# Uninstall the sendspin-player systemd daemon
uninstall-player-daemon:
	@echo "Removing sendspin-player daemon..."
	-systemctl stop sendspin-player 2>/dev/null
	-systemctl disable sendspin-player 2>/dev/null
	rm -f /etc/systemd/system/sendspin-player.service
	rm -f /usr/local/bin/sendspin-player
	systemctl daemon-reload
	@echo "Removed. /etc/default/sendspin-player and /etc/sendspin/player.yaml left in place (manual cleanup if desired)."

# Uninstall the sendspin-server systemd daemon
uninstall-server-daemon:
	@echo "Removing sendspin-server daemon..."
	-systemctl stop sendspin-server 2>/dev/null
	-systemctl disable sendspin-server 2>/dev/null
	rm -f /etc/systemd/system/sendspin-server.service
	rm -f /usr/local/bin/sendspin-server
	systemctl daemon-reload
	@echo "Removed. /etc/default/sendspin-server and /etc/sendspin/server.yaml left in place (manual cleanup if desired)."

# Aggregate: uninstall both daemons
uninstall-daemon: uninstall-player-daemon uninstall-server-daemon
```

- [ ] **Step 5: Update the `help` target**

Find the help-text block (around line 168). Replace the single daemon line:

```make
	@echo "  make install-daemon   - Install as systemd service (Linux, requires root)"
	@echo "  make uninstall-daemon - Remove systemd service"
```

With:

```make
	@echo "  make install-daemon           - Install both player and server as systemd daemons (Linux, requires root)"
	@echo "  make install-player-daemon    - Install only the player daemon"
	@echo "  make install-server-daemon    - Install only the server daemon"
	@echo "  make uninstall-daemon         - Remove both daemons"
	@echo "  make uninstall-player-daemon  - Remove only the player daemon"
	@echo "  make uninstall-server-daemon  - Remove only the server daemon"
```

- [ ] **Step 6: Sanity check — `make help` still parses**

```bash
make help
```

Expected: help text lists the new targets; no Make parse errors.

- [ ] **Step 7: Dry-run the new targets to confirm they parse**

```bash
make -n install-server-daemon
make -n install-daemon
make -n uninstall-server-daemon
make -n uninstall-daemon
```

Expected: each prints the commands it *would* run (the `install`, `rm`, `systemctl` calls), without actually executing anything. If Make reports missing variables or syntax errors, fix them before committing.

**Not run here:** actual `make install-daemon` execution requires root on a Linux box. Document in the final verification step for a Linux smoke test.

- [ ] **Step 8: Commit**

```bash
git add Makefile
git commit -m "build: split install-daemon into per-binary targets

install-daemon now aggregates install-player-daemon and
install-server-daemon (same for uninstall). Operators can install or
remove either side alone; running 'make install-daemon' still installs
both, preserving existing behavior."
```

---

### Task 10: README — add Server config-file section and daemon pointer

**Files:**
- Modify: `README.md` (around line 232, right after the "Server Options" list)

- [ ] **Step 1: Add `--config` and `--daemon` to the Server Options list**

Find the Server Options bullet list (around lines 222-232). After the existing `--no-tui` bullet at line 232, append:

```markdown
- `--daemon` - Daemon mode: log to stdout only (journalctl-friendly), no TUI, no log file. Recommended under systemd.
- `--config` - Path to `server.yaml` config file. Default search: `$SENDSPIN_SERVER_CONFIG`, `~/.config/sendspin/server.yaml`, `/etc/sendspin/server.yaml`.
```

- [ ] **Step 2: Add a `Configuration File (server.yaml)` subsection after the Server TUI section**

After the Server TUI block (around line 243, before the `### Player` heading at line 244), insert:

```markdown
#### Configuration File (`server.yaml`)

Every CLI flag has a matching key in `server.yaml`. Keys use `snake_case` (`--no-mdns` ↔ `no_mdns`).

A fully-commented starter file lives at [`dist/config/server.example.yaml`](dist/config/server.example.yaml) — copy it to `~/.config/sendspin/server.yaml` (user install) or `/etc/sendspin/server.yaml` (daemon) and uncomment the keys you want to set.

**Search order** (first existing file wins; missing is not an error):

1. `--config <path>` flag
2. `$SENDSPIN_SERVER_CONFIG`
3. `~/.config/sendspin/server.yaml` (macOS: `~/Library/Application Support/sendspin/server.yaml`; Windows: `%AppData%\sendspin\server.yaml`)
4. `/etc/sendspin/server.yaml` (daemon/system-wide)

**Value precedence**, for every flag: CLI > `SENDSPIN_SERVER_<UPPER_SNAKE>` env > `server.yaml` > built-in default.

#### Running under systemd

`make install-daemon` installs both `sendspin-player` and `sendspin-server` as systemd units. To install only one side:

```bash
sudo make install-server-daemon   # server only
sudo make install-player-daemon   # player only
```

Then enable and start the server:

```bash
sudo systemctl enable --now sendspin-server
journalctl -u sendspin-server -f
```

Configure via `/etc/sendspin/server.yaml` (preferred) or `SENDSPIN_SERVER_OPTS` in `/etc/default/sendspin-server`.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document server.yaml config and --daemon flag

Mirrors the existing player config docs. Adds a brief 'Running under
systemd' subsection covering install-daemon and the split per-binary
targets."
```

---

### Task 11: Final verification

No code changes — this is a verification pass. Run the full test suite and the full build to catch anything the per-task smoke tests missed.

- [ ] **Step 1: Run the full test suite**

```bash
go test ./... -short
```

Expected: all tests PASS.

**Known flake:** `TestServerStartStop` on port 8929 is a pre-existing environment issue on Chris's box (not introduced by this plan). If it's the only failure, record it and move on — do not chase it.

If cgo build errors appear, prepend `/c/msys64/mingw64/bin` to `PATH` and retry.

- [ ] **Step 2: Run the linter**

```bash
make lint
```

Expected: no lint failures.

- [ ] **Step 3: Build both binaries**

```bash
make
```

Expected: `sendspin-player` and `sendspin-server` built successfully.

- [ ] **Step 4: End-to-end server smoke — default**

```bash
./sendspin-server --no-tui
```

Expected: starts on port 8927, mDNS advertisement logs appear, Ctrl+C exits cleanly.

- [ ] **Step 5: End-to-end server smoke — config + env + CLI precedence**

Create `/tmp/s.yaml`:
```yaml
port: 9000
```

Verify the three-layer precedence:

```bash
./sendspin-server --no-tui --config /tmp/s.yaml
# Expected: port 9000

SENDSPIN_SERVER_PORT=9001 ./sendspin-server --no-tui --config /tmp/s.yaml
# Expected: port 9001

SENDSPIN_SERVER_PORT=9001 ./sendspin-server --no-tui --port 9002 --config /tmp/s.yaml
# Expected: port 9002
```

- [ ] **Step 6: End-to-end server smoke — daemon mode**

```bash
rm -f sendspin-server.log
./sendspin-server --daemon
# Ctrl+C
ls sendspin-server.log 2>&1 || echo "confirmed: no log file created"
```

Expected: no log file; stdout had the "Daemon mode: logging to stdout only" banner.

- [ ] **Step 7 (Linux-only, manual): systemd install smoke**

On a Linux box with the repo checked out:

```bash
sudo make install-server-daemon
sudo systemctl enable --now sendspin-server
journalctl -u sendspin-server -f
# Expected: "Starting Sendspin Server: ..." and "Daemon mode: ..." lines
sudo systemctl stop sendspin-server
sudo make uninstall-server-daemon
# Expected: /etc/sendspin/server.yaml and /etc/default/sendspin-server remain; binary + unit removed
```

If you're not on Linux, note this step as pending and flag it in the PR description.

- [ ] **Step 8: Player regression smoke**

Because Task 1 touched the player call-site, confirm the player still starts and connects:

```bash
./sendspin-player --no-tui --server ws://localhost:8927
```

Expected: mDNS-skipped direct connect, normal startup output. Ctrl+C exits cleanly.

- [ ] **Step 9: Final commit (only if anything was found and fixed)**

If steps 1-8 revealed issues that needed fixes, commit them with a descriptive message. If everything passed, nothing to commit here.

---

## Out-of-scope reminders

- No `server_id` persistent identity — explicitly deferred in the spec.
- No README deep-dive on daemon operation beyond the brief pointer above.
- No refactor of `pkg/sendspin` into a `configfile` sub-package — YAGNI until a third consumer arrives.
