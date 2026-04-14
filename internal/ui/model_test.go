// ABOUTME: Tests for TUI model and state management
// ABOUTME: Tests status updates, message handling, and state transitions
package ui

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/sync"
)

func TestNewModel(t *testing.T) {
	model := NewModel(nil) // VolumeControl is optional for testing

	// Check initial state
	if model.connected {
		t.Error("expected connected to be false initially")
	}

	if model.volume != 100 {
		t.Errorf("expected default volume 100, got %d", model.volume)
	}

	if model.muted {
		t.Error("expected muted to be false initially")
	}

	if model.showDebug {
		t.Error("expected showDebug to be false initially")
	}
}

func TestStatusMsgConnected(t *testing.T) {
	model := NewModel(nil)

	connected := true
	msg := StatusMsg{
		Connected:  &connected,
		ServerName: "test-server",
	}

	model.applyStatus(msg)

	if !model.connected {
		t.Error("expected connected to be true after status update")
	}

	if model.serverName != "test-server" {
		t.Errorf("expected serverName 'test-server', got '%s'", model.serverName)
	}
}

func TestStatusMsgDisconnected(t *testing.T) {
	model := NewModel(nil)

	// First connect
	connected := true
	model.applyStatus(StatusMsg{Connected: &connected})

	// Then disconnect
	disconnected := false
	model.applyStatus(StatusMsg{Connected: &disconnected})

	if model.connected {
		t.Error("expected connected to be false after disconnect")
	}
}

func TestStatusMsgSyncStats(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		SyncRTT:     5000,
		SyncQuality: sync.QualityGood,
	}

	model.applyStatus(msg)

	if model.syncRTT != 5000 {
		t.Errorf("expected syncRTT 5000, got %d", model.syncRTT)
	}

	if model.syncQuality != sync.QualityGood {
		t.Errorf("expected QualityGood, got %v", model.syncQuality)
	}
}

func TestStatusMsgStreamInfo(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	model.applyStatus(msg)

	if model.codec != "opus" {
		t.Errorf("expected codec 'opus', got '%s'", model.codec)
	}

	if model.sampleRate != 48000 {
		t.Errorf("expected sampleRate 48000, got %d", model.sampleRate)
	}

	if model.channels != 2 {
		t.Errorf("expected channels 2, got %d", model.channels)
	}

	if model.bitDepth != 16 {
		t.Errorf("expected bitDepth 16, got %d", model.bitDepth)
	}
}

func TestStatusMsgMetadata(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		Title:  "Test Song",
		Artist: "Test Artist",
		Album:  "Test Album",
	}

	model.applyStatus(msg)

	if model.title != "Test Song" {
		t.Errorf("expected title 'Test Song', got '%s'", model.title)
	}

	if model.artist != "Test Artist" {
		t.Errorf("expected artist 'Test Artist', got '%s'", model.artist)
	}

	if model.album != "Test Album" {
		t.Errorf("expected album 'Test Album', got '%s'", model.album)
	}
}

func TestStatusMsgArtworkPath(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		ArtworkPath: "/tmp/artwork.jpg",
	}

	model.applyStatus(msg)

	if model.artworkPath != "/tmp/artwork.jpg" {
		t.Errorf("expected artworkPath '/tmp/artwork.jpg', got '%s'", model.artworkPath)
	}
}

func TestStatusMsgVolume(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		Volume: 75,
	}

	model.applyStatus(msg)

	if model.volume != 75 {
		t.Errorf("expected volume 75, got %d", model.volume)
	}
}

func TestStatusMsgStats(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		Received:    1000,
		Played:      950,
		Dropped:     50,
		BufferDepth: 300,
	}

	model.applyStatus(msg)

	if model.received != 1000 {
		t.Errorf("expected received 1000, got %d", model.received)
	}

	if model.played != 950 {
		t.Errorf("expected played 950, got %d", model.played)
	}

	if model.dropped != 50 {
		t.Errorf("expected dropped 50, got %d", model.dropped)
	}

	if model.bufferDepth != 300 {
		t.Errorf("expected bufferDepth 300, got %d", model.bufferDepth)
	}
}

