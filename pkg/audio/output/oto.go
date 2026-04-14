// ABOUTME: Oto-based audio output implementation
// ABOUTME: Handles PCM playback with software volume control using oto library
package output

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/ebitengine/oto/v3"
)

// Oto output implementation using oto library
type Oto struct {
	ctx        context.Context
	cancel     context.CancelFunc
	otoCtx     *oto.Context
	player     *oto.Player
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	sampleRate int
	channels   int
	volume     int
	muted      bool
	ready      bool
}

// NewOto creates a new Oto output
func NewOto() Output {
	ctx, cancel := context.WithCancel(context.Background())

	return &Oto{
		ctx:    ctx,
		cancel: cancel,
		volume: 100,
		muted:  false,
	}
}

// Open initializes the output device
func (o *Oto) Open(sampleRate, channels, bitDepth int) error {
	// Output runs at float32, which carries 24-bit precision through oto to
	// the OS mixer. The requested bitDepth is informational only.
	_ = bitDepth

	// If already initialized with same format, reuse the existing context
	if o.otoCtx != nil && o.sampleRate == sampleRate && o.channels == channels {
		log.Printf("Audio output already initialized with same format, reusing context")
		return nil
	}

	// If format changed, we can't reinitialize oto (it only allows one context per process)
	// Log a warning but continue using the existing context
	if o.otoCtx != nil {
		log.Printf("Warning: format change detected (%dHz %dch -> %dHz %dch) but oto doesn't support reinitialization. Continuing with existing context.",
			o.sampleRate, o.channels, sampleRate, channels)
		return nil
	}

	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatFloat32LE,
	}

	ctx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return fmt.Errorf("failed to create oto context: %w", err)
	}

	<-readyChan

	o.otoCtx = ctx
	o.sampleRate = sampleRate
	o.channels = channels

	// Create pipe for continuous streaming
	o.pipeReader, o.pipeWriter = io.Pipe()

	// Create persistent player that reads from the pipe
	o.player = o.otoCtx.NewPlayer(o.pipeReader)
	o.player.Play()

	o.ready = true

	log.Printf("Audio output initialized: %dHz, %d channels", sampleRate, channels)

	return nil
}

// Write outputs audio samples (blocks until written)
func (o *Oto) Write(samples []int32) error {
	if !o.ready {
		return fmt.Errorf("output not initialized")
	}

	// Apply volume to samples (int32 format)
	volumedSamples := applyVolume(samples, o.volume, o.muted)

	// Normalize int32 24-bit samples to float32 in [-1.0, 1.0) and pack as
	// little-endian IEEE 754. float32 preserves full 24-bit precision.
	output := make([]byte, len(volumedSamples)*4)
	for i, s := range volumedSamples {
		bits := math.Float32bits(audio.SampleToFloat32(s))
		binary.LittleEndian.PutUint32(output[i*4:], bits)
	}

	// Write to pipe (which feeds the persistent player)
	// This blocks until the write completes
	if _, err := o.pipeWriter.Write(output); err != nil {
		return fmt.Errorf("pipe write failed: %w", err)
	}

	return nil
}

// Close releases output resources
func (o *Oto) Close() error {
	if o.pipeWriter != nil {
		o.pipeWriter.Close()
		o.pipeWriter = nil
	}
	if o.player != nil {
		o.player.Close()
		o.player = nil
	}
	if o.pipeReader != nil {
		o.pipeReader.Close()
		o.pipeReader = nil
	}
	if o.otoCtx != nil {
		o.otoCtx.Suspend()
		o.ready = false
	}
	o.cancel()
	return nil
}

// SetVolume sets the volume (0-100)
func (o *Oto) SetVolume(volume int) {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	o.volume = volume
	log.Printf("Volume set to %d", volume)
}

// SetMuted sets mute state
func (o *Oto) SetMuted(muted bool) {
	o.muted = muted
	log.Printf("Muted: %v", muted)
}

// GetVolume returns current volume
func (o *Oto) GetVolume() int {
	return o.volume
}

// IsMuted returns mute state
func (o *Oto) IsMuted() bool {
	return o.muted
}

// applyVolume applies volume and mute to samples with clipping protection
func applyVolume(samples []int32, volume int, muted bool) []int32 {
	multiplier := getVolumeMultiplier(volume, muted)

	result := make([]int32, len(samples))
	for i, sample := range samples {
		scaled := int64(float64(sample) * multiplier)

		// Clamp to 24-bit range to prevent overflow
		if scaled > audio.Max24Bit {
			scaled = audio.Max24Bit
		} else if scaled < audio.Min24Bit {
			scaled = audio.Min24Bit
		}

		result[i] = int32(scaled)
	}

	return result
}

// getVolumeMultiplier calculates volume multiplier
func getVolumeMultiplier(volume int, muted bool) float64 {
	if muted {
		return 0.0
	}
	return float64(volume) / 100.0
}
