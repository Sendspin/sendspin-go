// ABOUTME: Audio encoder package for the Sendspin server
// ABOUTME: Provides real-time Opus and FLAC encoders for PCM audio
// Package encode provides real-time audio encoders for the Sendspin server.
//
// Supports: Opus (lossy, bandwidth-efficient) and FLAC (lossless). Each
// encoder turns a block of interleaved PCM samples into a single codec
// frame/packet for streaming; FLAC additionally exposes its codec header
// for the stream/start handshake.
package encode
