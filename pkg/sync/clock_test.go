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

	now := time.Now().UnixMicro()
	cs.ProcessSyncResponse(now-10000, 1000000, 1000100, now)

	if !cs.filter.Synced() {
		t.Error("expected synced after first response")
	}

	_, quality := cs.GetStats()
	if quality != QualityGood {
		t.Errorf("expected QualityGood, got %v", quality)
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

	// Good quality: RTT < 50ms
	cs.ProcessSyncResponse(1000000, 1000, 1100, 1025000)
	_, quality := cs.GetStats()
	if quality != QualityGood {
		t.Errorf("expected QualityGood for 25ms RTT, got %v", quality)
	}

	// Degraded quality: RTT > 50ms
	cs.ProcessSyncResponse(2000000, 2000, 2100, 2080000)
	_, quality = cs.GetStats()
	if quality != QualityDegraded {
		t.Errorf("expected QualityDegraded for 80ms RTT, got %v", quality)
	}
}

func TestQualityDegradation(t *testing.T) {
	cs := NewClockSync()

	cs.ProcessSyncResponse(1000000, 1000, 1100, 1025000)

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

func TestHighRTTRejection(t *testing.T) {
	cs := NewClockSync()

	// First sync with good RTT
	cs.ProcessSyncResponse(1000000, 1000, 1100, 1025000)
	count1 := cs.sampleCount

	// Second sync with very high RTT (>100ms) should be discarded
	cs.ProcessSyncResponse(2000000, 2000, 2100, 2250000)
	count2 := cs.sampleCount

	if count2 != count1 {
		t.Errorf("expected sample count to stay at %d, got %d", count1, count2)
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
		t.Errorf("expected scaled (0.25) error < default (0.5); got scaled=%d default=%d",
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
