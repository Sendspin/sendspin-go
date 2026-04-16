// ABOUTME: Audio streaming orchestration for Server
// ABOUTME: Tick-driven chunk generation, codec negotiation, per-client encode/send
package sendspin

import (
	"encoding/binary"
	"log"
	"time"

	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

func (s *Server) streamAudio() {
	log.Printf("Audio streaming started")

	ticker := time.NewTicker(time.Duration(ChunkDurationMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.generateAndSendChunk()
		case <-s.stopChan:
			log.Printf("Audio streaming stopping")
			return
		}
	}
}

func (s *Server) generateAndSendChunk() {
	currentTime := s.getClockMicros()
	playbackTime := currentTime + (BufferAheadMs * 1000)

	chunkSamples := (s.audioSource.SampleRate() * ChunkDurationMs) / 1000
	totalSamples := chunkSamples * s.audioSource.Channels()

	samples := make([]int32, totalSamples)
	n, err := s.audioSource.Read(samples)
	if err != nil {
		s.consecutiveReadErrs++
		// Log every error for the first few, then throttle
		if s.consecutiveReadErrs <= 3 || s.consecutiveReadErrs%50 == 0 {
			log.Printf("Error reading audio source (%d consecutive): %v", s.consecutiveReadErrs, err)
		}
		// After 1 second of failures (50 ticks at 20ms), notify clients
		if s.consecutiveReadErrs == 50 {
			log.Printf("Audio source failed for 1s, sending stream/end to all clients")
			s.notifyStreamEnd()
		}
		return
	}
	s.consecutiveReadErrs = 0

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		var audioData []byte
		var encodeErr error

		c.mu.RLock()
		codec := c.codec
		opusEncoder := c.opusEncoder
		resampler := c.resampler
		c.mu.RUnlock()

		switch codec {
		case "opus":
			if opusEncoder != nil {
				samplesToEncode := samples[:n]

				// Resample when source rate != 48kHz (Opus is locked to 48kHz)
				if resampler != nil {
					outputSamples := resampler.OutputSamplesNeeded(len(samplesToEncode))
					resampled := make([]int32, outputSamples)
					samplesWritten := resampler.Resample(samplesToEncode, resampled)
					samplesToEncode = resampled[:samplesWritten]
				}

				samples16 := convertToInt16(samplesToEncode)
				audioData, encodeErr = opusEncoder.Encode(samples16)
				if encodeErr != nil {
					log.Printf("Opus encode error for %s: %v", c.name, encodeErr)
					continue
				}
			} else {
				continue
			}
		case "pcm":
			audioData = encodePCM(samples[:n])
		default:
			audioData = encodePCM(samples[:n])
		}

		chunk := createAudioChunk(playbackTime, audioData)

		if err := c.SendBinary(chunk); err != nil {
			if s.config.Debug {
				log.Printf("Error sending audio to %s: %v", c.name, err)
			}
		}
	}
}

