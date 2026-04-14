// ABOUTME: Multi-codec audio decoder
// ABOUTME: Supports Opus, FLAC, and PCM formats
package audio

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"gopkg.in/hraban/opus.v2"
)

type Decoder interface {
	Decode(data []byte) ([]int32, error)
	Close() error
}

func NewDecoder(format Format) (Decoder, error) {
	switch format.Codec {
	case "pcm":
		return &PCMDecoder{bitDepth: format.BitDepth}, nil
	case "opus":
		return NewOpusDecoder(format)
	case "flac":
		return NewFLACDecoder(format)
	default:
		return nil, fmt.Errorf("unsupported codec: %s", format.Codec)
	}
}

type PCMDecoder struct {
	bitDepth int
}

func (d *PCMDecoder) Decode(data []byte) ([]int32, error) {
	if d.bitDepth == 24 {
		// 24-bit PCM: 3 bytes per sample
		numSamples := len(data) / 3
		samples := make([]int32, numSamples)
		for i := 0; i < numSamples; i++ {
			b := [3]byte{data[i*3], data[i*3+1], data[i*3+2]}
			samples[i] = SampleFrom24Bit(b)
		}
		return samples, nil
	} else {
		// 16-bit PCM: 2 bytes per sample (default)
		numSamples := len(data) / 2
		samples := make([]int32, numSamples)
		for i := 0; i < numSamples; i++ {
			sample16 := int16(binary.LittleEndian.Uint16(data[i*2:]))
			samples[i] = SampleFromInt16(sample16)
		}
		return samples, nil
	}
}

func (d *PCMDecoder) Close() error {
	return nil
}

type OpusDecoder struct {
	decoder *opus.Decoder
	format  Format
}

func NewOpusDecoder(format Format) (*OpusDecoder, error) {
	dec, err := opus.NewDecoder(format.SampleRate, format.Channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus decoder: %w", err)
	}

	return &OpusDecoder{
		decoder: dec,
		format:  format,
	}, nil
}

func (d *OpusDecoder) Decode(data []byte) ([]int32, error) {
	pcmSize := 5760 * d.format.Channels // Max frame size
	pcm16 := make([]int16, pcmSize)

	n, err := d.decoder.Decode(data, pcm16)
	if err != nil {
		return nil, fmt.Errorf("opus decode failed: %w", err)
	}

	// Convert int16 to int32 (Opus is always 16-bit)
	actualSamples := n * d.format.Channels
	pcm32 := make([]int32, actualSamples)
	for i := 0; i < actualSamples; i++ {
		pcm32[i] = SampleFromInt16(pcm16[i])
	}
	return pcm32, nil
}

func (d *OpusDecoder) Close() error {
	return nil
}

type FLACDecoder struct {
	format Format
}

func NewFLACDecoder(format Format) (*FLACDecoder, error) {
	// FLAC decoder will be created per-chunk if needed
	// For now, basic support
	return &FLACDecoder{
		format: format,
	}, nil
}

func (d *FLACDecoder) Decode(data []byte) ([]int32, error) {
	// For streaming FLAC, we need to handle frame-by-frame decoding
	// This is a simplified implementation
	// In production, would use mewkiz/flac's streaming API
	return nil, fmt.Errorf("FLAC streaming decode not implemented")
}

func (d *FLACDecoder) Close() error {
	return nil
}

func DecodeBase64Header(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}
