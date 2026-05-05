// ABOUTME: Integration tests for Server API
// ABOUTME: Tests server creation, startup, client connections, and streaming
package sendspin

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
)

// waitForServerPort polls until server.Addr() returns the bound TCP port
// after Start runs with Port: 0. Fails the test if the listener isn't
// bound within 2s.
func waitForServerPort(t *testing.T, server *Server) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("server did not bind within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return server.Addr().(*net.TCPAddr).Port
}

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
				Port:   0,
				Name:   "Test Server",
				Source: source,
			},
			expectErr: false,
		},
		{
			name: "missing source",
			config: ServerConfig{
				Port: 0,
				Name: "Test Server",
			},
			expectErr: true,
		},
		{
			name: "ephemeral port (omitted)",
			config: ServerConfig{
				Name:   "Test Server",
				Source: source,
			},
			expectErr: false,
		},
		{
			name: "default name",
			config: ServerConfig{
				Port:   0,
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

			// NewServer no longer substitutes a default Port — Port: 0 is
			// honored as "let the OS pick" so tests bind ephemerally.
			// Name still defaults when omitted.
			if server.config.Name == "" {
				t.Error("name should have been set to default")
			}
		})
	}
}

func TestServerStartStop(t *testing.T) {
	source := NewTestTone(48000, 2)

	server, err := NewServer(ServerConfig{
		Port:   0,
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

	// Wait for listener to bind, then give the rest of Start a moment.
	waitForServerPort(t, server)
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
		Port:   0,
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

	// Wait for listener to bind, then give the rest of Start a moment.
	port := waitForServerPort(t, server)
	time.Sleep(200 * time.Millisecond)

	// Connect as client
	wsURL := fmt.Sprintf("ws://localhost:%d/sendspin", port)
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
			SupportedRoles: []string{"player@v1", "metadata@v1"},
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

	// Read the three control messages in any order. After the role
	// dispatcher migration (M5), group/update comes from Group.addClient
	// (synchronous) while stream/start and server/state come from role
	// handlers dispatched asynchronously — so the order is not guaranteed.
	// Audio binary chunks may also arrive interleaved with the control
	// messages since the streaming ticker runs independently.
	expectedMsgs := map[string]bool{
		"group/update": false,
		"stream/start": false,
		"server/state": false,
	}
	var firstAudioChunk []byte
	controlCount := 0
	for controlCount < 3 {
		msgType, rawData, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("failed to read message (have %d of 3 control msgs): %v", controlCount, err)
		}
		if msgType == websocket.BinaryMessage {
			// Audio chunk arrived before all control messages; save the
			// first one so we can validate it below.
			if firstAudioChunk == nil {
				firstAudioChunk = rawData
			}
			continue
		}
		if err := json.Unmarshal(rawData, &msg); err != nil {
			t.Fatalf("failed to unmarshal control message: %v", err)
		}
		if _, ok := expectedMsgs[msg.Type]; !ok {
			t.Fatalf("unexpected message type: %s", msg.Type)
		}
		expectedMsgs[msg.Type] = true
		controlCount++
	}
	for msgType, received := range expectedMsgs {
		if !received {
			t.Errorf("never received expected %s message", msgType)
		}
	}

	// Read audio chunk — either we already captured one above or read next.
	var data []byte
	if firstAudioChunk != nil {
		data = firstAudioChunk
	} else {
		var readMsgType int
		readMsgType, data, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("failed to read audio chunk: %v", err)
		}
		if readMsgType != websocket.BinaryMessage {
			t.Errorf("expected binary message, got type %d", readMsgType)
		}
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
		Port:   0,
		Name:   "Test Server",
		Source: source,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	go server.Start()
	port := waitForServerPort(t, server)
	time.Sleep(200 * time.Millisecond)

	// Connect multiple clients
	clients := make([]*websocket.Conn, 3)
	wsURL := fmt.Sprintf("ws://localhost:%d/sendspin", port)
	for i := 0; i < 3; i++ {
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
		Port:   0,
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

	port := waitForServerPort(t, server)
	time.Sleep(200 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://localhost:%d/sendspin", port), nil)
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
		Port:   0,
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

	port := waitForServerPort(t, server)
	time.Sleep(200 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://localhost:%d/sendspin", port), nil)
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
// Note: NewServer always registers player and metadata GroupRoles on
// the default group, so those families are always active via the group
// registry regardless of SupportedRoles. This test verifies that
// SupportedRoles adds additional families (artwork) beyond those.
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
	// metadata is active because NewServer registers MetadataGroupRole
	// on the default group, which auto-activates it.
	if !gotSet["metadata@v1"] {
		t.Error("metadata should be active (registered via GroupRole in NewServer)")
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
		Port:   0,
		Name:   "Test Server",
		Source: source,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	go server.Start()
	port := waitForServerPort(t, server)
	time.Sleep(200 * time.Millisecond)

	// Connect first client
	wsURL := fmt.Sprintf("ws://localhost:%d/sendspin", port)
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

// TestServer_FLACStreamNegotiation verifies that a client advertising
// FLAC as its preferred codec receives stream/start with codec="flac"
// and a valid codec_header, followed by binary FLAC audio chunks.
func TestServer_FLACStreamNegotiation(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "FLAC Test Server",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	errChan := make(chan error, 1)
	go func() { errChan <- s.Start() }()
	defer func() {
		s.Stop()
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
			t.Error("server stop timeout")
		}
	}()

	port := waitForServerPort(t, s)
	time.Sleep(200 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://localhost:%d/sendspin", port), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Advertise FLAC as first format (simulates --preferred-codec flac)
	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "flac-test-client",
			Name:           "FLAC Test Client",
			Version:        1,
			SupportedRoles: []string{"player@v1", "metadata@v1"},
			PlayerV1Support: &protocol.PlayerV1Support{
				SupportedFormats: []protocol.AudioFormat{
					{Codec: "flac", SampleRate: 48000, Channels: 2, BitDepth: 24},
					{Codec: "pcm", SampleRate: 48000, Channels: 2, BitDepth: 24},
				},
				BufferCapacity: 1048576,
			},
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// Read messages looking for stream/start with FLAC codec.
	// The order of messages after server/hello is non-deterministic
	// (group/update, stream/start, server/state may interleave).
	var foundStreamStart bool
	var codecHeader string
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}

		if msgType == websocket.BinaryMessage {
			// Got a binary audio chunk before finding stream/start
			if !foundStreamStart {
				t.Fatal("received binary chunk before stream/start")
			}
			// Verify it's a valid audio chunk (at least header + some data)
			if len(data) < 10 {
				t.Errorf("audio chunk too small: %d bytes", len(data))
			}
			t.Logf("Received FLAC audio chunk: %d bytes", len(data))
			break // Success — we got stream/start + at least one chunk
		}

		// Text message — parse the envelope
		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if msg.Type == "stream/start" {
			startData, _ := json.Marshal(msg.Payload)
			var streamStart protocol.StreamStart
			if err := json.Unmarshal(startData, &streamStart); err != nil {
				t.Fatalf("unmarshal stream/start: %v", err)
			}

			if streamStart.Player == nil {
				t.Fatal("stream/start has no player info")
			}
			if streamStart.Player.Codec != "flac" {
				t.Errorf("codec = %q, want flac", streamStart.Player.Codec)
			}
			if streamStart.Player.CodecHeader == "" {
				t.Error("codec_header is empty — FLAC requires STREAMINFO")
			}
			if streamStart.Player.SampleRate != 48000 {
				t.Errorf("sample_rate = %d, want 48000", streamStart.Player.SampleRate)
			}
			if streamStart.Player.BitDepth != 24 {
				t.Errorf("bit_depth = %d, want 24", streamStart.Player.BitDepth)
			}

			codecHeader = streamStart.Player.CodecHeader
			foundStreamStart = true
			t.Logf("stream/start received: codec=flac, codec_header=%d chars (base64)", len(codecHeader))
		}
	}

	if !foundStreamStart {
		t.Fatal("never received stream/start with FLAC")
	}
}

// TestServer_BufferTrackerLimitsChunks verifies that the server's
// BufferTracker prevents buffer overflow. A client advertising a tiny
// buffer_capacity should receive fewer chunks than one with a large
// capacity, because the server skips sends when the tracker is full.
func TestServer_BufferTrackerLimitsChunks(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "Buffer Test",
		Source: NewTestTone(48000, 2),
		Debug:  true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	errChan := make(chan error, 1)
	go func() { errChan <- s.Start() }()
	defer func() {
		s.Stop()
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
			t.Error("server stop timeout")
		}
	}()

	port := waitForServerPort(t, s)
	time.Sleep(200 * time.Millisecond)

	// Connect with a small buffer capacity (20000 bytes).
	// At 48kHz stereo 24-bit PCM, one 20ms chunk is ~5760 bytes of audio
	// plus the 9-byte chunk header (~5769 total). So 20000 bytes fits ~3
	// chunks at a time. As playback time advances, PruneConsumed frees
	// slots, but the tracker still throttles the send rate well below 50
	// chunks/second.
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://localhost:%d/sendspin", port), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hello := protocol.Message{
		Type: "client/hello",
		Payload: protocol.ClientHello{
			ClientID:       "buffer-test-client",
			Name:           "Buffer Test Client",
			Version:        1,
			SupportedRoles: []string{"player@v1", "metadata@v1"},
			PlayerV1Support: &protocol.PlayerV1Support{
				SupportedFormats: []protocol.AudioFormat{
					{Codec: "pcm", SampleRate: 48000, Channels: 2, BitDepth: 24},
				},
				BufferCapacity: 20000, // Small — fits ~3 PCM chunks at a time
			},
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// Read messages for 1 second and count binary chunks received.
	binaryCount := 0
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		msgType, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage {
			binaryCount++
		}
	}

	// At 50 chunks/second for 1 second, an unlimited client would get ~50 chunks.
	// With a 5000-byte capacity, the tracker should limit this significantly.
	// The exact number depends on timing (PruneConsumed frees slots as
	// playback time passes), but it should be well under 50.
	t.Logf("Received %d binary chunks in 1s (unlimited would be ~50)", binaryCount)

	// We just verify the tracker is working — some chunks should arrive
	// (the first one always fits), but far fewer than 50.
	if binaryCount >= 50 {
		t.Errorf("received %d chunks — buffer tracker doesn't seem to be limiting", binaryCount)
	}
	if binaryCount == 0 {
		t.Error("received 0 chunks — at least the first should have been sent")
	}
}
