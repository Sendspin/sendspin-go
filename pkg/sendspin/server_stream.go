// ABOUTME: Audio streaming orchestration for Server
// ABOUTME: Tick-driven chunk generation, codec negotiation, per-client encode/send
package sendspin

import (
	"encoding/binary"
	"log"
	"time"

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
		flacEncoder := c.flacEncoder
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
		case "flac":
			if flacEncoder != nil {
				audioData, encodeErr = flacEncoder.Encode(samples[:n])
				if encodeErr != nil {
					log.Printf("FLAC encode error for %s: %v", c.name, encodeErr)
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

		chunk := CreateAudioChunk(playbackTime, audioData)

		if err := c.SendBinary(chunk); err != nil {
			if s.config.Debug {
				log.Printf("Error sending audio to %s: %v", c.name, err)
			}
		}
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

// negotiateCodec picks the best codec by scanning the client's advertised
// formats in order. The client controls preference (via --preferred-codec);
// the server accepts the first codec it can handle.
//
// Supported: pcm (at source rate), flac, opus. Falls back to pcm.
func negotiateCodec(c *ServerClient, sourceSampleRate int) string {
	if c.capabilities == nil {
		return "pcm"
	}

	for _, format := range c.capabilities.SupportedFormats {
		switch format.Codec {
		case "pcm":
			if format.SampleRate == sourceSampleRate && format.BitDepth == DefaultBitDepth {
				return "pcm"
			}
		case "flac":
			return "flac"
		case "opus":
			return "opus"
		}
	}

	return "pcm"
}

func strPtr(s string) *string {
	return &s
}

// CreateAudioChunk packs timestamp + payload into a Sendspin binary frame:
// [1 byte message type][8 byte big-endian timestamp (µs)][audio bytes].
func CreateAudioChunk(timestamp int64, audioData []byte) []byte {
	chunk := make([]byte, 1+8+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:9], uint64(timestamp))
	copy(chunk[9:], audioData)
	return chunk
}

// CreateArtworkChunk packs an artwork frame: [1 byte message type][8 byte timestamp (us)][image bytes].
// Channel is 0-3, mapping to the artwork channel message types.
func CreateArtworkChunk(channel int, timestamp int64, imageData []byte) []byte {
	chunk := make([]byte, protocol.BinaryMessageHeaderSize+len(imageData))
	chunk[0] = byte(protocol.ArtworkChannel0MessageType + channel)
	binary.BigEndian.PutUint64(chunk[1:protocol.BinaryMessageHeaderSize], uint64(timestamp))
	copy(chunk[protocol.BinaryMessageHeaderSize:], imageData)
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
