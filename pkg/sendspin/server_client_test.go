// ABOUTME: Tests for the ServerClient exported accessor surface
// ABOUTME: Guards the shape the Group/GroupRole layer will build on in M2+
package sendspin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
)

// TestServerClient_Accessors confirms that the six exported accessors on
// ServerClient return the values the struct was constructed with. These are
// the methods the future Group/GroupRole layer will depend on, so their
// shape is load-bearing — adding a test now pins it.
func TestServerClient_Accessors(t *testing.T) {
	sc := &ServerClient{
		id:    "client-abc",
		name:  "Living Room",
		roles: []string{"player@v1", "metadata@v1"},
	}

	if got := sc.ID(); got != "client-abc" {
		t.Errorf("ID() = %q, want %q", got, "client-abc")
	}
	if got := sc.Name(); got != "Living Room" {
		t.Errorf("Name() = %q, want %q", got, "Living Room")
	}
	roles := sc.Roles()
	if len(roles) != 2 || roles[0] != "player@v1" || roles[1] != "metadata@v1" {
		t.Errorf("Roles() = %v, want [player@v1 metadata@v1]", roles)
	}
}

// TestServerClient_RolesReturnsCopy guards the defensive-copy contract.
// Mutating the returned slice must not affect a subsequent Roles() call.
func TestServerClient_RolesReturnsCopy(t *testing.T) {
	sc := &ServerClient{roles: []string{"player@v1", "metadata@v1"}}

	first := sc.Roles()
	first[0] = "tampered"

	second := sc.Roles()
	if second[0] != "player@v1" {
		t.Errorf("Roles() returned aliased slice: got %q after mutation, want %q", second[0], "player@v1")
	}
}

// TestServerClient_HasRole covers both exact matches and versioned matches
// (e.g., "player" should match "player@v1"). This mirrors the existing
// Server.hasRole behavior, which the accessor replaces.
func TestServerClient_HasRole(t *testing.T) {
	sc := &ServerClient{roles: []string{"player@v1", "metadata@v1"}}

	cases := []struct {
		role string
		want bool
	}{
		{"player", true},
		{"player@v1", true},
		{"metadata", true},
		{"controller", false},
		{"artwork", false},
	}
	for _, tc := range cases {
		if got := sc.HasRole(tc.role); got != tc.want {
			t.Errorf("HasRole(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

// TestServerClient_SendBufferFull guards the back-pressure behavior: Send
// must not block when the buffered sendChan is full. Returning an error
// lets the caller decide whether to drop, log, or disconnect.
func TestServerClient_SendBufferFull(t *testing.T) {
	sc := &ServerClient{
		sendChan: make(chan interface{}, 1),
	}

	if err := sc.Send("server/state", map[string]string{"a": "b"}); err != nil {
		t.Fatalf("first Send should succeed, got %v", err)
	}
	if err := sc.Send("server/state", map[string]string{"c": "d"}); err == nil {
		t.Error("second Send to full buffer should return error, got nil")
	}
}

// TestServerClient_SendBinaryBufferFull is the same back-pressure check
// for the binary path (audio chunks go through here).
func TestServerClient_SendBinaryBufferFull(t *testing.T) {
	sc := &ServerClient{
		sendChan: make(chan interface{}, 1),
	}
	if err := sc.SendBinary([]byte{0x01}); err != nil {
		t.Fatalf("first SendBinary should succeed, got %v", err)
	}
	if err := sc.SendBinary([]byte{0x02}); err == nil {
		t.Error("second SendBinary to full buffer should return error, got nil")
	}
}

// TestServerClient_StateAccessors confirms that State/Volume/Muted/Codec
// return the mutable playback fields under the client's RWMutex. These
// are the fields the M2 Group event bus carries in ClientStateChangedEvent,
// so their shape is load-bearing for M3's role handlers.
func TestServerClient_StateAccessors(t *testing.T) {
	sc := &ServerClient{
		state:  "synchronized",
		volume: 72,
		muted:  true,
		codec:  "opus",
	}

	if got := sc.State(); got != "synchronized" {
		t.Errorf("State() = %q, want %q", got, "synchronized")
	}
	if got := sc.Volume(); got != 72 {
		t.Errorf("Volume() = %d, want 72", got)
	}
	if got := sc.Muted(); got != true {
		t.Errorf("Muted() = %v, want true", got)
	}
	if got := sc.Codec(); got != "opus" {
		t.Errorf("Codec() = %q, want %q", got, "opus")
	}
}

// TestServerClient_StateAccessorsConcurrent is a light race-detector bait.
// Running Write/Read in parallel under -race should flag any missing lock.
func TestServerClient_StateAccessorsConcurrent(t *testing.T) {
	sc := &ServerClient{state: "synchronized", volume: 50}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			sc.mu.Lock()
			sc.volume = i % 100
			sc.mu.Unlock()
		}
		close(done)
	}()

	for i := 0; i < 1000; i++ {
		_ = sc.Volume()
		_ = sc.State()
	}
	<-done
}

// TestNewServerClientFromConn_SendAndClose confirms the constructor +
// writer + Close lifecycle works: messages enqueued via Send arrive on
// the WebSocket, and Close stops the writer without panic.
func TestNewServerClientFromConn_SendAndClose(t *testing.T) {
	// Use an in-process WebSocket pair via httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the one message we expect.
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		if msg.Type != "server/hello" {
			t.Errorf("got type %q, want server/hello", msg.Type)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sc := NewServerClientFromConn(conn, "test-id", "Test", []string{"player@v1"}, nil)

	if err := sc.Send("server/hello", map[string]string{"name": "test"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Give the writer time to flush.
	time.Sleep(50 * time.Millisecond)

	sc.Close()
	// Double-close should not panic.
	sc.Close()
}

// TestCreateAudioChunk confirms the exported helper produces the
// correct binary frame format.
func TestCreateAudioChunk(t *testing.T) {
	chunk := CreateAudioChunk(1000000, []byte{0xAA, 0xBB})
	if chunk[0] != AudioChunkMessageType {
		t.Errorf("type byte = %d, want %d", chunk[0], AudioChunkMessageType)
	}
	if len(chunk) != 9+2 {
		t.Errorf("len = %d, want 11", len(chunk))
	}
}

// TestCreateArtworkChunk confirms channel mapping and frame format.
func TestCreateArtworkChunk(t *testing.T) {
	chunk := CreateArtworkChunk(2, 5000000, []byte{0xFF})
	expectedType := byte(protocol.ArtworkChannel0MessageType + 2)
	if chunk[0] != expectedType {
		t.Errorf("type byte = %d, want %d", chunk[0], expectedType)
	}
	if len(chunk) != protocol.BinaryMessageHeaderSize+1 {
		t.Errorf("len = %d, want %d", len(chunk), protocol.BinaryMessageHeaderSize+1)
	}
}
