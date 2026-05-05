// ABOUTME: Clock synchronization using Kalman time filter
// ABOUTME: Tracks offset and drift between client and server clocks
package sync

import (
	"log"
	"sync"
	"time"
)

// ClockSync manages clock synchronization using a Kalman time filter.
type ClockSync struct {
	mu          sync.RWMutex
	filter      *TimeFilter
	rtt         int64
	quality     Quality
	lastSync    time.Time
	sampleCount int
}

type Quality int

const (
	QualityGood Quality = iota
	QualityDegraded
	QualityLost
)

// Quality thresholds in microseconds of offset σ (filter.GetError()).
// QualityGood: filter has converged; sub-100µs sync uncertainty.
// QualityDegraded: filter still useful but uncertainty is large.
// QualityLost: no recent sync OR uncertainty so high the estimate is suspect.
const (
	qualityGoodMaxErrorUs     = 100
	qualityDegradedMaxErrorUs = 5000
)

func qualityFromError(errUs int64) Quality {
	switch {
	case errUs < qualityGoodMaxErrorUs:
		return QualityGood
	case errUs < qualityDegradedMaxErrorUs:
		return QualityDegraded
	default:
		return QualityLost
	}
}

func NewClockSync() *ClockSync {
	return NewClockSyncWithConfig(DefaultTimeFilterConfig())
}

// NewClockSyncWithConfig creates a ClockSync with a custom filter configuration.
func NewClockSyncWithConfig(cfg TimeFilterConfig) *ClockSync {
	return &ClockSync{
		filter:  NewTimeFilter(cfg),
		quality: QualityLost,
	}
}

// ProcessSyncResponse processes a server/time response.
// t1: client send (Unix µs), t2: server receive (server µs),
// t3: server send (server µs), t4: client receive (Unix µs)
func (cs *ClockSync) ProcessSyncResponse(t1, t2, t3, t4 int64) {
	rtt := (t4 - t1) - (t3 - t2)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.rtt = rtt
	cs.lastSync = time.Now()

	// NTP-style offset and uncertainty.
	measurement := ((t2 - t1) + (t3 - t4)) / 2
	maxError := rtt / 2

	cs.filter.Update(measurement, maxError, t4)
	cs.quality = qualityFromError(cs.filter.GetError())

	cs.sampleCount++

	if cs.sampleCount <= 5 {
		filterErr := cs.filter.GetError()
		log.Printf("Sync #%d: rtt=%dμs, offset=%dμs, error=%dμs",
			cs.sampleCount, rtt, measurement, filterErr)
	}
}

func (cs *ClockSync) GetStats() (rtt int64, quality Quality) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.rtt, cs.quality
}

// CheckQuality updates quality based on time since last sync
func (cs *ClockSync) CheckQuality() Quality {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if time.Since(cs.lastSync) > 5*time.Second {
		cs.quality = QualityLost
	}

	return cs.quality
}

// ServerToLocalTime converts server timestamp (µs) to local wall clock time.
func (cs *ClockSync) ServerToLocalTime(serverTime int64) time.Time {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.filter.Synced() {
		return time.Unix(0, serverTime*1000)
	}

	// server→client conversion gives us client Unix µs
	clientMicros := cs.filter.ComputeClientTime(serverTime)
	return time.UnixMicro(clientMicros)
}

// ServerMicrosNow returns current time in server's reference frame (us).
// This is the instance method equivalent of the deprecated package-level ServerMicrosNow().
func (cs *ClockSync) ServerMicrosNow() int64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.filter.Synced() {
		return time.Now().UnixMicro()
	}

	return cs.filter.ComputeServerTime(time.Now().UnixMicro())
}
