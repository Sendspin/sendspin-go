// ABOUTME: Tests for Kalman filter time synchronization
// ABOUTME: Validates offset tracking, drift compensation, and adaptive forgetting
package sync

import (
	"math"
	"testing"
)

func TestTimeFilterFirstMeasurement(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	tf.Update(5000, 100, 1000000)

	if !tf.Synced() {
		t.Fatal("expected synced after first measurement")
	}

	// After one sample, server_time = client_time + offset (≈5000)
	st := tf.ComputeServerTime(1000000)
	diff := st - (1000000 + 5000)
	if abs64(diff) > 10 {
		t.Errorf("expected server time near %d, got %d", 1000000+5000, st)
	}
}

func TestTimeFilterConvergesOnStableOffset(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	// Feed 50 measurements with constant offset=10000µs, low noise
	for i := 0; i < 50; i++ {
		clientTime := int64(1000000 + i*100000) // 100ms apart
		tf.Update(10000, 50, clientTime)
	}

	// Should converge close to 10000µs offset
	clientNow := int64(1000000 + 50*100000)
	st := tf.ComputeServerTime(clientNow)
	offset := st - clientNow
	if abs64(offset-10000) > 100 {
		t.Errorf("expected offset near 10000, got %d", offset)
	}
}

func TestTimeFilterDriftTracking(t *testing.T) {
	cfg := DefaultTimeFilterConfig()
	cfg.DriftProcessStdDev = 0.1 // allow drift model
	tf := NewTimeFilter(cfg)

	// Simulate a clock drifting at 10µs per second (10ppm)
	// Measurements taken 1s apart, offset increases by 10 each time
	for i := 0; i < 200; i++ {
		clientTime := int64(i) * 1000000 // 1s apart
		trueOffset := int64(5000 + i*10) // drifting 10µs/s
		tf.Update(trueOffset, 50, clientTime)
	}

	// Check that drift-compensated conversion is more accurate than offset-only
	futureClient := int64(250 * 1000000)                // 50s in the future
	trueServerTime := futureClient + int64(5000+250*10) // true offset at t=250s
	predicted := tf.ComputeServerTime(futureClient)

	err := abs64(predicted - trueServerTime)
	if err > 1000 { // within 1ms is reasonable for 50s extrapolation
		t.Errorf("drift prediction error %dµs, expected <1000µs", err)
	}
}

func TestTimeFilterAdaptiveForgetting(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	// Build up stable estimate at offset=5000
	for i := 0; i < 150; i++ {
		clientTime := int64(1000000 + i*100000)
		tf.Update(5000, 50, clientTime)
	}

	// Sudden jump to offset=15000 (server clock adjusted)
	jumpTime := int64(1000000 + 150*100000)
	tf.Update(15000, 50, jumpTime)

	// After a few more samples at the new offset, should converge
	for i := 151; i < 200; i++ {
		clientTime := int64(1000000 + i*100000)
		tf.Update(15000, 50, clientTime)
	}

	clientNow := int64(1000000 + 200*100000)
	st := tf.ComputeServerTime(clientNow)
	offset := st - clientNow
	if abs64(offset-15000) > 1000 {
		t.Errorf("expected offset near 15000 after jump, got %d", offset)
	}
}

func TestTimeFilterComputeClientTime(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	for i := 0; i < 20; i++ {
		clientTime := int64(1000000 + i*100000)
		tf.Update(8000, 50, clientTime)
	}

	// Round-trip: client→server→client should be identity (within rounding)
	clientTime := int64(5000000)
	serverTime := tf.ComputeServerTime(clientTime)
	backToClient := tf.ComputeClientTime(serverTime)

	if abs64(backToClient-clientTime) > 2 {
		t.Errorf("round-trip error: %d → %d → %d", clientTime, serverTime, backToClient)
	}
}

func TestTimeFilterRejectsNonMonotonic(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	tf.Update(5000, 100, 2000000)
	tf.Update(5000, 100, 1000000) // earlier timestamp, should be ignored

	// Only one sample counted
	tf.mu.Lock()
	count := tf.count
	tf.mu.Unlock()
	if count != 1 {
		t.Errorf("expected count=1 after non-monotonic update, got %d", count)
	}
}

