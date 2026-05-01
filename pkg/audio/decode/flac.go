// ABOUTME: FLAC streaming decoder using io.Pipe + mewkiz/flac
// ABOUTME: Decodes FLAC frames to int32 samples for the playback pipeline
package decode

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/mewkiz/flac"
)

// FLACDecoder decodes streaming FLAC frames to int32 PCM samples. It
// bridges chunk-by-chunk network delivery with mewkiz/flac's io.Reader
// API using an io.Pipe. A background goroutine reads FLAC frames from
// the pipe and pushes decoded samples to an internal channel.
type FLACDecoder struct {
	format     audio.Format
	pipeWriter *io.PipeWriter
	sampleCh   chan []int32
	errCh      chan error
	closed     bool
	mu         sync.Mutex
}

func NewFLAC(format audio.Format) (Decoder, error) {
	if format.Codec != "flac" {
		return nil, fmt.Errorf("invalid codec for FLAC decoder: %s", format.Codec)
	}
	if len(format.CodecHeader) == 0 {
		return nil, fmt.Errorf("FLAC decoder requires CodecHeader (STREAMINFO)")
	}

	pr, pw := io.Pipe()

	d := &FLACDecoder{
		format:     format,
		pipeWriter: pw,
		sampleCh:   make(chan []int32, 16),
		errCh:      make(chan error, 1),
	}

	go d.runDecoder(pr, format.CodecHeader)

	return d, nil
}

// runDecoder writes the codec header, initializes the FLAC stream, and
// loops calling ParseNext to decode frames. Runs in a background
// goroutine for the lifetime of the decoder.
func (d *FLACDecoder) runDecoder(pr *io.PipeReader, codecHeader []byte) {
	defer close(d.sampleCh)
	defer pr.Close()

	// Create a reader that starts with the codec_header, then reads
	// from the pipe for the frame data.
	combined := io.MultiReader(bytes.NewReader(codecHeader), pr)

	stream, err := flac.New(combined)
	if err != nil {
		select {
		case d.errCh <- fmt.Errorf("flac.New: %w", err):
		default:
		}
		return
	}

	channels := int(stream.Info.NChannels)
	bitDepth := int(stream.Info.BitsPerSample)

	for {
		frame, err := stream.ParseNext()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
			// Pipe closed = normal shutdown
			if err.Error() == "io: read/write on closed pipe" {
				return
			}
			log.Printf("FLAC ParseNext error: %v", err)
			return
		}

		blockSize := int(frame.BlockSize)
		samples := make([]int32, blockSize*channels)
		idx := 0

		for i := 0; i < blockSize; i++ {
			for ch := 0; ch < channels; ch++ {
				sample := frame.Subframes[ch].Samples[i]

				// Convert to 24-bit int32 range (same logic as FLACSource
				// in internal/server/audio_source.go)
				var converted int32
				if bitDepth == 16 {
					converted = sample << 8
				} else if bitDepth == 24 {
					converted = sample
				} else {
					shift := bitDepth - 24
					if shift > 0 {
						converted = sample >> shift
					} else {
						converted = sample << -shift
					}
				}
				samples[idx] = converted
				idx++
			}
		}

		d.sampleCh <- samples
	}
}

func (d *FLACDecoder) Decode(data []byte) ([]int32, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, fmt.Errorf("decoder closed")
	}
	d.mu.Unlock()

	// Check for initialization errors from the background goroutine.
	select {
	case err := <-d.errCh:
		return nil, err
	default:
	}

	// Write the chunk data to the pipe. This feeds the background
	// goroutine's ParseNext loop.
	_, err := d.pipeWriter.Write(data)
	if err != nil {
		return nil, fmt.Errorf("write to FLAC pipe: %w", err)
	}

	// Return at most one frame per call. The server guarantees 1 chunk
	// = 1 FLAC frame (encoder block size matches ChunkDurationMs), so
	// each Decode call should yield exactly one frame's samples.
	//
	// Draining all queued frames into one return value would tag every
	// frame with the current chunk's timestamp — collapsing per-frame
	// timing into a single PlayAt, which both breaks multi-room sync
	// and hands the playback ring buffer multiples of its capacity in
	// one Write (see issue: "ring buffer full, dropped N samples").
	//
	// If the parsing goroutine has raced ahead and queued more than
	// one frame, the extras stay buffered on sampleCh and surface on
	// subsequent Decode calls (one per call). The frame may also span
	// multiple chunks; in that case the goroutine has not produced
	// anything yet and we return (nil, nil) — the receiver skips the
	// chunk and the frame surfaces on a later Decode.
	select {
	case samples, ok := <-d.sampleCh:
		if !ok {
			return nil, io.EOF
		}
		return samples, nil
	default:
		return nil, nil
	}
}

func (d *FLACDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true

	d.pipeWriter.Close()

	// Drain remaining samples so the goroutine can exit.
	for range d.sampleCh {
	}

	return nil
}
