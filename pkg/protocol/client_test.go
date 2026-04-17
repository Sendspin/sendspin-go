// ABOUTME: Tests for WebSocket client implementation
// ABOUTME: Tests connection, handshake, and message routing
package protocol

import (
	"bytes"
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
			name:   "bare default is player+metadata+controller",
			config: Config{},
			want:   []string{"player@v1", "metadata@v1", "controller@v1"},
		},
		{
			name:   "artwork support adds artwork@v1",
			config: Config{ArtworkV1Support: &ArtworkV1Support{}},
			want:   []string{"player@v1", "metadata@v1", "controller@v1", "artwork@v1"},
		},
		{
			name:   "visualizer support adds visualizer@v1",
			config: Config{VisualizerV1Support: &VisualizerV1Support{}},
			want:   []string{"player@v1", "metadata@v1", "controller@v1", "visualizer@v1"},
		},
		{
			name: "both support structs set",
			config: Config{
				ArtworkV1Support:    &ArtworkV1Support{},
				VisualizerV1Support: &VisualizerV1Support{},
			},
			want: []string{"player@v1", "metadata@v1", "controller@v1", "artwork@v1", "visualizer@v1"},
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

	// ServerHello() and RawServerHello() must return the handshake data
	// after Start() completes. The test server above responds with a hello
	// whose ServerID is "test-server" and Name is "Test Server", so both
	// accessors should agree.
	parsed := client.ServerHello()
	if parsed == nil {
		t.Fatal("ServerHello() returned nil after successful handshake")
	}
	if parsed.ServerID != "test-server" {
		t.Errorf("ServerHello().ServerID = %q, want test-server", parsed.ServerID)
	}
	if parsed.Name != "Test Server" {
		t.Errorf("ServerHello().Name = %q, want Test Server", parsed.Name)
	}

	raw := client.RawServerHello()
	if len(raw) == 0 {
		t.Fatal("RawServerHello() returned empty bytes after successful handshake")
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("RawServerHello() did not return valid JSON: %v", err)
	}
	if envelope["type"] != "server/hello" {
		t.Errorf("raw envelope type = %v, want server/hello", envelope["type"])
	}

	// Confirm RawServerHello returns a copy: mutating the returned slice
	// must not affect subsequent calls.
	if len(raw) > 0 {
		raw[0] = 0xFF
	}
	raw2 := client.RawServerHello()
	if bytes.Equal(raw, raw2) {
		t.Error("RawServerHello returned a shared reference; mutation leaked into the client")
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

// TestServerHello_BeforeHandshake guards against nil confusion: the
// accessors must return nil/empty before Start() runs, not stale data
// from a previous client or a zero-value ServerHello.
func TestServerHello_BeforeHandshake(t *testing.T) {
	client := NewClient(Config{ServerAddr: "localhost:0", ClientID: "t", Name: "t"})

	if hello := client.ServerHello(); hello != nil {
		t.Errorf("ServerHello() before handshake = %+v, want nil", hello)
	}
	if raw := client.RawServerHello(); raw != nil {
		t.Errorf("RawServerHello() before handshake = %v, want nil", raw)
	}
}

// TestClientSend_WritesEnvelope verifies that Client.Send emits a correctly
// shaped {"type": ..., "payload": ...} envelope on the wire. The test
// server reads a client/command message after handshake and asserts on the
// envelope shape, which is exactly what the conformance adapter's
// controller scenarios need.
func TestClientSend_WritesEnvelope(t *testing.T) {
	capturedCommand := make(chan map[string]any, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		// Handshake: read client/hello, write server/hello, read client/state.
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read client/hello: %v", err)
			return
		}
		if err := conn.WriteJSON(Message{
			Type:    "server/hello",
			Payload: ServerHello{ServerID: "srv", Name: "srv", Version: 1},
		}); err != nil {
			t.Errorf("write server/hello: %v", err)
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read client/state: %v", err)
			return
		}

		// Now read the client/command the test below will send via Send().
		_, cmdBytes, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read client/command: %v", err)
			return
		}
		var envelope map[string]any
		if err := json.Unmarshal(cmdBytes, &envelope); err != nil {
			t.Errorf("unmarshal envelope: %v", err)
			return
		}
		capturedCommand <- envelope

		time.Sleep(20 * time.Millisecond)
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	client := NewClientFromConn(Config{
		ClientID:       "sender",
		Name:           "Sender Test",
		Version:        1,
		SupportedRoles: []string{"controller@v1"},
	}, conn)
	defer client.Close()

	if err := client.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	payload := map[string]any{"controller": map[string]any{"command": "next"}}
	if err := client.Send("client/command", payload); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case envelope := <-capturedCommand:
		if envelope["type"] != "client/command" {
			t.Errorf("envelope.type = %v, want client/command", envelope["type"])
		}
		innerPayload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("envelope.payload not a map: %v", envelope["payload"])
		}
		controller, ok := innerPayload["controller"].(map[string]any)
		if !ok {
			t.Fatalf("payload.controller not a map: %v", innerPayload["controller"])
		}
		if controller["command"] != "next" {
			t.Errorf("controller.command = %v, want next", controller["command"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never captured a client/command")
	}
}

// TestHandshake_PlayerSupportGatedByRoles is a regression test for a real
// bug caught by the conformance harness against aiosendspin. The Go
// library used to emit player@v1_support in every client/hello regardless
// of the advertised role list. aiosendspin's schema marks
// ClientHelloPlayerSupport.supported_formats as non-nullable, so a zero-
// value Go PlayerV1Support (whose nil slice encodes as JSON null) caused
// aiosendspin's mashumaro deserializer to reject the entire hello and
// close the connection with code 1000. sendspin-go then reported
// "handshake failed: failed to read server/hello: websocket: close 1000".
//
// The fix: only set hello.PlayerV1Support when player@v1 is in the
// final advertised role list. This mirrors how ArtworkV1Support and
// VisualizerV1Support were already being handled (only set when
// non-nil in config), and aligns with aiosendspin's own validation
// comment: "player@v1_support must be provided when 'player@v1' is in
// supported_roles".
//
// The test captures the raw hello bytes on the wire and asserts the
// top-level payload key is absent for non-player roles, and present
// for player@v1 (the default).
func TestHandshake_PlayerSupportGatedByRoles(t *testing.T) {
	// playerSupport is populated for the "advertised player" cases so the
	// emitted player@v1_support block is a well-formed one — matches what
	// a real caller would pass — and the secondary null-check assertion
	// below is meaningful rather than a false positive.
	playerSupport := PlayerV1Support{
		SupportedFormats: []AudioFormat{
			{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
		},
		BufferCapacity:    1_000_000,
		SupportedCommands: []string{"volume", "mute"},
	}

	cases := []struct {
		name            string
		supportedRoles  []string
		playerV1Support PlayerV1Support
		wantPlayerKey   bool
	}{
		{
			name:           "controller only omits player support",
			supportedRoles: []string{"controller@v1"},
			wantPlayerKey:  false,
		},
		{
			name:           "metadata only omits player support",
			supportedRoles: []string{"metadata@v1"},
			wantPlayerKey:  false,
		},
		{
			name:           "artwork only omits player support",
			supportedRoles: []string{"artwork@v1"},
			wantPlayerKey:  false,
		},
		{
			name:            "player advertised keeps player support",
			supportedRoles:  []string{"player@v1"},
			playerV1Support: playerSupport,
			wantPlayerKey:   true,
		},
		{
			name:            "player plus other roles keeps player support",
			supportedRoles:  []string{"player@v1", "controller@v1"},
			playerV1Support: playerSupport,
			wantPlayerKey:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capturedHello := make(chan []byte, 1)

			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			handler := func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upgrade: %v", err)
					return
				}
				defer conn.Close()

				_, helloBytes, err := conn.ReadMessage()
				if err != nil {
					t.Errorf("read client/hello: %v", err)
					return
				}
				// Copy the slice; gorilla reuses its underlying buffer.
				snapshot := make([]byte, len(helloBytes))
				copy(snapshot, helloBytes)
				capturedHello <- snapshot

				// Drive the rest of the handshake so the client goroutine
				// doesn't error out on a hard close mid-stream.
				_ = conn.WriteJSON(Message{
					Type:    "server/hello",
					Payload: ServerHello{ServerID: "srv", Name: "srv", Version: 1},
				})
				_, _, _ = conn.ReadMessage() // client/state
				time.Sleep(20 * time.Millisecond)
			}

			server := httptest.NewServer(http.HandlerFunc(handler))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}

			client := NewClientFromConn(Config{
				ClientID:        "test",
				Name:            "Test",
				Version:         1,
				SupportedRoles:  tc.supportedRoles,
				PlayerV1Support: tc.playerV1Support,
			}, conn)
			defer client.Close()

			if err := client.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}

			var helloBytes []byte
			select {
			case helloBytes = <-capturedHello:
			case <-time.After(2 * time.Second):
				t.Fatal("server never captured a client/hello")
			}

			var envelope map[string]any
			if err := json.Unmarshal(helloBytes, &envelope); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			payload, ok := envelope["payload"].(map[string]any)
			if !ok {
				t.Fatalf("envelope.payload is not a map: %v", envelope["payload"])
			}

			_, havePlayerKey := payload["player@v1_support"]
			if havePlayerKey != tc.wantPlayerKey {
				if tc.wantPlayerKey {
					t.Errorf("client/hello missing player@v1_support when player@v1 is advertised:\n%s", helloBytes)
				} else {
					t.Errorf("client/hello includes player@v1_support when player@v1 is NOT advertised (%v):\n%s",
						tc.supportedRoles, helloBytes)
				}
			}

			// When PlayerSupport is emitted and its supported_formats is nil,
			// the JSON encoder produces null. Catch that too — a nil-slice
			// null would still fail against aiosendspin's schema even if
			// we correctly gated on roles.
			if havePlayerKey {
				playerSupport, ok := payload["player@v1_support"].(map[string]any)
				if !ok {
					t.Fatalf("player@v1_support is not a map: %v", payload["player@v1_support"])
				}
				if playerSupport["supported_formats"] == nil {
					t.Error("player@v1_support.supported_formats serialized as null; aiosendspin's schema rejects this")
				}
			}
		})
	}
}