func TestStatusMsgRuntimeStats(t *testing.T) {
	model := NewModel(nil)

	msg := StatusMsg{
		Goroutines: 42,
		MemAlloc:   1024 * 1024,
		MemSys:     2048 * 1024,
	}

	model.applyStatus(msg)

	if model.goroutines != 42 {
		t.Errorf("expected goroutines 42, got %d", model.goroutines)
	}

	if model.memAlloc != 1024*1024 {
		t.Errorf("expected memAlloc %d, got %d", 1024*1024, model.memAlloc)
	}

	if model.memSys != 2048*1024 {
		t.Errorf("expected memSys %d, got %d", 2048*1024, model.memSys)
	}
}

func TestMultipleStatusUpdates(t *testing.T) {
	model := NewModel(nil)

	// First update
	connected := true
	model.applyStatus(StatusMsg{
		Connected: &connected,
		Codec:     "opus",
	})

	if model.codec != "opus" {
		t.Error("first update failed")
	}

	// Second update (partial) - sampleRate requires Codec to be in same message
	model.applyStatus(StatusMsg{
		Codec:      "opus",
		SampleRate: 48000,
	})

	// Previous values should be retained
	if model.codec != "opus" {
		t.Error("previous codec value was lost")
	}

	if model.sampleRate != 48000 {
		t.Error("new sampleRate not applied")
	}
}

func TestStatusMsgZeroValues(t *testing.T) {
	model := NewModel(nil)

	// Set some non-zero values first
	model.applyStatus(StatusMsg{
		Volume:   75,
		Received: 100,
	})

	// Apply zero values - Volume should not update (0 is ignored), but stats should
	model.applyStatus(StatusMsg{
		Volume:   0,
		Received: 0,
	})

	// Volume should NOT be updated to 0 (0 is special case)
	if model.volume == 0 {
		t.Error("volume should not be updated to 0")
	}

	// Stats should be updated (0 is valid)
	if model.received != 0 {
		t.Error("received stats should be updated to 0")
	}
}

func TestTruncateFunction(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly ten c", 14, "exactly ten c"},
		{"this is longer than allowed", 10, "this is..."},
		{"this is longer than allowed", 15, "this is long..."}, // 15-3=12 chars + "..."
		{"", 10, ""},
		{"a", 10, "a"},
		{"abc", 3, "abc"},
		{"abcd", 4, "abcd"},
		{"abcde", 4, "a..."},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, expected %q",
				tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestChannelNameFunction(t *testing.T) {
	tests := []struct {
		channels int
		expected string
	}{
		{1, "Mono"},
		{2, "Stereo"},
		{3, "Stereo"}, // Function only distinguishes 1 vs other
		{6, "Stereo"},
		{0, "Stereo"},
	}

	for _, tt := range tests {
		result := channelName(tt.channels)
		if result != tt.expected {
			t.Errorf("channelName(%d) = %q, expected %q",
				tt.channels, result, tt.expected)
		}
	}
}

func TestSyncQualityDisplay(t *testing.T) {
	model := NewModel(nil)

	// Test different quality levels
	// Note: quality is only applied when SyncRTT or SyncOffset is non-zero
	qualities := []sync.Quality{
		sync.QualityGood,
		sync.QualityDegraded,
		sync.QualityLost,
	}

	for _, q := range qualities {
		model.applyStatus(StatusMsg{
			SyncQuality: q,
			SyncRTT:     1000, // Must include RTT for quality to be applied
		})

		if model.syncQuality != q {
			t.Errorf("quality not updated to %v", q)
		}
	}
}

func TestMetadataClearing(t *testing.T) {
	model := NewModel(nil)

	// Set metadata
	model.applyStatus(StatusMsg{
		Title:  "Song",
		Artist: "Artist",
		Album:  "Album",
	})

	// Clear metadata with empty strings
	model.applyStatus(StatusMsg{
		Title:  "",
		Artist: "",
		Album:  "",
	})

	// Empty strings should not clear (only non-empty values are applied)
	if model.title != "Song" {
		t.Error("title should not be cleared by empty string")
	}
}

// NOTE: TestConcurrentStatusUpdates was removed because Bubble Tea
// guarantees sequential Update() calls - the Model is never accessed
// concurrently in real usage, so testing concurrent access is unrealistic.
