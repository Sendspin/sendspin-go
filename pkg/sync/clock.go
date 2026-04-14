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

// Quality represents sync quality
type Quality int

const (
	QualityGood Quality = iota
	QualityDegraded
	QualityLost
)

// NewClockSync creates a new clock synchronizer
func NewClockSync() *ClockSync {
	return &ClockSync{
		filter:  NewTimeFilter(DefaultTimeFilterConfig()),
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

	// Discard samples with high RTT (network congestion)
	if rtt > 100000 { // 100ms
		log.Printf("Discarding sync sample: high RTT %dμs", rtt)
		return
	}

	// NTP-style offset and uncertainty
	measurement := ((t2 - t1) + (t3 - t4)) / 2
	maxError := rtt / 2

	cs.filter.Update(measurement, maxError, t4)

	// Update quality based on RTT
	if rtt < 50000 {
		cs.quality = QualityGood
	} else {
		cs.quality = QualityDegraded
	}

	cs.sampleCount++

	if cs.sampleCount <= 5 {
		filterErr := cs.filter.GetError()
		log.Printf("Sync #%d: rtt=%dμs, offset=%dμs, error=%dμs",
			cs.sampleCount, rtt, measurement, filterErr)
	}
}

// GetStats returns sync statistics
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

// Deprecated: ServerMicrosNow returns current time in server's reference frame (us).
// Use ClockSync.ServerMicrosNow() on the instance from Receiver.ClockSync() instead.
func ServerMicrosNow() int64 {
	cs := globalClockSync
	if cs == nil {
		return time.Now().UnixMicro()
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if !cs.filter.Synced() {
		return time.Now().UnixMicro()
	}

	return cs.filter.ComputeServerTime(time.Now().UnixMicro())
}

var (
	globalClockSync         *ClockSync
	globalDeprecationWarned bool
)

// Deprecated: SetGlobalClockSync sets the global clock sync instance.
// Use Receiver.ClockSync() instead for new code.
func SetGlobalClockSync(cs *ClockSync) {
	if !globalDeprecationWarned {
		log.Printf("Warning: SetGlobalClockSync is deprecated, use Receiver.ClockSync() instead")
		globalDeprecationWarned = true
	}
	globalClockSync = cs
}
