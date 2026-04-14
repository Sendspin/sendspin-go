// ABOUTME: Audio output interface definition
// ABOUTME: Common interface for audio playback backends
package output

// Output represents an audio output device
type Output interface {
	Open(sampleRate, channels, bitDepth int) error
	Write(samples []int32) error
	Close() error
	SetVolume(volume int)
	SetMuted(muted bool)
}
