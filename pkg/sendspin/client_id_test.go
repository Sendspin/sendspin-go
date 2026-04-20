// ABOUTME: Tests for ResolveClientID precedence and the MAC picker filter
package sendspin

import (
	"errors"
	"net"
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

type persistRecorder struct {
	calls []string
	err   error
}

func (p *persistRecorder) persist(id string) error {
	p.calls = append(p.calls, id)
	return p.err
}

func withStubInterfaces(t *testing.T, stub func() []net.Interface) {
	t.Helper()
	prev := allInterfaces
	allInterfaces = stub
	t.Cleanup(func() { allInterfaces = prev })
}

func TestResolveClientID_OverrideWinsAndPersistsWhenDifferent(t *testing.T) {
	rec := &persistRecorder{}

	id, err := ResolveClientID("my-override", "", rec.persist)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "my-override" {
		t.Errorf("id = %q, want my-override", id)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "my-override" {
		t.Errorf("persist calls = %v, want [my-override]", rec.calls)
	}
}

func TestResolveClientID_OverrideMatchingConfigDoesNotPersist(t *testing.T) {
	rec := &persistRecorder{}

	id, err := ResolveClientID("same-id", "same-id", rec.persist)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "same-id" {
		t.Errorf("id = %q, want same-id", id)
	}
	if len(rec.calls) != 0 {
		t.Errorf("persist calls = %v, want no-op when override matches config", rec.calls)
	}
}

func TestResolveClientID_ConfigBeatsMAC(t *testing.T) {
	rec := &persistRecorder{}
	withStubInterfaces(t, func() []net.Interface {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
		}
	})

	id, err := ResolveClientID("", "from-config", rec.persist)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "from-config" {
		t.Errorf("id = %q, want from-config (config must beat MAC)", id)
	}
	if len(rec.calls) != 0 {
		t.Errorf("persist calls = %v, want none (config path uses as-is)", rec.calls)
	}
}

func TestResolveClientID_MACNotPersisted(t *testing.T) {
	rec := &persistRecorder{}
	withStubInterfaces(t, func() []net.Interface {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp, HardwareAddr: mac(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff)},
		}
	})

	id, err := ResolveClientID("", "", rec.persist)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("id = %q, want MAC", id)
	}
	if len(rec.calls) != 0 {
		t.Errorf("persist calls = %v, want none (MAC is inherently stable)", rec.calls)
	}
}

func TestResolveClientID_GeneratedUUIDPersisted(t *testing.T) {
	rec := &persistRecorder{}
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := ResolveClientID("", "", rec.persist)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == "" {
		t.Fatal("resolve returned empty id")
	}
	if len(rec.calls) != 1 || rec.calls[0] != id {
		t.Errorf("persist calls = %v, want [%q]", rec.calls, id)
	}
}

func TestResolveClientID_NilPersistStillReturnsID(t *testing.T) {
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := ResolveClientID("", "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == "" {
		t.Error("resolve with nil persist must still return a non-empty id")
	}
}

func TestResolveClientID_PersistErrorIsNotFatal(t *testing.T) {
	rec := &persistRecorder{err: errors.New("disk full")}
	withStubInterfaces(t, func() []net.Interface { return nil })

	id, err := ResolveClientID("", "", rec.persist)
	if err != nil {
		t.Fatalf("resolve should not fail when persist fails: %v", err)
	}
	if id == "" {
		t.Error("resolve returned empty id")
	}
	if len(rec.calls) != 1 {
		t.Errorf("persist should have been attempted once, got %d calls", len(rec.calls))
	}
}
