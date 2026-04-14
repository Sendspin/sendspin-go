// ABOUTME: Test tone generator for audio source
// ABOUTME: Generates 440Hz sine wave for testing
package server

import (
	"math"
	"sync"
)

type TestToneSource struct {
	sampleIndex uint64
	sampleMu    sync.Mutex
	frequency   float64
}

func NewTestToneSource() *TestToneSource {
	return &TestToneSource{
		frequency: 440.0, // A4 note
	}
}

func (s *TestToneSource) Read(samples []int32) (int, error) {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()

	numSamples := len(samples) / 2 // Stereo

	for i := 0; i < numSamples; i++ {
		t := float64(s.sampleIndex+uint64(i)) / float64(DefaultSampleRate)
		sample := math.Sin(2 * math.Pi * s.frequency * t)

		// 50% amplitude to avoid clipping on a full-scale sine
		const max24bit = 8388607 // 2^23 - 1
		pcmValue := int32(sample * max24bit * 0.5)

		samples[i*2] = pcmValue
		samples[i*2+1] = pcmValue
	}

	s.sampleIndex += uint64(numSamples)

	return len(samples), nil
}

func (s *TestToneSource) SampleRate() int { return DefaultSampleRate }
func (s *TestToneSource) Channels() int   { return DefaultChannels }
func (s *TestToneSource) Metadata() (string, string, string) {
	return "Test Tone", "Sendspin Server", "Reference Implementation"
}
func (s *TestToneSource) Close() error { return nil }
