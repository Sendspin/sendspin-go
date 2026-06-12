// ABOUTME: Built-in 440Hz test-tone AudioSource for demos and tests
// ABOUTME: Server-side reference source; generates a sine wave at the given rate
package sendspin

import (
	"math"
	"sync"
)

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
		t := float64(s.sampleIndex+uint64(i)) / float64(s.sampleRate)
		sample := math.Sin(2 * math.Pi * s.frequency * t)

		// Scale to 24-bit range; 50% amplitude avoids clipping on decode
		const max24bit = 8388607 // 2^23 - 1
		pcmValue := int32(sample * max24bit * 0.5)

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
