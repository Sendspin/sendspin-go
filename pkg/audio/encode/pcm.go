// ABOUTME: PCM audio encoder
// ABOUTME: Encodes int32 samples to 16-bit or 24-bit PCM bytes
package encode

import (
	"encoding/binary"
	"fmt"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

type PCMEncoder struct {
	bitDepth int
}

func NewPCM(format audio.Format) (Encoder, error) {
	if format.Codec != "pcm" {
		return nil, fmt.Errorf("invalid codec for PCM encoder: %s", format.Codec)
	}

	if format.BitDepth != 16 && format.BitDepth != 24 {
		return nil, fmt.Errorf("unsupported bit depth: %d (supported: 16, 24)", format.BitDepth)
	}

	return &PCMEncoder{
		bitDepth: format.BitDepth,
	}, nil
}

func (e *PCMEncoder) Encode(samples []int32) ([]byte, error) {
	if e.bitDepth == 24 {
		output := make([]byte, len(samples)*3)
		for i, sample := range samples {
			bytes := audio.SampleTo24Bit(sample)
			output[i*3] = bytes[0]
			output[i*3+1] = bytes[1]
			output[i*3+2] = bytes[2]
		}
		return output, nil
	} else {
		output := make([]byte, len(samples)*2)
		for i, sample := range samples {
			sample16 := audio.SampleToInt16(sample)
			binary.LittleEndian.PutUint16(output[i*2:], uint16(sample16))
		}
		return output, nil
	}
}

func (e *PCMEncoder) Close() error {
	return nil
}
