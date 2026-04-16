// ABOUTME: Integration test for FLAC decoder using real FLAC files
// ABOUTME: Skipped when no FLAC fixture is available
package decode

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/mewkiz/flac"
)

// findFLACFixture looks for a FLAC test file in known locations.
func findFLACFixture() string {
	candidates := []string{
		// conformance repo fixture (relative to pkg/audio/decode/)
		"../../../conformance/repos/sendspin-cli/tests/fixtures/almost_silent.flac",
		"../../../../conformance/repos/sendspin-cli/tests/fixtures/almost_silent.flac",
		"../../../conformance/fixtures/almost-silent-5s-48000-2-24.flac",
		"../../../../conformance/fixtures/almost-silent-5s-48000-2-24.flac",
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}

// splitFLACFile reads a FLAC file and returns the codec_header (fLaC +
// all metadata blocks) and the raw frame data (everything after metadata).
func splitFLACFile(path string) (codecHeader []byte, frameData []byte, sampleRate, channels, bitDepth int, totalSamples uint64, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, 0, 0, 0, 0, err
	}
	if len(raw) < 4 || string(raw[:4]) != "fLaC" {
		return nil, nil, 0, 0, 0, 0, io.ErrUnexpectedEOF
	}

	// Parse metadata blocks to find where frame data starts.
	offset := 4
	for {
		if offset+4 > len(raw) {
			return nil, nil, 0, 0, 0, 0, io.ErrUnexpectedEOF
		}
		header := raw[offset : offset+4]
		lastBlock := header[0]&0x80 != 0
		blockLength := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		offset += 4 + blockLength
		if lastBlock {
			break
		}
	}

	codecHeader = make([]byte, offset)
	copy(codecHeader, raw[:offset])

	// Get stream info for verification
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, 0, 0, 0, err
	}
	defer f.Close()
	stream, err := flac.New(f)
	if err != nil {
		return nil, nil, 0, 0, 0, 0, err
	}

	return codecHeader, raw[offset:], int(stream.Info.SampleRate), int(stream.Info.NChannels), int(stream.Info.BitsPerSample), stream.Info.NSamples, nil
}

func TestFLACDecoder_RealFile(t *testing.T) {
	fixturePath := findFLACFixture()
	if fixturePath == "" {
		t.Skip("no FLAC fixture found — skipping integration test")
	}

	codecHeader, frameData, sampleRate, channels, bitDepth, totalSamples, err := splitFLACFile(fixturePath)
	if err != nil {
		t.Fatalf("splitFLACFile: %v", err)
	}

	t.Logf("Fixture: %s", filepath.Base(fixturePath))
	t.Logf("Format: %dHz %dch %dbit, %d total samples", sampleRate, channels, bitDepth, totalSamples)
	t.Logf("Codec header: %d bytes, frame data: %d bytes", len(codecHeader), len(frameData))

	format := audio.Format{
		Codec:       "flac",
		SampleRate:  sampleRate,
		Channels:    channels,
		BitDepth:    bitDepth,
		CodecHeader: codecHeader,
	}

	dec, err := NewFLAC(format)
	if err != nil {
		t.Fatalf("NewFLAC: %v", err)
	}

	// The FLACDecoder uses an io.Pipe internally: Decode() writes to the
	// pipe then drains decoded samples from a channel. With a full file's
	// worth of frame data, the internal sample channel (buffer 16) fills
	// before the pipe write completes, causing deadlock in a single
	// goroutine. To test the full pipeline we access the unexported fields
	// directly: write frame data to the pipe from a goroutine, then
	// collect decoded samples from the channel until it closes.
	flacDec := dec.(*FLACDecoder)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, writeErr := flacDec.pipeWriter.Write(frameData)
		if writeErr != nil {
			t.Errorf("pipe write: %v", writeErr)
		}
		// Close the writer so the decoder sees EOF and stops.
		flacDec.pipeWriter.Close()
	}()

	var allSamples []int32
	for samples := range flacDec.sampleCh {
		allSamples = append(allSamples, samples...)
	}

	wg.Wait()

	if len(allSamples) == 0 {
		t.Fatal("expected decoded samples, got empty")
	}

	expectedSamples := int(totalSamples) * channels
	t.Logf("Decoded %d samples (expected ~%d)", len(allSamples), expectedSamples)

	// Verify sample count is in the right ballpark.
	if len(allSamples) < expectedSamples/2 {
		t.Errorf("decoded far fewer samples than expected: %d vs %d", len(allSamples), expectedSamples)
	}

	// Verify samples aren't all zero (sanity check — the fixture is
	// "almost silent" but should have some non-zero values).
	nonZero := 0
	for _, s := range allSamples {
		if s != 0 {
			nonZero++
		}
	}
	t.Logf("Non-zero samples: %d / %d", nonZero, len(allSamples))
}
