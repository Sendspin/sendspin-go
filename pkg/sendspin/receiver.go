// ABOUTME: Receiver handles connection, sync, decode, and scheduling
// ABOUTME: Emits decoded audio.Buffer via Output() channel for consumers
package sendspin

import (
	"context"
	"fmt"
	"log"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
)

// ReceiverConfig configures a Receiver
type ReceiverConfig struct {
	ServerAddr     string
	PlayerName     string
	BufferMs       int
	DeviceInfo     DeviceInfo
	DecoderFactory func(audio.Format) (decode.Decoder, error)
	OnMetadata     func(Metadata)
	OnStreamStart  func(audio.Format)
	OnStreamEnd    func()
	OnError        func(error)
}

// ReceiverStats contains receiver pipeline statistics
type ReceiverStats struct {
	Received    int64
	Played      int64
	Dropped     int64
	BufferDepth int
	SyncRTT     int64
	SyncQuality sync.Quality
}

// Receiver handles connection, clock sync, decoding, and scheduling.
// It emits decoded, time-stamped audio buffers via the Output() channel.
type Receiver struct {
	config          ReceiverConfig
	client          *protocol.Client
	clockSync       *sync.ClockSync
	scheduler       *Scheduler
	decoder         decode.Decoder
	format          audio.Format
	output          chan audio.Buffer
	ctx             context.Context
	cancel          context.CancelFunc
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc
	serverAddr      string
	connected       bool
}

// NewReceiver creates a new Receiver with the given configuration.
// ServerAddr is required; other fields have defaults.
func NewReceiver(config ReceiverConfig) (*Receiver, error) {
	if config.ServerAddr == "" {
		return nil, fmt.Errorf("ReceiverConfig.ServerAddr is required")
	}

	// Set defaults
	if config.BufferMs == 0 {
		config.BufferMs = 500
	}
	if config.DeviceInfo.ProductName == "" {
		config.DeviceInfo.ProductName = "Sendspin Player"
	}
	if config.DeviceInfo.Manufacturer == "" {
		config.DeviceInfo.Manufacturer = "Sendspin"
	}
	if config.DeviceInfo.SoftwareVersion == "" {
		config.DeviceInfo.SoftwareVersion = "1.0.0"
	}

	ctx, cancel := context.WithCancel(context.Background())

	clockSync := sync.NewClockSync()

	r := &Receiver{
		config:     config,
		clockSync:  clockSync,
		output:     make(chan audio.Buffer, 10),
		ctx:        ctx,
		cancel:     cancel,
		serverAddr: config.ServerAddr,
	}

	return r, nil
}

// Output returns the channel that emits decoded, time-stamped audio buffers.
func (r *Receiver) Output() <-chan audio.Buffer {
	return r.output
}

// ClockSync returns the clock synchronization instance used by this Receiver.
func (r *Receiver) ClockSync() *sync.ClockSync {
	return r.clockSync
}

// Stats returns current pipeline statistics from the scheduler and clock sync.
func (r *Receiver) Stats() ReceiverStats {
	stats := ReceiverStats{}

	if r.scheduler != nil {
		s := r.scheduler.Stats()
		stats.Received = s.Received
		stats.Played = s.Played
		stats.Dropped = s.Dropped
		stats.BufferDepth = r.scheduler.BufferDepth()
	}

	if r.clockSync != nil {
		rtt, quality := r.clockSync.GetStats()
		stats.SyncRTT = rtt
		stats.SyncQuality = quality
	}

	return stats
}

// Close shuts down the Receiver, releasing all resources.
func (r *Receiver) Close() error {
	r.cancel()

	if r.client != nil {
		r.client.SendGoodbye("shutdown")
		r.client.Close()
	}

	if r.scheduler != nil {
		r.scheduler.Stop()
	}

	if r.decoder != nil {
		if err := r.decoder.Close(); err != nil {
			log.Printf("Receiver: decoder close error: %v", err)
		}
	}

	close(r.output)

	return nil
}
