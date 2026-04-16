// ABOUTME: Audio fundamentals package providing core types and utilities
// ABOUTME: Defines Format, Buffer types and sample conversion functions
// Package audio provides fundamental audio types and utilities for hi-res audio processing.
//
// This package defines core types used throughout the sendspin library:
//   - Format: Describes audio stream format (codec, sample rate, channels, bit depth)
//   - Buffer: Represents decoded PCM audio with timestamp information
//
// It also provides utilities for converting between different sample formats:
//   - 16-bit ↔ 24-bit conversions
//   - int32 ↔ packed byte conversions
//
// Example:
//
//	format := audio.Format{
//	    Codec:      "pcm",
//	    SampleRate: 192000,
//	    Channels:   2,
//	    BitDepth:   24,
//	}
//
//	// Convert 16-bit sample to 24-bit range
//	sample24 := audio.SampleFromInt16(sample16)
package audio
