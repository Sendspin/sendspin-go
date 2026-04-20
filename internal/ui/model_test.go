// ABOUTME: Tests for TUI model and state management
// ABOUTME: Tests status updates, message handling, and state transitions
package ui

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/sync"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	model := NewModel(Config{}) // VolumeControl and TransportControl are optional for testing

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

	if model.playbackState != "idle" {
		t.Errorf("expected playbackState 'idle', got '%s'", model.playbackState)
	}
}

func TestStatusMsgConnected(t *testing.T) {
	model := NewModel(Config{})

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
	model := NewModel(Config{})

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
	model := NewModel(Config{})

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
	model := NewModel(Config{})

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
	model := NewModel(Config{})

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
	model := NewModel(Config{})

	msg := StatusMsg{
		ArtworkPath: "/tmp/artwork.jpg",
	}

	model.applyStatus(msg)

	if model.artworkPath != "/tmp/artwork.jpg" {
		t.Errorf("expected artworkPath '/tmp/artwork.jpg', got '%s'", model.artworkPath)
	}
}

func TestStatusMsgVolume(t *testing.T) {
	model := NewModel(Config{})

	msg := StatusMsg{
		Volume: 75,
	}

	model.applyStatus(msg)

	if model.volume != 75 {
		t.Errorf("expected volume 75, got %d", model.volume)
	}
}

func TestStatusMsgStats(t *testing.T) {
	model := NewModel(Config{})

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
	model := NewModel(Config{})

	msg := StatusMsg{
		Goroutines: 42,
	}

	model.applyStatus(msg)

	if model.goroutines != 42 {
		t.Errorf("expected goroutines 42, got %d", model.goroutines)
	}
}

