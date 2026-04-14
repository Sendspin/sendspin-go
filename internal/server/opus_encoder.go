// ABOUTME: Opus audio encoder for bandwidth-efficient streaming
// ABOUTME: Wraps libopus to encode PCM audio to Opus format
package server

import (
	"fmt"
	"log"

	"gopkg.in/hraban/opus.v2"
)

// OpusEncoder wraps the Opus encoder
type OpusEncoder struct {
	encoder    *opus.Encoder
	sampleRate int
	channels   int
	frameSize  int // samples per channel per frame
}

// NewOpusEncoder creates a new Opus encoder.
// frameSize is in samples per channel (e.g., 960 for 20ms at 48kHz).
func NewOpusEncoder(sampleRate, channels, frameSize int) (*OpusEncoder, error) {
	// AppAudio mode tunes the encoder for full-bandwidth music rather than speech
	encoder, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus encoder: %w", err)
	}

	// 128 kbps per channel — aggressive enough for transparent music quality
	bitrate := 128000 * channels
	if err := encoder.SetBitrate(bitrate); err != nil {
		log.Printf("Warning: Failed to set Opus bitrate: %v", err)
	}

	return &OpusEncoder{
		encoder:    encoder,
		sampleRate: sampleRate,
		channels:   channels,
		frameSize:  frameSize,
	}, nil
}

// Encode encodes PCM samples to Opus.
// Input: []int16 interleaved samples; output: single Opus packet.
func (e *OpusEncoder) Encode(pcm []int16) ([]byte, error) {
	// Opus spec maximum packet size is 4000 bytes
	output := make([]byte, 4000)

	n, err := e.encoder.Encode(pcm, output)
	if err != nil {
		return nil, fmt.Errorf("opus encode failed: %w", err)
	}

	return output[:n], nil
}

// Close is a no-op; opus.Encoder has no Close method.
func (e *OpusEncoder) Close() error {
	return nil
}
