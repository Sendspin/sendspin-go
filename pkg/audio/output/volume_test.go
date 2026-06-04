// ABOUTME: Tests for shared volume/mute helpers in pkg/audio/output
// ABOUTME: Covers multiplier table, half-scale, mute, and 24-bit clamping
package output

import (
	"math"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

func TestVolumeMultiplier(t *testing.T) {
	const tol = 1e-9
	tests := []struct {
		volume   int
		muted    bool
		expected float64
	}{
		{100, false, 1.0},               // full scale is unchanged by the curve
		{50, false, 0.3535533905932738}, // (0.5)^1.5 ≈ -9 dB, per the perceptual curve
		{0, false, 0.0},                 // silence
		{80, true, 0.0},                 // muted overrides volume
	}

	for _, tt := range tests {
		result := getVolumeMultiplier(tt.volume, tt.muted)
		if math.Abs(result-tt.expected) > tol {
			t.Errorf("volume=%d muted=%v: expected %f, got %f",
				tt.volume, tt.muted, tt.expected, result)
		}
	}
}

func TestApplyVolume_PerceptualMidScale(t *testing.T) {
	// At volume 50 the perceptual curve (0.5)^1.5 yields ~0.354 gain, quieter
	// than the 0.5 a linear mapping would produce. This is the audible
	// conformance fix, so guard against a regression back to linear gain.
	const sample = int32(1000 << 8)
	samples := []int32{sample, -sample}

	result := applyVolume(samples, 50, false)

	want := int32(float64(sample) * math.Pow(0.5, 1.5))
	if result[0] != want {
		t.Errorf("sample 0: expected %d, got %d", want, result[0])
	}
	if result[1] != -want {
		t.Errorf("sample 1: expected %d, got %d", -want, result[1])
	}
	if result[0] >= sample/2 {
		t.Errorf("perceptual gain at vol 50 (%d) should be quieter than linear half (%d)",
			result[0], sample/2)
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
