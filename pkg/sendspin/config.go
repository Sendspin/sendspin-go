// ABOUTME: YAML config-file support for the player and server (paths, env overlay, write-back)
// ABOUTME: Flat keys 1:1 with CLI flags; precedence CLI > env > file > built-in default
package sendspin

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlayerEnvPrefix is the namespace for environment overrides of player config
// values. Env key = PlayerEnvPrefix + upper-snake(flag name). Example:
// "-buffer-ms" -> SENDSPIN_PLAYER_BUFFER_MS.
const PlayerEnvPrefix = "SENDSPIN_PLAYER_"

// PlayerConfigFile mirrors the player's CLI flags. Fields with "zero" values
// that could reasonably be meaningful (bool, int) are pointers so absence in
// the YAML can be distinguished from an explicit false/0.
type PlayerConfigFile struct {
	Name           string `yaml:"name,omitempty"`
	Server         string `yaml:"server,omitempty"`
	Port           *int   `yaml:"port,omitempty"`
	BufferMs       *int   `yaml:"buffer_ms,omitempty"`
	StaticDelayMs  *int   `yaml:"static_delay_ms,omitempty"`
	LogFile        string `yaml:"log_file,omitempty"`
	NoTUI          *bool  `yaml:"no_tui,omitempty"`
	StreamLogs     *bool  `yaml:"stream_logs,omitempty"`
	ProductName    string `yaml:"product_name,omitempty"`
	Manufacturer   string `yaml:"manufacturer,omitempty"`
	NoReconnect    *bool  `yaml:"no_reconnect,omitempty"`
	Daemon         *bool  `yaml:"daemon,omitempty"`
	PreferredCodec string `yaml:"preferred_codec,omitempty"`
	BufferCapacity *int   `yaml:"buffer_capacity,omitempty"`
	ClientID       string `yaml:"client_id,omitempty"`
	AudioDevice    string `yaml:"audio_device,omitempty"`
	MaxSampleRate  *int   `yaml:"max_sample_rate,omitempty"`
	MaxBitDepth    *int   `yaml:"max_bit_depth,omitempty"`
}

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

// LoadPlayerConfig searches for a player.yaml and returns its parsed contents
// along with the path that was loaded (empty if none was found).
//
// Search order (first existing wins):
//  1. explicitPath if non-empty
//  2. $SENDSPIN_PLAYER_CONFIG if set
//  3. $XDG_CONFIG_HOME or OS equivalent + /sendspin/player.yaml
//  4. /etc/sendspin/player.yaml
//
// A missing file is not an error; the caller gets (nil, "", nil).
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

// userConfigPath returns <UserConfigDir>/sendspin/<relative>. Matches the
// canonical path layout for both player.yaml and server.yaml.
func userConfigPath(relative string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "sendspin", relative), nil
}

// DefaultPlayerConfigPath returns the canonical user-level player.yaml path
// for this OS. Used when we need to auto-create the config for write-back.
func DefaultPlayerConfigPath() (string, error) {
	return userConfigPath("player.yaml")
}

func playerConfigSearchPaths(explicit string) []string {
	paths := make([]string, 0, 4)
	if explicit != "" {
		paths = append(paths, explicit)
	}
	if env := os.Getenv("SENDSPIN_PLAYER_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	if p, err := userConfigPath("player.yaml"); err == nil {
		paths = append(paths, p)
	}
	paths = append(paths, "/etc/sendspin/player.yaml")
	return paths
}

// ApplyEnvAndFile overlays <envPrefix> env vars and YAML config-file values
// into the given FlagSet, but only for flags the user did NOT set on the CLI.
// Precedence: CLI (untouched here) > env > file > flag default.
//
// envPrefix is the namespace for env-var lookups (e.g. "SENDSPIN_PLAYER_").
// fileValues is the flat flag-key → value map the caller derives from its
// typed config struct (see PlayerConfigFile.AsStringMap and
// ServerConfigFile.AsStringMap). A nil map is treated as empty.
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

// AsStringMap returns only the keys the user actually set in the YAML, as
// strings suitable for flag.Set. Absent keys are omitted so the overlay
// correctly falls through to the flag default.
func (c *PlayerConfigFile) AsStringMap() map[string]string {
	m := make(map[string]string)
	if c == nil {
		return m
	}
	if c.Name != "" {
		m["name"] = c.Name
	}
	if c.Server != "" {
		m["server"] = c.Server
	}
	if c.Port != nil {
		m["port"] = strconv.Itoa(*c.Port)
	}
	if c.BufferMs != nil {
		m["buffer_ms"] = strconv.Itoa(*c.BufferMs)
	}
	if c.StaticDelayMs != nil {
		m["static_delay_ms"] = strconv.Itoa(*c.StaticDelayMs)
	}
	if c.LogFile != "" {
		m["log_file"] = c.LogFile
	}
	if c.NoTUI != nil {
		m["no_tui"] = strconv.FormatBool(*c.NoTUI)
	}
	if c.StreamLogs != nil {
		m["stream_logs"] = strconv.FormatBool(*c.StreamLogs)
	}
	if c.ProductName != "" {
		m["product_name"] = c.ProductName
	}
	if c.Manufacturer != "" {
		m["manufacturer"] = c.Manufacturer
	}
	if c.NoReconnect != nil {
		m["no_reconnect"] = strconv.FormatBool(*c.NoReconnect)
	}
	if c.Daemon != nil {
		m["daemon"] = strconv.FormatBool(*c.Daemon)
	}
	if c.PreferredCodec != "" {
		m["preferred_codec"] = c.PreferredCodec
	}
	if c.BufferCapacity != nil {
		m["buffer_capacity"] = strconv.Itoa(*c.BufferCapacity)
	}
	if c.ClientID != "" {
		m["client_id"] = c.ClientID
	}
	if c.AudioDevice != "" {
		m["audio_device"] = c.AudioDevice
	}
	if c.MaxSampleRate != nil {
		m["max_sample_rate"] = strconv.Itoa(*c.MaxSampleRate)
	}
	if c.MaxBitDepth != nil {
		m["max_bit_depth"] = strconv.Itoa(*c.MaxBitDepth)
	}
	return m
}

// WriteStringKey reads the YAML at path (if any), sets the given top-level
// string key to value, and atomically writes the result back. Comments and
// existing keys are preserved via yaml.Node round-tripping. Used to persist
// the auto-generated client_id and the --client-id override.
func WriteStringKey(path, key, value string) error {
	var root yaml.Node

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse existing config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	mapping := topLevelMapping(&root)

	setOrAppendStringKey(mapping, key, value)

	buf, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return atomicWriteFile(path, buf)
}

// topLevelMapping returns the mapping node that backs the top of a YAML
// document. If root is empty or non-document, it's initialized in place.
func topLevelMapping(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 && root.Content[0].Kind == yaml.MappingNode {
		return root.Content[0]
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	root.Kind = yaml.DocumentNode
	root.Content = []*yaml.Node{mapping}
	return mapping
}

// setOrAppendStringKey updates the value for key in a MappingNode, or appends
// a new key/value pair if the key is not present. Leaves all other entries
// (and their comments) untouched.
func setOrAppendStringKey(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Kind = yaml.ScalarNode
			mapping.Content[i+1].Tag = "!!str"
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Style = 0
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

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

// atomicWriteFile writes data to path via tempfile + rename. Matches the
// atomicity guarantees the old writePersistedClientID used to provide.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
