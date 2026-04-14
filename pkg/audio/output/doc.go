// ABOUTME: Audio output package for playing audio
// ABOUTME: Provides Output interface with malgo implementation
// Package output provides audio playback interfaces.
//
// Currently supports:
//   - malgo (miniaudio): 16/24/32-bit output, format re-initialization supported
//
// Example:
//
//	out := output.NewMalgo()
//	err := out.Open(192000, 2, 24)  // 192kHz, stereo, 24-bit
//	err = out.Write(samples)
package output
