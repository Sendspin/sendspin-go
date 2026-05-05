// ABOUTME: Kalman filter for NTP-style time synchronization
// ABOUTME: Tracks clock offset and drift between client and server
package sync

import (
	"math"
	"sync"
)

// TimeFilter is a two-dimensional Kalman filter that tracks clock offset and
// drift rate between client and server using NTP-style time messages.
// Implements the Sendspin time filter specification.
type TimeFilter struct {
	mu sync.Mutex

	lastUpdate int64

	offset float64
	drift  float64

	offsetCovariance      float64
	offsetDriftCovariance float64
	driftCovariance       float64

	processVariance              float64
	driftProcessVariance         float64
	forgetVarianceFactor         float64
	adaptiveForgettingCutoff     float64
	driftSignificanceThresholdSq float64
	maxErrorScale                float64

	useDrift   bool
	count      uint8
	minSamples uint8
}

// TimeFilterConfig holds configuration for the Kalman filter.
type TimeFilterConfig struct {
	// ProcessStdDev is the standard deviation of offset process noise in µs.
	ProcessStdDev float64
	// DriftProcessStdDev is the standard deviation of drift process noise in µs/s.
	DriftProcessStdDev float64
	// ForgetFactor (>1) applied to covariances when large residuals are detected.
	ForgetFactor float64
	// AdaptiveCutoff is the fraction of max_error (0-1) that triggers forgetting.
	AdaptiveCutoff float64
	// MinSamples before adaptive forgetting is enabled.
	MinSamples uint8
	// DriftSignificanceThreshold is the SNR threshold for applying drift compensation.
	DriftSignificanceThreshold float64
	// MaxErrorScale scales max_error before use as the measurement std dev.
	// Spec recommends 0.5; values <1 indicate max_error overestimates noise.
	MaxErrorScale float64
}

// DefaultTimeFilterConfig returns the canonical defaults from the upstream
// Sendspin/time-filter reference (PR #6, 2026-04-27).
func DefaultTimeFilterConfig() TimeFilterConfig {
	return TimeFilterConfig{
		ProcessStdDev:              0.0,
		DriftProcessStdDev:         1e-11,
		ForgetFactor:               2.0,
		AdaptiveCutoff:             3.0,
		MinSamples:                 100,
		DriftSignificanceThreshold: 2.0,
		MaxErrorScale:              0.5,
	}
}

// NewTimeFilter creates a Kalman filter for time synchronization.
func NewTimeFilter(cfg TimeFilterConfig) *TimeFilter {
	maxErrorScale := cfg.MaxErrorScale
	if maxErrorScale <= 0 {
		maxErrorScale = 1.0 // avoid zero variance → div-by-zero in Kalman gain
	}
	tf := &TimeFilter{
		processVariance:              cfg.ProcessStdDev * cfg.ProcessStdDev,
		driftProcessVariance:         cfg.DriftProcessStdDev * cfg.DriftProcessStdDev,
		forgetVarianceFactor:         cfg.ForgetFactor * cfg.ForgetFactor,
		adaptiveForgettingCutoff:     cfg.AdaptiveCutoff,
		driftSignificanceThresholdSq: cfg.DriftSignificanceThreshold * cfg.DriftSignificanceThreshold,
		maxErrorScale:                maxErrorScale,
		minSamples:                   cfg.MinSamples,
	}
	tf.reset()
	return tf
}

