// ABOUTME: Player YAML config: typed PlayerConfigFile, load, search paths, write-back
// ABOUTME: Precedence CLI > env (SENDSPIN_PLAYER_*) > file > built-in default
package sendspin

import (
	"os"
	"strconv"
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

// PersistStaticDelay writes static_delay_ms to the YAML config at path so the
// value survives restarts and the embedder need not re-supply it on every
// connect (per spec). It is a no-op when current already equals value, so a
// launch that merely reuses the persisted value doesn't rewrite the file.
// Returns whether a write occurred.
func PersistStaticDelay(path string, current *int, value int) (bool, error) {
	if current != nil && *current == value {
		return false, nil
	}
	if err := WriteIntKey(path, "static_delay_ms", value); err != nil {
		return false, err
	}
	return true, nil
}
