// ABOUTME: Integration test for server-initiated client discovery
// ABOUTME: Fakes an mDNS advertisement and verifies the server dials + handshakes

package sendspin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/mdns"
)

func TestServerDiscoversAndDialsClient(t *testing.T) {
	// mDNS multicast is unreliable in some environments (CI sandboxes,
	// Windows boxes with strict firewalls or Hyper-V virtual switches).
	// Opt in with SENDSPIN_MULTICAST_TESTS=1 when running locally.
	if os.Getenv("SENDSPIN_MULTICAST_TESTS") == "" {
		t.Skip("requires multicast; set SENDSPIN_MULTICAST_TESTS=1 to enable")
	}

	// 1. Stand up a fake "player" HTTP server that upgrades to WS and
	//    sends client/hello.
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	playerReady := make(chan string, 1) // receives serverID via server/hello

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		hello := protocol.Message{
			Type: "client/hello",
			Payload: protocol.ClientHello{
				ClientID:       "test-player-1",
				Name:           "Test Player",
				SupportedRoles: []string{"player@v1"},
			},
		}
		data, _ := json.Marshal(hello)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			t.Errorf("write hello: %v", err)
			return
		}

		_, resp, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg protocol.Message
		if err := json.Unmarshal(resp, &msg); err != nil {
			return
		}
		if msg.Type == "server/hello" {
			playerReady <- "ok"
		}

		// Block until the server closes us.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	// 2. Advertise an mDNS _sendspin._tcp record pointing at httptest.
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	svc, err := mdns.NewMDNSService(
		"test-player-instance",
		"_sendspin._tcp",
		"",
		"",
		port,
		ips,
		[]string{"path=/", "name=Test Player"},
	)
	if err != nil {
		t.Fatalf("mdns service: %v", err)
	}
	mdnsServer, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		t.Fatalf("mdns server: %v", err)
	}
	defer mdnsServer.Shutdown()

	// 3. Start a Sendspin server with DiscoverClients=true.
	source := NewTestTone(48000, 2)
	srv, err := NewServer(ServerConfig{
		Port:            findFreePort(t),
		Name:            "Test Server",
		Source:          source,
		EnableMDNS:      false,
		DiscoverClients: true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Start() }()
	defer srv.Stop()

	// 4. Wait for the player to confirm it received server/hello.
	select {
	case <-playerReady:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for dialed handshake")
	}
}

// findFreePort returns a TCP port that is free at call time.
func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
