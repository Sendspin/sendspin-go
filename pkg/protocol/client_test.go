// ABOUTME: Tests for WebSocket client implementation
// ABOUTME: Tests connection, handshake, and message routing
package protocol

import (
	"encoding/binary"
	"testing"
	"time"
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
