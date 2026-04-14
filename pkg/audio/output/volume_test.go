// ABOUTME: Tests for shared volume/mute helpers in pkg/audio/output
// ABOUTME: Covers multiplier table, half-scale, mute, and 24-bit clamping
package output

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestVolumeMultiplier(t *testing.T) {
	tests := []struct {
		volume   int
		muted    bool
		expected float64
	}{
		{100, false, 1.0},
		{50, false, 0.5},
		{0, false, 0.0},
		{80, true, 0.0}, // muted overrides volume
	}

	for _, tt := range tests {
		result := getVolumeMultiplier(tt.volume, tt.muted)
		if result != tt.expected {
			t.Errorf("volume=%d muted=%v: expected %f, got %f",
				tt.volume, tt.muted, tt.expected, result)
		}
	}
}

func TestApplyVolume_HalfScale(t *testing.T) {
	samples := []int32{1000 << 8, -1000 << 8}

	result := applyVolume(samples, 50, false)

	if result[0] != int32(500<<8) {
		t.Errorf("sample 0: expected %d, got %d", 500<<8, result[0])
	}
	if result[1] != int32(-500<<8) {
		t.Errorf("sample 1: expected %d, got %d", -500<<8, result[1])
	}
}

func TestApplyVolume_Muted(t *testing.T) {
	samples := []int32{audio.Max24Bit, audio.Min24Bit, 1 << 20}

	result := applyVolume(samples, 100, true)

	for i, got := range result {
		if got != 0 {
			t.Errorf("sample %d: expected 0 when muted, got %d", i, got)
		}
	}
}

func TestApplyVolume_Clamps24Bit(t *testing.T) {
	// Inputs outside the 24-bit range must clamp, not overflow.
	overMax := int32(audio.Max24Bit + 1)
	underMin := int32(audio.Min24Bit - 1)

	result := applyVolume([]int32{overMax, underMin}, 100, false)

	if result[0] != audio.Max24Bit {
		t.Errorf("max clamp: expected %d, got %d", audio.Max24Bit, result[0])
	}
	if result[1] != audio.Min24Bit {
		t.Errorf("min clamp: expected %d, got %d", audio.Min24Bit, result[1])
	}
}
