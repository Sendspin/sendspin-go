// ABOUTME: Receiver handles connection, sync, decode, and scheduling
// ABOUTME: Emits decoded audio.Buffer via Output() channel for consumers
package sendspin

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
	"github.com/google/uuid"
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

// Connect establishes a connection to the server, performs initial clock sync,
// and starts background goroutines for connection watching and clock sync.
func (r *Receiver) Connect() error {
	clientID := uuid.New().String()

	clientConfig := protocol.Config{
		ServerAddr: r.serverAddr,
		ClientID:   clientID,
		Name:       r.config.PlayerName,
		Version:    1,
		DeviceInfo: protocol.DeviceInfo{
			ProductName:     r.config.DeviceInfo.ProductName,
			Manufacturer:    r.config.DeviceInfo.Manufacturer,
			SoftwareVersion: r.config.DeviceInfo.SoftwareVersion,
		},
		PlayerV1Support: protocol.PlayerV1Support{
			SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 176400, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 96000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 88200, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
				{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
			},
			BufferCapacity:    1048576,
			SupportedCommands: []string{"volume", "mute"},
		},
		ArtworkV1Support: &protocol.ArtworkV1Support{
			Channels: []protocol.ArtworkChannel{
				{Source: "album", Format: "jpeg", MediaWidth: 600, MediaHeight: 600},
			},
		},
		VisualizerV1Support: &protocol.VisualizerV1Support{
			BufferCapacity: 1048576,
		},
	}

	r.client = protocol.NewClient(clientConfig)

	if err := r.client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	log.Printf("Connected to server: %s", r.serverAddr)
	r.connected = true

	if err := r.performInitialSync(); err != nil {
		log.Printf("Initial clock sync failed: %v", err)
	}

	go r.watchConnection()
	go r.clockSyncLoop()

	return nil
}

// watchConnection monitors the protocol client and cancels the receiver context
// if the connection is lost, ensuring all goroutines exit cleanly.
func (r *Receiver) watchConnection() {
	select {
	case <-r.client.Done():
		log.Printf("Server connection lost, shutting down receiver")
		r.connected = false
		r.notifyError(fmt.Errorf("server connection lost"))
		r.cancel()
	case <-r.ctx.Done():
		return
	}
}

// performInitialSync does multiple sync rounds before audio starts.
func (r *Receiver) performInitialSync() error {
	log.Printf("Performing initial clock synchronization...")

	for i := 0; i < 5; i++ {
		t1 := time.Now().UnixMicro()
		r.client.SendTimeSync(t1)

		select {
		case resp := <-r.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			r.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)
		case <-time.After(500 * time.Millisecond):
			log.Printf("Initial sync round %d timeout", i+1)
		}

		time.Sleep(100 * time.Millisecond)
	}

	rtt, quality := r.clockSync.GetStats()
	log.Printf("Initial clock sync complete: rtt=%dus, quality=%v", rtt, quality)

	return nil
}

// clockSyncLoop continuously syncs the clock with the server.
func (r *Receiver) clockSyncLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for {
				select {
				case <-r.client.TimeSyncResp:
					log.Printf("Discarded stale time sync response")
				default:
					goto sendRequest
				}
			}

		sendRequest:
			t1 := time.Now().UnixMicro()
			r.client.SendTimeSync(t1)

		case resp := <-r.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			r.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)

		case <-r.ctx.Done():
			return
		}
	}
}

// notifyError calls the OnError callback if set, otherwise logs the error.
func (r *Receiver) notifyError(err error) {
	if r.config.OnError != nil {
		r.config.OnError(err)
	} else {
		log.Printf("Receiver error: %v", err)
	}
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