func TestTimeFilterReset(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	tf.Update(5000, 100, 1000000)
	if !tf.Synced() {
		t.Fatal("expected synced")
	}

	tf.Reset()
	if tf.Synced() {
		t.Fatal("expected not synced after reset")
	}
}

func TestTimeFilterGetError(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	// Before any measurements, covariance is Inf → error should be large
	err0 := tf.GetError()
	if err0 < 1000000 {
		t.Errorf("expected very large error before sync, got %d", err0)
	}

	// After many low-noise measurements, error should be small
	for i := 0; i < 100; i++ {
		tf.Update(5000, 20, int64(1000000+i*100000))
	}
	err1 := tf.GetError()
	if err1 > 50 {
		t.Errorf("expected small error after 100 samples, got %d", err1)
	}
}

func TestTimeFilterConcurrentAccess(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				clientTime := int64(id*10000000 + j*100000)
				tf.Update(5000, 50, clientTime)
				tf.ComputeServerTime(clientTime)
				tf.ComputeClientTime(clientTime + 5000)
				tf.GetError()
				tf.Synced()
			}
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Verify Inf covariance doesn't cause NaN propagation
func TestTimeFilterInitialInfCovariance(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	tf.mu.Lock()
	if !math.IsInf(tf.offsetCovariance, 1) {
		t.Error("expected +Inf initial offset covariance")
	}
	tf.mu.Unlock()

	// First update should produce finite values
	tf.Update(5000, 100, 1000000)
	tf.mu.Lock()
	if math.IsInf(tf.offsetCovariance, 0) || math.IsNaN(tf.offsetCovariance) {
		t.Errorf("expected finite covariance after first update, got %f", tf.offsetCovariance)
	}
	tf.mu.Unlock()
}

// Phase A: MaxErrorScale must default to 1.0 so existing callers see
// bit-identical measurement variance (max_error * 1.0)² == max_error².
func TestTimeFilterMaxErrorScaleDefault(t *testing.T) {
	if got := DefaultTimeFilterConfig().MaxErrorScale; got != 1.0 {
		t.Errorf("expected MaxErrorScale default 1.0, got %v", got)
	}
}

// Phase A: a smaller MaxErrorScale means lower measurement variance, so the
// posterior offset covariance should be tighter after the same number of
// identical measurements.
func TestTimeFilterMaxErrorScaleConvergence(t *testing.T) {
	cfgDefault := DefaultTimeFilterConfig()
	cfgScaled := DefaultTimeFilterConfig()
	cfgScaled.MaxErrorScale = 0.5

	tfDefault := NewTimeFilter(cfgDefault)
	tfScaled := NewTimeFilter(cfgScaled)

	for i := 0; i < 30; i++ {
		clientTime := int64(1000000 + i*100000)
		tfDefault.Update(5000, 50, clientTime)
		tfScaled.Update(5000, 50, clientTime)
	}

	errDefault := tfDefault.GetError()
	errScaled := tfScaled.GetError()
	if !(errScaled < errDefault) {
		t.Errorf("expected scaled (0.5) error < default (1.0); got scaled=%d default=%d",
			errScaled, errDefault)
	}
}

// Phase A: GetCovariance mirrors upstream C++ get_covariance(). Initially
// covariance is +Inf (clamped to MaxInt64); after convergence it shrinks
// to a small positive value in µs².
func TestTimeFilterGetCovariance(t *testing.T) {
	tf := NewTimeFilter(DefaultTimeFilterConfig())

	if got := tf.GetCovariance(); got != math.MaxInt64 {
		t.Errorf("expected MaxInt64 before any update, got %d", got)
	}

	for i := 0; i < 50; i++ {
		tf.Update(5000, 20, int64(1000000+i*100000))
	}

	cov := tf.GetCovariance()
	if cov <= 0 {
		t.Errorf("expected positive covariance after convergence, got %d", cov)
	}
	if cov >= 50000 {
		t.Errorf("expected covariance < 50000 µs² after convergence, got %d", cov)
	}
}
