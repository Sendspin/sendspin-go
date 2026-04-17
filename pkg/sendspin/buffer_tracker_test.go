// ABOUTME: Tests for the per-client BufferTracker
// ABOUTME: Verifies capacity gating, duration cap, prune, and ring growth
package sendspin

import (
	"testing"
)

func TestBufferTracker_BasicFlow(t *testing.T) {
	tracker := NewBufferTracker(1000) // 1000 bytes capacity

	// Should be able to send when empty
	if !tracker.CanSend(100, 20000) {
		t.Error("should be able to send when empty")
	}

	// Register a chunk
	tracker.Register(100000, 100, 20000) // ends at 100ms, 100 bytes, 20ms duration

	if tracker.BufferedBytes() != 100 {
		t.Errorf("buffered bytes = %d, want 100", tracker.BufferedBytes())
	}

	// Should still be able to send more
	if !tracker.CanSend(100, 20000) {
		t.Error("should be able to send with capacity remaining")
	}
}

func TestBufferTracker_ByteCapacity(t *testing.T) {
	tracker := NewBufferTracker(200)

	// Fill to capacity
	tracker.Register(100000, 100, 20000)
	tracker.Register(200000, 100, 20000)

	// Should NOT be able to send — at capacity
	if tracker.CanSend(100, 20000) {
		t.Error("should not be able to send at byte capacity")
	}

	// Even a 1-byte chunk should be rejected
	if tracker.CanSend(1, 20000) {
		t.Error("should not be able to send when over byte capacity")
	}

	// Prune the first chunk (simulate time passing)
	tracker.PruneConsumed(100000) // now = 100ms, first chunk ends at 100ms

	// Should be able to send again
	if !tracker.CanSend(100, 20000) {
		t.Error("should be able to send after pruning")
	}
	if tracker.BufferedBytes() != 100 {
		t.Errorf("buffered bytes after prune = %d, want 100", tracker.BufferedBytes())
	}
}

func TestBufferTracker_DurationCap(t *testing.T) {
	tracker := NewBufferTracker(1000000) // large byte capacity

	// Fill 30 seconds of 20ms chunks (1500 chunks × 20000μs = 30s)
	endTime := int64(0)
	for i := 0; i < 1500; i++ {
		endTime += 20000
		tracker.Register(endTime, 100, 20000)
	}

	// At exactly 30s — should NOT be able to send more
	if tracker.CanSend(100, 20000) {
		t.Error("should not be able to send at 30s duration cap")
	}

	// Prune 1 chunk — should free up space
	tracker.PruneConsumed(20000)
	if !tracker.CanSend(100, 20000) {
		t.Error("should be able to send after pruning one chunk")
	}
}

func TestBufferTracker_PruneMultiple(t *testing.T) {
	tracker := NewBufferTracker(10000)

	tracker.Register(100000, 50, 20000)
	tracker.Register(200000, 60, 20000)
	tracker.Register(300000, 70, 20000)

	if tracker.BufferedBytes() != 180 {
		t.Errorf("bytes = %d, want 180", tracker.BufferedBytes())
	}

	// Prune first two
	tracker.PruneConsumed(200000)
	if tracker.BufferedBytes() != 70 {
		t.Errorf("after prune bytes = %d, want 70", tracker.BufferedBytes())
	}
	if tracker.BufferedDurationUs() != 20000 {
		t.Errorf("after prune duration = %d, want 20000", tracker.BufferedDurationUs())
	}
}

func TestBufferTracker_PruneAll(t *testing.T) {
	tracker := NewBufferTracker(10000)

	tracker.Register(100000, 50, 20000)
	tracker.Register(200000, 60, 20000)

	tracker.PruneConsumed(300000) // past both

	if tracker.BufferedBytes() != 0 {
		t.Errorf("bytes after full prune = %d, want 0", tracker.BufferedBytes())
	}
	if tracker.BufferedDurationUs() != 0 {
		t.Errorf("duration after full prune = %d, want 0", tracker.BufferedDurationUs())
	}
}

func TestBufferTracker_RingGrowth(t *testing.T) {
	tracker := NewBufferTracker(1000000)

	// Fill beyond the initial ring capacity (1500)
	for i := 0; i < 2000; i++ {
		endTime := int64((i + 1) * 20000)
		tracker.Register(endTime, 10, 20000)
	}

	// Prune half
	tracker.PruneConsumed(1000 * 20000) // prune first 1000

	if tracker.BufferedBytes() != 10000 {
		t.Errorf("bytes after partial prune = %d, want 10000", tracker.BufferedBytes())
	}

	// Should still work correctly after growth
	if !tracker.CanSend(10, 20000) {
		t.Error("should be able to send after partial prune")
	}
}

func TestBufferTracker_EmptyPrune(t *testing.T) {
	tracker := NewBufferTracker(1000)

	// Prune on empty tracker should not panic
	tracker.PruneConsumed(999999)

	if tracker.BufferedBytes() != 0 {
		t.Error("empty tracker should have 0 bytes")
	}
}

func TestBufferTracker_ZeroCapacity(t *testing.T) {
	tracker := NewBufferTracker(0) // should default to 1MB

	if !tracker.CanSend(1000, 20000) {
		t.Error("zero capacity should default to 1MB")
	}
}
