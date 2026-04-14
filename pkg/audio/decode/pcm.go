// ABOUTME: PCM audio decoder
// ABOUTME: Decodes 16-bit and 24-bit PCM audio to int32 samples
package decode

import (
	"encoding/binary"
	"fmt"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

type PCMDecoder struct {
	bitDepth int
}

func NewPCM(format audio.Format) (Decoder, error) {
	if format.Codec != "pcm" {
		return nil, fmt.Errorf("invalid codec for PCM decoder: %s", format.Codec)
	}

	if format.BitDepth != 16 && format.BitDepth != 24 {
		return nil, fmt.Errorf("unsupported bit depth: %d (supported: 16, 24)", format.BitDepth)
	}

	return &PCMDecoder{
		bitDepth: format.BitDepth,
	}, nil
}

func (d *PCMDecoder) Decode(data []byte) ([]int32, error) {
	if d.bitDepth == 24 {
		numSamples := len(data) / 3
		samples := make([]int32, numSamples)
		for i := 0; i < numSamples; i++ {
			b := [3]byte{data[i*3], data[i*3+1], data[i*3+2]}
			samples[i] = audio.SampleFrom24Bit(b)
		}
		return samples, nil
	} else {
		numSamples := len(data) / 2
		samples := make([]int32, numSamples)
		for i := 0; i < numSamples; i++ {
			sample16 := int16(binary.LittleEndian.Uint16(data[i*2:]))
			samples[i] = audio.SampleFromInt16(sample16)
		}
		return samples, nil
	}
}

func (d *PCMDecoder) Close() error {
	return nil
}
