// ABOUTME: Stable client_id resolution (override -> config value -> MAC -> generated UUID)
// ABOUTME: Persistence of new values is delegated to a caller-supplied callback
package sendspin

import (
	"bytes"
	"log"
	"net"
	"sort"

	"github.com/google/uuid"
)

type clientIDSource string

const (
	sourceOverride  clientIDSource = "override"
	sourceConfig    clientIDSource = "config"
	sourceMAC       clientIDSource = "mac"
	sourceGenerated clientIDSource = "generated"
)

// PersistFn is called by ResolveClientID whenever a new client_id needs to be
// written back to the config file. Implementations typically call
// WriteStringKey(path, "client_id", id). Returning an error is logged but not
// fatal — the player will still boot with the resolved id; the id just won't
// survive a restart.
type PersistFn func(id string) error

// ResolveClientID returns a stable client_id, following this precedence:
//  1. override  — used as-is and persisted via persist (so a one-time seed sticks)
//  2. fromConfig — used as-is, no persistence (value came from the file we'd write to)
//  3. primary network interface MAC (xx:xx:xx:xx:xx:xx) — not persisted; MAC is inherently stable
//  4. freshly generated UUID — persisted so the next launch takes path 2
//
// Persisted value beating MAC is deliberate: once Music Assistant has
// registered a player under one id, switching to another creates a duplicate
// entry. Deleting client_id from the config file is the explicit "re-derive".
//
// persist may be nil if the caller cannot write back (e.g. no config path
// resolvable). In that case generated/override ids still work for the session
// but won't survive a restart.
func ResolveClientID(override, fromConfig string, persist PersistFn) (string, error) {
	if override != "" {
		if override != fromConfig {
			tryPersist(persist, override, "override")
		}
		logClientID(override, sourceOverride, persist != nil && override != fromConfig)
		return override, nil
	}

	if fromConfig != "" {
		logClientID(fromConfig, sourceConfig, false)
		return fromConfig, nil
	}

	if mac, ok := pickStableMAC(allInterfaces()); ok {
		logClientID(mac, sourceMAC, false)
		return mac, nil
	}

	generated := uuid.New().String()
	tryPersist(persist, generated, "generated id")
	logClientID(generated, sourceGenerated, persist != nil)
	return generated, nil
}

func tryPersist(persist PersistFn, id, what string) {
	if persist == nil {
		return
	}
	if err := persist(id); err != nil {
		log.Printf("client_id: could not persist %s: %v", what, err)
	}
}

func logClientID(id string, source clientIDSource, persisted bool) {
	if persisted {
		log.Printf("client_id: %s (source: %s, persisted)", id, source)
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
