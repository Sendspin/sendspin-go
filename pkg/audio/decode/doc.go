// ABOUTME: Audio decoder package for multiple codec support
// ABOUTME: Provides Decoder interface and implementations for PCM, Opus, FLAC
// Package decode provides audio decoders for various codecs.
//
// Supports: PCM (16-bit and 24-bit), Opus, FLAC (stub)
//
// All decoders implement the Decoder interface and output int32 samples
// in 24-bit range for consistent hi-res audio processing.
//
// Example:
//
//	decoder, err := decode.NewPCM(format)
//	samples, err := decoder.Decode(audioData)
package decode
