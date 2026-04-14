// ABOUTME: Audio streaming engine for Sendspin server
// ABOUTME: Generates test tones and streams timestamped audio to clients
package server

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

const (
	// Audio format constants - Hi-Res Audio (192kHz/24-bit)
	DefaultSampleRate = 192000
	DefaultChannels   = 2
	DefaultBitDepth   = 24

	// Chunk timing
	ChunkDurationMs = 20 // 20ms chunks

	// Buffering
	BufferAheadMs     = 500  // Send audio 500ms ahead for PCM
	BufferAheadOpusMs = 1000 // Send audio 1000ms ahead for Opus (more processing overhead)
)

// AudioEngine manages audio generation and streaming
type AudioEngine struct {
	server *Server

	// Active clients
	clients   map[string]*Client
	clientsMu sync.RWMutex

	// Audio source (file or test tone)
	source AudioSource

	sampleBuf []int32 // pre-allocated sample buffer reused each chunk

	stopChan chan struct{}
	stopOnce sync.Once // Ensure Stop() is only called once
}

func NewAudioEngine(server *Server) (*AudioEngine, error) {
	source, err := NewAudioSource(server.config.AudioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create audio source: %w", err)
	}

	// Pre-allocate sample buffer for the 20ms chunk generation loop
	chunkSamples := (source.SampleRate() * ChunkDurationMs) / 1000
	totalSamples := chunkSamples * source.Channels()

	return &AudioEngine{
		server:    server,
		clients:   make(map[string]*Client),
		source:    source,
		sampleBuf: make([]int32, totalSamples),
		stopChan:  make(chan struct{}),
	}, nil
}

func (e *AudioEngine) Start() {
	log.Printf("Audio engine starting")

	ticker := time.NewTicker(time.Duration(ChunkDurationMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.generateAndSendChunk()
		case <-e.stopChan:
			log.Printf("Audio engine stopping")
			return
		}
	}
}

func (e *AudioEngine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopChan)
		if e.source != nil {
			if err := e.source.Close(); err != nil {
				log.Printf("Error closing audio source: %v", err)
			}
		}
	})
}

func (e *AudioEngine) AddClient(client *Client) {
	e.clientsMu.Lock()
	defer e.clientsMu.Unlock()

	codec := e.negotiateCodec(client)

	var opusEncoder *OpusEncoder
	var resampler *Resampler
	sourceRate := e.source.SampleRate()

	switch codec {
	case "opus":
		// Opus requires 48kHz - create resampler if source rate is different
		if sourceRate != 48000 {
			resampler = NewResampler(sourceRate, 48000, e.source.Channels())
			log.Printf("Created resampler: %dHz → 48kHz for Opus (client: %s)", sourceRate, client.Name)
		}

		// Create Opus encoder (always at 48kHz)
		opusChunkSamples := (48000 * ChunkDurationMs) / 1000
		encoder, err := NewOpusEncoder(48000, e.source.Channels(), opusChunkSamples)
		if err != nil {
			log.Printf("Failed to create Opus encoder for %s, falling back to PCM: %v", client.Name, err)
			codec = "pcm"
			resampler = nil // Clear resampler if Opus encoder failed
		} else {
			opusEncoder = encoder
		}
	case "flac":
		// FLAC is a file format, not a streaming codec
		// It requires headers at the start and can't be split into independent chunks
		// Fall back to PCM for lossless streaming
		log.Printf("FLAC streaming not supported for %s, using PCM for lossless audio", client.Name)
		codec = "pcm"
	}

	client.mu.Lock()
	client.Codec = codec
	client.OpusEncoder = opusEncoder
	client.Resampler = resampler
	client.mu.Unlock()

	e.clients[client.ID] = client

	log.Printf("Audio engine: added client %s with codec %s (format: %dHz/%dbit/%dch)",
		client.Name, codec, e.source.SampleRate(), DefaultBitDepth, e.source.Channels())

	streamStart := protocol.StreamStart{
		Player: &protocol.StreamStartPlayer{
			Codec:      codec,
			SampleRate: e.source.SampleRate(),
			Channels:   e.source.Channels(),
			BitDepth:   DefaultBitDepth,
		},
	}

	msg := protocol.Message{
		Type:    "stream/start",
		Payload: streamStart,
	}

	select {
	case client.sendChan <- msg:
	default:
		log.Printf("Warning: Could not send stream/start to %s (channel full)", client.Name)
	}

	title, artist, album := e.source.Metadata()
	metadata := protocol.StreamMetadata{
		Title:  title,
		Artist: artist,
		Album:  album,
	}

	metaMsg := protocol.Message{
		Type:    "stream/metadata",
		Payload: metadata,
	}

	select {
	case client.sendChan <- metaMsg:
	default:
		log.Printf("Warning: Could not send metadata to %s (channel full)", client.Name)
	}
}

