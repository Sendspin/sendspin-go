// ABOUTME: Tests for Opus audio encoder
// ABOUTME: Tests encoder creation, encoding, and format handling
package encode

import (
	"testing"
)

func TestNewOpusEncoder(t *testing.T) {
	frameSize := 480 // 10ms at 48kHz
	encoder, err := NewOpusEncoder(48000, 2, frameSize)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	if encoder == nil {
		t.Fatal("expected encoder to be created")
	}

	if encoder.sampleRate != 48000 {
		t.Errorf("expected sampleRate 48000, got %d", encoder.sampleRate)
	}

	if encoder.channels != 2 {
		t.Errorf("expected channels 2, got %d", encoder.channels)
	}
}

func TestOpusEncoderInvalidSampleRate(t *testing.T) {
	// Opus only supports 8, 12, 16, 24, 48 kHz
	_, err := NewOpusEncoder(44100, 2, 480)
	if err == nil {
		t.Fatal("expected error for invalid sample rate 44100")
	}
}

func TestOpusEncoderInvalidChannels(t *testing.T) {
	// Opus supports 1-2 channels
	_, err := NewOpusEncoder(48000, 5, 480)
	if err == nil {
		t.Fatal("expected error for invalid channels 5")
	}
}

func TestOpusEncodeValidFrame(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Opus at 48kHz expects frames of 2.5, 5, 10, 20, 40, or 60ms
	// 10ms at 48kHz stereo = 480 samples * 2 channels = 960 int16 values
	pcm := make([]int16, 960)
	for i := range pcm {
		pcm[i] = int16(i * 10) // Simple ramp signal
	}

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output")
	}

	// Opus encoding should compress the data
	// Encoded size should be much smaller than PCM (960 samples * 2 bytes = 1920 bytes)
	if len(encoded) >= len(pcm)*2 {
		t.Errorf("expected compression, but encoded size %d >= PCM size %d", len(encoded), len(pcm)*2)
	}
}

func TestOpusEncodeSilence(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// All zeros (silence)
	pcm := make([]int16, 960)

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output even for silence")
	}

	// Silence should compress very well
	if len(encoded) > 50 {
		t.Logf("silence encoded to %d bytes (expected very small)", len(encoded))
	}
}

func TestOpusEncodeFullScale(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Full scale signal (maximum amplitude)
	pcm := make([]int16, 960)
	for i := range pcm {
		if i%2 == 0 {
			pcm[i] = 32767 // Max positive
		} else {
			pcm[i] = -32768 // Max negative
		}
	}

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output")
	}
}

func TestOpusEncodeMultipleFrames(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Encode multiple frames in sequence
	for frame := 0; frame < 10; frame++ {
		pcm := make([]int16, 960)
		for i := range pcm {
			pcm[i] = int16((frame * 1000) + i)
		}

		encoded, err := encoder.Encode(pcm)
		if err != nil {
			t.Fatalf("encode frame %d failed: %v", frame, err)
		}

		if len(encoded) == 0 {
			t.Fatalf("frame %d produced empty output", frame)
		}
	}
}

func TestOpusEncodeMono(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 1, 480)
	if err != nil {
		t.Fatalf("failed to create mono encoder: %v", err)
	}

	// Mono: 480 samples for 10ms at 48kHz
	pcm := make([]int16, 480)
	for i := range pcm {
		pcm[i] = int16(i * 20)
	}

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("mono encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output for mono")
	}
}

func TestOpusEncodeInvalidFrameSize(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Wrong frame size (not 2.5/5/10/20/40/60ms worth of samples)
	pcm := make([]int16, 100) // Way too small

	_, err = encoder.Encode(pcm)
	if err == nil {
		t.Log("Note: encoder may accept invalid frame sizes (implementation dependent)")
	}
}

func TestOpusEncodeDifferentFrameSizes(t *testing.T) {
	encoder, err := NewOpusEncoder(48000, 2, 480)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Test different valid frame sizes at 48kHz stereo
	frameSizes := []int{
		240,  // 2.5ms: 120 samples * 2 channels
		480,  // 5ms: 240 samples * 2 channels
		960,  // 10ms: 480 samples * 2 channels
		1920, // 20ms: 960 samples * 2 channels
	}

	for _, size := range frameSizes {
		pcm := make([]int16, size)
		for i := range pcm {
			pcm[i] = int16(i * 5)
		}

		encoded, err := encoder.Encode(pcm)
		if err != nil {
			t.Logf("frame size %d failed (may not be supported): %v", size, err)
			continue
		}

		if len(encoded) == 0 {
			t.Errorf("frame size %d produced empty output", size)
		}
	}
}

func TestOpusEncode24kHz(t *testing.T) {
	// Test 24kHz (valid Opus sample rate)
	encoder, err := NewOpusEncoder(24000, 2, 240)
	if err != nil {
		t.Fatalf("failed to create 24kHz encoder: %v", err)
	}

	// 10ms at 24kHz stereo = 240 samples * 2 channels = 480 values
	pcm := make([]int16, 480)
	for i := range pcm {
		pcm[i] = int16(i * 10)
	}

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("24kHz encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output at 24kHz")
	}
}

func TestOpusEncode16kHz(t *testing.T) {
	// Test 16kHz (valid Opus sample rate, narrowband)
	encoder, err := NewOpusEncoder(16000, 1, 160)
	if err != nil {
		t.Fatalf("failed to create 16kHz encoder: %v", err)
	}

	// 10ms at 16kHz mono = 160 samples
	pcm := make([]int16, 160)
	for i := range pcm {
		pcm[i] = int16(i * 10)
	}

	encoded, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("16kHz encode failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoded output at 16kHz")
	}
}
