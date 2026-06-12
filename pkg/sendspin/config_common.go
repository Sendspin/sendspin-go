// ABOUTME: Shared config-file overlay machinery (YAML load, env/file precedence, write-back)
// ABOUTME: Used by both player and server config; flat keys 1:1 with CLI flags
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

// userConfigPath returns <UserConfigDir>/sendspin/<relative>. Matches the
// canonical path layout for both player.yaml and server.yaml.
func userConfigPath(relative string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "sendspin", relative), nil
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

// WriteStringKey reads the YAML at path (if any), sets the given top-level
// string key to value, and atomically writes the result back. Comments and
// existing keys are preserved via yaml.Node round-tripping. Used to persist
// the auto-generated client_id and the --client-id override.
func WriteStringKey(path, key, value string) error {
	return writeScalarKey(path, key, value, "!!str")
}

// WriteIntKey is like WriteStringKey but writes an integer scalar, for numeric
// config fields (e.g. static_delay_ms) that load into int. A string scalar
// would fail to unmarshal back into the typed config.
func WriteIntKey(path, key string, value int) error {
	return writeScalarKey(path, key, strconv.Itoa(value), "!!int")
}

// writeScalarKey reads the YAML at path (if any), sets the given top-level key
// to a scalar carrying valueTag, and atomically writes the result back.
// Comments and existing keys are preserved via yaml.Node round-tripping.
func writeScalarKey(path, key, value, valueTag string) error {
	var root yaml.Node

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse existing config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	mapping := topLevelMapping(&root)

	setOrAppendScalarKey(mapping, key, value, valueTag)

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

// setOrAppendScalarKey updates the value for key in a MappingNode, or appends
// a new key/value pair if the key is not present. The value node carries
// valueTag (e.g. "!!str" or "!!int"); the key is always a string. Leaves all
// other entries (and their comments) untouched.
func setOrAppendScalarKey(mapping *yaml.Node, key, value, valueTag string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Kind = yaml.ScalarNode
			mapping.Content[i+1].Tag = valueTag
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Style = 0
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: valueTag, Value: value},
	)
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
