// ABOUTME: Opus audio encoder
// ABOUTME: Encodes int32 samples to Opus bytes
package encode

import (
	"fmt"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"gopkg.in/hraban/opus.v2"
)

type OpusEncoder struct {
	encoder    *opus.Encoder
	sampleRate int
	channels   int
	frameSize  int
	pcmBuf     []int16 // reusable conversion buffer
	outBuf     []byte  // reusable encode output buffer
}

func NewOpus(format audio.Format) (Encoder, error) {
	if format.Codec != "opus" {
		return nil, fmt.Errorf("invalid codec for Opus encoder: %s", format.Codec)
	}

	encoder, err := opus.NewEncoder(format.SampleRate, format.Channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus encoder: %w", err)
	}

	// Opus frame size depends on sample rate
	frameSize := format.SampleRate / 50 // 20ms frame

	return &OpusEncoder{
		encoder:    encoder,
		sampleRate: format.SampleRate,
		channels:   format.Channels,
		frameSize:  frameSize,
		pcmBuf:     make([]int16, frameSize*format.Channels),
		outBuf:     make([]byte, 4000),
	}, nil
}

func (e *OpusEncoder) Encode(samples []int32) ([]byte, error) {
	if len(samples) > len(e.pcmBuf) {
		e.pcmBuf = make([]int16, len(samples))
	}

	pcm := e.pcmBuf[:len(samples)]
	for i, sample := range samples {
		pcm[i] = audio.SampleToInt16(sample)
	}

	n, err := e.encoder.Encode(pcm, e.outBuf)
	if err != nil {
		return nil, fmt.Errorf("opus encode error: %w", err)
	}

	return append([]byte(nil), e.outBuf[:n]...), nil
}

func (e *OpusEncoder) Close() error {
	return nil
}
