// ABOUTME: Volume and mute helpers shared by audio output backends
// ABOUTME: Extracted from oto.go during the oto removal cleanup
package output

import "github.com/Sendspin/sendspin-go/pkg/audio"

// applyVolume applies volume and mute to samples with clipping protection.
// Samples are expected to be in the int32 24-bit range.
func applyVolume(samples []int32, volume int, muted bool) []int32 {
	multiplier := getVolumeMultiplier(volume, muted)

	result := make([]int32, len(samples))
	for i, sample := range samples {
		scaled := int64(float64(sample) * multiplier)

		if scaled > audio.Max24Bit {
			scaled = audio.Max24Bit
		} else if scaled < audio.Min24Bit {
			scaled = audio.Min24Bit
		}

		result[i] = int32(scaled)
	}

	return result
}

// getVolumeMultiplier returns the float multiplier for a given volume/mute state.
func getVolumeMultiplier(volume int, muted bool) float64 {
	if muted {
		return 0.0
	}
	return float64(volume) / 100.0
}
