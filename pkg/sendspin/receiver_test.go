// ABOUTME: Tests for Receiver type construction and basic lifecycle
package sendspin

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

func TestNewReceiver_Defaults(t *testing.T) {
	config := ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Receiver",
	}

	r, err := NewReceiver(config)
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer r.Close()

	if r == nil {
		t.Fatal("Expected non-nil Receiver")
	}

	if r.clockSync == nil {
		t.Error("Expected clockSync to be initialized")
	}

	if r.Output() == nil {
		t.Error("Expected Output() channel to be non-nil")
	}

	if r.config.BufferMs != 500 {
		t.Errorf("Expected default BufferMs=500, got %d", r.config.BufferMs)
	}

	if r.config.DeviceInfo.ProductName == "" {
		t.Error("Expected default DeviceInfo.ProductName to be set")
	}

	if r.config.DeviceInfo.Manufacturer == "" {
		t.Error("Expected default DeviceInfo.Manufacturer to be set")
	}

	if r.config.DeviceInfo.SoftwareVersion == "" {
		t.Error("Expected default DeviceInfo.SoftwareVersion to be set")
	}
}

func TestNewReceiver_RequiresServerAddr(t *testing.T) {
	config := ReceiverConfig{
		PlayerName: "Test Receiver",
		// ServerAddr intentionally omitted
	}

	r, err := NewReceiver(config)
	if err == nil {
		t.Error("Expected error when ServerAddr is empty")
		if r != nil {
			r.Close()
		}
	}

	if r != nil {
		t.Error("Expected nil Receiver on error")
	}
}

func TestReceiver_Connect_BadAddress(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:99999",
		PlayerName: "Test",
	})

	err := recv.Connect()
	if err == nil {
		t.Fatal("expected connection error for bad address")
	}
}

func TestReceiver_ClockSyncIsOwnInstance(t *testing.T) {
	config1 := ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Receiver One",
	}
	config2 := ReceiverConfig{
		ServerAddr: "localhost:8928",
		PlayerName: "Receiver Two",
	}

	r1, err := NewReceiver(config1)
	if err != nil {
		t.Fatalf("NewReceiver r1 failed: %v", err)
	}
	defer r1.Close()

	r2, err := NewReceiver(config2)
	if err != nil {
		t.Fatalf("NewReceiver r2 failed: %v", err)
	}
	defer r2.Close()

	if r1.ClockSync() == r2.ClockSync() {
		t.Error("Expected each Receiver to have its own ClockSync instance")
	}
}

func TestReceiver_CloseBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	// Should not panic
	err := recv.Close()
	if err != nil {
		t.Fatalf("unexpected error closing unconnected receiver: %v", err)
	}
}

func TestReceiver_StatsBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	stats := recv.Stats()
	if stats.Received != 0 || stats.Played != 0 || stats.Dropped != 0 {
		t.Error("expected zero stats before connect")
	}
}

func TestReceiver_OnStreamStartCallback(t *testing.T) {
	var receivedFormat audio.Format
	_, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		OnStreamStart: func(f audio.Format) {
			receivedFormat = f
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Callback is stored but not invoked until stream starts
	if receivedFormat.Codec != "" {
		t.Error("expected empty format before stream start")
	}
}

func TestReceiver_CustomDecoderFactory(t *testing.T) {
	factoryCalled := false
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		DecoderFactory: func(f audio.Format) (decode.Decoder, error) {
			factoryCalled = true
			return nil, fmt.Errorf("test decoder")
		},
	})

	if recv.config.DecoderFactory == nil {
		t.Error("expected DecoderFactory to be stored")
	}
	if factoryCalled {
		t.Error("factory should not be called before connect")
	}
}

// maxRateAndDepth scans formats and returns the largest sample rate and bit
// depth present. Test helper: lets assertions describe the cap by intent
// ("nothing above 48k/16") rather than counting list entries.
func maxRateAndDepth(formats []protocol.AudioFormat) (int, int) {
	var maxRate, maxDepth int
	for _, f := range formats {
		if f.SampleRate > maxRate {
			maxRate = f.SampleRate
		}
		if f.BitDepth > maxDepth {
			maxDepth = f.BitDepth
		}
	}
	return maxRate, maxDepth
}

