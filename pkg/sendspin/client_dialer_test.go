// ABOUTME: Tests for outbound client discovery and dialing
// ABOUTME: Uses injected dial functions — no real network I/O

package sendspin

import (
	"testing"

	"github.com/Sendspin/sendspin-go/internal/discovery"
)

func TestClientInfoURL(t *testing.T) {
	tests := []struct {
		name string
		info *discovery.ClientInfo
		want string
	}{
		{
			name: "ipv4 host",
			info: &discovery.ClientInfo{Host: "192.168.1.42", Port: 8928, Path: "/sendspin"},
			want: "ws://192.168.1.42:8928/sendspin",
		},
		{
			name: "path missing leading slash is normalized",
			info: &discovery.ClientInfo{Host: "192.168.1.42", Port: 8928, Path: "sendspin"},
			want: "ws://192.168.1.42:8928/sendspin",
		},
		{
			name: "custom path preserved",
			info: &discovery.ClientInfo{Host: "10.0.0.5", Port: 9000, Path: "/custom/endpoint"},
			want: "ws://10.0.0.5:9000/custom/endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientInfoURL(tt.info)
			if got != tt.want {
				t.Errorf("clientInfoURL = %q, want %q", got, tt.want)
			}
		})
	}
}
