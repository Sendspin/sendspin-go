// ABOUTME: Tests for mDNS discovery
// ABOUTME: Tests service advertisement and discovery
package discovery

import (
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

func TestNewManager(t *testing.T) {
	config := Config{
		ServiceName: "Test Player",
		Port:        8927,
	}

	mgr := NewManager(config)
	if mgr == nil {
		t.Fatal("expected manager to be created")
	}
}

func TestClientInfoFromEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry *mdns.ServiceEntry
		want  *ClientInfo
	}{
		{
			name: "full entry with path and name",
			entry: &mdns.ServiceEntry{
				Name:       "living-room._sendspin._tcp.local.",
				Host:       "living-room.local.",
				AddrV4:     net.ParseIP("192.168.1.42"),
				Port:       8928,
				InfoFields: []string{"path=/sendspin", "name=Living Room"},
			},
			want: &ClientInfo{
				Instance: "living-room._sendspin._tcp.local.",
				Name:     "Living Room",
				Host:     "192.168.1.42",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "missing path defaults to /sendspin",
			entry: &mdns.ServiceEntry{
				Name:       "kitchen._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.43"),
				Port:       8928,
				InfoFields: []string{"name=Kitchen"},
			},
			want: &ClientInfo{
				Instance: "kitchen._sendspin._tcp.local.",
				Name:     "Kitchen",
				Host:     "192.168.1.43",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "missing name falls back to entry.Name",
			entry: &mdns.ServiceEntry{
				Name:       "bedroom._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.44"),
				Port:       8928,
				InfoFields: []string{"path=/sendspin"},
			},
			want: &ClientInfo{
				Instance: "bedroom._sendspin._tcp.local.",
				Name:     "bedroom._sendspin._tcp.local.",
				Host:     "192.168.1.44",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "no IPv4 address returns nil",
			entry: &mdns.ServiceEntry{
				Name:       "ipv6-only._sendspin._tcp.local.",
				Port:       8928,
				InfoFields: []string{"path=/sendspin"},
			},
			want: nil,
		},
		{
			name: "zero port returns nil",
			entry: &mdns.ServiceEntry{
				Name:       "bad._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.45"),
				Port:       0,
				InfoFields: []string{"path=/sendspin"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientInfoFromEntry(tt.entry)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("clientInfoFromEntry nil-ness mismatch: got %+v, want %+v", got, tt.want)
			}
			if got == nil {
				return
			}
			if *got != *tt.want {
				t.Errorf("clientInfoFromEntry = %+v, want %+v", got, tt.want)
			}
		})
	}
}