func TestBuildSupportedFormats_NoCaps(t *testing.T) {
	got := buildSupportedFormats("", 0, 0)
	if len(got) == 0 {
		t.Fatal("expected non-empty format list with no caps")
	}
	maxRate, maxDepth := maxRateAndDepth(got)
	if maxRate != 192000 {
		t.Errorf("expected max rate 192000 with no caps, got %d", maxRate)
	}
	if maxDepth != 24 {
		t.Errorf("expected max depth 24 with no caps, got %d", maxDepth)
	}
}

func TestBuildSupportedFormats_RateCap(t *testing.T) {
	got := buildSupportedFormats("", 48000, 0)
	if len(got) == 0 {
		t.Fatal("expected non-empty list with 48000Hz cap")
	}
	for _, f := range got {
		if f.SampleRate > 48000 {
			t.Errorf("expected no rate > 48000, got %s @ %dHz", f.Codec, f.SampleRate)
		}
	}
	// Boundary: 48000Hz formats must survive.
	hasFortyEight := false
	for _, f := range got {
		if f.SampleRate == 48000 {
			hasFortyEight = true
			break
		}
	}
	if !hasFortyEight {
		t.Error("expected at least one 48000Hz format to survive the cap")
	}
}

func TestBuildSupportedFormats_BitDepthCap(t *testing.T) {
	got := buildSupportedFormats("", 0, 16)
	if len(got) == 0 {
		t.Fatal("expected non-empty list with 16-bit cap")
	}
	for _, f := range got {
		if f.BitDepth > 16 {
			t.Errorf("expected no depth > 16, got %s @ %d-bit", f.Codec, f.BitDepth)
		}
	}
}

func TestBuildSupportedFormats_BothCaps(t *testing.T) {
	got := buildSupportedFormats("", 48000, 16)
	if len(got) == 0 {
		t.Fatal("expected non-empty list with 48k/16 caps")
	}
	for _, f := range got {
		if f.SampleRate > 48000 || f.BitDepth > 16 {
			t.Errorf("expected no entries beyond 48000Hz/16-bit, got %s @ %dHz/%d-bit",
				f.Codec, f.SampleRate, f.BitDepth)
		}
	}
}

func TestBuildSupportedFormats_PreferredCodecAfterFilter(t *testing.T) {
	// Filter eliminates high-res FLAC; the survivor should still be ordered
	// with FLAC at the front when preferredCodec="flac".
	got := buildSupportedFormats("flac", 48000, 24)
	if len(got) == 0 {
		t.Fatal("expected non-empty list")
	}
	if got[0].Codec != "flac" {
		t.Errorf("expected preferred codec at front, got %s", got[0].Codec)
	}
}

func TestBuildSupportedFormats_CapsAtExactBoundary(t *testing.T) {
	// maxSampleRate=192000, maxBitDepth=24 should keep everything.
	full := buildSupportedFormats("", 0, 0)
	bounded := buildSupportedFormats("", 192000, 24)
	if len(bounded) != len(full) {
		t.Errorf("boundary cap should keep all formats: full=%d bounded=%d", len(full), len(bounded))
	}
}

func TestBuildSupportedFormats_AggressiveCapEmpty(t *testing.T) {
	// User asks for ≤ 8000Hz / ≤ 8-bit. Nothing in our list matches; we
	// honestly return empty rather than fabricating a fallback. The handshake
	// will fail, which is the right user-visible signal that the cap is wrong.
	got := buildSupportedFormats("", 8000, 8)
	if len(got) != 0 {
		t.Errorf("expected empty list for impossible caps, got %d entries", len(got))
	}
}

// metadataStateWithKeys builds a MetadataState by JSON-marshaling the input
// map and decoding through the real wire path so presentKeys is populated
// correctly. This is the only way to construct a MetadataState whose
// HasField returns false for unspecified keys, since presentKeys is
// unexported. Tests that rely on tristate semantics must use this helper.
func metadataStateWithKeys(t *testing.T, fields map[string]any) *protocol.MetadataState {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("metadataStateWithKeys: marshal: %v", err)
	}
	var m protocol.MetadataState
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("metadataStateWithKeys: unmarshal: %v", err)
	}
	return &m
}

// receiverForMetadataTests builds a Receiver with a fixed fake clock and an
// OnMetadata callback that captures every snapshot. The captured slice is
// guarded by its own mutex because OnMetadata may be invoked from either
// the channel reader goroutine or the apply-loop goroutine.
type metadataCapture struct {
	mu       sync.Mutex
	captured []Metadata
}

