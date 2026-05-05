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

// TestMetadataState_TristateUnmarshal verifies the wire-protocol contract:
// an omitted JSON key, an explicit null, and a value must each produce a
// distinguishable receiver-side signal. The merge layer in
// pkg/sendspin.Receiver depends on this.
func TestMetadataState_TristateUnmarshal(t *testing.T) {
	cases := []struct {
		name         string
		json         string
		wantPtrSet   bool   // is Title pointer non-nil?
		wantTitle    string // value if non-nil
		wantHasField bool   // HasField("title")?
	}{
		{"omitted", `{"timestamp": 1}`, false, "", false},
		{"null", `{"timestamp": 1, "title": null}`, false, "", true},
		{"value", `{"timestamp": 1, "title": "Hello"}`, true, "Hello", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m MetadataState
			if err := json.Unmarshal([]byte(tc.json), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if (m.Title != nil) != tc.wantPtrSet {
				t.Errorf("Title pointer non-nil = %v, want %v", m.Title != nil, tc.wantPtrSet)
			}
			if m.Title != nil && *m.Title != tc.wantTitle {
				t.Errorf("Title value = %q, want %q", *m.Title, tc.wantTitle)
			}
			if got := m.HasField("title"); got != tc.wantHasField {
				t.Errorf("HasField(\"title\") = %v, want %v", got, tc.wantHasField)
			}
			// Timestamp must always decode.
			if m.Timestamp != 1 {
				t.Errorf("Timestamp = %d, want 1", m.Timestamp)
			}
			// HasField("timestamp") should be true in every case
			// because the wire JSON always carries it here.
			if !m.HasField("timestamp") {
				t.Error("HasField(\"timestamp\") = false, want true")
			}
		})
	}
}

// TestMetadataState_HasFieldInGoConstruction confirms that messages built
// in process via field assignment (the conformance adapter and any
// server-side helpers do this) report HasField == true for every field.
// This is the backwards-compat guarantee callers can rely on.
func TestMetadataState_HasFieldInGoConstruction(t *testing.T) {
	title := "T"
	m := MetadataState{Timestamp: 42, Title: &title}
	for _, key := range []string{"title", "artist", "album", "progress", "shuffle", "anything"} {
		if !m.HasField(key) {
			t.Errorf("HasField(%q) = false on Go-constructed value, want true", key)
		}
	}
}

// TestMetadataState_TristateAcrossAllFields exercises every tristate field
// at once to make sure the alias-based decoder did not accidentally drop a
// field type (pointer-to-int, pointer-to-bool, pointer-to-struct).
func TestMetadataState_TristateAcrossAllFields(t *testing.T) {
	raw := `{
		"timestamp": 100,
		"title": "T",
		"artist": null,
		"album": "A",
		"album_artist": null,
		"artwork_url": "http://x",
		"year": 2020,
		"track": null,
		"progress": {"track_progress": 1000, "track_duration": 5000, "playback_speed": 1000},
		"shuffle": true
	}`
	var m MetadataState
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Set fields decode to non-nil pointers.
	if m.Title == nil || *m.Title != "T" {
		t.Errorf("Title = %v, want pointer to \"T\"", m.Title)
	}
	if m.Album == nil || *m.Album != "A" {
		t.Errorf("Album = %v, want pointer to \"A\"", m.Album)
	}
	if m.ArtworkURL == nil || *m.ArtworkURL != "http://x" {
		t.Errorf("ArtworkURL = %v", m.ArtworkURL)
	}
	if m.Year == nil || *m.Year != 2020 {
		t.Errorf("Year = %v, want pointer to 2020", m.Year)
	}
	if m.Shuffle == nil || *m.Shuffle != true {
		t.Errorf("Shuffle = %v, want pointer to true", m.Shuffle)
	}
	if m.Progress == nil {
		t.Error("Progress = nil, want non-nil")
	} else if m.Progress.TrackDuration != 5000 {
		t.Errorf("Progress.TrackDuration = %d, want 5000", m.Progress.TrackDuration)
	}
	// Null fields decode to nil pointers but HasField is true.
	for _, key := range []string{"artist", "album_artist", "track"} {
		if !m.HasField(key) {
			t.Errorf("HasField(%q) = false, want true (null is present)", key)
		}
	}
	if m.Artist != nil {
		t.Errorf("Artist = %v, want nil (null on wire)", m.Artist)
	}
	if m.AlbumArtist != nil {
		t.Errorf("AlbumArtist = %v, want nil (null on wire)", m.AlbumArtist)
	}
	if m.Track != nil {
		t.Errorf("Track = %v, want nil (null on wire)", m.Track)
	}
	// Omitted fields: HasField false.
	if m.HasField("repeat") {
		t.Error("HasField(\"repeat\") = true, want false (omitted)")
	}
	if m.Repeat != nil {
		t.Errorf("Repeat = %v, want nil (omitted)", m.Repeat)
	}
}

// TestMetadataState_RoundTripPreservesValues ensures encoding then
// decoding a populated MetadataState preserves every value. The wire
// shape must be unchanged by the new decoder.
func TestMetadataState_RoundTripPreservesValues(t *testing.T) {
	title := "Song"
	artist := "Artist"
	album := "Album"
	original := MetadataState{
		Timestamp: 12345,
		Title:     &title,
		Artist:    &artist,
		Album:     &album,
		Progress: &ProgressState{
			TrackProgress: 1000, TrackDuration: 60000, PlaybackSpeed: 1000,
		},
	}
	data, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded MetadataState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Title == nil || *decoded.Title != title {
		t.Errorf("Title = %v", decoded.Title)
	}
	if decoded.Progress == nil || decoded.Progress.TrackDuration != 60000 {
		t.Errorf("Progress = %+v", decoded.Progress)
	}
	// Round-trip preserves presence: the original was constructed in Go
	// (presentKeys nil → HasField always true), then re-marshaled (omitempty
	// drops nil pointers), then re-decoded (presentKeys captures only the
	// fields that survived omitempty). HasField must report present for the
	// fields we set.
	if !decoded.HasField("title") || !decoded.HasField("progress") {
		t.Error("decoded.HasField missed a set field after round-trip")
	}
}
