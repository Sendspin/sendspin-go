package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLegacyFieldsInJSON(t *testing.T) {
	legacyPlayerSupport := PlayerSupport{
		SupportedFormats: []AudioFormat{
			{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
		},
		BufferCapacity:    1048576,
		SupportedCommands: []string{"volume"},
	}

	legacyMetadataSupport := MetadataSupport{
		SupportPictureFormats: []string{"jpeg", "png", "webp"},
		MediaWidth:            600,
		MediaHeight:           600,
	}

	hello := ClientHello{
		ClientID:        "test",
		Name:            "Test",
		Version:         1,
		SupportedRoles:  []string{"player@v1"},
		PlayerSupport:   &legacyPlayerSupport,   // Legacy field
		MetadataSupport: &legacyMetadataSupport, // Legacy field
	}

	msg := Message{
		Type:    "client/hello",
		Payload: hello,
	}

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	t.Logf("Generated JSON:\n%s", string(data))

	// Check if player_support appears in the JSON
	if !bytes.Contains(data, []byte(`"player_support"`)) {
		t.Error("FAIL: player_support field is MISSING from JSON!")
		t.Log("This means Music Assistant won't accept the handshake")
	} else {
		t.Log("✓ SUCCESS: player_support field is in the JSON!")
	}

	// Check if metadata_support appears in the JSON
	if !bytes.Contains(data, []byte(`"metadata_support"`)) {
		t.Error("FAIL: metadata_support field is MISSING from JSON!")
	} else {
		t.Log("✓ SUCCESS: metadata_support field is in the JSON!")
	}
}
