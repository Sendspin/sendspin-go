// ABOUTME: Tests for WebSocket client implementation
// ABOUTME: Tests connection, handshake, and message routing
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewClient(t *testing.T) {
	config := Config{
		ServerAddr: "localhost:8927",
		ClientID:   "test-client",
		Name:       "Test Player",
	}

	client := NewClient(config)
	if client == nil {
		t.Fatal("expected client to be created")
	}

	if client.config.ServerAddr != "localhost:8927" {
		t.Errorf("expected server addr localhost:8927, got %s", client.config.ServerAddr)
	}

	if client.ArtworkChunks == nil {
		t.Error("expected ArtworkChunks channel to be initialized")
	}
}

// buildBinaryFrame constructs a binary protocol frame matching what the server
// emits: [1 byte type][8 byte big-endian timestamp µs][payload].
func buildBinaryFrame(msgType byte, timestamp int64, payload []byte) []byte {
	frame := make([]byte, BinaryMessageHeaderSize+len(payload))
	frame[0] = msgType
	binary.BigEndian.PutUint64(frame[1:BinaryMessageHeaderSize], uint64(timestamp))
	copy(frame[BinaryMessageHeaderSize:], payload)
	return frame
}

// recvWithTimeout waits briefly for a value on a channel. Returns the zero
// value + false if nothing arrives in time.
func recvAudioChunk(t *testing.T, ch <-chan AudioChunk) (AudioChunk, bool) {
	t.Helper()
	select {
	case chunk := <-ch:
		return chunk, true
	case <-time.After(100 * time.Millisecond):
		return AudioChunk{}, false
	}
}

func recvArtworkChunk(t *testing.T, ch <-chan ArtworkChunk) (ArtworkChunk, bool) {
	t.Helper()
	select {
	case chunk := <-ch:
		return chunk, true
	case <-time.After(100 * time.Millisecond):
		return ArtworkChunk{}, false
	}
}

func TestHandleBinaryMessage_AudioChunkRouting(t *testing.T) {
	client := NewClient(Config{ServerAddr: "localhost:0", ClientID: "t", Name: "t"})
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	client.handleBinaryMessage(buildBinaryFrame(AudioChunkMessageType, 123_456, payload))

	chunk, ok := recvAudioChunk(t, client.AudioChunks)
	if !ok {
		t.Fatal("expected an AudioChunk on the channel")
	}
	if chunk.Timestamp != 123_456 {
		t.Errorf("timestamp = %d, want 123456", chunk.Timestamp)
	}
	if string(chunk.Data) != string(payload) {
		t.Errorf("data = %x, want %x", chunk.Data, payload)
	}
}

// TestHandleBinaryMessage_ArtworkRouting covers all four artwork channel IDs
// (types 8, 9, 10, 11 → channels 0, 1, 2, 3). Closes #27: "Unknown binary
// message type: 8" was the spec-defined ArtworkChannel0MessageType being
// silently dropped.
func TestHandleBinaryMessage_ArtworkRouting(t *testing.T) {
	cases := []struct {
		name    string
		msgType byte
		channel int
	}{
		{"channel 0", ArtworkChannel0MessageType, 0},
		{"channel 1", ArtworkChannel1MessageType, 1},
		{"channel 2", ArtworkChannel2MessageType, 2},
		{"channel 3", ArtworkChannel3MessageType, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(Config{ServerAddr: "localhost:0", ClientID: "t", Name: "t"})
			imageBytes := []byte("fake-jpeg-bytes-here")
			timestamp := int64(999_888_777)

			client.handleBinaryMessage(buildBinaryFrame(tc.msgType, timestamp, imageBytes))

			chunk, ok := recvArtworkChunk(t, client.ArtworkChunks)
			if !ok {
				t.Fatalf("expected an ArtworkChunk on the channel (msgType=%d)", tc.msgType)
			}
			if chunk.Channel != tc.channel {
				t.Errorf("channel = %d, want %d", chunk.Channel, tc.channel)
			}
			if chunk.Timestamp != timestamp {
				t.Errorf("timestamp = %d, want %d", chunk.Timestamp, timestamp)
			}
			if string(chunk.Data) != string(imageBytes) {
				t.Errorf("data = %q, want %q", chunk.Data, imageBytes)
			}
		})
	}
}

// TestHandleBinaryMessage_UnknownTypeLogged confirms unknown types are still
// dropped (and not routed anywhere) after the artwork additions. This guards
// the switch-default branch so a future add-without-test doesn't turn a
// real bug into silent mis-routing.
func TestHandleBinaryMessage_UnknownTypeLogged(t *testing.T) {
	client := NewClient(Config{ServerAddr: "localhost:0", ClientID: "t", Name: "t"})

	// Type 99 is not defined anywhere in the spec.
	client.handleBinaryMessage(buildBinaryFrame(99, 0, []byte{0xff}))

	if _, ok := recvAudioChunk(t, client.AudioChunks); ok {
		t.Error("unknown type was routed to AudioChunks")
	}
	if _, ok := recvArtworkChunk(t, client.ArtworkChunks); ok {
		t.Error("unknown type was routed to ArtworkChunks")
	}
}

