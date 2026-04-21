// ABOUTME: Malgo-based audio output implementation with 24-bit support
// ABOUTME: Uses miniaudio library via malgo for true hi-res audio playback
package output

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/gen2brain/malgo"
)

// PlaybackDevice describes a playback endpoint discoverable via miniaudio.
// Returned by ListPlaybackDevices and used to select a specific device when
// constructing a Malgo output.
type PlaybackDevice struct {
	Name      string
	IsDefault bool
	ID        malgo.DeviceID
}

type Malgo struct {
	ctx        context.Context
	cancel     context.CancelFunc
	malgoCtx   *malgo.AllocatedContext
	device     *malgo.Device
	deviceName string // empty = use default
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

// NewMalgo constructs a new malgo-backed audio output. deviceName selects a
// specific playback device by name (as reported by ListPlaybackDevices). An
// empty deviceName lets miniaudio pick the platform default.
func NewMalgo(deviceName string) Output {
	ctx, cancel := context.WithCancel(context.Background())

	return &Malgo{
		ctx:        ctx,
		cancel:     cancel,
		deviceName: deviceName,
		volume:     100,
		muted:      false,
	}
}

// ListPlaybackDevices enumerates every playback device miniaudio can see.
// It creates a fresh context and tears it down before returning, so it is
// safe to call before any player/device has been initialized.
func ListPlaybackDevices() ([]PlaybackDevice, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	infos, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, fmt.Errorf("enumerate playback devices: %w", err)
	}

	out := make([]PlaybackDevice, 0, len(infos))
	for _, info := range infos {
		out = append(out, PlaybackDevice{
			Name:      info.Name(),
			IsDefault: info.IsDefault != 0,
			ID:        info.ID,
		})
	}
	return out, nil
}

