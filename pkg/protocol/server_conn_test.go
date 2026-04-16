// ABOUTME: Tests for ServerConn typed connection wrapper
// ABOUTME: Verifies Send/SendBinary/Close lifecycle and frame helpers
package protocol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestServerConn_SendAndClose(t *testing.T) {
	received := make(chan Message, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		received <- msg
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sc := NewServerConn(conn, "test-id", "Test Server")

	if err := sc.Send("server/hello", map[string]string{"name": "test"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case msg := <-received:
		if msg.Type != "server/hello" {
			t.Errorf("type = %q, want server/hello", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	sc.Close()
	sc.Close() // double-close must not panic
}

func TestServerConn_SendBinary(t *testing.T) {
	received := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if msgType != websocket.BinaryMessage {
			t.Errorf("message type = %d, want binary", msgType)
		}
		received <- data
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sc := NewServerConn(conn, "test-id", "Test")
	defer sc.Close()

	chunk := CreateAudioChunk(1000000, []byte{0xAA, 0xBB})
	if err := sc.SendBinary(chunk); err != nil {
		t.Fatalf("SendBinary: %v", err)
	}

	select {
	case data := <-received:
		if data[0] != AudioChunkMessageType {
			t.Errorf("type byte = %d, want %d", data[0], AudioChunkMessageType)
		}
		if len(data) != BinaryMessageHeaderSize+2 {
			t.Errorf("len = %d, want %d", len(data), BinaryMessageHeaderSize+2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for binary message")
	}
}

func TestServerConn_Accessors(t *testing.T) {
	sc := &ServerConn{id: "abc", name: "Test"}
	if sc.ID() != "abc" {
		t.Errorf("ID() = %q, want abc", sc.ID())
	}
	if sc.Name() != "Test" {
		t.Errorf("Name() = %q, want Test", sc.Name())
	}
}

func TestCreateAudioChunk_Format(t *testing.T) {
	chunk := CreateAudioChunk(1000000, []byte{0xAA, 0xBB})
	if chunk[0] != AudioChunkMessageType {
		t.Errorf("type = %d, want %d", chunk[0], AudioChunkMessageType)
	}
	if len(chunk) != BinaryMessageHeaderSize+2 {
		t.Errorf("len = %d, want %d", len(chunk), BinaryMessageHeaderSize+2)
	}
}

func TestCreateArtworkChunk_Format(t *testing.T) {
	chunk := CreateArtworkChunk(2, 5000000, []byte{0xFF})
	expectedType := byte(ArtworkChannel0MessageType + 2)
	if chunk[0] != expectedType {
		t.Errorf("type = %d, want %d", chunk[0], expectedType)
	}
	if len(chunk) != BinaryMessageHeaderSize+1 {
		t.Errorf("len = %d, want %d", len(chunk), BinaryMessageHeaderSize+1)
	}
}
