// ABOUTME: Volume and mute helpers shared by audio output backends
// ABOUTME: Extracted from oto.go during the oto removal cleanup
package output

import (
	"math"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

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
// Volume is mapped through the Sendspin perceptual curve (vol/100)^1.5 so the
// gain tracks perceived loudness rather than raw amplitude. A linear vol/100
// mapping outputs ~0.5 (-6 dB) at volume 50 where a conformant player outputs
// ~0.354 (-9 dB); in a multi-room group that divergence makes this client
// audibly louder than its peers at any volume below 100.
func getVolumeMultiplier(volume int, muted bool) float64 {
	if muted {
		return 0.0
	}
	return math.Pow(float64(volume)/100.0, 1.5)
}
