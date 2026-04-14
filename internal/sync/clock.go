// ABOUTME: Clock synchronization with drift compensation
// ABOUTME: Tracks both offset AND drift to handle clock frequency differences
package sync

import (
	"log"
	"sync"
	"time"
)

type ClockSync struct {
	mu                  sync.RWMutex
	serverLoopStartUnix int64 // Unix microseconds when server loop started
	rtt                 int64 // Latest round-trip time
	quality             Quality
	lastSync            time.Time
	sampleCount         int
	synced              bool // True after first successful sync
}

type Quality int

const (
	QualityGood Quality = iota
	QualityDegraded
	QualityLost
)

func NewClockSync() *ClockSync {
	return &ClockSync{
		quality: QualityLost,
		synced:  false,
	}
}

// ProcessSyncResponse processes a server/time response
// t1: client send (Unix µs)
// t2: server receive (server loop µs)
// t3: server send (server loop µs)
// t4: client receive (Unix µs)
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

	// On first successful sync, compute when the server loop started in Unix µs
	// t2 is server_received (server loop µs), t4 is our Unix µs
	if !cs.synced {
		cs.serverLoopStartUnix = time.Now().UnixMicro() - t2
		cs.synced = true
		cs.quality = QualityGood
		cs.sampleCount++
		log.Printf("Clock sync established: serverLoopStart=%d, rtt=%dμs", cs.serverLoopStartUnix, rtt)
		return
	}

	// Update quality based on RTT
	if rtt < 50000 { // <50ms
		cs.quality = QualityGood
	} else {
		cs.quality = QualityDegraded
	}

	cs.sampleCount++

	if cs.sampleCount < 10 {
		log.Printf("Sync #%d: rtt=%dμs, quality=%v", cs.sampleCount, rtt, cs.quality)
	}
}

func (cs *ClockSync) GetStats() (rtt int64, quality Quality) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.rtt, cs.quality
}

func (cs *ClockSync) CheckQuality() Quality {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if time.Since(cs.lastSync) > 5*time.Second {
		cs.quality = QualityLost
	}

	return cs.quality
}

// ServerToLocalTime converts server timestamp (loop µs) to local wall clock time
func (cs *ClockSync) ServerToLocalTime(serverTime int64) time.Time {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// If we haven't synced yet, assume server time = client time
	if !cs.synced {
		return time.Unix(0, serverTime*1000)
	}

	// Convert server loop µs to Unix µs
	unixMicros := cs.serverLoopStartUnix + serverTime

	return time.UnixMicro(unixMicros)
}

// ServerMicrosNow returns current time in server's reference frame (server loop µs)
func ServerMicrosNow() int64 {
	cs := globalClockSync
	if cs == nil {
		// Before sync initialized, use raw client time
		return time.Now().UnixMicro()
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// If we haven't synced yet, return Unix time
	if !cs.synced {
		return time.Now().UnixMicro()
	}

	// Calculate server loop µs from current Unix time
	return time.Now().UnixMicro() - cs.serverLoopStartUnix
}

// SetGlobalClockSync sets the global clock sync instance
func SetGlobalClockSync(cs *ClockSync) {
	globalClockSync = cs
}

var globalClockSync *ClockSync
