// ABOUTME: End-to-end test for the receiver's client/hello advertised roles
// ABOUTME: Guards against advertising capabilities the client does not implement
package sendspin

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestReceiver_DoesNotAdvertiseVisualizer connects a real Receiver to a real
// Server and asserts the advertised role list excludes visualizer@v1. The
// receiver implements no visualizer behavior, and advertising a stale
// visualizer support struct breaks the handshake against spec-current servers
// (e.g. Music Assistant 2.9.0's aiosendspin requires fields the Go struct does
// not send, so the whole client/hello fails to parse). See issue #136.
func TestReceiver_DoesNotAdvertiseVisualizer(t *testing.T) {
	server, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "Hello Roles Test Server",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	errChan := make(chan error, 1)
	go func() { errChan <- server.Start() }()
	defer func() {
		server.Stop()
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
			t.Error("server stop timeout")
		}
	}()

	port := waitForServerPort(t, server)

	recv, err := NewReceiver(ReceiverConfig{
		ServerAddr: fmt.Sprintf("localhost:%d", port),
		PlayerName: "Hello Roles Client",
		ClientID:   "hello-roles-client",
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	defer recv.Close()

	if err := recv.Connect(); err != nil {
		t.Fatalf("Receiver.Connect: %v", err)
	}

	// Drain decoded audio so the scheduler never blocks on a full channel.
	go func() {
		for range recv.Output() {
		}
	}()

	// The server records each client's advertised roles verbatim from the
	// client/hello. Wait for the join, then inspect what the receiver claimed.
	var roles []string
	waitFor(t, 3*time.Second, func() bool {
		clients := server.Group().Clients()
		if len(clients) == 0 {
			return false
		}
		roles = clients[0].Roles()
		return true
	}, "receiver to join the server group")

	hasPlayer := false
	for _, role := range roles {
		if role == "player@v1" || strings.HasPrefix(role, "player@") {
			hasPlayer = true
		}
		if role == "visualizer@v1" || strings.HasPrefix(role, "visualizer@") {
			t.Errorf("receiver advertised %q; the visualizer role must not be advertised "+
				"(no visualizer support is implemented and it breaks the MA 2.9.0 handshake). roles=%v",
				role, roles)
		}
	}
	if !hasPlayer {
		t.Errorf("receiver did not advertise the player role; got roles=%v", roles)
	}
}
