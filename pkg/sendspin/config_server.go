// ABOUTME: Server YAML config: typed ServerConfigFile, load, search paths
// ABOUTME: Precedence CLI > env (SENDSPIN_SERVER_*) > file > built-in default
package sendspin

import (
	"os"
	"strconv"
)

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