// matchDevice picks a PlaybackDevice from a list based on a requested name.
//
// Empty requested name -> the device with IsDefault set, else the first in
// the list, else nil if the list is empty (caller falls back to whatever
// miniaudio's default-config path does).
//
// Non-empty requested name -> exact Name match first, then short-name match
// (the text before the first ", "). Miniaudio's Linux/ALSA backend builds
// device names from snd_device_name_hint's DESC field, which follows a
// "<card-short>, <stream-description>" convention, so users naturally try
// just the short part. If the short-name match is ambiguous, we error out
// instead of picking one silently.
//
// Fail-loud on no-match: the error lists every available device name, each
// quoted with %q so embedded commas are distinguishable from the list
// separator. Silent fallback to default is the behavior this feature
// exists to correct.
func matchDevice(devices []PlaybackDevice, requested string) (*PlaybackDevice, error) {
	if requested == "" {
		if len(devices) == 0 {
			return nil, nil
		}
		for i := range devices {
			if devices[i].IsDefault {
				return &devices[i], nil
			}
		}
		return &devices[0], nil
	}
	for i := range devices {
		if devices[i].Name == requested {
			return &devices[i], nil
		}
	}
	var shortMatches []int
	for i, d := range devices {
		if idx := strings.Index(d.Name, ", "); idx > 0 && d.Name[:idx] == requested {
			shortMatches = append(shortMatches, i)
		}
	}
	if len(shortMatches) == 1 {
		return &devices[shortMatches[0]], nil
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("audio device %q not found (no playback devices available)", requested)
	}
	quoted := make([]string, len(devices))
	for i, d := range devices {
		quoted[i] = fmt.Sprintf("%q", d.Name)
	}
	sort.Strings(quoted)
	if len(shortMatches) > 1 {
		return nil, fmt.Errorf("audio device %q is ambiguous (matches %d devices by short name); use the full quoted name. Available: %s", requested, len(shortMatches), strings.Join(quoted, ", "))
	}
	return nil, fmt.Errorf("audio device %q not found; available: %s", requested, strings.Join(quoted, ", "))
}

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

	if m.malgoCtx == nil {
		ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			return fmt.Errorf("failed to initialize malgo context: %w", err)
		}
		m.malgoCtx = ctx
	}

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

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = format
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap = 1

	// Resolve the playback device. When m.deviceName is empty, miniaudio's
	// enumerated default is picked (and logged so the operator knows what
	// they're getting). When non-empty, the device must exist or Open fails
	// loudly — silent fallback defeats the point of the knob.
	infos, err := m.malgoCtx.Devices(malgo.Playback)
	if err != nil {
		return fmt.Errorf("enumerate playback devices: %w", err)
	}
	catalog := make([]PlaybackDevice, 0, len(infos))
	for _, info := range infos {
		catalog = append(catalog, PlaybackDevice{
			Name:      info.Name(),
			IsDefault: info.IsDefault != 0,
			ID:        info.ID,
		})
	}
	chosen, err := matchDevice(catalog, m.deviceName)
	if err != nil {
		return err
	}
	// Hand miniaudio a pointer to the selected device ID, pinned across the
	// cgo call so Go 1.21+'s pointer check accepts it.
	//
	// Pinning &chosen.ID[0] directly would fail: chosen is an element inside
	// a []PlaybackDevice, and the containing heap object also holds the Go
	// string Name — whose backing bytes are another Go pointer that cgo's
	// recursive scan would find unpinned and reject. Copying the ID bytes
	// into a standalone []byte isolates the pointer target: a byte slice's
	// backing array contains only bytes (no further Go pointers), so the
	// scan finds nothing to complain about.
	var pinner runtime.Pinner
	defer pinner.Unpin()
	chosenLabel := "(miniaudio default)"
	if chosen != nil {
		idBuf := append([]byte(nil), chosen.ID[:]...)
		pinner.Pin(&idBuf[0])
		deviceConfig.Playback.DeviceID = unsafe.Pointer(&idBuf[0])
		if chosen.IsDefault {
			chosenLabel = fmt.Sprintf("%q (default)", chosen.Name)
		} else {
			chosenLabel = fmt.Sprintf("%q", chosen.Name)
		}
	}

	onSamples := func(pOutputSample, pInputSamples []byte, frameCount uint32) {
		m.dataCallback(pOutputSample, frameCount)
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onSamples,
	}

	device, err := malgo.InitDevice(m.malgoCtx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		return fmt.Errorf("failed to initialize playback device: %w", err)
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		return fmt.Errorf("failed to start device: %w", err)
	}

	m.device = device
	m.sampleRate = sampleRate
	m.channels = channels
	m.bitDepth = bitDepth
	m.ready = true

	log.Printf("Audio output initialized: device=%s %dHz/%dch/%d-bit (malgo/%s)",
		chosenLabel, sampleRate, channels, bitDepth, formatName(format))

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

	m.ringBuffer.Read(samples)

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
		sample16 := audio.SampleToInt16(sample)
		output[i*2] = byte(sample16)
		output[i*2+1] = byte(sample16 >> 8)
	}
}

// write24Bit converts int32 samples to 24-bit output (3 bytes per sample)
func (m *Malgo) write24Bit(output []byte, samples []int32) {
	for i, sample := range samples {
		output[i*3] = byte(sample)
		output[i*3+1] = byte(sample >> 8)
		output[i*3+2] = byte(sample >> 16)
	}
}

// write32Bit converts int32 samples to 32-bit output
func (m *Malgo) write32Bit(output []byte, samples []int32) {
	for i, sample := range samples {
		// Left-shift 24-bit value to fill the upper bits of the 32-bit container
		sample32 := sample << 8
		output[i*4] = byte(sample32)
		output[i*4+1] = byte(sample32 >> 8)
		output[i*4+2] = byte(sample32 >> 16)
		output[i*4+3] = byte(sample32 >> 24)
	}
}

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

// closeDevice stops and uninitializes the device; caller must hold m.mu.
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

func (m *Malgo) SetMuted(muted bool) {
	m.muted = muted
	log.Printf("Muted: %v", muted)
}

func (m *Malgo) GetVolume() int {
	return m.volume
}

func (m *Malgo) IsMuted() bool {
	return m.muted
}

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
