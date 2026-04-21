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
