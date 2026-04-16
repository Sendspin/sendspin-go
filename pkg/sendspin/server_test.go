// ABOUTME: Integration tests for Server API
// ABOUTME: Tests server creation, startup, client connections, and streaming
package sendspin

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
)

func TestNewServer(t *testing.T) {
	source := NewTestTone(48000, 2)

	tests := []struct {
		name      string
		config    ServerConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: ServerConfig{
				Port:   8928,
				Name:   "Test Server",
				Source: source,
			},
			expectErr: false,
		},
		{
			name: "missing source",
			config: ServerConfig{
				Port: 8928,
				Name: "Test Server",
			},
			expectErr: true,
		},
		{
			name: "default port",
			config: ServerConfig{
				Name:   "Test Server",
				Source: source,
			},
			expectErr: false,
		},
		{
			name: "default name",
			config: ServerConfig{
				Port:   8928,
				Source: source,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config)

			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if server == nil {
				t.Fatal("expected server to be created")
			}

			// Verify defaults
			if server.config.Port == 0 {
				t.Error("port should have been set to default")
			}
			if server.config.Name == "" {
				t.Error("name should have been set to default")
			}
		})
	}
}

func TestServerStartStop(t *testing.T) {
	source := NewTestTone(48000, 2)

	server, err := NewServer(ServerConfig{
		Port:   8929,
		Name:   "Test Server",
		Source: source,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop server
	server.Stop()

	// Wait for server to stop
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not stop within timeout")
	}
}

func TestServerClientConnection(t *testing.T) {
	source := NewTestTone(48000, 2)

	server, err := NewServer(ServerConfig{
		Port:   8930,
		Name:   "Test Server",
		Source: source,
		Debug:  true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Connect as client
	wsURL := "ws://localhost:8930/sendspin"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send client/hello with versioned roles per spec
	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "test-client-1",
			Name:           "Test Client",
			Version:        1,
			SupportedRoles: []string{"player@v1"},
			PlayerV1Support: &protocol.PlayerV1Support{
				SupportedFormats: []protocol.AudioFormat{
					{
						Codec:      "pcm",
						Channels:   2,
						SampleRate: 48000,
						BitDepth:   24,
					},
				},
				BufferCapacity:    1048576,
				SupportedCommands: []string{"volume", "mute"},
			},
		},
	}

	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	// Read server/hello response
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read server hello: %v", err)
	}

	if msg.Type != "server/hello" {
		t.Errorf("expected server/hello, got %s", msg.Type)
	}

	// Parse server hello
	helloData, _ := json.Marshal(msg.Payload)
	var serverHello protocol.ServerHello
	if err := json.Unmarshal(helloData, &serverHello); err != nil {
		t.Fatalf("failed to unmarshal server hello: %v", err)
	}

	if serverHello.Name != "Test Server" {
		t.Errorf("expected server name 'Test Server', got %s", serverHello.Name)
	}

	// Verify active_roles is present per spec
	if len(serverHello.ActiveRoles) == 0 {
		t.Error("expected active_roles to be set")
	}

	// Verify connection_reason is present per spec
	if serverHello.ConnectionReason == "" {
		t.Error("expected connection_reason to be set")
	}

	// Read stream/start
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read stream/start: %v", err)
	}

	if msg.Type != "stream/start" {
		t.Errorf("expected stream/start, got %s", msg.Type)
	}

	// Read server/state (replaces stream/metadata per spec)
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read server/state: %v", err)
	}

	if msg.Type != "server/state" {
		t.Errorf("expected server/state, got %s", msg.Type)
	}

	// Read group/update per spec
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read group/update: %v", err)
	}

	if msg.Type != "group/update" {
		t.Errorf("expected group/update, got %s", msg.Type)
	}

	// Read audio chunk (binary message)
	msgType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read audio chunk: %v", err)
	}

	if msgType != websocket.BinaryMessage {
		t.Errorf("expected binary message, got type %d", msgType)
	}

	// Verify chunk format: [type:1][timestamp:8][audio_data:N]
	if len(data) < 9 {
		t.Errorf("audio chunk too small: %d bytes", len(data))
	}

	// Per spec: audio chunks use message type 4 (player role, slot 0)
	if data[0] != AudioChunkMessageType {
		t.Errorf("expected message type %d, got %d", AudioChunkMessageType, data[0])
	}

	// Check that clients list includes our client
	clients := server.Clients()
	if len(clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(clients))
	}

	if clients[0].ID != "test-client-1" {
		t.Errorf("expected client ID 'test-client-1', got %s", clients[0].ID)
	}

	// Close connection
	conn.Close()

	// Give server time to handle disconnect
	time.Sleep(100 * time.Millisecond)

	// Verify client was removed
	clients = server.Clients()
	if len(clients) != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", len(clients))
	}

	// Stop server
	server.Stop()

	select {
	case <-errChan:
	case <-time.After(5 * time.Second):
		t.Error("server did not stop within timeout")
	}
}

