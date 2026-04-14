// ABOUTME: Tests for outbound client discovery and dialing
// ABOUTME: Uses injected dial functions — no real network I/O

package sendspin

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestClientDialerDedupesByInstance(t *testing.T) {
	in := make(chan *discovery.ClientInfo, 10)

	var dialCalls int32
	var mu sync.Mutex
	var seen []string

	dial := func(ctx context.Context, info *discovery.ClientInfo) error {
		atomic.AddInt32(&dialCalls, 1)
		mu.Lock()
		seen = append(seen, info.Instance)
		mu.Unlock()
		return nil
	}

	d := newClientDialer(in, dial)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		d.run(ctx)
		close(done)
	}()

	// Emit the same instance 3 times and a different instance once
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "b._sendspin._tcp.local.", Host: "1.1.1.2", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}

	// Wait until at least 2 unique dials have happened
	deadline := time.After(500 * time.Millisecond)
	for {
		if atomic.LoadInt32(&dialCalls) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for dial calls, got %d", atomic.LoadInt32(&dialCalls))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Give any extra (incorrect) calls a chance to fire
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	if got := atomic.LoadInt32(&dialCalls); got != 2 {
		t.Errorf("dialCalls = %d, want 2 (one per unique instance)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("seen = %v, want 2 entries", seen)
	}
}
