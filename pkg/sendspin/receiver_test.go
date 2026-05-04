// ABOUTME: Tests for Receiver type construction and basic lifecycle
package sendspin

import (
	"fmt"
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
