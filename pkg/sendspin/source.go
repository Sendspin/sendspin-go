// ABOUTME: Audio source abstraction for Sendspin streaming
// ABOUTME: Provides AudioSource interface and common implementations
package sendspin

import (
	"fmt"
	"math"
	"sync"
)

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

// TestToneSource generates a 440Hz test tone for testing
type TestToneSource struct {
	sampleIndex uint64
	sampleMu    sync.Mutex
	frequency   float64
	sampleRate  int
	channels    int
}

// NewTestTone creates a new test tone generator
// Generates a 440Hz sine wave at the specified sample rate and channels
func NewTestTone(sampleRate, channels int) *TestToneSource {
	if sampleRate == 0 {
		sampleRate = DefaultSampleRate
	}
	if channels == 0 {
		channels = DefaultChannels
	}

	return &TestToneSource{
		frequency:  440.0, // A4 note
		sampleRate: sampleRate,
		channels:   channels,
	}
}

func (s *TestToneSource) Read(samples []int32) (int, error) {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()

	numSamples := len(samples) / s.channels

	for i := 0; i < numSamples; i++ {
		// Generate sine wave
		t := float64(s.sampleIndex+uint64(i)) / float64(s.sampleRate)
		sample := math.Sin(2 * math.Pi * s.frequency * t)

		// Convert to 24-bit PCM (using int32)
		// Scale to 24-bit range and apply 50% volume to avoid clipping
		const max24bit = 8388607 // 2^23 - 1
		pcmValue := int32(sample * max24bit * 0.5)

		// Duplicate to all channels
		for ch := 0; ch < s.channels; ch++ {
			samples[i*s.channels+ch] = pcmValue
		}
	}

	s.sampleIndex += uint64(numSamples)

	return len(samples), nil
}

func (s *TestToneSource) SampleRate() int { return s.sampleRate }
func (s *TestToneSource) Channels() int   { return s.channels }
func (s *TestToneSource) Metadata() (string, string, string) {
	return "Test Tone", "Sendspin", "Test Signal"
}
func (s *TestToneSource) Close() error { return nil }

// FileSource streams audio from a file
// Note: This is a placeholder - users should use server.NewAudioSource() for file streaming
// which supports MP3, FLAC, and other formats
type FileSource struct {
	// This is intentionally not implemented in the public API yet
	// Users can use internal/server.NewAudioSource() for now
	// TODO: Move file source implementations to pkg/audio/decode
}

// NewFileSource creates an audio source from a file
// Supported formats: MP3, FLAC
// Returns an error if the file cannot be opened or decoded
func NewFileSource(path string) (AudioSource, error) {
	// For now, we use the internal implementation
	// TODO: Migrate file sources to pkg/audio/decode
	return nil, fmt.Errorf("file source not implemented: use internal/server.NewAudioSource()")
}
