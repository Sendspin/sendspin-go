// ABOUTME: Tests for Kalman-filter-based clock synchronization
// ABOUTME: Tests RTT calculation, time conversion, quality tracking
package sync

import (
	"testing"
	"time"
)

func TestRTTCalculation(t *testing.T) {
	t1 := int64(1000000)
	t2 := int64(2000)
	t3 := int64(2500)
	t4 := int64(1005000)

	cs := NewClockSync()
	cs.ProcessSyncResponse(t1, t2, t3, t4)

	// RTT = (t4-t1) - (t3-t2) = 5000 - 500 = 4500µs
	rtt, _ := cs.GetStats()
	if rtt != 4500 {
		t.Errorf("expected RTT 4500µs, got %dµs", rtt)
	}
}

func TestSyncEstablishment(t *testing.T) {
	cs := NewClockSync()

	if cs.filter.Synced() {
		t.Error("expected not synced initially")
	}

	// One low-noise sample is enough to mark the filter Synced.
	cs.ProcessSyncResponse(1_000_000, 500_000, 500_100, 1_000_200)

	if !cs.filter.Synced() {
		t.Error("expected synced after first response")
	}

	// Drive enough low-noise samples to converge to QualityGood.
	for i := 1; i < 60; i++ {
		t1 := int64(1_000_000 + i*100_000)
		cs.ProcessSyncResponse(t1, t1+50, t1+150, t1+200) // ~200µs RTT
	}
	_, quality := cs.GetStats()
	if quality != QualityGood {
		t.Errorf("expected QualityGood after convergence, got %v", quality)
	}
}

func TestServerToLocalTimeConversion(t *testing.T) {
	cs := NewClockSync()

	clientNow := time.Now().UnixMicro()
	serverTime := int64(5000000) // 5s into server loop

	// Feed several samples to let the filter converge
	for i := 0; i < 10; i++ {
		ct := clientNow + int64(i*100000) // 100ms apart
		st := serverTime + int64(i*100000)
		cs.ProcessSyncResponse(ct-1000, st, st+50, ct)
	}

	// Convert a server time 100ms in the future
	futureServer := serverTime + 10*100000 + 100000
	localTime := cs.ServerToLocalTime(futureServer)

	expectedLocal := time.UnixMicro(clientNow + 10*100000 + 100000)
	diff := localTime.Sub(expectedLocal).Microseconds()

	if diff < -50000 || diff > 50000 {
		t.Errorf("time conversion off by %dµs", diff)
	}
}

func TestQualityTracking(t *testing.T) {
	cs := NewClockSync()

	// Single noisy sample → high σ → not yet QualityGood.
	cs.ProcessSyncResponse(1000000, 1000, 1100, 1025000)
	_, quality := cs.GetStats()
	if quality == QualityGood {
		t.Errorf("expected non-Good quality on first sample, got %v", quality)
	}

	// Drive enough low-noise samples to converge below the QualityGood threshold.
	for i := 1; i < 60; i++ {
		t1 := int64(1_000_000 + i*100_000)
		cs.ProcessSyncResponse(t1, t1+50, t1+150, t1+200) // ~200µs RTT
	}
	_, quality = cs.GetStats()
	if quality != QualityGood {
		t.Errorf("expected QualityGood after convergence, got %v (filter err=%d)",
			quality, cs.filter.GetError())
	}
}

func TestQualityDegradation(t *testing.T) {
	cs := NewClockSync()

	// Drive enough low-noise samples to reach QualityGood.
	for i := 0; i < 60; i++ {
		t1 := int64(1_000_000 + i*100_000)
		cs.ProcessSyncResponse(t1, t1+50, t1+150, t1+200) // ~200µs RTT
	}

	quality := cs.CheckQuality()
	if quality != QualityGood {
		t.Errorf("expected QualityGood initially, got %v", quality)
	}

	cs.mu.Lock()
	cs.lastSync = time.Now().Add(-6 * time.Second)
	cs.mu.Unlock()

	quality = cs.CheckQuality()
	if quality != QualityLost {
		t.Errorf("expected QualityLost after 6s, got %v", quality)
	}
}

func TestClockSync_ServerMicrosNow(t *testing.T) {
	cs := NewClockSync()

	// Before sync, should return roughly current Unix micros
	now1 := cs.ServerMicrosNow()
	unixNow := time.Now().UnixMicro()
	if abs64(now1-unixNow) > 1000000 {
		t.Errorf("before sync: expected ~%d, got %d", unixNow, now1)
	}

	// After sync, should return server-frame time
	cs.ProcessSyncResponse(1000, 500000, 500100, 1200)
	now2 := cs.ServerMicrosNow()
	if now2 == 0 {
		t.Error("after sync: got zero")
	}
}

func TestNewClockSyncWithConfig(t *testing.T) {
	cfg := DefaultTimeFilterConfig()
	cfg.MaxErrorScale = 0.25

	csDefault := NewClockSync()
	csScaled := NewClockSyncWithConfig(cfg)

	const samples = 30
	for i := 0; i < samples; i++ {
		t1 := int64(1_000_000 + i*100_000)
		t2 := int64(500_000 + i*100_000)
		t3 := t2 + 100
		t4 := t1 + 1000 // ~1ms RTT
		csDefault.ProcessSyncResponse(t1, t2, t3, t4)
		csScaled.ProcessSyncResponse(t1, t2, t3, t4)
	}

	errDefault := csDefault.filter.GetError()
	errScaled := csScaled.filter.GetError()
	if !(errScaled < errDefault) {
		t.Errorf("expected scaled (0.25) error < default (1.0); got scaled=%d default=%d",
			errScaled, errDefault)
	}
}

func TestConcurrentAccess(t *testing.T) {
	cs := NewClockSync()

	cs.ProcessSyncResponse(1000000, 1000, 1100, 1025000)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cs.GetStats()
				cs.CheckQuality()
				cs.ServerMicrosNow()
				cs.ServerToLocalTime(int64(j * 1000))
				cs.ProcessSyncResponse(
					int64(1000000+j), int64(1000+j),
					int64(1100+j), int64(1025000+j),
				)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	rtt, quality := cs.GetStats()
	if rtt <= 0 {
		t.Error("invalid RTT after concurrent access")
	}
	if quality == QualityLost {
		t.Error("unexpected QualityLost after concurrent access")
	}
}
