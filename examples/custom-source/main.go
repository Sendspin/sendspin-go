// ABOUTME: Custom AudioSource implementation example
// ABOUTME: Demonstrates how to create custom audio sources for Sendspin
package main

import (
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Sendspin/sendspin-go/pkg/sendspin"
)

// MultiToneSource generates multiple sine waves at different frequencies
// This demonstrates how to implement the AudioSource interface
type MultiToneSource struct {
	frequencies []float64
	sampleRate  int
	channels    int
	sampleIndex uint64
	mu          sync.Mutex
}

// NewMultiTone creates a source that mixes multiple frequencies
func NewMultiTone(sampleRate, channels int, frequencies []float64) *MultiToneSource {
	return &MultiToneSource{
		frequencies: frequencies,
		sampleRate:  sampleRate,
		channels:    channels,
	}
}

// Read implements AudioSource.Read
// Generates PCM samples by mixing multiple sine waves
func (s *MultiToneSource) Read(samples []int32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	numFrames := len(samples) / s.channels

	for i := 0; i < numFrames; i++ {
		// Calculate time for this sample
		t := float64(s.sampleIndex+uint64(i)) / float64(s.sampleRate)

		// Mix all frequencies together
		var mixedSample float64
		for _, freq := range s.frequencies {
			mixedSample += math.Sin(2 * math.Pi * freq * t)
		}

		// Average the mixed signal
		if len(s.frequencies) > 0 {
			mixedSample /= float64(len(s.frequencies))
		}

		// Convert to 24-bit PCM (int32)
		// Scale to 24-bit range with 50% volume to prevent clipping
		const max24bit = 8388607 // 2^23 - 1
		pcmValue := int32(mixedSample * max24bit * 0.5)

		// Duplicate to all channels
		for ch := 0; ch < s.channels; ch++ {
			samples[i*s.channels+ch] = pcmValue
		}
	}

	s.sampleIndex += uint64(numFrames)

	return len(samples), nil
}

// SampleRate implements AudioSource.SampleRate
func (s *MultiToneSource) SampleRate() int {
	return s.sampleRate
}

// Channels implements AudioSource.Channels
func (s *MultiToneSource) Channels() int {
	return s.channels
}

// Metadata implements AudioSource.Metadata
func (s *MultiToneSource) Metadata() (title, artist, album string) {
	return "Multi-Tone Test Signal", "Sendspin Examples", "Custom Sources"
}

// Close implements AudioSource.Close
func (s *MultiToneSource) Close() error {
	return nil
}

// SweepSource generates a frequency sweep (chirp)
// Demonstrates time-varying audio generation
type SweepSource struct {
	startFreq   float64
	endFreq     float64
	duration    float64 // seconds
	sampleRate  int
	channels    int
	sampleIndex uint64
	mu          sync.Mutex
}

// NewSweep creates a frequency sweep source
func NewSweep(sampleRate, channels int, startFreq, endFreq, duration float64) *SweepSource {
	return &SweepSource{
		startFreq:  startFreq,
		endFreq:    endFreq,
		duration:   duration,
		sampleRate: sampleRate,
		channels:   channels,
	}
}

// Read implements AudioSource.Read
func (s *SweepSource) Read(samples []int32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	numFrames := len(samples) / s.channels

	for i := 0; i < numFrames; i++ {
		t := float64(s.sampleIndex+uint64(i)) / float64(s.sampleRate)

		// Loop the sweep
		t = math.Mod(t, s.duration)

		// Linear frequency sweep
		progress := t / s.duration
		freq := s.startFreq + (s.endFreq-s.startFreq)*progress

		// Generate sine wave at current frequency
		sample := math.Sin(2 * math.Pi * freq * t)

		// Convert to 24-bit PCM
		const max24bit = 8388607
		pcmValue := int32(sample * max24bit * 0.5)

		// Duplicate to all channels
		for ch := 0; ch < s.channels; ch++ {
			samples[i*s.channels+ch] = pcmValue
		}
	}

	s.sampleIndex += uint64(numFrames)

	return len(samples), nil
}

func (s *SweepSource) SampleRate() int { return s.sampleRate }
func (s *SweepSource) Channels() int   { return s.channels }
func (s *SweepSource) Metadata() (string, string, string) {
	return "Frequency Sweep", "Sendspin Examples", "Custom Sources"
}
func (s *SweepSource) Close() error { return nil }

func main() {
	port := flag.Int("port", 8927, "Server port")
	mode := flag.String("mode", "chord", "Source mode: chord, sweep, single")
	sampleRate := flag.Int("rate", 192000, "Sample rate (Hz)")
	channels := flag.Int("channels", 2, "Number of channels")
	flag.Parse()

	var source sendspin.AudioSource

	// Create audio source based on mode
	switch *mode {
	case "chord":
		// A major chord: A4, C#5, E5
		frequencies := []float64{440.0, 554.37, 659.25}
		source = NewMultiTone(*sampleRate, *channels, frequencies)
		log.Printf("Creating A major chord: %v Hz", frequencies)

	case "sweep":
		// Sweep from 220 Hz (A3) to 880 Hz (A5) over 5 seconds
		source = NewSweep(*sampleRate, *channels, 220.0, 880.0, 5.0)
		log.Printf("Creating frequency sweep: 220 - 880 Hz over 5 seconds")

	case "single":
		// Single 440 Hz tone (same as basic example)
		source = sendspin.NewTestTone(*sampleRate, *channels)
		log.Printf("Creating single tone: 440 Hz")

	default:
		log.Fatalf("Invalid mode: %s (use: chord, sweep, single)", *mode)
	}

	config := sendspin.ServerConfig{
		Port:       *port,
		Name:       "Custom Source Example",
		Source:     source,
		EnableMDNS: true,
		Debug:      false,
	}

	server, err := sendspin.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("Starting server on port %d...", *port)
	log.Printf("Audio: %dHz, %d channels, 24-bit", *sampleRate, *channels)

	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			errChan <- err
		}
	}()

	log.Printf("\nServer running. Press Ctrl+C to stop")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Printf("Shutting down...")
	case err := <-errChan:
		log.Printf("Server error: %v", err)
	}

	server.Stop()
	log.Printf("Server stopped")
}
