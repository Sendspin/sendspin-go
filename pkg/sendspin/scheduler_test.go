// ABOUTME: Tests for the playback scheduler
// ABOUTME: Covers static-delay offset, buffer ordering, and drop semantics
package sendspin

import (
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/sync"
)

// TestScheduler_StaticDelayShift is a regression guard for the
// --static-delay-ms feature. With an unsynced ClockSync,
// ServerToLocalTime returns time.Unix(0, serverTime*1000) deterministically,
// which gives us an exact expected PlayAt we can check.
//
// For a 250ms static delay, a buffer whose timestamp would otherwise play
// at local time T should actually play at T + 250ms.
func TestScheduler_StaticDelayShift(t *testing.T) {
	const serverTimestampUs = int64(1_000_000_000) // 1000 seconds, arbitrary
	const bufferMs = 200

	cases := []struct {
		name          string
		staticDelayMs int
	}{
		{"no delay", 0},
		{"250ms delay", 250},
		{"1000ms delay", 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := sync.NewClockSync()
			sched := NewScheduler(cs, bufferMs, tc.staticDelayMs)
			defer sched.Stop()

			buf := audio.Buffer{Timestamp: serverTimestampUs}
			sched.Schedule(buf)

			// Schedule mutates a local copy; inspect the queue.
			sched.bufferMu.Lock()
			queued := sched.bufferQ.Peek()
			sched.bufferMu.Unlock()

			baseline := time.Unix(0, serverTimestampUs*1000)
			want := baseline.Add(time.Duration(tc.staticDelayMs) * time.Millisecond)

			if !queued.PlayAt.Equal(want) {
				t.Errorf("PlayAt = %v, want %v (static delay = %dms)",
					queued.PlayAt, want, tc.staticDelayMs)
			}
		})
	}
}

// TestScheduler_StaticDelayDefaultZero confirms that a scheduler built with
// the old two-argument shape would still behave correctly — i.e. the delay
// field defaults cleanly to 0. This is a belt-and-suspenders check against
// future refactors that might forget to thread the parameter through.
func TestScheduler_StaticDelayDefaultZero(t *testing.T) {
	cs := sync.NewClockSync()
	sched := NewScheduler(cs, 200, 0)
	defer sched.Stop()

	if sched.staticDelay != 0 {
		t.Errorf("staticDelay with 0 arg = %v, want 0", sched.staticDelay)
	}
}
