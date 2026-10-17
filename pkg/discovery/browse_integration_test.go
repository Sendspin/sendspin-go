// ABOUTME: Integration test for client-side server browsing
// ABOUTME: Advertises a fake server over mDNS and verifies BrowseContext finds it and cleans up

package discovery

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

func TestBrowseContextDiscoversServer(t *testing.T) {
	// Requires multicast; opt in with SENDSPIN_MULTICAST_TESTS=1.
	if os.Getenv("SENDSPIN_MULTICAST_TESTS") == "" {
		t.Skip("requires multicast; set SENDSPIN_MULTICAST_TESTS=1 to enable")
	}

	const instance = "test-browse-server-instance"
	svc, err := mdns.NewMDNSService(
		instance,
		"_sendspin-server._tcp",
		"",
		"",
		8927,
		[]net.IP{net.ParseIP("127.0.0.1")},
		[]string{"path=/sendspin"},
	)
	if err != nil {
		t.Fatalf("mdns service: %v", err)
	}
	mdnsServer, err := mdns.NewServer(&mdns.Config{Zone: svc, Logger: silentLogger})
	if err != nil {
		t.Fatalf("mdns server: %v", err)
	}
	defer mdnsServer.Shutdown()

	mgr := NewManager(Config{ServiceName: "Test", Port: 8931})
	defer mgr.Stop()

	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.BrowseContext(ctx); err != nil {
		t.Fatalf("BrowseContext: %v", err)
	}

	// Real servers on the LAN may also answer; wait for ours.
	timeout := time.After(10 * time.Second)
	for found := false; !found; {
		select {
		case server := <-mgr.Servers():
			found = strings.HasPrefix(server.Name, instance+".")
			if found && server.Port != 8927 {
				t.Errorf("port = %d, want 8927", server.Port)
			}
		case <-timeout:
			cancel()
			t.Fatal("timed out waiting for advertised server")
		}
	}

	// Cancel while queries are in flight; the browse MUST release its goroutines.
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			t.Fatalf("browse goroutines leaked: %d running, baseline %d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
