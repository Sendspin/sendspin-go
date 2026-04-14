// ABOUTME: Unit tests for Opus encoder
// ABOUTME: Tests Opus encoding functionality
package encode

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestNewOpus(t *testing.T) {
	tests := []struct {
		name        string
		format      audio.Format
		wantErr     bool
		errContains string
	}{
		{
			name: "valid Opus 48kHz stereo",
			format: audio.Format{
				Codec:      "opus",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   16,
			},
			wantErr: false,
		},
		{
			name: "valid Opus 48kHz mono",
			format: audio.Format{
				Codec:      "opus",
				SampleRate: 48000,
				Channels:   1,
				BitDepth:   16,
			},
			wantErr: false,
		},
		{
			name: "invalid codec",
			format: audio.Format{
				Codec:      "pcm",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   16,
			},
			wantErr:     true,
			errContains: "invalid codec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder, err := NewOpus(tt.format)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewOpus() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("NewOpus() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("NewOpus() unexpected error = %v", err)
				}
				if encoder == nil {
					t.Errorf("NewOpus() returned nil encoder")
				}
				if encoder != nil {
					encoder.Close()
				}
			}
		})
	}
}

func TestOpusEncoder_Encode(t *testing.T) {
	format := audio.Format{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	encoder, err := NewOpus(format)
	if err != nil {
		t.Fatalf("NewOpus() failed: %v", err)
	}
	defer encoder.Close()

	// 20ms frame at 48kHz = 960 samples per channel
	frameSize := 48000 / 50               // 20ms
	samples := make([]int32, frameSize*2) // stereo

	for i := 0; i < len(samples); i++ {
		samples[i] = int32((i % 1000) * 8388) // Simple pattern
	}

	output, err := encoder.Encode(samples)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}

	if len(output) == 0 {
		t.Errorf("Encode() returned empty output")
	}
	if len(output) > 4000 {
		t.Errorf("Encode() output size %d exceeds max Opus packet size 4000", len(output))
	}
}

func TestOpusEncoder_EncodeSilence(t *testing.T) {
	format := audio.Format{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	encoder, err := NewOpus(format)
	if err != nil {
		t.Fatalf("NewOpus() failed: %v", err)
	}
	defer encoder.Close()

	// 20ms frame at 48kHz = 960 samples per channel
	frameSize := 48000 / 50               // 20ms
	samples := make([]int32, frameSize*2) // stereo, all zeros

	output, err := encoder.Encode(samples)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}

	// Even silence should produce valid Opus packets
	if len(output) == 0 {
		t.Errorf("Encode() returned empty output for silence")
	}
	if len(output) > 4000 {
		t.Errorf("Encode() output size %d exceeds max Opus packet size 4000", len(output))
	}
}

func TestOpusEncoder_Close(t *testing.T) {
	format := audio.Format{
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	encoder, err := NewOpus(format)
	if err != nil {
		t.Fatalf("NewOpus() failed: %v", err)
	}

	err = encoder.Close()
	if err != nil {
		t.Errorf("Close() unexpected error = %v", err)
	}
}
