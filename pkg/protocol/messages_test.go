// ABOUTME: Tests for Sendspin Protocol message types
// ABOUTME: Verifies JSON marshaling/unmarshaling of protocol messages
package protocol

import (
	"encoding/json"
	"testing"
)

func TestClientHelloMarshaling(t *testing.T) {
	hello := ClientHello{
		ClientID:       "test-id",
		Name:           "Test Player",
		Version:        1,
		SupportedRoles: []string{"player@v1"},
		DeviceInfo: &DeviceInfo{
			ProductName:     "Test Product",
			Manufacturer:    "Test Mfg",
			SoftwareVersion: "0.1.0",
		},
		PlayerV1Support: &PlayerV1Support{
			SupportedFormats: []AudioFormat{
				{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "flac", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
			},
			BufferCapacity:    1048576,
			SupportedCommands: []string{"volume", "mute"},
		},
	}

	msg := Message{
		Type:    "client/hello",
		Payload: hello,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "client/hello" {
		t.Errorf("expected type client/hello, got %s", decoded.Type)
	}
}

func TestClientStateMarshaling(t *testing.T) {
	state := ClientStateMessage{
		Player: &PlayerState{
			State:  "synchronized",
			Volume: 80,
			Muted:  false,
		},
	}

	msg := Message{
		Type:    "client/state",
		Payload: state,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "client/state" {
		t.Errorf("expected type client/state, got %s", decoded.Type)
	}
}

func TestServerHelloMarshaling(t *testing.T) {
	hello := ServerHello{
		ServerID:         "server-123",
		Name:             "Test Server",
		Version:          1,
		ActiveRoles:      []string{"player@v1", "metadata@v1"},
		ConnectionReason: "playback",
	}

	msg := Message{
		Type:    "server/hello",
		Payload: hello,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "server/hello" {
		t.Errorf("expected type server/hello, got %s", decoded.Type)
	}
}

// TestStreamStartArtwork_RoundTrip verifies the StreamStart message can carry
// an artwork channel list alongside (or instead of) the player field. The
// wire format must use width/height (not media_width/media_height, which is
// the hello-message convention).
func TestStreamStartArtwork_RoundTrip(t *testing.T) {
	original := StreamStart{
		Artwork: &StreamStartArtwork{
			Channels: []ArtworkStreamChannel{
				{Source: "album", Format: "jpeg", Width: 256, Height: 256},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Spot-check the wire shape matches what peers expect.
	want := `{"artwork":{"channels":[{"source":"album","format":"jpeg","width":256,"height":256}]}}`
	if string(data) != want {
		t.Errorf("marshal output = %s, want %s", data, want)
	}

	var decoded StreamStart
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Artwork == nil {
		t.Fatal("decoded.Artwork is nil")
	}
	if len(decoded.Artwork.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(decoded.Artwork.Channels))
	}
	ch := decoded.Artwork.Channels[0]
	if ch.Source != "album" || ch.Format != "jpeg" || ch.Width != 256 || ch.Height != 256 {
		t.Errorf("decoded channel = %+v", ch)
	}
}

// TestStreamStart_PlayerOmittedWhenNil confirms that existing callers who
// only set Player (never Artwork) still produce the same wire shape.
func TestStreamStart_PlayerOnlyUnchanged(t *testing.T) {
	msg := StreamStart{
		Player: &StreamStartPlayer{
			Codec: "pcm", SampleRate: 48000, Channels: 2, BitDepth: 24,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Artwork field must be omitted entirely when nil.
	want := `{"player":{"codec":"pcm","sample_rate":48000,"channels":2,"bit_depth":24}}`
	if string(data) != want {
		t.Errorf("marshal output = %s, want %s", data, want)
	}
}
