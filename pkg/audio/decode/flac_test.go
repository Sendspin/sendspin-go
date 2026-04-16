// ABOUTME: Tests for the FLAC streaming decoder
// ABOUTME: Lifecycle tests — create with header, error without, close cleanly
package decode

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

// buildMinimalFLACHeader creates a syntactically valid FLAC codec_header
// (fLaC marker + STREAMINFO metadata block) for testing decoder lifecycle.
// The header is valid enough for mewkiz/flac to parse STREAMINFO, though
// no frames follow it.
func buildMinimalFLACHeader(sampleRate, channels, bitDepth, blockSize int) []byte {
	header := make([]byte, 0, 42)

	// fLaC marker
	header = append(header, 'f', 'L', 'a', 'C')

	// Metadata block header: last=1 (0x80), type=0 (STREAMINFO), length=34
	header = append(header, 0x80, 0x00, 0x00, 34)

	// STREAMINFO (34 bytes)
	streamInfo := make([]byte, 34)
	// min block size (bytes 0-1)
	streamInfo[0] = byte(blockSize >> 8)
	streamInfo[1] = byte(blockSize)
	// max block size (bytes 2-3)
	streamInfo[2] = byte(blockSize >> 8)
	streamInfo[3] = byte(blockSize)
	// min/max frame size (bytes 4-9): 0 = unknown
	// sample rate (20 bits) | channels-1 (3 bits) | bps-1 (5 bits) | total samples (36 bits)
	// packed into bytes 10-17
	packed := uint64(sampleRate)<<44 | uint64(channels-1)<<41 | uint64(bitDepth-1)<<36
	for i := 0; i < 8; i++ {
		streamInfo[10+i] = byte(packed >> (56 - 8*i))
	}
	// MD5 (bytes 18-33): zeros

	header = append(header, streamInfo...)
	return header
}

func TestNewFLAC_RequiresCodecHeader(t *testing.T) {
	format := audio.Format{
		Codec:      "flac",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   24,
	}
	_, err := NewFLAC(format)
	if err == nil {
		t.Error("expected error when CodecHeader is nil")
	}
}

func TestNewFLAC_InvalidCodec(t *testing.T) {
	format := audio.Format{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   24,
	}
	_, err := NewFLAC(format)
	if err == nil {
		t.Fatal("expected error for invalid codec")
	}
}

func TestNewFLAC_ValidHeader(t *testing.T) {
	codecHeader := buildMinimalFLACHeader(48000, 2, 24, 4096)
	format := audio.Format{
		Codec:       "flac",
		SampleRate:  48000,
		Channels:    2,
		BitDepth:    24,
		CodecHeader: codecHeader,
	}
	dec, err := NewFLAC(format)
	if err != nil {
		t.Fatalf("NewFLAC: %v", err)
	}
	defer dec.Close()
}

func TestFLACDecoder_CloseWithoutDecode(t *testing.T) {
	codecHeader := buildMinimalFLACHeader(48000, 2, 24, 4096)
	format := audio.Format{
		Codec:       "flac",
		SampleRate:  48000,
		Channels:    2,
		BitDepth:    24,
		CodecHeader: codecHeader,
	}
	dec, err := NewFLAC(format)
	if err != nil {
		t.Fatalf("NewFLAC: %v", err)
	}
	if err := dec.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Double close should not panic
	if err := dec.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}

func TestFLACDecoder_DecodeAfterClose(t *testing.T) {
	codecHeader := buildMinimalFLACHeader(48000, 2, 24, 4096)
	format := audio.Format{
		Codec:       "flac",
		SampleRate:  48000,
		Channels:    2,
		BitDepth:    24,
		CodecHeader: codecHeader,
	}
	dec, err := NewFLAC(format)
	if err != nil {
		t.Fatalf("NewFLAC: %v", err)
	}
	dec.Close()

	_, err = dec.Decode([]byte{0x00})
	if err == nil {
		t.Error("expected error after Close")
	}
}
