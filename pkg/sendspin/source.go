// ABOUTME: AudioSource — the contract a server audio source implements
// ABOUTME: Built-in implementations live alongside (see source_testtone.go)
package sendspin

// AudioSource provides PCM audio samples for streaming
type AudioSource interface {
	// Read reads PCM samples into the buffer (int32 for 24-bit support).
	// Returns number of samples read or error.
	Read(samples []int32) (int, error)

	// SampleRate returns the sample rate of the audio
	SampleRate() int

	// Channels returns the number of channels
	Channels() int

	// Metadata returns title, artist, album
	Metadata() (title, artist, album string)

	// Close closes the audio source
	Close() error
}
