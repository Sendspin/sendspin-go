// ABOUTME: Tests for PCM decoder
// ABOUTME: Tests 16-bit and 24-bit PCM decoding
package decode

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestNewPCM(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	decoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if decoder == nil {
		t.Fatal("expected decoder to be created")
	}
}

func TestPCMDecode16Bit(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	decoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	input := []byte{0x00, 0x01, 0x02, 0x03}
	output, err := decoder.Decode(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	expectedSamples := len(input) / 2
	if len(output) != expectedSamples {
		t.Errorf("expected %d samples, got %d", expectedSamples, len(output))
	}

	// Verify little-endian conversion with 24-bit scaling
	// 0x00, 0x01 -> 0x0100 = 256 (16-bit) -> 256<<8 = 65536 (24-bit)
	// 0x02, 0x03 -> 0x0302 = 770 (16-bit) -> 770<<8 = 197120 (24-bit)
	expected0 := int32(256 << 8)
	if output[0] != expected0 {
		t.Errorf("expected first sample %d, got %d", expected0, output[0])
	}
	expected1 := int32(770 << 8)
	if output[1] != expected1 {
		t.Errorf("expected second sample %d, got %d", expected1, output[1])
	}
}

func TestPCMDecode24Bit(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 192000,
		Channels:   2,
		BitDepth:   24,
	}

	decoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	input := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	output, err := decoder.Decode(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	expectedSamples := len(input) / 3
	if len(output) != expectedSamples {
		t.Errorf("expected %d samples, got %d", expectedSamples, len(output))
	}

	// Verify 24-bit little-endian conversion
	// 0x00, 0x01, 0x02 -> 0x020100 = 131328
	expected0 := int32(0x020100)
	if output[0] != expected0 {
		t.Errorf("expected first sample %d, got %d", expected0, output[0])
	}

	// 0x03, 0x04, 0x05 -> 0x050403 = 328707
	expected1 := int32(0x050403)
	if output[1] != expected1 {
		t.Errorf("expected second sample %d, got %d", expected1, output[1])
	}
}

func TestNewPCM_InvalidCodec(t *testing.T) {
	format := audio.Format{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	decoder, err := NewPCM(format)
	if err == nil {
		t.Fatal("expected error for invalid codec, got nil")
	}

	if decoder != nil {
		t.Fatal("expected decoder to be nil for invalid codec")
	}

	expectedError := "invalid codec for PCM decoder: opus"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

func TestNewPCM_UnsupportedBitDepth(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   32,
	}

	decoder, err := NewPCM(format)
	if err == nil {
		t.Fatal("expected error for unsupported bit depth, got nil")
	}

	if decoder != nil {
		t.Fatal("expected decoder to be nil for unsupported bit depth")
	}

	expectedError := "unsupported bit depth: 32 (supported: 16, 24)"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

func TestPCMDecode_EmptyInput(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	decoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	output, err := decoder.Decode([]byte{})
	if err != nil {
		t.Fatalf("decode failed with empty input: %v", err)
	}

	if len(output) != 0 {
		t.Errorf("expected 0 samples from empty input, got %d", len(output))
	}
}
