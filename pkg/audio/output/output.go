// ABOUTME: Audio output interface definition
// ABOUTME: Common interface for audio playback backends
package output

// Output represents an audio output device
type Output interface {
	// Open initializes the output device
	Open(sampleRate, channels, bitDepth int) error

	// Write outputs audio samples (blocks until written)
	Write(samples []int32) error

	// Close releases output resources
	Close() error

	// SetVolume sets the volume (0-100)
	SetVolume(volume int)

	// SetMuted sets mute state
	SetMuted(muted bool)
}
