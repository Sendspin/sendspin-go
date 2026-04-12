// ABOUTME: Malgo-based audio output implementation with 24-bit support
// ABOUTME: Uses miniaudio library via malgo for true hi-res audio playback
package output

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/gen2brain/malgo"
)

// Malgo output implementation using malgo/miniaudio library
type Malgo struct {
	ctx        context.Context
	cancel     context.CancelFunc
	malgoCtx   *malgo.AllocatedContext
	device     *malgo.Device
	sampleRate int
	channels   int
	bitDepth   int
	volume     int
	muted      bool
	ready      bool

	// Ring buffer for callback-based playback
	ringBuffer *RingBuffer
	mu         sync.Mutex
}

// RingBuffer provides thread-safe circular buffer for audio samples
type RingBuffer struct {
	buffer   []int32
	readPos  int
	writePos int
	size     int
	count    int // Number of samples currently in buffer
	mu       sync.Mutex
}

// NewRingBuffer creates a ring buffer with given capacity (in samples)
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buffer: make([]int32, capacity),
		size:   capacity,
	}
}

// Write adds samples to the ring buffer
func (rb *RingBuffer) Write(samples []int32) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	written := 0
	for i := 0; i < len(samples) && rb.count < rb.size; i++ {
		rb.buffer[rb.writePos] = samples[i]
		rb.writePos = (rb.writePos + 1) % rb.size
		rb.count++
		written++
	}
	return written
}

// Read retrieves samples from the ring buffer
func (rb *RingBuffer) Read(samples []int32) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	read := 0
	for i := 0; i < len(samples) && rb.count > 0; i++ {
		samples[i] = rb.buffer[rb.readPos]
		rb.readPos = (rb.readPos + 1) % rb.size
		rb.count--
		read++
	}

	// Zero-fill remaining if underrun
	for i := read; i < len(samples); i++ {
		samples[i] = 0
	}

	return read
}

// Available returns the number of samples available to read
func (rb *RingBuffer) Available() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Free returns the number of free slots in the buffer
func (rb *RingBuffer) Free() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size - rb.count
}

// NewMalgo creates a new Malgo output
func NewMalgo() Output {
	ctx, cancel := context.WithCancel(context.Background())

	return &Malgo{
		ctx:    ctx,
		cancel: cancel,
		volume: 100,
		muted:  false,
	}
}

// Open initializes the output device with specified format
func (m *Malgo) Open(sampleRate, channels, bitDepth int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already initialized with same format, reuse
	if m.device != nil && m.sampleRate == sampleRate && m.channels == channels && m.bitDepth == bitDepth {
		log.Printf("Audio output already initialized with same format, reusing device")
		return nil
	}

	// If format changed, reinitialize
	if m.device != nil {
		log.Printf("Format change detected (%dHz/%dch/%dbit -> %dHz/%dch/%dbit), reinitializing device",
			m.sampleRate, m.channels, m.bitDepth, sampleRate, channels, bitDepth)
		if err := m.closeDevice(); err != nil {
			return fmt.Errorf("failed to close old device: %w", err)
		}
	}

	// Create malgo context if needed
	if m.malgoCtx == nil {
		ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			return fmt.Errorf("failed to initialize malgo context: %w", err)
		}
		m.malgoCtx = ctx
	}

	// Map bit depth to malgo format
	var format malgo.FormatType
	switch bitDepth {
	case 16:
		format = malgo.FormatS16
	case 24:
		format = malgo.FormatS24
	case 32:
		format = malgo.FormatS32
	default:
		return fmt.Errorf("unsupported bit depth: %d (supported: 16, 24, 32)", bitDepth)
	}

	// Create ring buffer (80ms capacity - tuned for Music Assistant)
	bufferSamples := (sampleRate * channels * 80) / 1000
	m.ringBuffer = NewRingBuffer(bufferSamples)

	// Configure device
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = format
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap = 1

	// Set up callbacks
	onSamples := func(pOutputSample, pInputSamples []byte, frameCount uint32) {
		m.dataCallback(pOutputSample, frameCount)
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onSamples,
	}

	// Initialize device
	device, err := malgo.InitDevice(m.malgoCtx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		return fmt.Errorf("failed to initialize playback device: %w", err)
	}

	// Start device
	if err := device.Start(); err != nil {
		device.Uninit()
		return fmt.Errorf("failed to start device: %w", err)
	}

	m.device = device
	m.sampleRate = sampleRate
	m.channels = channels
	m.bitDepth = bitDepth
	m.ready = true

	log.Printf("Audio output initialized: %dHz, %d channels, %d-bit (malgo/%s)",
		sampleRate, channels, bitDepth, formatName(format))

	return nil
}

