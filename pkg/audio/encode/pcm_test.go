// ABOUTME: Unit tests for PCM encoder
// ABOUTME: Tests 16-bit and 24-bit PCM encoding
package encode

import (
	"encoding/binary"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestNewPCM(t *testing.T) {
	tests := []struct {
		name        string
		format      audio.Format
		wantErr     bool
		errContains string
	}{
		{
			name: "valid 16-bit PCM",
			format: audio.Format{
				Codec:      "pcm",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   16,
			},
			wantErr: false,
		},
		{
			name: "valid 24-bit PCM",
			format: audio.Format{
				Codec:      "pcm",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   24,
			},
			wantErr: false,
		},
		{
			name: "invalid codec",
			format: audio.Format{
				Codec:      "opus",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   16,
			},
			wantErr:     true,
			errContains: "invalid codec",
		},
		{
			name: "unsupported bit depth",
			format: audio.Format{
				Codec:      "pcm",
				SampleRate: 48000,
				Channels:   2,
				BitDepth:   32,
			},
			wantErr:     true,
			errContains: "unsupported bit depth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder, err := NewPCM(tt.format)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewPCM() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("NewPCM() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("NewPCM() unexpected error = %v", err)
				}
				if encoder == nil {
					t.Errorf("NewPCM() returned nil encoder")
				}
			}
		})
	}
}

func TestPCMEncoder_Encode16Bit(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	encoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("NewPCM() failed: %v", err)
	}
	defer encoder.Close()

	samples := []int32{
		0,         // silence
		0x7FFF00,  // max positive 16-bit (left-justified in 24-bit)
		-0x800000, // max negative 16-bit (left-justified in 24-bit)
		0x123400,  // arbitrary positive value
		-0x567800, // arbitrary negative value
	}

	output, err := encoder.Encode(samples)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}

	expectedSize := len(samples) * 2
	if len(output) != expectedSize {
		t.Errorf("Encode() output size = %d, want %d", len(output), expectedSize)
	}

	for i, sample := range samples {
		expected := audio.SampleToInt16(sample)
		actual := int16(binary.LittleEndian.Uint16(output[i*2:]))
		if actual != expected {
			t.Errorf("Sample %d: got %d, want %d", i, actual, expected)
		}
	}
}

func TestPCMEncoder_Encode24Bit(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   24,
	}

	encoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("NewPCM() failed: %v", err)
	}
	defer encoder.Close()

	samples := []int32{
		0,         // silence
		0x7FFFFF,  // max positive 24-bit
		-0x800000, // max negative 24-bit
		0x123456,  // arbitrary positive value
		-0x567890, // arbitrary negative value
	}

	output, err := encoder.Encode(samples)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}

	expectedSize := len(samples) * 3
	if len(output) != expectedSize {
		t.Errorf("Encode() output size = %d, want %d", len(output), expectedSize)
	}

	for i, sample := range samples {
		expected := audio.SampleTo24Bit(sample)
		actual := [3]byte{
			output[i*3],
			output[i*3+1],
			output[i*3+2],
		}
		if actual != expected {
			t.Errorf("Sample %d: got %v, want %v", i, actual, expected)
		}
	}
}

func TestPCMEncoder_Close(t *testing.T) {
	format := audio.Format{
		Codec:      "pcm",
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   16,
	}

	encoder, err := NewPCM(format)
	if err != nil {
		t.Fatalf("NewPCM() failed: %v", err)
	}

	err = encoder.Close()
	if err != nil {
		t.Errorf("Close() unexpected error = %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