// Update processes a new time synchronization measurement.
//
//	measurement: ((T2-T1)+(T3-T4))/2 in microseconds
//	maxError:    ((T4-T1)-(T3-T2))/2 in microseconds
//	timeAdded:   client timestamp when measurement was taken, in microseconds
func (tf *TimeFilter) Update(measurement, maxError, timeAdded int64) {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	if timeAdded <= tf.lastUpdate {
		return // skip non-monotonic timestamps
	}

	dt := float64(timeAdded - tf.lastUpdate)
	dtSq := dt * dt
	tf.lastUpdate = timeAdded

	updateStdDev := float64(maxError) * tf.maxErrorScale
	measVar := updateStdDev * updateStdDev

	// First measurement: establish offset baseline
	if tf.count == 0 {
		tf.count++
		tf.offset = float64(measurement)
		tf.offsetCovariance = measVar
		tf.drift = 0
		return
	}

	// Second measurement: initial drift estimate via finite differences
	if tf.count == 1 {
		tf.count++
		tf.drift = (float64(measurement) - tf.offset) / dt
		tf.offset = float64(measurement)
		tf.driftCovariance = (tf.offsetCovariance + measVar) / dtSq
		tf.offsetCovariance = measVar
		return
	}

	// --- Kalman prediction ---
	predOffset := tf.offset + tf.drift*dt

	driftProcVar := dt * tf.driftProcessVariance
	newDriftCov := tf.driftCovariance + driftProcVar
	newOffsetDriftCov := tf.offsetDriftCovariance + tf.driftCovariance*dt
	offsetProcVar := dt * tf.processVariance
	newOffsetCov := tf.offsetCovariance + 2*tf.offsetDriftCovariance*dt +
		tf.driftCovariance*dtSq + offsetProcVar

	// --- Innovation and adaptive forgetting ---
	residual := float64(measurement) - predOffset
	cutoff := float64(maxError) * tf.adaptiveForgettingCutoff

	if tf.count < tf.minSamples {
		tf.count++
	} else if math.Abs(residual) > cutoff {
		newDriftCov *= tf.forgetVarianceFactor
		newOffsetDriftCov *= tf.forgetVarianceFactor
		newOffsetCov *= tf.forgetVarianceFactor
	}

	// --- Kalman update ---
	invS := 1.0 / (newOffsetCov + measVar)
	offsetGain := newOffsetCov * invS
	driftGain := newOffsetDriftCov * invS

	tf.offset = predOffset + offsetGain*residual
	tf.drift += driftGain * residual

	tf.driftCovariance = newDriftCov - driftGain*newOffsetDriftCov
	tf.offsetDriftCovariance = newOffsetDriftCov - driftGain*newOffsetCov
	tf.offsetCovariance = newOffsetCov - offsetGain*newOffsetCov

	driftSq := tf.drift * tf.drift
	tf.useDrift = driftSq > tf.driftSignificanceThresholdSq*tf.driftCovariance
}

// ComputeServerTime converts a client timestamp to server time.
func (tf *TimeFilter) ComputeServerTime(clientTime int64) int64 {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	dt := float64(clientTime - tf.lastUpdate)
	effectiveDrift := 0.0
	if tf.useDrift {
		effectiveDrift = tf.drift
	}
	offset := math.Round(tf.offset + effectiveDrift*dt)
	return clientTime + int64(offset)
}

// ComputeClientTime converts a server timestamp to client time.
func (tf *TimeFilter) ComputeClientTime(serverTime int64) int64 {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	effectiveDrift := 0.0
	if tf.useDrift {
		effectiveDrift = tf.drift
	}
	return int64(math.Round(
		(float64(serverTime) - tf.offset + effectiveDrift*float64(tf.lastUpdate)) /
			(1.0 + effectiveDrift)))
}

// GetError returns the estimated standard deviation of the offset in µs.
func (tf *TimeFilter) GetError() int64 {
	tf.mu.Lock()
	defer tf.mu.Unlock()
	v := math.Sqrt(tf.offsetCovariance)
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return math.MaxInt64
	}
	return int64(math.Round(v))
}

// GetCovariance returns the offset variance in µs². Returns MaxInt64 before any update.
func (tf *TimeFilter) GetCovariance() int64 {
	tf.mu.Lock()
	defer tf.mu.Unlock()
	v := tf.offsetCovariance
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return math.MaxInt64
	}
	return int64(math.Round(v))
}

// Synced returns true after at least one measurement has been processed.
func (tf *TimeFilter) Synced() bool {
	tf.mu.Lock()
	defer tf.mu.Unlock()
	return tf.count > 0
}

// Reset clears all state.
func (tf *TimeFilter) Reset() {
	tf.mu.Lock()
	defer tf.mu.Unlock()
	tf.reset()
}

func (tf *TimeFilter) reset() {
	tf.count = 0
	tf.offset = 0
	tf.drift = 0
	tf.offsetCovariance = math.Inf(1)
	tf.offsetDriftCovariance = 0
	tf.driftCovariance = 0
	tf.lastUpdate = 0
	tf.useDrift = false
}
