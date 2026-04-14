// ABOUTME: Regression tests for AudioEngine codec negotiation and resampler setup
// ABOUTME: Covers hi-res source + Opus client path (github issue #2)
package server

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// fakeAudioSource is a minimal AudioSource used to drive AudioEngine under test.
type fakeAudioSource struct {
	sampleRate int
	channels   int
}

func (f *fakeAudioSource) Read(samples []int32) (int, error)  { return len(samples), nil }
func (f *fakeAudioSource) SampleRate() int                    { return f.sampleRate }
func (f *fakeAudioSource) Channels() int                      { return f.channels }
func (f *fakeAudioSource) Metadata() (string, string, string) { return "", "", "" }
func (f *fakeAudioSource) Close() error                       { return nil }

func newTestEngine(sampleRate, channels int) *AudioEngine {
	return &AudioEngine{
		clients:  make(map[string]*Client),
		source:   &fakeAudioSource{sampleRate: sampleRate, channels: channels},
		stopChan: make(chan struct{}),
	}
}

func newTestClient(id string, formats []protocol.AudioFormat) *Client {
	return &Client{
		ID:   id,
		Name: id,
		Capabilities: &protocol.PlayerV1Support{
			SupportedFormats: formats,
		},
		sendChan: make(chan interface{}, 4),
		done:     make(chan struct{}),
	}
}

// TestAddClient_HiResSource_OpusClient is a regression test for issue #2:
// a 192kHz source with an Opus-capable client must negotiate Opus and attach
// a resampler that converts 192kHz -> 48kHz. Previously the server fell back
// to PCM in this case, causing ~36x bandwidth usage.
func TestAddClient_HiResSource_OpusClient(t *testing.T) {
	engine := newTestEngine(192000, 2)
	client := newTestClient("opus-client", []protocol.AudioFormat{
		{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
	})

	engine.AddClient(client)
	defer engine.RemoveClient(client)

	if client.Codec != "opus" {
		t.Fatalf("expected codec %q, got %q", "opus", client.Codec)
	}
	if client.OpusEncoder == nil {
		t.Fatal("expected OpusEncoder to be attached for 192kHz source + opus client")
	}
	if client.Resampler == nil {
		t.Fatal("expected Resampler to be attached when source rate != 48kHz")
	}
}

// TestAddClient_HiResSource_PCMNativeClient verifies the lossless hi-res
// path: a PCM client advertising the exact source rate should receive PCM
// with no resampler, taking precedence over Opus bandwidth savings.
func TestAddClient_HiResSource_PCMNativeClient(t *testing.T) {
	engine := newTestEngine(192000, 2)
	client := newTestClient("pcm-client", []protocol.AudioFormat{
		{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: DefaultBitDepth},
		{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
	})

	engine.AddClient(client)
	defer engine.RemoveClient(client)

	if client.Codec != "pcm" {
		t.Fatalf("expected codec %q, got %q", "pcm", client.Codec)
	}
	if client.OpusEncoder != nil {
		t.Fatal("expected no OpusEncoder for native-rate PCM client")
	}
	if client.Resampler != nil {
		t.Fatal("expected no Resampler for native-rate PCM client")
	}
}

// TestAddClient_NativeOpusSource verifies the no-resample Opus path: when
// the source is already 48kHz, an Opus client should get an encoder but
// no resampler.
func TestAddClient_NativeOpusSource(t *testing.T) {
	engine := newTestEngine(48000, 2)
	client := newTestClient("opus-native", []protocol.AudioFormat{
		{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
	})

	engine.AddClient(client)
	defer engine.RemoveClient(client)

	if client.Codec != "opus" {
		t.Fatalf("expected codec %q, got %q", "opus", client.Codec)
	}
	if client.OpusEncoder == nil {
		t.Fatal("expected OpusEncoder to be attached")
	}
	if client.Resampler != nil {
		t.Fatal("expected no Resampler when source rate == 48kHz")
	}
}