func TestServerMultipleClients(t *testing.T) {
	source := NewTestTone(48000, 2)

	server, err := NewServer(ServerConfig{
		Port:   8931,
		Name:   "Test Server",
		Source: source,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	go server.Start()
	time.Sleep(200 * time.Millisecond)

	// Connect multiple clients
	clients := make([]*websocket.Conn, 3)
	for i := 0; i < 3; i++ {
		wsURL := "ws://localhost:8931/sendspin"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		clients[i] = conn

		// Send hello with versioned roles
		hello := protocol.Message{
			Type: "client/hello",
			Payload: protocol.ClientHello{
				ClientID:       fmt.Sprintf("test-client-%d", i),
				Name:           fmt.Sprintf("Test Client %d", i),
				Version:        1,
				SupportedRoles: []string{"player@v1"},
				PlayerV1Support: &protocol.PlayerV1Support{
					SupportedFormats: []protocol.AudioFormat{
						{
							Codec:      "pcm",
							Channels:   2,
							SampleRate: 48000,
							BitDepth:   24,
						},
					},
					BufferCapacity:    1048576,
					SupportedCommands: []string{"volume", "mute"},
				},
			},
		}

		if err := conn.WriteJSON(hello); err != nil {
			t.Fatalf("failed to send hello from client %d: %v", i, err)
		}

		// Read server/hello
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("failed to read server hello for client %d: %v", i, err)
		}
	}

	// Give server time to register all clients
	time.Sleep(100 * time.Millisecond)

	// Check that all clients are registered
	serverClients := server.Clients()
	if len(serverClients) != 3 {
		t.Errorf("expected 3 clients, got %d", len(serverClients))
	}

	// Close all connections
	for i, conn := range clients {
		if err := conn.Close(); err != nil {
			t.Errorf("failed to close client %d: %v", i, err)
		}
	}

	// Give server time to handle disconnects
	time.Sleep(100 * time.Millisecond)

	// Verify all clients were removed
	serverClients = server.Clients()
	if len(serverClients) != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", len(serverClients))
	}

	// Stop server
	server.Stop()
	time.Sleep(100 * time.Millisecond)
}

