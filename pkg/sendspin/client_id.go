// ABOUTME: Stable client_id resolution for the player (override -> persisted -> MAC -> generated)
// ABOUTME: Persists UUID fallback and overrides to the user config dir so identity survives restarts
package sendspin

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	clientIDConfigSubdir = "sendspin-player"
	clientIDConfigFile   = "client-id"
)

type clientIDSource string

const (
	sourceOverride  clientIDSource = "override"
	sourcePersisted clientIDSource = "persisted"
	sourceMAC       clientIDSource = "mac"
	sourceGenerated clientIDSource = "generated"
)

// ResolveClientID returns a stable client_id for this player, following the
// resolution order:
//  1. override — used and persisted to the config file
//  2. persisted value in the config file — used as-is
//  3. MAC-derived from the primary network interface — not persisted
//  4. freshly generated UUID — persisted for next launch
//
// Persisted value beats MAC on purpose: once Music Assistant has registered a
// player under a given ID, switching to a different ID on a later launch would
// create a duplicate player entry. Deleting the config file is the explicit
// "re-derive" action.
//
// configPathOverride, if non-empty, replaces the default OS-specific config
// path. Use this to run multiple players on a single host with distinct
// identities (one file per instance).
func ResolveClientID(override, configPathOverride string) (string, error) {
	if configPathOverride != "" {
		return resolveClientIDFrom(override, configPathOverride)
	}
	path, err := clientIDConfigPath()
	if err != nil {
		// No writable config dir; continue without persistence.
		return resolveClientIDFrom(override, "")
	}
	return resolveClientIDFrom(override, path)
}

func resolveClientIDFrom(override, configPath string) (string, error) {
	if override != "" {
		persistIfPossible(configPath, override, "override")
		logClientID(override, sourceOverride, configPath)
		return override, nil
	}

	if configPath != "" {
		if persisted, ok := readPersistedClientID(configPath); ok {
			logClientID(persisted, sourcePersisted, configPath)
			return persisted, nil
		}
	}

	if mac, ok := pickStableMAC(allInterfaces()); ok {
		logClientID(mac, sourceMAC, "")
		return mac, nil
	}

	generated := uuid.New().String()
	persistIfPossible(configPath, generated, "generated id")
	logClientID(generated, sourceGenerated, configPath)
	return generated, nil
}

func persistIfPossible(configPath, id, what string) {
	if configPath == "" {
		return
	}
	if err := writePersistedClientID(configPath, id); err != nil {
		log.Printf("client_id: could not persist %s to %s: %v", what, configPath, err)
	}
}

func logClientID(id string, source clientIDSource, path string) {
	if path != "" && (source == sourceGenerated || source == sourceOverride) {
		log.Printf("client_id: %s (source: %s, persisted: %s)", id, source, path)
		return
	}
	log.Printf("client_id: %s (source: %s)", id, source)
}

// allInterfaces is a package-level var so tests can stub it.
var allInterfaces = func() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	return ifaces
}

// pickStableMAC returns the MAC of the alphabetically-first eligible interface.
// Eligible means: up, not loopback, 6-byte hardware address, not zero, not broadcast.
// Alphabetical ordering gives deterministic selection across reboots and OSes
// (eth0 < wlan0, Ethernet < Wi-Fi).
func pickStableMAC(ifaces []net.Interface) (string, bool) {
	eligible := make([]net.Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) != 6 {
			continue
		}
		if isZeroMAC(iface.HardwareAddr) || isBroadcastMAC(iface.HardwareAddr) {
			continue
		}
		eligible = append(eligible, iface)
	}
	if len(eligible) == 0 {
		return "", false
	}
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Name < eligible[j].Name
	})
	return eligible[0].HardwareAddr.String(), true
}

func isZeroMAC(mac net.HardwareAddr) bool {
	return bytes.Equal(mac, []byte{0, 0, 0, 0, 0, 0})
}

func isBroadcastMAC(mac net.HardwareAddr) bool {
	return bytes.Equal(mac, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
}

func clientIDConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, clientIDConfigSubdir, clientIDConfigFile), nil
}

func readPersistedClientID(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", false
	}
	return id, true
}

// writePersistedClientID writes atomically via tempfile + rename so a crash
// mid-write cannot leave an empty or truncated config file in place.
func writePersistedClientID(path, id string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
