// ABOUTME: Wire and audio-pipeline constants shared by the client and server
// ABOUTME: Kept in a neutral file so neither half depends on the other's source
package sendspin

const (
	// ProtocolVersion is the Sendspin protocol version this library implements.
	ProtocolVersion = 1

	// AudioChunkMessageType is the binary message-type ID for audio chunks.
	// Binary message type IDs per spec (bits 7-2 for role, bits 1-0 for slot);
	// player role is 000001xx (4-7), slot 0 = 4.
	AudioChunkMessageType = 4

	// Default audio parameters for built-in sources.
	DefaultSampleRate = 192000
	DefaultChannels   = 2
	DefaultBitDepth   = 24

	// ChunkDurationMs is the audio chunk duration in milliseconds (50 chunks/s).
	// Interop-critical invariant shared by the server encoder and the client
	// scheduler — do not change without re-running the conformance suite.
	ChunkDurationMs = 20

	// BufferAheadMs is how far ahead of playback the server sends audio.
	BufferAheadMs = 500
)
