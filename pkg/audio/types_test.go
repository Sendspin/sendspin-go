// ABOUTME: Tests for audio types
// ABOUTME: Tests sample conversion functions
package audio

import "testing"

func TestSampleFromInt16(t *testing.T) {
	tests := []struct {
		name     string
		input    int16
		expected int32
	}{
		{"zero", 0, 0},
		{"positive", 100, 100 << 8},
		{"negative", -100, -100 << 8},
		{"max", 32767, 32767 << 8},
		{"min", -32768, -32768 << 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SampleFromInt16(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestSampleToInt16(t *testing.T) {
	tests := []struct {
		name     string
		input    int32
		expected int16
	}{
		{"zero", 0, 0},
		{"positive", 100 << 8, 100},
		{"negative", -100 << 8, -100},
		{"24bit positive", 1000000, 3906}, // 1000000 >> 8 = 3906
		{"24bit negative", -1000000, -3907},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SampleToInt16(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestSampleToFloat32(t *testing.T) {
	tests := []struct {
		name     string
		input    int32
		expected float32
	}{
		{"zero", 0, 0.0},
		{"max 24-bit", Max24Bit, float32(Max24Bit) / float32(1<<23)},
		{"min 24-bit", Min24Bit, -1.0},
		{"half scale positive", 1 << 22, 0.5},
		{"half scale negative", -(1 << 22), -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SampleToFloat32(tt.input)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}

	for _, s := range []int32{Max24Bit, Min24Bit, 0, 1, -1, 1234567, -1234567} {
		f := SampleToFloat32(s)
		if f < -1.0 || f >= 1.0 {
			t.Errorf("sample %d normalized to %f, outside [-1.0, 1.0)", s, f)
		}
	}
}

func TestSampleTo24Bit(t *testing.T) {
	tests := []struct {
		name     string
		input    int32
		expected [3]byte
	}{
		{"zero", 0, [3]byte{0, 0, 0}},
		{"positive", 0x123456, [3]byte{0x56, 0x34, 0x12}},
		{"negative", -256, [3]byte{0x00, 0xFF, 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SampleTo24Bit(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSampleFrom24Bit(t *testing.T) {
	tests := []struct {
		name     string
		input    [3]byte
		expected int32
	}{
		{"zero", [3]byte{0, 0, 0}, 0},
		{"positive", [3]byte{0x56, 0x34, 0x12}, 0x123456},
		{"negative", [3]byte{0x00, 0xFF, 0xFF}, -256},
		{"max positive", [3]byte{0xFF, 0xFF, 0x7F}, Max24Bit},
		{"max negative", [3]byte{0x00, 0x00, 0x80}, Min24Bit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SampleFrom24Bit(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestRoundTrip16Bit(t *testing.T) {
	// Test that 16-bit samples survive round-trip conversion
	samples := []int16{0, 100, -100, 1000, -1000, 32767, -32768}

	for _, original := range samples {
		sample32 := SampleFromInt16(original)
		result := SampleToInt16(sample32)
		if result != original {
			t.Errorf("round-trip failed: %d -> %d -> %d", original, sample32, result)
		}
	}
}

func TestRoundTrip24Bit(t *testing.T) {
	// Test that 24-bit samples survive round-trip conversion
	samples := []int32{0, 100000, -100000, Max24Bit, Min24Bit}

	for _, original := range samples {
		bytes := SampleTo24Bit(original)
		result := SampleFrom24Bit(bytes)
		// Mask to 24-bit for comparison
		expected := original & 0xFFFFFF
		if expected&0x800000 != 0 {
			expected |= ^0xFFFFFF
		}
		if result != expected {
			t.Errorf("round-trip failed: %d -> %v -> %d (expected %d)", original, bytes, result, expected)
		}
	}
}
