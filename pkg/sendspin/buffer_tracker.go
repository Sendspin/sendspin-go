// ABOUTME: Per-client buffer tracker for send-ahead pacing
// ABOUTME: Tracks in-flight bytes and duration to prevent client buffer overflow
package sendspin

const (
	// DefaultMaxBufferDurationUs caps buffer duration at 30 seconds
	// regardless of byte capacity, matching aiosendspin's behavior.
	DefaultMaxBufferDurationUs = 30_000_000
)

// bufferedChunk records one sent chunk's end time, size, and duration.
type bufferedChunk struct {
	endTimeUs  int64
	byteCount  int
	durationUs int64
}

// BufferTracker tracks the estimated bytes and duration of audio that a
// client has buffered (sent but not yet played). The server checks
// CanSend before each chunk send and skips the client if the buffer is
// full. PruneConsumed removes chunks whose playback time has passed.
//
// All methods are called from the same goroutine (the audio tick loop)
// so no synchronization is needed.
type BufferTracker struct {
	ring  []bufferedChunk
	head  int // index of the oldest entry
	count int // number of valid entries

	bufferedBytes      int
	bufferedDurationUs int64

	capacityBytes int
	maxDurationUs int64
}

// NewBufferTracker creates a tracker with the given byte capacity and
// a 30-second duration cap.
func NewBufferTracker(capacityBytes int) *BufferTracker {
	// Pre-allocate for ~30s of 20ms chunks = 1500 slots.
	// The ring grows if needed but this avoids early resizes.
	initialCap := 1500
	if capacityBytes <= 0 {
		capacityBytes = 1048576 // 1MB default
	}
	return &BufferTracker{
		ring:          make([]bufferedChunk, initialCap),
		capacityBytes: capacityBytes,
		maxDurationUs: DefaultMaxBufferDurationUs,
	}
}

// PruneConsumed removes all chunks whose end time has passed. Call this
// once per tick before CanSend.
func (t *BufferTracker) PruneConsumed(nowUs int64) {
	for t.count > 0 {
		entry := t.ring[t.head]
		if entry.endTimeUs > nowUs {
			break
		}
		t.bufferedBytes -= entry.byteCount
		t.bufferedDurationUs -= entry.durationUs
		t.head = (t.head + 1) % len(t.ring)
		t.count--
	}
}

// CanSend reports whether a chunk of the given size and duration can be
// sent without exceeding the client's buffer capacity or the duration cap.
func (t *BufferTracker) CanSend(byteCount int, durationUs int64) bool {
	if t.bufferedBytes+byteCount > t.capacityBytes {
		return false
	}
	if t.bufferedDurationUs+durationUs > t.maxDurationUs {
		return false
	}
	return true
}

// Register records a sent chunk. Call this after CanSend returns true
// and the chunk has been enqueued for transmission.
func (t *BufferTracker) Register(endTimeUs int64, byteCount int, durationUs int64) {
	if t.count == len(t.ring) {
		t.grow()
	}
	idx := (t.head + t.count) % len(t.ring)
	t.ring[idx] = bufferedChunk{
		endTimeUs:  endTimeUs,
		byteCount:  byteCount,
		durationUs: durationUs,
	}
	t.count++
	t.bufferedBytes += byteCount
	t.bufferedDurationUs += durationUs
}

// BufferedBytes returns the current estimated bytes in the client's buffer.
func (t *BufferTracker) BufferedBytes() int {
	return t.bufferedBytes
}

// BufferedDurationUs returns the current estimated duration in the client's buffer.
func (t *BufferTracker) BufferedDurationUs() int64 {
	return t.bufferedDurationUs
}

func (t *BufferTracker) grow() {
	newCap := len(t.ring) * 2
	newRing := make([]bufferedChunk, newCap)
	for i := 0; i < t.count; i++ {
		newRing[i] = t.ring[(t.head+i)%len(t.ring)]
	}
	t.ring = newRing
	t.head = 0
}
