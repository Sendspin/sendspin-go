// ABOUTME: Encoder interface definition
// ABOUTME: Common interface for all audio encoders
package encode

// Encoder encodes PCM int32 samples to various formats
type Encoder interface {
	Encode(samples []int32) ([]byte, error)
	Close() error
}
