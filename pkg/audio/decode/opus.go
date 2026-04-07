// ABOUTME: Opus audio decoder
// ABOUTME: Decodes Opus audio to int32 samples
package decode

import (
	"fmt"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"gopkg.in/hraban/opus.v2"
)

// OpusDecoder decodes Opus audio
type OpusDecoder struct {
	decoder  *opus.Decoder
	format   audio.Format
	pcm16Buf []int16 // reusable decode buffer to avoid per-frame allocation
}

// NewOpus creates a new Opus decoder
func NewOpus(format audio.Format) (Decoder, error) {
	if format.Codec != "opus" {
		return nil, fmt.Errorf("invalid codec for Opus decoder: %s", format.Codec)
	}

	dec, err := opus.NewDecoder(format.SampleRate, format.Channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus decoder: %w", err)
	}

	return &OpusDecoder{
		decoder:  dec,
		format:   format,
		pcm16Buf: make([]int16, 5760*format.Channels),
	}, nil
}

// Decode converts Opus bytes to int32 samples
func (d *OpusDecoder) Decode(data []byte) ([]int32, error) {
	// Reuse pre-allocated int16 buffer for decode (avoids 23KB alloc per frame)
	n, err := d.decoder.Decode(data, d.pcm16Buf)
	if err != nil {
		return nil, fmt.Errorf("opus decode failed: %w", err)
	}

	// Convert int16 to int32 (caller owns the returned slice)
	actualSamples := n * d.format.Channels
	pcm32 := make([]int32, actualSamples)
	for i := 0; i < actualSamples; i++ {
		pcm32[i] = audio.SampleFromInt16(d.pcm16Buf[i])
	}
	return pcm32, nil
}

// Close releases decoder resources
func (d *OpusDecoder) Close() error {
	return nil
}