// TestServer_GroupReceivesEventsFromRealHandshake is the real wiring
// guard for M2: it stands up an actual Server, subscribes to the
// default group BEFORE the server starts accepting connections, then
// drives a full WebSocket handshake via the gorilla client and asserts
// that ClientJoinedEvent and ClientLeftEvent are delivered through the
// event bus. A future refactor that moved defaultGroup.addClient out of
// handleConnection (or routed it through a different Group) would fail
// this test, which is exactly the guarantee the helper-level test cannot
// provide.
func TestServer_GroupReceivesEventsFromRealHandshake(t *testing.T) {
	server, err := NewServer(ServerConfig{
		Port:   8933,
		Name:   "Group Wiring Test",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Subscribe BEFORE Start so the join event is guaranteed to land.
	events, unsubscribe := server.Group().Subscribe()
	defer unsubscribe()

	errChan := make(chan error, 1)
	go func() { errChan <- server.Start() }()
	defer func() {
		server.Stop()
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop within timeout")
		}
	}()

	time.Sleep(200 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8933/sendspin", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "wiring-test-client",
			Name:           "Wiring Test Client",
			Version:        1,
			SupportedRoles: []string{"player@v1"},
			PlayerV1Support: &protocol.PlayerV1Support{
				SupportedFormats: []protocol.AudioFormat{
					{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 24},
				},
				BufferCapacity: 1048576,
			},
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		conn.Close()
		t.Fatalf("write hello: %v", err)
	}

	// Wait for the ClientJoinedEvent delivered through the real wiring.
	select {
	case evt := <-events:
		joined, ok := evt.(ClientJoinedEvent)
		if !ok {
			conn.Close()
			t.Fatalf("first event = %T, want ClientJoinedEvent", evt)
		}
		if joined.Client.ID() != "wiring-test-client" {
			t.Errorf("joined Client.ID() = %q, want %q", joined.Client.ID(), "wiring-test-client")
		}
	case <-time.After(2 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ClientJoinedEvent from real handshake")
	}

	// Disconnect and assert the matching ClientLeftEvent fires.
	conn.Close()

	select {
	case evt := <-events:
		left, ok := evt.(ClientLeftEvent)
		if !ok {
			t.Fatalf("second event = %T, want ClientLeftEvent", evt)
		}
		if left.ClientID != "wiring-test-client" {
			t.Errorf("left ClientID = %q, want %q", left.ClientID, "wiring-test-client")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ClientLeftEvent after disconnect")
	}
}

// TestServer_ControllerCommandEndToEnd exercises the full client/command
// pipeline: Server + ControllerGroupRole + real WebSocket handshake +
// client/command message → OnCommand callback fires.
func TestServer_ControllerCommandEndToEnd(t *testing.T) {
	commandReceived := make(chan string, 1)

	server, err := NewServer(ServerConfig{
		Port:   8934,
		Name:   "Controller Test",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next", "previous"},
		OnCommand: func(c *ServerClient, command string) {
			commandReceived <- command
		},
	})
	server.Group().RegisterRole(ctrl)

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

	time.Sleep(200 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8934/sendspin", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "ctrl-test-client",
			Name:           "Controller Test Client",
			Version:        1,
			SupportedRoles: []string{"controller@v1"},
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// Drain the server/hello response.
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read server/hello: %v", err)
	}

	// Give the join event time to dispatch to the controller role.
	time.Sleep(100 * time.Millisecond)

	// Send a controller command.
	cmd := protocol.Message{
		Type: "client/command",
		Payload: map[string]interface{}{
			"controller": map[string]interface{}{
				"command": "next",
			},
		},
	}
	if err := conn.WriteJSON(cmd); err != nil {
		t.Fatalf("write client/command: %v", err)
	}

	select {
	case got := <-commandReceived:
		if got != "next" {
			t.Errorf("command = %q, want %q", got, "next")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnCommand callback")
	}
}

// TestServer_ActivateRolesDefaultsToPlayerMetadata confirms backward
// compat: when ServerConfig.SupportedRoles is nil, activateRoles still
// accepts "player" and "metadata" and rejects unknown families.
func TestServer_ActivateRolesDefault(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "test",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	got := s.activateRoles([]string{"player@v1", "metadata@v1", "controller@v1", "artwork@v1"})

	want := map[string]bool{"player@v1": true, "metadata@v1": true}
	gotSet := make(map[string]bool)
	for _, r := range got {
		gotSet[r] = true
	}
	if len(gotSet) != len(want) {
		t.Errorf("activateRoles default = %v, want player+metadata only", got)
	}
	for r := range want {
		if !gotSet[r] {
			t.Errorf("missing expected role %s in %v", r, got)
		}
	}
}

// TestServer_ActivateRolesFromConfig confirms that explicitly listing
// roles in ServerConfig.SupportedRoles controls what gets activated.
func TestServer_ActivateRolesFromConfig(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:           0,
		Name:           "test",
		Source:         NewTestTone(48000, 2),
		SupportedRoles: []string{"player", "artwork"},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	got := s.activateRoles([]string{"player@v1", "metadata@v1", "artwork@v1"})

	gotSet := make(map[string]bool)
	for _, r := range got {
		gotSet[r] = true
	}
	if gotSet["metadata@v1"] {
		t.Error("metadata should NOT be active when not in SupportedRoles")
	}
	if !gotSet["player@v1"] || !gotSet["artwork@v1"] {
		t.Errorf("expected player+artwork, got %v", got)
	}
}

// TestServer_ActivateRolesFromGroupRegistry confirms that registering a
// GroupRole on the server's Group auto-activates that role family even
// when it's not in ServerConfig.SupportedRoles.
func TestServer_ActivateRolesFromGroupRegistry(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "test",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Register a controller role — should auto-activate "controller".
	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next"},
	})
	s.Group().RegisterRole(ctrl)

	got := s.activateRoles([]string{"player@v1", "controller@v1"})

	gotSet := make(map[string]bool)
	for _, r := range got {
		gotSet[r] = true
	}
	if !gotSet["player@v1"] {
		t.Error("player should be active (default)")
	}
	if !gotSet["controller@v1"] {
		t.Error("controller should be active (registered via GroupRole)")
	}
}

func TestServerDuplicateClientID(t *testing.T) {
	source := NewTestTone(48000, 2)

	server, err := NewServer(ServerConfig{
		Port:   8932,
		Name:   "Test Server",
		Source: source,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	go server.Start()
	time.Sleep(200 * time.Millisecond)

	// Connect first client
	wsURL := "ws://localhost:8932/sendspin"
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first client: %v", err)
	}
	defer conn1.Close()

	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "duplicate-id",
			Name:           "First Client",
			Version:        1,
			SupportedRoles: []string{"player@v1"},
		},
	}

	if err := conn1.WriteJSON(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	// Read server/hello
	var msg protocol.Message
	if err := conn1.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read server hello: %v", err)
	}

	// Try to connect second client with same ID
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect second client: %v", err)
	}
	defer conn2.Close()

	if err := conn2.WriteJSON(hello); err != nil {
		t.Fatalf("failed to send hello from second client: %v", err)
	}

	// Second client should be rejected - connection should close
	// Try to read a message - should get an error
	conn2.SetReadDeadline(time.Now().Add(1 * time.Second))
	err = conn2.ReadJSON(&msg)
	if err == nil {
		// If we got a message, it should be an error message
		if msg.Type == "server/error" {
			// This is expected
		} else {
			t.Errorf("expected error or connection close for duplicate ID, got message type: %s", msg.Type)
		}
	}

	// Verify only one client is registered
	serverClients := server.Clients()
	if len(serverClients) != 1 {
		t.Errorf("expected 1 client, got %d", len(serverClients))
	}

	// Stop server
	server.Stop()
	time.Sleep(100 * time.Millisecond)
}
