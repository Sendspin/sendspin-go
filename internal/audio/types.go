// ABOUTME: Audio type definitions
// ABOUTME: Defines audio formats and decoded buffers
package audio

import "time"

const (
	// 24-bit audio range constants
	Max24Bit = 8388607  // 2^23 - 1
	Min24Bit = -8388608 // -2^23
)

type Format struct {
	Codec       string
	SampleRate  int
	Channels    int
	BitDepth    int
	CodecHeader []byte // For FLAC, Opus, etc.
}

type Buffer struct {
	Timestamp int64     // Server timestamp (microseconds)
	PlayAt    time.Time // Local play time
	Samples   []int32   // PCM samples (int32 to support both 16-bit and 24-bit)
	Format    Format
}

// SampleToInt16 converts int32 sample to int16 (for 16-bit playback)
func SampleToInt16(sample int32) int16 {
	// Right-shift to convert 24-bit (or 16-bit) to 16-bit range
	return int16(sample >> 8)
}

// SampleFromInt16 converts int16 sample to int32 (left-justified in 24-bit)
func SampleFromInt16(sample int16) int32 {
	// Left-shift to position 16-bit value in upper bits
	return int32(sample) << 8
}

// SampleTo24Bit converts int32 to 24-bit packed bytes (little-endian)
func SampleTo24Bit(sample int32) [3]byte {
	// Take lower 24 bits, pack little-endian
	return [3]byte{
		byte(sample),
		byte(sample >> 8),
		byte(sample >> 16),
	}
}

// SampleFrom24Bit converts 24-bit packed bytes to int32 (little-endian)
func SampleFrom24Bit(b [3]byte) int32 {
	// Reconstruct 24-bit value and sign-extend to 32-bit
	val := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
	// Sign extend from 24-bit to 32-bit
	if val&0x800000 != 0 {
		val |= ^0xFFFFFF // Set upper 8 bits to 1 for negative values
	}
	return val
}
