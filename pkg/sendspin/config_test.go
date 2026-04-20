// ABOUTME: Tests for YAML config loading, env/file overlay precedence, and write-back round-trip
package sendspin

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPlayerConfig_ExplicitPathWithAllKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.yaml")
	body := `# My config
name: "Kitchen"
server: "192.168.1.100:8927"
port: 8999
buffer_ms: 250
static_delay_ms: 10
log_file: "custom.log"
no_tui: true
stream_logs: false
product_name: "Test Speaker"
manufacturer: "Acme"
no_reconnect: true
daemon: false
preferred_codec: "flac"
buffer_capacity: 2097152
client_id: "aa:bb:cc:dd:ee:ff"
audio_device: "USB Audio Device"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, used, err := LoadPlayerConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if used != path {
		t.Errorf("used path = %q, want %q", used, path)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	if cfg.Name != "Kitchen" || cfg.Server != "192.168.1.100:8927" {
		t.Errorf("string keys: %+v", cfg)
	}
	if cfg.Port == nil || *cfg.Port != 8999 {
		t.Errorf("port = %v, want 8999", cfg.Port)
	}
	if cfg.NoTUI == nil || !*cfg.NoTUI {
		t.Errorf("no_tui = %v, want true", cfg.NoTUI)
	}
	if cfg.StreamLogs == nil || *cfg.StreamLogs {
		t.Errorf("stream_logs = %v, want false", cfg.StreamLogs)
	}
	if cfg.ClientID != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("client_id = %q", cfg.ClientID)
	}
	if cfg.AudioDevice != "USB Audio Device" {
		t.Errorf("audio_device = %q", cfg.AudioDevice)
	}
}

func TestLoadPlayerConfig_MissingFileIsNotAnError(t *testing.T) {
	cfg, used, err := LoadPlayerConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg != nil || used != "" {
		t.Errorf("expected nil/empty for missing file, got cfg=%v path=%q", cfg, used)
	}
}

func TestLoadPlayerConfig_InvalidYAMLIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.yaml")
	if err := os.WriteFile(path, []byte("not: valid: : yaml"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := LoadPlayerConfig(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadPlayerConfig_EnvPathHonored(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "from-env.yaml")
	if err := os.WriteFile(envPath, []byte("name: EnvName\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("SENDSPIN_PLAYER_CONFIG", envPath)

	cfg, used, err := LoadPlayerConfig("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if used != envPath {
		t.Errorf("used = %q, want %q", used, envPath)
	}
	if cfg.Name != "EnvName" {
		t.Errorf("name = %q", cfg.Name)
	}
}

// applyPlayerFlags builds a flag.FlagSet mirroring the real player CLI so
// ApplyEnvAndFile can be tested end-to-end.
func newTestFlagSet() (*flag.FlagSet, map[string]*string, map[string]*int, map[string]*bool) {
	fs := flag.NewFlagSet("player", flag.ContinueOnError)
	strs := map[string]*string{
		"name":            fs.String("name", "", "player name"),
		"server":          fs.String("server", "", "server addr"),
		"log-file":        fs.String("log-file", "default.log", "log file"),
		"product-name":    fs.String("product-name", "", "product name"),
		"manufacturer":    fs.String("manufacturer", "", "mfg"),
		"preferred-codec": fs.String("preferred-codec", "", "codec"),
		"client-id":       fs.String("client-id", "", "client id"),
	}
	ints := map[string]*int{
		"port":            fs.Int("port", 8927, "port"),
		"buffer-ms":       fs.Int("buffer-ms", 150, "buffer ms"),
		"static-delay-ms": fs.Int("static-delay-ms", 0, "static delay"),
		"buffer-capacity": fs.Int("buffer-capacity", 1048576, "buffer cap"),
	}
	bools := map[string]*bool{
		"no-tui":       fs.Bool("no-tui", false, "no tui"),
		"stream-logs":  fs.Bool("stream-logs", false, "stream logs"),
		"no-reconnect": fs.Bool("no-reconnect", false, "no reconnect"),
		"daemon":       fs.Bool("daemon", false, "daemon"),
	}
	return fs, strs, ints, bools
}

func TestApplyEnvAndFile_FileFillsUnsetFlags(t *testing.T) {
	fs, strs, ints, bools := newTestFlagSet()
	// Simulate user passing only "-name Foo"
	if err := fs.Parse([]string{"-name", "CLI-Name"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	setByUser := map[string]bool{"name": true}

	port := 9000
	noTUI := true
	cfg := &PlayerConfigFile{
		Server: "file.example:1234",
		Port:   &port,
		NoTUI:  &noTUI,
	}

	if err := ApplyEnvAndFile(fs, setByUser, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *strs["name"] != "CLI-Name" {
		t.Errorf("CLI should win: name = %q", *strs["name"])
	}
	if *strs["server"] != "file.example:1234" {
		t.Errorf("file should fill: server = %q", *strs["server"])
	}
	if *ints["port"] != 9000 {
		t.Errorf("file should fill: port = %d", *ints["port"])
	}
	if !*bools["no-tui"] {
		t.Errorf("file should fill: no_tui = %v", *bools["no-tui"])
	}
	// Unset elsewhere keeps default
	if *ints["buffer-ms"] != 150 {
		t.Errorf("default should stick: buffer-ms = %d", *ints["buffer-ms"])
	}
}

func TestApplyEnvAndFile_EnvBeatsFile(t *testing.T) {
	fs, strs, ints, _ := newTestFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Setenv("SENDSPIN_PLAYER_SERVER", "env.example:9999")
	t.Setenv("SENDSPIN_PLAYER_PORT", "5555")

	port := 9000
	cfg := &PlayerConfigFile{
		Server: "file.example:1234",
		Port:   &port,
	}

	if err := ApplyEnvAndFile(fs, map[string]bool{}, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *strs["server"] != "env.example:9999" {
		t.Errorf("env should beat file: server = %q", *strs["server"])
	}
	if *ints["port"] != 5555 {
		t.Errorf("env should beat file: port = %d", *ints["port"])
	}
}

func TestApplyEnvAndFile_NilConfigStillHonorsEnv(t *testing.T) {
	fs, strs, _, _ := newTestFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Setenv("SENDSPIN_PLAYER_NAME", "env-only")

	if err := ApplyEnvAndFile(fs, map[string]bool{}, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *strs["name"] != "env-only" {
		t.Errorf("name = %q, want env-only", *strs["name"])
	}
}

func TestApplyEnvAndFile_InvalidEnvReturnsError(t *testing.T) {
	fs, _, _, _ := newTestFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Setenv("SENDSPIN_PLAYER_PORT", "not-a-number")

	err := ApplyEnvAndFile(fs, map[string]bool{}, nil)
	if err == nil {
		t.Fatal("expected error on invalid env int")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("error should mention the offending flag: %v", err)
	}
}

func TestWriteStringKey_CreatesFileWithSingleKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "player.yaml")

	if err := WriteStringKey(path, "client_id", "aa:bb:cc:dd:ee:ff"); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, _, err := LoadPlayerConfig(path)
	if err != nil {
		t.Fatalf("load after write: %v", err)
	}
	if cfg.ClientID != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("client_id = %q", cfg.ClientID)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tempfile should be cleaned up: %v", err)
	}
}

func TestWriteStringKey_UpdatesExistingKeyPreservingOthersAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.yaml")
	initial := `# Living room speaker
name: "Living Room"
server: "192.168.1.100:8927"
client_id: "old-id"
buffer_ms: 250
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteStringKey(path, "client_id", "new-id"); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(got)

	if !strings.Contains(text, `client_id: "new-id"`) && !strings.Contains(text, `client_id: new-id`) {
		t.Errorf("client_id not updated in file:\n%s", text)
	}
	if strings.Contains(text, "old-id") {
		t.Errorf("old value still present:\n%s", text)
	}
	// Other keys preserved.
	for _, want := range []string{"Living Room", "192.168.1.100:8927", "buffer_ms"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing preserved content %q in:\n%s", want, text)
		}
	}
	// The leading comment survives round-trip.
	if !strings.Contains(text, "# Living room speaker") {
		t.Errorf("leading comment lost:\n%s", text)
	}
}

func TestWriteStringKey_AppendsKeyWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.yaml")
	initial := "name: Kitchen\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteStringKey(path, "client_id", "new-value"); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, _, err := LoadPlayerConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Name != "Kitchen" {
		t.Errorf("original key lost: name = %q", cfg.Name)
	}
	if cfg.ClientID != "new-value" {
		t.Errorf("new key missing: client_id = %q", cfg.ClientID)
	}
}