// Write queues audio samples for playback.
// Blocks briefly if the ring buffer is full, waiting for the audio callback
// to drain space. Returns an error if samples are dropped after the timeout.
func (m *Malgo) Write(samples []int32) error {
	if !m.ready {
		return fmt.Errorf("output not initialized")
	}

	const (
		retryInterval = 1 * time.Millisecond
		maxWait       = 50 * time.Millisecond
	)

	// Apply volume and mute
	volumedSamples := applyVolume(samples, m.volume, m.muted)

	written := 0
	waited := time.Duration(0)
	for written < len(volumedSamples) {
		n := m.ringBuffer.Write(volumedSamples[written:])
		written += n

		if written < len(volumedSamples) {
			if waited >= maxWait {
				dropped := len(volumedSamples) - written
				return fmt.Errorf("ring buffer full, dropped %d samples after %v", dropped, maxWait)
			}
			time.Sleep(retryInterval)
			waited += retryInterval
		}
	}

	return nil
}

// dataCallback is called by malgo to fill the audio output buffer
func (m *Malgo) dataCallback(pOutput []byte, frameCount uint32) {
	totalSamples := int(frameCount) * m.channels
	samples := make([]int32, totalSamples)

	// Read from ring buffer
	m.ringBuffer.Read(samples)

	// Convert int32 to output format
	switch m.bitDepth {
	case 16:
		m.write16Bit(pOutput, samples)
	case 24:
		m.write24Bit(pOutput, samples)
	case 32:
		m.write32Bit(pOutput, samples)
	}
}

// write16Bit converts int32 samples to 16-bit output
func (m *Malgo) write16Bit(output []byte, samples []int32) {
	for i, sample := range samples {
		// Convert 24-bit (int32) to 16-bit
		sample16 := audio.SampleToInt16(sample)
		output[i*2] = byte(sample16)
		output[i*2+1] = byte(sample16 >> 8)
	}
}

// write24Bit converts int32 samples to 24-bit output (3 bytes per sample)
func (m *Malgo) write24Bit(output []byte, samples []int32) {
	for i, sample := range samples {
		// Pack 24-bit value (little-endian)
		output[i*3] = byte(sample)
		output[i*3+1] = byte(sample >> 8)
		output[i*3+2] = byte(sample >> 16)
	}
}

// write32Bit converts int32 samples to 32-bit output
func (m *Malgo) write32Bit(output []byte, samples []int32) {
	for i, sample := range samples {
		// Write as 32-bit (little-endian), left-shifted for proper 24-bit representation
		sample32 := sample << 8 // Shift 24-bit value to upper bits of 32-bit container
		output[i*4] = byte(sample32)
		output[i*4+1] = byte(sample32 >> 8)
		output[i*4+2] = byte(sample32 >> 16)
		output[i*4+3] = byte(sample32 >> 24)
	}
}

// Close releases output resources
func (m *Malgo) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.closeDevice(); err != nil {
		return err
	}

	if m.malgoCtx != nil {
		if err := m.malgoCtx.Uninit(); err != nil {
			log.Printf("Warning: malgo context uninit error: %v", err)
		}
		m.malgoCtx.Free()
		m.malgoCtx = nil
	}

	m.cancel()
	return nil
}

// closeDevice stops and uninitializes the device (must hold m.mu)
func (m *Malgo) closeDevice() error {
	if m.device != nil {
		if err := m.device.Stop(); err != nil {
			log.Printf("Warning: device stop error: %v", err)
		}
		m.device.Uninit()
		m.device = nil
		m.ready = false
	}
	return nil
}

// SetVolume sets the volume (0-100)
func (m *Malgo) SetVolume(volume int) {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	m.volume = volume
	log.Printf("Volume set to %d", volume)
}

// SetMuted sets mute state
func (m *Malgo) SetMuted(muted bool) {
	m.muted = muted
	log.Printf("Muted: %v", muted)
}

// GetVolume returns current volume
func (m *Malgo) GetVolume() int {
	return m.volume
}

// IsMuted returns mute state
func (m *Malgo) IsMuted() bool {
	return m.muted
}

// formatName returns human-readable format name
func formatName(format malgo.FormatType) string {
	switch format {
	case malgo.FormatS16:
		return "S16"
	case malgo.FormatS24:
		return "S24"
	case malgo.FormatS32:
		return "S32"
	default:
		return fmt.Sprintf("Unknown(%d)", format)
	}
}