func (mc *metadataCapture) record(m Metadata) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.captured = append(mc.captured, m)
}

func (mc *metadataCapture) latest() (Metadata, bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.captured) == 0 {
		return Metadata{}, false
	}
	return mc.captured[len(mc.captured)-1], true
}

func (mc *metadataCapture) count() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return len(mc.captured)
}

// newMetadataReceiver returns a Receiver wired up for direct enqueue/apply
// testing — no Connect, no protocol client, no scheduler. clockNow is
// overridden to a fixed value so timestamp-deferral tests are deterministic.
func newMetadataReceiver(t *testing.T, fakeNow int64, mc *metadataCapture) *Receiver {
	t.Helper()
	r, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Metadata Test",
		OnMetadata: mc.record,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	r.clockNow = func() int64 { return fakeNow }
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestReceiver_MetadataMergePreservesUnchangedFields is the headline
// regression: a track A snapshot followed by a track B diff_update that
// only carries `title` must not blank out artist/album. Pre-fix, the
// receiver dereffed nil pointers and dispatched empty strings for both.
func TestReceiver_MetadataMergePreservesUnchangedFields(t *testing.T) {
	mc := &metadataCapture{}
	r := newMetadataReceiver(t, 0, mc)

	// Snapshot: title + artist + album set.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"title":     "Song A",
		"artist":    "Artist X",
		"album":     "Album Y",
	}))

	// Diff: only title changes. Artist + album omitted → must preserve.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"title":     "Song B",
	}))

	got, ok := mc.latest()
	if !ok {
		t.Fatal("OnMetadata was not invoked")
	}
	t.Logf("merged snapshot after diff: %+v", got)
	if got.Title != "Song B" {
		t.Errorf("Title = %q, want %q", got.Title, "Song B")
	}
	if got.Artist != "Artist X" {
		t.Errorf("Artist = %q, want %q (must be preserved across diff)", got.Artist, "Artist X")
	}
	if got.Album != "Album Y" {
		t.Errorf("Album = %q, want %q (must be preserved across diff)", got.Album, "Album Y")
	}
	if mc.count() != 2 {
		t.Errorf("OnMetadata invocation count = %d, want 2", mc.count())
	}
}

// TestReceiver_MetadataNullClearsField verifies the "null" leg of the
// tristate contract: a wire-explicit null must reset the prior value.
func TestReceiver_MetadataNullClearsField(t *testing.T) {
	mc := &metadataCapture{}
	r := newMetadataReceiver(t, 0, mc)

	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp":   0,
		"title":       "Song A",
		"artist":      "Artist X",
		"artwork_url": "http://example/a.jpg",
	}))

	// Explicit null on artwork_url and artist: must clear both.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp":   0,
		"artist":      nil,
		"artwork_url": nil,
	}))

	got, ok := mc.latest()
	if !ok {
		t.Fatal("OnMetadata was not invoked")
	}
	t.Logf("merged snapshot after null-clear: %+v", got)
	if got.Title != "Song A" {
		t.Errorf("Title = %q, want %q (omitted, should be preserved)", got.Title, "Song A")
	}
	if got.Artist != "" {
		t.Errorf("Artist = %q, want empty (null on wire clears it)", got.Artist)
	}
	if got.ArtworkURL != "" {
		t.Errorf("ArtworkURL = %q, want empty (null on wire clears it)", got.ArtworkURL)
	}
}