// TestBuildSupportedRoles_Default covers the old auto-built path. Keep this
// test lean; a table test for every permutation of the four V1 support
// fields is over-investment for what's essentially a handful of if
// statements.
func TestBuildSupportedRoles_Default(t *testing.T) {
	cases := []struct {
		name   string
		config Config
		want   []string
	}{
		{
			name:   "bare default is player+metadata",
			config: Config{},
			want:   []string{"player@v1", "metadata@v1"},
		},
		{
			name:   "artwork support adds artwork@v1",
			config: Config{ArtworkV1Support: &ArtworkV1Support{}},
			want:   []string{"player@v1", "metadata@v1", "artwork@v1"},
		},
		{
			name:   "visualizer support adds visualizer@v1",
			config: Config{VisualizerV1Support: &VisualizerV1Support{}},
			want:   []string{"player@v1", "metadata@v1", "visualizer@v1"},
		},
		{
			name: "both support structs set",
			config: Config{
				ArtworkV1Support:    &ArtworkV1Support{},
				VisualizerV1Support: &VisualizerV1Support{},
			},
			want: []string{"player@v1", "metadata@v1", "artwork@v1", "visualizer@v1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(tc.config)
			got := client.buildSupportedRoles()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildSupportedRoles() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildSupportedRoles_ExplicitOverride verifies that Config.SupportedRoles
// wins over the V1 support struct auto-build. This is the load-bearing
// behavior for conformance metadata-only and controller scenarios that
// need to NOT advertise player@v1.
func TestBuildSupportedRoles_ExplicitOverride(t *testing.T) {
	cases := []struct {
		name   string
		config Config
		want   []string
	}{
		{
			name:   "controller only",
			config: Config{SupportedRoles: []string{"controller@v1"}},
			want:   []string{"controller@v1"},
		},
		{
			name:   "metadata only",
			config: Config{SupportedRoles: []string{"metadata@v1"}},
			want:   []string{"metadata@v1"},
		},
		{
			// Confirms that setting ArtworkV1Support alongside an override
			// does NOT inject artwork@v1 — the caller's override is canon.
			name: "override wins over artwork support struct",
			config: Config{
				SupportedRoles:   []string{"player@v1"},
				ArtworkV1Support: &ArtworkV1Support{},
			},
			want: []string{"player@v1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(tc.config)
			got := client.buildSupportedRoles()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildSupportedRoles() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewClientFromConn_HandshakeAndClose spins up a local WebSocket server
// that plays the Sendspin handshake dance (read client/hello, send
// server/hello, read client/state, close). The test then uses
// NewClientFromConn + Start to drive the client side over an accepted
// connection, proving that server-initiated scenarios can use the library
// without hand-rolling the message loop.
func TestNewClientFromConn_HandshakeAndClose(t *testing.T) {
	// Capture the roles the client advertises so we can assert the override
	// plumbed through to the wire.
	capturedHello := make(chan ClientHello, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		// Read client/hello.
		_, helloBytes, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read client/hello: %v", err)
			return
		}
		var envelope Message
		if err := json.Unmarshal(helloBytes, &envelope); err != nil {
			t.Errorf("unmarshal client/hello: %v", err)
			return
		}
		payloadBytes, _ := json.Marshal(envelope.Payload)
		var hello ClientHello
		_ = json.Unmarshal(payloadBytes, &hello)
		capturedHello <- hello

		// Send server/hello.
		serverHello := Message{
			Type: "server/hello",
			Payload: ServerHello{
				ServerID:    "test-server",
				Name:        "Test Server",
				Version:     1,
				ActiveRoles: hello.SupportedRoles,
			},
		}
		if err := conn.WriteJSON(serverHello); err != nil {
			t.Errorf("write server/hello: %v", err)
			return
		}

		// Read client/state (the library sends this immediately after
		// a successful handshake — see handshake() in client.go).
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read client/state: %v", err)
			return
		}

		// Hold the connection open briefly so the client's read loop has
		// something to read, then close.
		time.Sleep(50 * time.Millisecond)
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	// Dial it ourselves to simulate a server-initiated scenario where the
	// caller has an accepted *websocket.Conn and hands it to the library.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	client := NewClientFromConn(Config{
		ClientID:       "test-from-conn",
		Name:           "FromConn Test",
		Version:        1,
		SupportedRoles: []string{"controller@v1"},
	}, conn)
	defer client.Close()

	if err := client.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case hello := <-capturedHello:
		if !reflect.DeepEqual(hello.SupportedRoles, []string{"controller@v1"}) {
			t.Errorf("server saw SupportedRoles = %v, want [controller@v1]", hello.SupportedRoles)
		}
		if hello.ClientID != "test-from-conn" {
			t.Errorf("server saw ClientID = %q, want test-from-conn", hello.ClientID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never captured a client/hello")
	}
}

// TestStart_NoConnection confirms Start refuses to run without a connection
// in place. Prevents a future caller from assuming Start does its own dialing.
func TestStart_NoConnection(t *testing.T) {
	client := NewClient(Config{ServerAddr: "localhost:0", ClientID: "t", Name: "t"})
	err := client.Start()
	if err == nil {
		t.Fatal("expected error when Start is called with no connection")
	}
	if !strings.Contains(err.Error(), "no connection") {
		t.Errorf("error message = %q, want substring %q", err.Error(), "no connection")
	}
}