func (e *AudioEngine) RemoveClient(client *Client) {
	e.clientsMu.Lock()
	defer e.clientsMu.Unlock()

	client.mu.Lock()
	if client.OpusEncoder != nil {
		client.OpusEncoder.Close()
		client.OpusEncoder = nil
	}
	if client.Resampler != nil {
		client.Resampler = nil
	}
	client.mu.Unlock()

	delete(e.clients, client.ID)
	log.Printf("Audio engine: removed client %s", client.Name)
}

// negotiateCodec selects the best codec based on client capabilities.
// With resampling support, we prefer PCM for native-rate hi-res clients and
// Opus (with resampling) for everything else to save bandwidth.
func (e *AudioEngine) negotiateCodec(client *Client) string {
	if client.Capabilities == nil {
		return "pcm"
	}

	sourceRate := e.source.SampleRate()

	// Strategy:
	// 1. If client supports PCM at native rate → use PCM (lossless hi-res)
	// 2. If client supports Opus → use Opus with resampling (bandwidth efficient)
	// 3. Otherwise → fall back to PCM

	for _, format := range client.Capabilities.SupportedFormats {
		if format.Codec == "pcm" && format.SampleRate == sourceRate && format.BitDepth == DefaultBitDepth {
			return "pcm"
		}
	}

	for _, format := range client.Capabilities.SupportedFormats {
		if format.Codec == "opus" {
			return "opus"
		}
	}

	return "pcm"
}

func (e *AudioEngine) generateAndSendChunk() {
	currentTime := e.server.getClockMicros()

	samples := e.sampleBuf
	n, err := e.source.Read(samples)
	if err != nil {
		log.Printf("Error reading audio source: %v", err)
		return
	}

	e.clientsMu.RLock()
	defer e.clientsMu.RUnlock()

	for _, client := range e.clients {
		var audioData []byte
		var encodeErr error

		client.mu.RLock()
		codec := client.Codec
		opusEncoder := client.OpusEncoder
		resampler := client.Resampler
		client.mu.RUnlock()

		switch codec {
		case "opus":
			if opusEncoder != nil {
				samplesToEncode := samples[:n]

				if resampler != nil {
					outputSamples := resampler.OutputSamplesNeeded(len(samplesToEncode))
					resampled := make([]int32, outputSamples)
					samplesWritten := resampler.Resample(samplesToEncode, resampled)
					samplesToEncode = resampled[:samplesWritten]
				}

				// Convert int32 to int16 for Opus (Opus only supports 16-bit)
				samples16 := convertToInt16(samplesToEncode)
				audioData, encodeErr = opusEncoder.Encode(samples16)
				if encodeErr != nil {
					log.Printf("Opus encode error for %s: %v", client.Name, encodeErr)
					continue
				}
			} else {
				log.Printf("Warning: Client %s has opus codec but no encoder", client.Name)
				continue
			}
		case "pcm":
			audioData = encodePCM(samples[:n])
		default:
			// Unknown codec, fall back to PCM
			log.Printf("Warning: Unknown codec %s for client %s, using PCM", codec, client.Name)
			audioData = encodePCM(samples[:n])
		}

		bufferAhead := BufferAheadMs
		if codec == "opus" {
			bufferAhead = BufferAheadOpusMs
		}
		playbackTime := currentTime + (int64(bufferAhead) * 1000)

		chunk := CreateAudioChunk(playbackTime, audioData)

		if err := e.server.sendBinary(client, chunk); err != nil {
			log.Printf("Error sending audio to %s: %v", client.Name, err)
		}
	}
}

// convertToInt16 converts int32 samples to int16 by dropping the lowest byte
// (24-bit → 16-bit range shift required by the Opus encoder).
func convertToInt16(samples []int32) []int16 {
	result := make([]int16, len(samples))
	for i, s := range samples {
		result[i] = int16(s >> 8)
	}
	return result
}

// encodePCM packs int32 samples as 24-bit little-endian PCM (3 bytes per sample).
func encodePCM(samples []int32) []byte {
	output := make([]byte, len(samples)*3)
	for i, sample := range samples {
		output[i*3] = byte(sample)
		output[i*3+1] = byte(sample >> 8)
		output[i*3+2] = byte(sample >> 16)
	}
	return output
}
