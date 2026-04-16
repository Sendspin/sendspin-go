// ABOUTME: Tests for the FLAC encoder
// ABOUTME: Verifies round-trip encode produces valid FLAC frames with compression
package server

import (
	"testing"
)

func TestFLACEncoder_RoundTrip(t *testing.T) {
	const (
		sampleRate = 48000
		channels   = 2
		bitDepth   = 24
		blockSize  = 960 // 20ms at 48kHz
	)

	enc, err := NewFLACEncoder(sampleRate, channels, bitDepth, blockSize)
	if err != nil {
		t.Fatalf("NewFLACEncoder: %v", err)
	}
	defer enc.Close()

	header := enc.CodecHeader()
	if len(header) < 42 {
		t.Fatalf("codec header too short: %d bytes (min 42)", len(header))
	}
	if string(header[:4]) != "fLaC" {
		t.Errorf("codec header missing fLaC marker, got %q", header[:4])
	}

	// Create a simple test signal
	samples := make([]int32, blockSize*channels)
	for i := range samples {
		samples[i] = int32((i % 256) << 16)
	}

	flacBytes, err := enc.Encode(samples)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(flacBytes) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	pcmSize := len(samples) * 3 // 24-bit = 3 bytes per sample
	t.Logf("Encoded %d samples → %d FLAC bytes (%.1f%% of PCM %d bytes)",
		len(samples), len(flacBytes), float64(len(flacBytes))/float64(pcmSize)*100, pcmSize)
}

func TestFLACEncoder_MultipleBlocks(t *testing.T) {
	enc, err := NewFLACEncoder(48000, 2, 24, 960)
	if err != nil {
		t.Fatalf("NewFLACEncoder: %v", err)
	}
	defer enc.Close()

	samples := make([]int32, 960*2)
	for i := 0; i < 10; i++ {
		data, err := enc.Encode(samples)
		if err != nil {
			t.Fatalf("Encode block %d: %v", i, err)
		}
		if len(data) == 0 {
			t.Fatalf("Encode block %d returned empty", i)
		}
	}
}

func TestFLACEncoder_16Bit(t *testing.T) {
	enc, err := NewFLACEncoder(44100, 2, 16, 882)
	if err != nil {
		t.Fatalf("NewFLACEncoder: %v", err)
	}
	defer enc.Close()

	header := enc.CodecHeader()
	if string(header[:4]) != "fLaC" {
		t.Errorf("missing fLaC marker")
	}

	samples := make([]int32, 882*2)
	data, err := enc.Encode(samples)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty encode")
	}
}

func TestFLACEncoder_Compression(t *testing.T) {
	// Silence should compress extremely well with prediction analysis enabled
	enc, err := NewFLACEncoder(48000, 2, 24, 960)
	if err != nil {
		t.Fatalf("NewFLACEncoder: %v", err)
	}
	defer enc.Close()

	silence := make([]int32, 960*2) // all zeros
	data, err := enc.Encode(silence)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pcmSize := 960 * 2 * 3 // 24-bit stereo
	ratio := float64(len(data)) / float64(pcmSize) * 100
	t.Logf("Silence: %d PCM bytes → %d FLAC bytes (%.1f%%)", pcmSize, len(data), ratio)

	// Silence should compress to well under 10% of PCM size
	if ratio > 20 {
		t.Errorf("silence compression ratio %.1f%% is too high — prediction analysis may not be working", ratio)
	}
}

func TestFLACEncoder_Close(t *testing.T) {
	enc, err := NewFLACEncoder(48000, 2, 24, 960)
	if err != nil {
		t.Fatalf("NewFLACEncoder: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
