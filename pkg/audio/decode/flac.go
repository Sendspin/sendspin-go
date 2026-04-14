// ABOUTME: FLAC audio decoder
// ABOUTME: Decodes FLAC audio to int32 samples
package decode

import (
	"fmt"

	"github.com/Sendspin/sendspin-go/pkg/audio"
)

type FLACDecoder struct {
	format audio.Format
}

func NewFLAC(format audio.Format) (Decoder, error) {
	if format.Codec != "flac" {
		return nil, fmt.Errorf("invalid codec for FLAC decoder: %s", format.Codec)
	}

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
	return nil, fmt.Errorf("FLAC streaming not yet implemented")
}

func (d *FLACDecoder) Close() error {
	return nil
}