func (s *Server) addClientToStream(c *ServerClient) {
	codec := s.negotiateCodec(c)

	var opusEncoder *server.OpusEncoder
	var resampler *audio.Resampler
	sourceRate := s.audioSource.SampleRate()

	switch codec {
	case "opus":
		// Opus requires 48kHz — create resampler if source rate differs
		if sourceRate != 48000 {
			resampler = audio.NewResampler(sourceRate, 48000, s.audioSource.Channels())
			log.Printf("Created resampler: %dHz -> 48kHz for Opus (client: %s)", sourceRate, c.name)
		}

		opusChunkSamples := (48000 * ChunkDurationMs) / 1000
		encoder, err := server.NewOpusEncoder(48000, s.audioSource.Channels(), opusChunkSamples)
		if err != nil {
			log.Printf("Failed to create Opus encoder for %s, falling back to PCM: %v", c.name, err)
			codec = "pcm"
			resampler = nil
		} else {
			opusEncoder = encoder
		}
	case "flac":
		log.Printf("FLAC streaming not supported for %s, using PCM", c.name)
		codec = "pcm"
	}

	c.mu.Lock()
	c.codec = codec
	c.opusEncoder = opusEncoder
	c.resampler = resampler
	c.mu.Unlock()

	log.Printf("Added client %s with codec %s", c.name, codec)

	// For Opus, report 48kHz to the client since that's what it will decode
	// (the resampler runs server-side before encoding).
	streamSampleRate := s.audioSource.SampleRate()
	streamBitDepth := DefaultBitDepth
	if codec == "opus" {
		streamSampleRate = 48000
		streamBitDepth = 16
	}

	streamStart := protocol.StreamStart{
		Player: &protocol.StreamStartPlayer{
			Codec:      codec,
			SampleRate: streamSampleRate,
			Channels:   s.audioSource.Channels(),
			BitDepth:   streamBitDepth,
		},
	}

	if err := c.Send("stream/start", streamStart); err != nil {
		log.Printf("Error sending stream/start to %s: %v", c.name, err)
	}

	// server/state carries the initial metadata snapshot per spec.
	title, artist, album := s.audioSource.Metadata()
	serverState := protocol.ServerStateMessage{
		Metadata: &protocol.MetadataState{
			Timestamp: s.getClockMicros(),
			Title:     strPtr(title),
			Artist:    strPtr(artist),
			Album:     strPtr(album),
		},
	}

	if err := c.Send("server/state", serverState); err != nil {
		log.Printf("Error sending server/state to %s: %v", c.name, err)
	}

	// group/update is required by spec even when we host a single implicit group.
	groupID := s.serverID
	playbackState := "playing"
	groupUpdate := protocol.GroupUpdate{
		GroupID:       &groupID,
		PlaybackState: &playbackState,
	}

	if err := c.Send("group/update", groupUpdate); err != nil {
		log.Printf("Error sending group/update to %s: %v", c.name, err)
	}
}

func (s *Server) notifyStreamEnd() {
	streamEnd := protocol.StreamEnd{
		Roles: []string{"player"},
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		if c.HasRole("player") {
			if err := c.Send("stream/end", streamEnd); err != nil {
				log.Printf("Error sending stream/end to %s: %v", c.name, err)
			}
		}
	}
}

// negotiateCodec picks PCM at the source's native rate when advertised
// (lossless hi-res), then falls through to Opus for bandwidth savings, then
// PCM as a last resort.
func (s *Server) negotiateCodec(c *ServerClient) string {
	if c.capabilities == nil {
		return "pcm"
	}

	sourceRate := s.audioSource.SampleRate()

	for _, format := range c.capabilities.SupportedFormats {
		if format.Codec == "pcm" && format.SampleRate == sourceRate && format.BitDepth == DefaultBitDepth {
			return "pcm"
		}
	}

	for _, format := range c.capabilities.SupportedFormats {
		if format.Codec == "opus" {
			return "opus"
		}
	}

	return "pcm"
}

func strPtr(s string) *string {
	return &s
}

// createAudioChunk packs timestamp + payload into a Sendspin binary frame:
// [1 byte message type][8 byte big-endian timestamp (µs)][audio bytes].
func createAudioChunk(timestamp int64, audioData []byte) []byte {
	chunk := make([]byte, 1+8+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:9], uint64(timestamp))
	copy(chunk[9:], audioData)
	return chunk
}

// convertToInt16 converts int32 samples to int16 (for Opus encoding)
func convertToInt16(samples []int32) []int16 {
	result := make([]int16, len(samples))
	for i, s := range samples {
		result[i] = int16(s >> 8)
	}
	return result
}

// encodePCM encodes int32 samples as 24-bit PCM bytes
func encodePCM(samples []int32) []byte {
	output := make([]byte, len(samples)*3)
	for i, sample := range samples {
		output[i*3] = byte(sample)
		output[i*3+1] = byte(sample >> 8)
		output[i*3+2] = byte(sample >> 16)
	}
	return output
}
