// ABOUTME: Tests for ResolveClientID precedence, MAC picker filter, and atomic file persistence
package sendspin

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func mac(b ...byte) net.HardwareAddr { return net.HardwareAddr(b) }

func TestPickStableMAC(t *testing.T) {
	up := net.FlagUp
	lo := net.FlagUp | net.FlagLoopback
	down := net.Flags(0)

	tests := []struct {
		name   string
		ifaces []net.Interface
		want   string
		wantOk bool
	}{
		{
			name: "single eligible",
			ifaces: []net.Interface{
				{Name: "eth0", Flags: up, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
			},
			want:   "aa:bb:cc:dd:ee:ff",
			wantOk: true,
		},
		{
			name: "skips loopback",
			ifaces: []net.Interface{
				{Name: "lo", Flags: lo, HardwareAddr: mac(1, 2, 3, 4, 5, 6)},
				{Name: "eth0", Flags: up, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
			},
			want:   "aa:bb:cc:dd:ee:ff",
			wantOk: true,
		},
		{
			name: "skips interfaces that are not up",
			ifaces: []net.Interface{
				{Name: "eth0", Flags: down, HardwareAddr: mac(1, 1, 1, 1, 1, 1)},
				{Name: "wlan0", Flags: up, HardwareAddr: mac(2, 2, 2, 2, 2, 2)},
			},
			want:   "02:02:02:02:02:02",
			wantOk: true,
		},
		{
			name: "skips zero MAC",
			ifaces: []net.Interface{
				{Name: "eth0", Flags: up, HardwareAddr: mac(0, 0, 0, 0, 0, 0)},
				{Name: "wlan0", Flags: up, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
			},
			want:   "aa:bb:cc:dd:ee:ff",
			wantOk: true,
		},
		{
			name: "skips broadcast MAC",
			ifaces: []net.Interface{
				{Name: "eth0", Flags: up, HardwareAddr: mac(0xff, 0xff, 0xff, 0xff, 0xff, 0xff)},
				{Name: "wlan0", Flags: up, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
			},
			want:   "aa:bb:cc:dd:ee:ff",
			wantOk: true,
		},
		{
			name: "skips short hardware address",
			ifaces: []net.Interface{
				{Name: "eth0", Flags: up, HardwareAddr: mac(1, 2, 3, 4)},
				{Name: "wlan0", Flags: up, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
			},
			want:   "aa:bb:cc:dd:ee:ff",
			wantOk: true,
		},
		{
			name: "alphabetical sort: eth0 wins over wlan0",
			ifaces: []net.Interface{
				{Name: "wlan0", Flags: up, HardwareAddr: mac(0xaa, 0, 0, 0, 0, 0x01)},
				{Name: "eth0", Flags: up, HardwareAddr: mac(0xbb, 0, 0, 0, 0, 0x02)},
			},
			want:   "bb:00:00:00:00:02",
			wantOk: true,
		},
		{
			name: "nothing eligible",
			ifaces: []net.Interface{
				{Name: "lo", Flags: lo, HardwareAddr: mac(0, 0, 0, 0, 0, 0)},
				{Name: "eth0", Flags: down, HardwareAddr: mac(1, 1, 1, 1, 1, 1)},
			},
			want:   "",
			wantOk: false,
		},
		{
			name:   "empty input",
			ifaces: nil,
			want:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pickStableMAC(tt.ifaces)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("mac = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPersistedClientID_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendspin-player", "client-id")

	if _, ok := readPersistedClientID(path); ok {
		t.Fatal("read on nonexistent file should return ok=false")
	}

	if err := writePersistedClientID(path, "my-id-1234"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, ok := readPersistedClientID(path)
	if !ok || got != "my-id-1234" {
		t.Errorf("after first write: ok=%v got=%q, want my-id-1234", ok, got)
	}

	if err := writePersistedClientID(path, "my-id-5678"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ = readPersistedClientID(path)
	if got != "my-id-5678" {
		t.Errorf("after overwrite: got=%q, want my-id-5678", got)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tempfile should have been renamed, stat err = %v", err)
	}
}

func TestPersistedClientID_TrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	if err := os.WriteFile(path, []byte("  padded-id\n\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, ok := readPersistedClientID(path)
	if !ok || got != "padded-id" {
		t.Errorf("got=%q ok=%v, want padded-id true", got, ok)
	}
}

func TestPersistedClientID_EmptyFileIsNotUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	if err := os.WriteFile(path, []byte("\n  \n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := readPersistedClientID(path); ok {
		t.Error("whitespace-only file should be treated as absent")
	}
}

func withStubInterfaces(t *testing.T, stub func() []net.Interface) {
	t.Helper()
	prev := allInterfaces
	allInterfaces = stub
	t.Cleanup(func() { allInterfaces = prev })
}

func TestResolveClientIDFrom_OverrideWinsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")

	id, err := resolveClientIDFrom("my-override", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "my-override" {
		t.Errorf("id = %q, want my-override", id)
	}
	persisted, ok := readPersistedClientID(path)
	if !ok || persisted != "my-override" {
		t.Errorf("override not persisted: ok=%v, persisted=%q", ok, persisted)
	}
}

func TestResolveClientIDFrom_OverrideReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	if err := writePersistedClientID(path, "stale-id"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	id, err := resolveClientIDFrom("fresh-override", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "fresh-override" {
		t.Errorf("id = %q, want fresh-override", id)
	}
	persisted, _ := readPersistedClientID(path)
	if persisted != "fresh-override" {
		t.Errorf("file still has %q, want fresh-override", persisted)
	}
}

func TestResolveClientIDFrom_PersistedBeatsMAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	if err := writePersistedClientID(path, "persisted-id"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	withStubInterfaces(t, func() []net.Interface {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
		}
	})

	id, err := resolveClientIDFrom("", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "persisted-id" {
		t.Errorf("id = %q, want persisted-id (persisted must beat MAC)", id)
	}
}

func TestResolveClientIDFrom_MACWhenNoPersistedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	withStubInterfaces(t, func() []net.Interface {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
		}
	})

	id, err := resolveClientIDFrom("", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("id = %q, want MAC", id)
	}
	if _, ok := readPersistedClientID(path); ok {
		t.Error("MAC-derived id should not be written to the config file")
	}
}

func TestResolveClientIDFrom_GeneratedUUIDIsPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-id")
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := resolveClientIDFrom("", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == "" {
		t.Fatal("resolve returned empty id")
	}
	persisted, ok := readPersistedClientID(path)
	if !ok {
		t.Fatal("generated id should have been persisted")
	}
	if persisted != id {
		t.Errorf("persisted=%q, returned=%q (must match)", persisted, id)
	}
}

func TestResolveClientIDFrom_NoConfigPathStillReturnsID(t *testing.T) {
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := resolveClientIDFrom("", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == "" {
		t.Error("resolve with no persistence still must return a non-empty id")
	}
}

// TestResolveClientID_ExplicitConfigPath verifies the public entry point
// honors a caller-supplied path override, which is how --client-id-file
// supports multiple instances on one host.
func TestResolveClientID_ExplicitConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-id")
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := ResolveClientID("", path)
	if err != nil {
		t.Fatalf("ResolveClientID: %v", err)
	}
	if id == "" {
		t.Fatal("id is empty")
	}
	persisted, ok := readPersistedClientID(path)
	if !ok {
		t.Fatal("generated id not written to the override path")
	}
	if persisted != id {
		t.Errorf("persisted=%q returned=%q", persisted, id)
	}

	// Second call should read from the same override path.
	id2, err := ResolveClientID("", path)
	if err != nil {
		t.Fatalf("second ResolveClientID: %v", err)
	}
	if id2 != id {
		t.Errorf("second resolve returned %q, want persisted %q", id2, id)
	}
}
