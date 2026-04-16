// ABOUTME: PlayerGroupRole handles player stream setup on client join
// ABOUTME: Codec negotiation, encoder creation, and stream/start message
package sendspin

import (
	"encoding/base64"
	"log"

	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// PlayerRoleConfig configures the PlayerGroupRole.
type PlayerRoleConfig struct {
	SampleRate int
	Channels   int
	BitDepth   int

	// NewEncoder creates an Opus encoder when needed. When nil, Opus
	// clients fall back to PCM. Typically set to server.NewOpusEncoder.
	NewEncoder func(sampleRate, channels, chunkSamples int) (*server.OpusEncoder, error)

	// NewFLACEncoder creates a FLAC encoder when needed. When nil, FLAC
	// clients fall back to PCM.
	NewFLACEncoder func(sampleRate, channels, bitDepth, blockSize int) (*server.FLACEncoder, error)
}

// PlayerGroupRole handles the "player" role family. On client join it
// negotiates a codec, creates any needed encoder/resampler, and sends
// stream/start. The audio chunk pipeline (generateAndSendChunk) stays
// on Server — this role only handles the per-client setup.
type PlayerGroupRole struct {
	config PlayerRoleConfig
}

// NewPlayerRole creates a PlayerGroupRole with the given config.
func NewPlayerRole(config PlayerRoleConfig) *PlayerGroupRole {
	return &PlayerGroupRole{config: config}
}

func (r *PlayerGroupRole) Role() string { return "player" }

// OnClientJoin negotiates a codec, creates encoder/resampler if needed,
// and sends stream/start to clients advertising the player role.
func (r *PlayerGroupRole) OnClientJoin(c *ServerClient) {
	if !c.HasRole("player") {
		return
	}

	codec := negotiateCodec(c, r.config.SampleRate)

	var opusEncoder *server.OpusEncoder
	var flacEncoder *server.FLACEncoder
	var resampler *audio.Resampler

	switch codec {
	case "opus":
		if r.config.SampleRate != 48000 {
			resampler = audio.NewResampler(r.config.SampleRate, 48000, r.config.Channels)
			log.Printf("Created resampler: %dHz -> 48kHz for Opus (client: %s)", r.config.SampleRate, c.Name())
		}

		opusChunkSamples := (48000 * ChunkDurationMs) / 1000
		if r.config.NewEncoder != nil {
			encoder, err := r.config.NewEncoder(48000, r.config.Channels, opusChunkSamples)
			if err != nil {
				log.Printf("Failed to create Opus encoder for %s, falling back to PCM: %v", c.Name(), err)
				codec = "pcm"
				resampler = nil
			} else {
				opusEncoder = encoder
			}
		} else {
			log.Printf("No Opus encoder factory for %s, falling back to PCM", c.Name())
			codec = "pcm"
			resampler = nil
		}
	case "flac":
		chunkSamples := (r.config.SampleRate * ChunkDurationMs) / 1000
		if r.config.NewFLACEncoder != nil {
			encoder, err := r.config.NewFLACEncoder(r.config.SampleRate, r.config.Channels, r.config.BitDepth, chunkSamples)
			if err != nil {
				log.Printf("Failed to create FLAC encoder for %s, falling back to PCM: %v", c.Name(), err)
				codec = "pcm"
			} else {
				flacEncoder = encoder
			}
		} else {
			log.Printf("No FLAC encoder factory for %s, falling back to PCM", c.Name())
			codec = "pcm"
		}
	}

	c.mu.Lock()
	c.codec = codec
	c.opusEncoder = opusEncoder
	c.flacEncoder = flacEncoder
	c.resampler = resampler
	c.mu.Unlock()

	log.Printf("Added client %s with codec %s", c.Name(), codec)

	streamSampleRate := r.config.SampleRate
	streamBitDepth := r.config.BitDepth
	if codec == "opus" {
		streamSampleRate = 48000
		streamBitDepth = 16
	}

	var codecHeaderB64 string
	if codec == "flac" && flacEncoder != nil {
		codecHeaderB64 = base64.StdEncoding.EncodeToString(flacEncoder.CodecHeader())
	}

	streamStart := protocol.StreamStart{
		Player: &protocol.StreamStartPlayer{
			Codec:       codec,
			SampleRate:  streamSampleRate,
			Channels:    r.config.Channels,
			BitDepth:    streamBitDepth,
			CodecHeader: codecHeaderB64,
		},
	}

	if err := c.Send("stream/start", streamStart); err != nil {
		log.Printf("Error sending stream/start to %s: %v", c.Name(), err)
	}
}

func (r *PlayerGroupRole) OnClientLeave(id string, name string) {}
