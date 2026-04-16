// ABOUTME: FLAC audio encoder for lossless streaming
// ABOUTME: Wraps mewkiz/flac to encode PCM int32 samples to FLAC frames
package server

import (
	"bytes"
	"fmt"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// FLACEncoder wraps a mewkiz/flac encoder for real-time FLAC frame
// generation. Each Encode call produces one FLAC frame from a block
// of interleaved int32 PCM samples. Prediction analysis is enabled
// so the encoder picks optimal Fixed/LPC prediction per block for
// real compression (not just verbatim framing).
type FLACEncoder struct {
	encoder     *flac.Encoder
	buf         *bytes.Buffer
	codecHeader []byte
	sampleRate  int
	channels    int
	bitDepth    int
	blockSize   int
}

// NewFLACEncoder creates a FLAC encoder with prediction analysis enabled.
// blockSize is samples per channel per frame (e.g., 960 for 20ms at 48kHz).
func NewFLACEncoder(sampleRate, channels, bitDepth, blockSize int) (*FLACEncoder, error) {
	buf := &bytes.Buffer{}

	info := &meta.StreamInfo{
		BlockSizeMin:  uint16(blockSize),
		BlockSizeMax:  uint16(blockSize),
		SampleRate:    uint32(sampleRate),
		NChannels:     uint8(channels),
		BitsPerSample: uint8(bitDepth),
	}

	enc, err := flac.NewEncoder(buf, info)
	if err != nil {
		return nil, fmt.Errorf("failed to create FLAC encoder: %w", err)
	}

	// Prediction analysis is enabled by default in mewkiz/flac v1.0.13.
	// Explicit call for clarity: WriteFrame will analyze subframes marked
	// PredVerbatim and pick the optimal prediction method (Constant/Fixed).
	enc.EnablePredictionAnalysis(true)

	// The encoder writes fLaC + STREAMINFO to the buffer immediately.
	codecHeader := make([]byte, buf.Len())
	copy(codecHeader, buf.Bytes())
	buf.Reset()

	return &FLACEncoder{
		encoder:     enc,
		buf:         buf,
		codecHeader: codecHeader,
		sampleRate:  sampleRate,
		channels:    channels,
		bitDepth:    bitDepth,
		blockSize:   blockSize,
	}, nil
}

// CodecHeader returns the FLAC codec header (fLaC + STREAMINFO metadata
// block). Base64-encode this for stream/start messages.
func (e *FLACEncoder) CodecHeader() []byte {
	return e.codecHeader
}

// Encode converts a block of interleaved int32 PCM samples into a FLAC
// frame. len(samples) must equal blockSize * channels.
func (e *FLACEncoder) Encode(samples []int32) ([]byte, error) {
	expected := e.blockSize * e.channels
	if len(samples) != expected {
		return nil, fmt.Errorf("expected %d samples (%d x %d), got %d",
			expected, e.blockSize, e.channels, len(samples))
	}

	// De-interleave into per-channel slices.
	subframes := make([]*frame.Subframe, e.channels)
	for ch := 0; ch < e.channels; ch++ {
		channelSamples := make([]int32, e.blockSize)
		for i := 0; i < e.blockSize; i++ {
			channelSamples[i] = samples[i*e.channels+ch]
		}
		subframes[ch] = &frame.Subframe{
			SubHeader: frame.SubHeader{
				// PredVerbatim signals the encoder to run prediction analysis
				// and pick the optimal method (Constant, Fixed, or Verbatim).
				Pred: frame.PredVerbatim,
			},
			Samples:  channelSamples,
			NSamples: e.blockSize,
		}
	}

	var channelAssign frame.Channels
	switch e.channels {
	case 1:
		channelAssign = frame.ChannelsMono
	case 2:
		channelAssign = frame.ChannelsLR
	default:
		channelAssign = frame.Channels(e.channels)
	}

	f := &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true,
			BlockSize:         uint16(e.blockSize),
			SampleRate:        uint32(e.sampleRate),
			Channels:          channelAssign,
			BitsPerSample:     uint8(e.bitDepth),
		},
		Subframes: subframes,
	}

	e.buf.Reset()
	if err := e.encoder.WriteFrame(f); err != nil {
		return nil, fmt.Errorf("FLAC encode failed: %w", err)
	}

	result := make([]byte, e.buf.Len())
	copy(result, e.buf.Bytes())
	return result, nil
}

// Close finalizes the encoder.
func (e *FLACEncoder) Close() error {
	return e.encoder.Close()
}