func TestMultipleStatusUpdates(t *testing.T) {
	model := NewModel(Config{})

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
	model := NewModel(Config{})

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
	model := NewModel(Config{})

	// Test different quality levels
	// Note: quality is only applied when SyncRTT is non-zero
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
	model := NewModel(Config{})

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

func TestPlaybackStateApplication(t *testing.T) {
	model := NewModel(Config{})

	// Initial state should be idle
	if model.playbackState != "idle" {
		t.Errorf("expected initial playbackState 'idle', got '%s'", model.playbackState)
	}

	// Apply playing state
	model.applyStatus(StatusMsg{PlaybackState: "playing"})
	if model.playbackState != "playing" {
		t.Errorf("expected playbackState 'playing', got '%s'", model.playbackState)
	}

	// Apply paused state
	model.applyStatus(StatusMsg{PlaybackState: "paused"})
	if model.playbackState != "paused" {
		t.Errorf("expected playbackState 'paused', got '%s'", model.playbackState)
	}

	// Empty string should not overwrite
	model.applyStatus(StatusMsg{PlaybackState: ""})
	if model.playbackState != "paused" {
		t.Errorf("expected playbackState to remain 'paused', got '%s'", model.playbackState)
	}
}

func TestTransportKeyToggle(t *testing.T) {
	tc := NewTransportControl()
	model := NewModel(Config{TransportCtrl: tc})
	model.width = 80
	model.height = 24

	// Simulate space key
	msg := fakeKeyMsg(" ")
	model.handleKey(msg)

	select {
	case cmd := <-tc.Commands:
		if cmd.Command != "toggle" {
			t.Errorf("expected 'toggle' command, got '%s'", cmd.Command)
		}
	default:
		t.Error("expected toggle command on transport channel")
	}
}

func TestTransportKeyNext(t *testing.T) {
	tc := NewTransportControl()
	model := NewModel(Config{TransportCtrl: tc})
	model.width = 80
	model.height = 24

	msg := fakeKeyMsg("n")
	model.handleKey(msg)

	select {
	case cmd := <-tc.Commands:
		if cmd.Command != "next" {
			t.Errorf("expected 'next' command, got '%s'", cmd.Command)
		}
	default:
		t.Error("expected next command on transport channel")
	}
}

func TestTransportKeyPrevious(t *testing.T) {
	tc := NewTransportControl()
	model := NewModel(Config{TransportCtrl: tc})
	model.width = 80
	model.height = 24

	msg := fakeKeyMsg("p")
	model.handleKey(msg)

	select {
	case cmd := <-tc.Commands:
		if cmd.Command != "previous" {
			t.Errorf("expected 'previous' command, got '%s'", cmd.Command)
		}
	default:
		t.Error("expected previous command on transport channel")
	}
}

func TestTransportKeyReconnect(t *testing.T) {
	tc := NewTransportControl()
	model := NewModel(Config{TransportCtrl: tc})
	model.width = 80
	model.height = 24

	msg := fakeKeyMsg("r")
	model.handleKey(msg)

	select {
	case cmd := <-tc.Commands:
		if cmd.Command != "reconnect" {
			t.Errorf("expected 'reconnect' command, got '%s'", cmd.Command)
		}
	default:
		t.Error("expected reconnect command on transport channel")
	}
}

func TestTransportNilSafe(t *testing.T) {
	// Transport keys should not panic when transportCtrl is nil
	model := NewModel(Config{})
	model.width = 80
	model.height = 24

	// These should not panic
	model.handleKey(fakeKeyMsg(" "))
	model.handleKey(fakeKeyMsg("n"))
	model.handleKey(fakeKeyMsg("p"))
	model.handleKey(fakeKeyMsg("r"))
}

func TestPlaybackStateDisplay(t *testing.T) {
	tests := []struct {
		state    string
		expected string
	}{
		{"playing", "\u25b6 Playing"},
		{"paused", "\u23f8 Paused"},
		{"stopped", "\u23f9 Stopped"},
		{"idle", "\u25cf Idle"},
		{"reconnecting", "\u21bb Reconnecting..."},
		{"unknown", "\u25cf Idle"},
	}

	for _, tt := range tests {
		result := playbackStateDisplay(tt.state)
		if result != tt.expected {
			t.Errorf("playbackStateDisplay(%q) = %q, expected %q",
				tt.state, result, tt.expected)
		}
	}
}

func TestFormatSampleRate(t *testing.T) {
	tests := []struct {
		rate     int
		expected string
	}{
		{44100, "44.1kHz"},
		{48000, "48kHz"},
		{96000, "96kHz"},
		{192000, "192kHz"},
		{88200, "88.2kHz"},
		{176400, "176.4kHz"},
		{32000, "32kHz"},
		{22050, "22.1kHz"},
	}

	for _, tt := range tests {
		result := formatSampleRate(tt.rate)
		if result != tt.expected {
			t.Errorf("formatSampleRate(%d) = %q, expected %q",
				tt.rate, result, tt.expected)
		}
	}
}

func TestFormatCodecDisplay(t *testing.T) {
	tests := []struct {
		codec      string
		sampleRate int
		bitDepth   int
		expected   string
	}{
		{"pcm", 192000, 24, "PCM 192kHz/24bit lossless"},
		{"opus", 48000, 16, "OPUS 48kHz/16bit"},
		{"flac", 48000, 24, "FLAC 48kHz/24bit lossless"},
		{"flac", 44100, 16, "FLAC 44.1kHz/16bit lossless"},
		{"aac", 44100, 16, "AAC 44.1kHz/16bit"},
	}

	for _, tt := range tests {
		result := formatCodecDisplay(tt.codec, tt.sampleRate, tt.bitDepth)
		if result != tt.expected {
			t.Errorf("formatCodecDisplay(%q, %d, %d) = %q, expected %q",
				tt.codec, tt.sampleRate, tt.bitDepth, result, tt.expected)
		}
	}
}

// fakeKeyMsg creates a tea.KeyMsg for testing.
func fakeKeyMsg(key string) tea.KeyMsg {
	if key == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(key)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// NOTE: TestConcurrentStatusUpdates was removed because Bubble Tea
// guarantees sequential Update() calls - the Model is never accessed
// concurrently in real usage, so testing concurrent access is unrealistic.