// TestReceiver_MetadataFutureTimestampDeferred verifies a future-dated
// update is held in pendingMetadata until the fake clock advances past
// the timestamp, then drainPendingMetadata flushes it.
func TestReceiver_MetadataFutureTimestampDeferred(t *testing.T) {
	mc := &metadataCapture{}
	// Fake clock starts at server-time = 1000 µs.
	r, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Defer Test",
		OnMetadata: mc.record,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	var now int64 = 1000
	r.clockNow = func() int64 { return now }

	// Apply-now snapshot to set baseline.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"title":     "Now Playing",
		"artist":    "Current Artist",
	}))
	if mc.count() != 1 {
		t.Fatalf("expected 1 immediate apply, got %d", mc.count())
	}

	// Future-dated update at server-time = 5000 µs. Should NOT apply yet.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 5000,
		"title":     "Up Next",
	}))
	if mc.count() != 1 {
		t.Errorf("future-dated update applied early; OnMetadata count = %d, want 1", mc.count())
	}

	r.metadataMu.Lock()
	pendingLen := len(r.pendingMetadata)
	r.metadataMu.Unlock()
	if pendingLen != 1 {
		t.Errorf("pendingMetadata length = %d, want 1", pendingLen)
	}

	// Drain at clock = 4000 µs (still before timestamp): nothing applies.
	now = 4000
	r.drainPendingMetadata()
	if mc.count() != 1 {
		t.Errorf("drain at clock<timestamp applied prematurely; count = %d, want 1", mc.count())
	}

	// Advance clock past the timestamp; drain should flush the queued update.
	now = 5500
	r.drainPendingMetadata()
	if mc.count() != 2 {
		t.Fatalf("expected drain to apply 1 deferred update; count = %d, want 2", mc.count())
	}

	got, _ := mc.latest()
	t.Logf("post-defer-flush snapshot: %+v", got)
	if got.Title != "Up Next" {
		t.Errorf("Title = %q, want %q", got.Title, "Up Next")
	}
	// Artist was omitted in the future-dated diff → must still be the
	// baseline value from the first update.
	if got.Artist != "Current Artist" {
		t.Errorf("Artist = %q, want %q (omitted in diff, preserved)", got.Artist, "Current Artist")
	}

	// Pending must be empty after drain.
	r.metadataMu.Lock()
	pendingLen = len(r.pendingMetadata)
	r.metadataMu.Unlock()
	if pendingLen != 0 {
		t.Errorf("pendingMetadata length after drain = %d, want 0", pendingLen)
	}
}

// TestReceiver_MetadataPendingSortedByTimestamp confirms enqueue maintains
// ascending-timestamp order even when callers send out-of-order updates.
// drainPendingMetadata depends on this to short-circuit at the first
// future-dated entry.
func TestReceiver_MetadataPendingSortedByTimestamp(t *testing.T) {
	mc := &metadataCapture{}
	r, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Sort Test",
		OnMetadata: mc.record,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.clockNow = func() int64 { return 0 }

	// Enqueue three future-dated updates out of order.
	for _, ts := range []int64{500, 100, 300} {
		r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
			"timestamp": ts,
			"title":     fmt.Sprintf("ts-%d", ts),
		}))
	}

	r.metadataMu.Lock()
	got := make([]int64, len(r.pendingMetadata))
	for i, m := range r.pendingMetadata {
		got[i] = m.Timestamp
	}
	r.metadataMu.Unlock()
	t.Logf("pending timestamps after out-of-order enqueue: %v", got)

	want := []int64{100, 300, 500}
	if len(got) != len(want) {
		t.Fatalf("pending length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pending[%d].Timestamp = %d, want %d", i, got[i], want[i])
		}
	}
}

// TestReceiver_MetadataProgressTristate exercises the progress field
// across set / null / omitted, since it's the only nested struct in the
// merge path. Per spec, progress is atomic — a non-null value always
// carries TrackDuration; a null clears Duration; an omitted progress
// preserves the prior Duration.
func TestReceiver_MetadataProgressTristate(t *testing.T) {
	mc := &metadataCapture{}
	r := newMetadataReceiver(t, 0, mc)

	// Set progress with track_duration = 60_000 ms (= 60s).
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"progress": map[string]any{
			"track_progress": 0,
			"track_duration": 60000,
			"playback_speed": 1000,
		},
	}))
	got, _ := mc.latest()
	if got.Duration != 60 {
		t.Errorf("Duration = %d, want 60 (60000ms / 1000)", got.Duration)
	}

	// Diff with progress omitted → Duration preserved.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"title":     "still playing",
	}))
	got, _ = mc.latest()
	if got.Duration != 60 {
		t.Errorf("Duration after omitted progress = %d, want 60 preserved", got.Duration)
	}

	// Explicit null progress → Duration cleared.
	r.enqueueMetadata(metadataStateWithKeys(t, map[string]any{
		"timestamp": 0,
		"progress":  nil,
	}))
	got, _ = mc.latest()
	t.Logf("snapshot after null progress: %+v", got)
	if got.Duration != 0 {
		t.Errorf("Duration after null progress = %d, want 0", got.Duration)
	}
}
