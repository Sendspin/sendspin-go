// ABOUTME: Receiver handles connection, sync, decode, and scheduling
// ABOUTME: Emits decoded audio.Buffer via Output() channel for consumers
package sendspin

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	stdsync "sync"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
)

// Time-sync burst parameters. Mirrors sendspin-cpp's TimeBurst defaults and
// the upstream Sendspin/time-filter README "Recommended Usage" guidance: a
// short burst of NTP-style exchanges, each waiting for its reply, with the
// best (lowest RTT) sample fed to the filter once per burst.
const (
	timeSyncBurstSize       = 8
	timeSyncBurstInterval   = 10 * time.Second
	timeSyncResponseTimeout = 500 * time.Millisecond
)

// metadataApplyTickInterval is the cadence at which metadataApplyLoop wakes
// to drain pending updates whose server timestamp has elapsed. 100 ms is
// imperceptible for metadata display lag; do not shorten without a real
// reason.
const metadataApplyTickInterval = 100 * time.Millisecond

type ReceiverConfig struct {
	ServerAddr     string
	PlayerName     string
	BufferMs       int
	StaticDelayMs  int    // optional static latency compensation (ms) applied to every scheduled play time
	PreferredCodec string // "pcm", "opus", or "flac" — reorders the advertised format list so the server picks this codec first
	BufferCapacity int    // buffer_capacity in bytes advertised to the server (default: 1048576 = 1MB)
	// MaxSampleRate caps the highest SampleRate advertised to the server.
	// 0 = no cap. Set this when the eventual audio output device cannot
	// sustain higher rates (e.g. Pi3 onboard bcm2835 headphones can't
	// actually drain 192k even though ALSA reports it accepts the format).
	MaxSampleRate int
	// MaxBitDepth caps the highest BitDepth advertised to the server.
	// 0 = no cap. See MaxSampleRate for the motivating case.
	MaxBitDepth int
	// ClientID is the already-resolved client_id to advertise in client/hello.
	// Required — callers should compute this once at startup (typically via
	// ResolveClientID) and thread the same value through reconnects.
	ClientID       string
	DeviceInfo     DeviceInfo
	DecoderFactory func(audio.Format) (decode.Decoder, error)
	// OnMetadata is invoked after each server metadata update is merged
	// onto the running snapshot. It may be called from either the
	// server-state reader goroutine (immediate updates) or the metadata
	// apply-loop goroutine (timestamp-deferred updates), and is invoked
	// while an internal mutex is held — callbacks must not block on
	// other Receiver methods. Implementations should serialize their
	// own state if needed.
	OnMetadata    func(Metadata)
	OnStreamStart func(audio.Format)
	OnStreamEnd   func()
	OnError       func(error)
}

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

	// Metadata merge state. mergedMetadata is the running snapshot fed to
	// OnMetadata; pendingMetadata holds future-dated updates sorted by
	// ascending Timestamp until clockNow() crosses each one.
	metadataMu      stdsync.Mutex
	mergedMetadata  Metadata
	pendingMetadata []*protocol.MetadataState

	// clockNow returns "current server time in microseconds". Indirected
	// from r.clockSync.ServerMicrosNow so tests can drive the
	// timestamp-deferral path with a fake clock.
	clockNow func() int64
}

// NewReceiver creates a new Receiver with the given configuration.
// ServerAddr is required; other fields have defaults.
func NewReceiver(config ReceiverConfig) (*Receiver, error) {
	if config.ServerAddr == "" {
		return nil, fmt.Errorf("ReceiverConfig.ServerAddr is required")
	}

	if config.BufferMs == 0 {
		config.BufferMs = 500
	}
	if config.BufferCapacity == 0 {
		config.BufferCapacity = 1048576 // 1MB default
	}
	if config.DeviceInfo.ProductName == "" {
		config.DeviceInfo.ProductName = "Sendspin Player"
	}
	if config.DeviceInfo.Manufacturer == "" {
		config.DeviceInfo.Manufacturer = "Sendspin"
	}
	if config.DeviceInfo.SoftwareVersion == "" {
		config.DeviceInfo.SoftwareVersion = "1.3.0"
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
	r.clockNow = r.clockSync.ServerMicrosNow

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

// Done returns a channel that is closed when the receiver's context is
// cancelled — either by Close() or by watchConnection detecting a dropped
// protocol client. Callers can use this to implement reconnect loops.
func (r *Receiver) Done() <-chan struct{} {
	return r.ctx.Done()
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
	if r.config.ClientID == "" {
		return fmt.Errorf("ReceiverConfig.ClientID is required (resolve via sendspin.ResolveClientID)")
	}

	supportedFormats := buildSupportedFormats(r.config.PreferredCodec, r.config.MaxSampleRate, r.config.MaxBitDepth)
	logAdvertisedFormats(supportedFormats, r.config.MaxSampleRate, r.config.MaxBitDepth)

	clientConfig := protocol.Config{
		ServerAddr: r.serverAddr,
		ClientID:   r.config.ClientID,
		Name:       r.config.PlayerName,
		Version:    1,
		DeviceInfo: protocol.DeviceInfo{
			ProductName:     r.config.DeviceInfo.ProductName,
			Manufacturer:    r.config.DeviceInfo.Manufacturer,
			SoftwareVersion: r.config.DeviceInfo.SoftwareVersion,
		},
		PlayerV1Support: protocol.PlayerV1Support{
			SupportedFormats:  supportedFormats,
			BufferCapacity:    r.config.BufferCapacity,
			SupportedCommands: []string{"volume", "mute"},
		},
		ArtworkV1Support: &protocol.ArtworkV1Support{
			Channels: []protocol.ArtworkChannel{
				{Source: "album", Format: "jpeg", MediaWidth: 600, MediaHeight: 600},
			},
		},
		VisualizerV1Support: &protocol.VisualizerV1Support{
			BufferCapacity: r.config.BufferCapacity,
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
	go r.handleStreamStart()
	go r.handleStreamClear()
	go r.handleStreamEnd()
	go r.handleAudioChunks()
	go r.handleServerState()
	go r.handleGroupUpdates()
	go r.metadataApplyLoop()

	return nil
}

func (r *Receiver) handleStreamStart() {
	for {
		select {
		case start := <-r.client.StreamStart:
			if start.Player == nil {
				log.Printf("Received stream/start with no player info")
				continue
			}

			log.Printf("Stream starting: %s %dHz %dch %dbit",
				start.Player.Codec, start.Player.SampleRate, start.Player.Channels, start.Player.BitDepth)

			format := audio.Format{
				Codec:      start.Player.Codec,
				SampleRate: start.Player.SampleRate,
				Channels:   start.Player.Channels,
				BitDepth:   start.Player.BitDepth,
			}

			if start.Player.CodecHeader != "" {
				headerBytes, err := base64.StdEncoding.DecodeString(start.Player.CodecHeader)
				if err != nil {
					log.Printf("Failed to decode codec_header: %v", err)
				} else {
					format.CodecHeader = headerBytes
				}
			}

			var decoder decode.Decoder
			var err error

			if r.config.DecoderFactory != nil {
				decoder, err = r.config.DecoderFactory(format)
			} else {
				decoder, err = r.defaultDecoder(format)
			}

			if err != nil {
				r.notifyError(fmt.Errorf("failed to create decoder: %w", err))
				continue
			}
			r.decoder = decoder
			r.format = format

			if r.config.OnStreamStart != nil {
				r.config.OnStreamStart(format)
			}

			if r.schedulerCancel != nil {
				r.schedulerCancel()
			}
			if r.scheduler != nil {
				r.scheduler.Stop()
			}

			r.schedulerCtx, r.schedulerCancel = context.WithCancel(r.ctx)
			r.scheduler = NewScheduler(r.clockSync, r.config.BufferMs, r.config.StaticDelayMs)
			go r.scheduler.Run()
			go r.pumpSchedulerOutput(r.schedulerCtx)

		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) defaultDecoder(format audio.Format) (decode.Decoder, error) {
	switch format.Codec {
	case "pcm":
		return decode.NewPCM(format)
	case "opus":
		return decode.NewOpus(format)
	case "flac":
		return decode.NewFLAC(format)
	default:
		return nil, fmt.Errorf("unsupported codec: %s", format.Codec)
	}
}

func (r *Receiver) handleAudioChunks() {
	for {
		select {
		case chunk := <-r.client.AudioChunks:
			if r.decoder == nil || r.scheduler == nil {
				continue
			}

			pcm, err := r.decoder.Decode(chunk.Data)
			if err != nil {
				r.notifyError(fmt.Errorf("decode error: %w", err))
				continue
			}
			if len(pcm) == 0 {
				continue
			}

			buf := audio.Buffer{
				Timestamp: chunk.Timestamp,
				Samples:   pcm,
				Format:    r.format,
			}
			r.scheduler.Schedule(buf)

		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) pumpSchedulerOutput(ctx context.Context) {
	for {
		select {
		case buf := <-r.scheduler.Output():
			select {
			case r.output <- buf:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleStreamClear() {
	for {
		select {
		case clear := <-r.client.StreamClear:
			log.Printf("Stream clear received for roles: %v", clear.Roles)
			if len(clear.Roles) == 0 || containsRole(clear.Roles, "player") {
				if r.scheduler != nil {
					r.scheduler.Clear()
				}
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Receiver) handleStreamEnd() {
	for {
		select {
		case end := <-r.client.StreamEnd:
			log.Printf("Stream end received for roles: %v", end.Roles)
			if len(end.Roles) == 0 || containsRole(end.Roles, "player") {
				if r.config.OnStreamEnd != nil {
					r.config.OnStreamEnd()
				}
			}
		case <-r.ctx.Done():
			return
		}
	}
}

// handleServerState reads server/state messages from the protocol client
// and feeds metadata updates into the merge layer. Non-metadata fields
// of ServerStateMessage are not used today; if/when they grow handlers
// they should branch off here.
func (r *Receiver) handleServerState() {
	for {
		select {
		case state := <-r.client.ServerState:
			if state.Metadata != nil {
				r.enqueueMetadata(state.Metadata)
			}
		case <-r.ctx.Done():
			return
		}
	}
}

// enqueueMetadata accepts a server metadata update and either applies it
// immediately (timestamp <= current server time, or zero timestamp) or
// queues it for future application sorted by ascending timestamp.
//
// Zero / negative timestamps apply immediately. Per spec, MetadataState
// always carries a timestamp, but defending against malformed servers
// costs nothing and keeps existing snapshot-style emitters working.
func (r *Receiver) enqueueMetadata(m *protocol.MetadataState) {
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()

	serverNow := r.clockNow()

	if m.Timestamp <= 0 || m.Timestamp <= serverNow {
		r.applyMetadataLocked(m)
		return
	}

	// Insertion-sort into pendingMetadata by ascending Timestamp.
	idx := sort.Search(len(r.pendingMetadata), func(i int) bool {
		return r.pendingMetadata[i].Timestamp >= m.Timestamp
	})
	r.pendingMetadata = append(r.pendingMetadata, nil)
	copy(r.pendingMetadata[idx+1:], r.pendingMetadata[idx:])
	r.pendingMetadata[idx] = m
}

// applyMetadataLocked merges the update onto mergedMetadata per tristate
// rules and fires OnMetadata. Caller must hold r.metadataMu.
//
// For each field: if the wire key was absent, preserve the prior value;
// if present and null (pointer is nil after decode), reset to zero; if
// present with a value, replace. The progress field is atomic per spec —
// a non-null progress always carries all three fields, so we either take
// TrackDuration or zero Duration.
func (r *Receiver) applyMetadataLocked(m *protocol.MetadataState) {
	if m.HasField("title") {
		if m.Title != nil {
			r.mergedMetadata.Title = *m.Title
		} else {
			r.mergedMetadata.Title = ""
		}
	}
	if m.HasField("artist") {
		if m.Artist != nil {
			r.mergedMetadata.Artist = *m.Artist
		} else {
			r.mergedMetadata.Artist = ""
		}
	}
	if m.HasField("album") {
		if m.Album != nil {
			r.mergedMetadata.Album = *m.Album
		} else {
			r.mergedMetadata.Album = ""
		}
	}
	if m.HasField("album_artist") {
		if m.AlbumArtist != nil {
			r.mergedMetadata.AlbumArtist = *m.AlbumArtist
		} else {
			r.mergedMetadata.AlbumArtist = ""
		}
	}
	if m.HasField("artwork_url") {
		if m.ArtworkURL != nil {
			r.mergedMetadata.ArtworkURL = *m.ArtworkURL
		} else {
			r.mergedMetadata.ArtworkURL = ""
		}
	}
	if m.HasField("track") {
		if m.Track != nil {
			r.mergedMetadata.Track = *m.Track
		} else {
			r.mergedMetadata.Track = 0
		}
	}
	if m.HasField("year") {
		if m.Year != nil {
			r.mergedMetadata.Year = *m.Year
		} else {
			r.mergedMetadata.Year = 0
		}
	}
	if m.HasField("progress") {
		if m.Progress != nil {
			r.mergedMetadata.Duration = m.Progress.TrackDuration / 1000
		} else {
			r.mergedMetadata.Duration = 0
		}
	}

	snapshot := r.mergedMetadata
	if r.config.OnMetadata != nil {
		r.config.OnMetadata(snapshot)
	}
}

// metadataApplyLoop drains pendingMetadata as server time crosses each
// queued update's timestamp. Started as a goroutine in Connect.
func (r *Receiver) metadataApplyLoop() {
	ticker := time.NewTicker(metadataApplyTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.drainPendingMetadata()
		case <-r.ctx.Done():
			return
		}
	}
}

// drainPendingMetadata applies every pending update whose timestamp has
// elapsed, in ascending timestamp order. Pending is kept sorted by
// enqueueMetadata, so we can stop at the first future-dated entry.
func (r *Receiver) drainPendingMetadata() {
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()

	serverNow := r.clockNow()
	applied := 0
	for _, m := range r.pendingMetadata {
		if m.Timestamp > serverNow {
			break
		}
		r.applyMetadataLocked(m)
		applied++
	}
	if applied > 0 {
		r.pendingMetadata = r.pendingMetadata[applied:]
	}
}

func (r *Receiver) handleGroupUpdates() {
	for {
		select {
		case update := <-r.client.GroupUpdate:
			if update.PlaybackState != nil {
				state := *update.PlaybackState
				log.Printf("Group playback state: %s", state)
				if (state == "paused" || state == "stopped") && r.scheduler != nil {
					r.scheduler.Clear()
				}
			}
			if update.GroupID != nil {
				log.Printf("Joined group: %s", *update.GroupID)
			}
		case <-r.ctx.Done():
			return
		}
	}
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

// performInitialSync drives a single immediate burst so the filter has
// multiple samples before the audio scheduler starts.
func (r *Receiver) performInitialSync() error {
	log.Printf("Performing initial clock synchronization (burst of %d)...", timeSyncBurstSize)
	r.runTimeSyncBurst(timeSyncBurstSize)

	rtt, quality := r.clockSync.GetStats()
	log.Printf("Initial clock sync complete: rtt=%dus, quality=%v", rtt, quality)
	return nil
}

// clockSyncLoop fires a time-sync burst every timeSyncBurstInterval. The old
// per-second single-message pattern was replaced by the burst-best strategy
// recommended by the upstream Sendspin/time-filter README and implemented by
// sendspin-cpp's TimeBurst — it converges faster and rejects high-RTT
// outliers without an explicit threshold.
func (r *Receiver) clockSyncLoop() {
	ticker := time.NewTicker(timeSyncBurstInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.runTimeSyncBurst(timeSyncBurstSize)
		case <-r.ctx.Done():
			return
		}
	}
}

// runTimeSyncBurst sends `size` time messages back-to-back, each waiting for
// its reply, tracks the sample with the lowest RTT, and feeds only that best
// sample to the clock-sync filter at burst end. Mirrors sendspin-cpp's
// TimeBurst loop. Strictly serial — bursts run on TCP/WebSocket where a
// delayed earlier message also delays its successors, so parallel sends
// would not give independent RTT measurements.
func (r *Receiver) runTimeSyncBurst(size int) {
	// Drain any responses left over from a prior burst (e.g. a timed-out
	// reply that arrived after the per-message timeout fired). Keeps the
	// next-message recv from picking up a stale sample.
drainLoop:
	for {
		select {
		case <-r.client.TimeSyncResp:
		default:
			break drainLoop
		}
	}

	var (
		bestT1, bestT2, bestT3, bestT4 int64
		bestRTT                        int64 = math.MaxInt64
		valid                                = 0
	)

	for i := 0; i < size; i++ {
		t1 := time.Now().UnixMicro()
		if err := r.client.SendTimeSync(t1); err != nil {
			log.Printf("Burst send %d/%d failed: %v", i+1, size, err)
			continue
		}

		select {
		case resp := <-r.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			rtt := (t4 - resp.ClientTransmitted) - (resp.ServerTransmitted - resp.ServerReceived)
			if rtt < bestRTT {
				bestRTT = rtt
				bestT1 = resp.ClientTransmitted
				bestT2 = resp.ServerReceived
				bestT3 = resp.ServerTransmitted
				bestT4 = t4
			}
			valid++
		case <-time.After(timeSyncResponseTimeout):
			log.Printf("Burst sample %d/%d timed out", i+1, size)
		case <-r.ctx.Done():
			return
		}
	}

	if valid == 0 {
		log.Printf("Burst produced 0 valid samples; filter not updated")
		return
	}

	r.clockSync.ProcessSyncResponse(bestT1, bestT2, bestT3, bestT4)
}

// buildSupportedFormats returns the player's advertised format list,
// optionally filtered by maxSampleRate / maxBitDepth (0 = no cap) and
// reordered so preferredCodec entries come first when set.
//
// Filter happens before reorder, so the surviving preferred-codec entries
// stay grouped at the head. Returns an empty slice when the caps exclude
// every format — callers can detect that and fail loudly. We do NOT
// fabricate a fallback entry: if the user asks for caps no format can
// satisfy, the honest response is empty, and the resulting handshake
// failure is the right user-visible signal.
func buildSupportedFormats(preferredCodec string, maxSampleRate, maxBitDepth int) []protocol.AudioFormat {
	allFormats := []protocol.AudioFormat{
		{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: 24},
		{Codec: "pcm", Channels: 2, SampleRate: 176400, BitDepth: 24},
		{Codec: "pcm", Channels: 2, SampleRate: 96000, BitDepth: 24},
		{Codec: "pcm", Channels: 2, SampleRate: 88200, BitDepth: 24},
		{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
		{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
		{Codec: "flac", Channels: 2, SampleRate: 192000, BitDepth: 24},
		{Codec: "flac", Channels: 2, SampleRate: 96000, BitDepth: 24},
		{Codec: "flac", Channels: 2, SampleRate: 48000, BitDepth: 24},
		{Codec: "flac", Channels: 2, SampleRate: 44100, BitDepth: 16},
		{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
	}

	filtered := make([]protocol.AudioFormat, 0, len(allFormats))
	for _, f := range allFormats {
		if maxSampleRate > 0 && f.SampleRate > maxSampleRate {
			continue
		}
		if maxBitDepth > 0 && f.BitDepth > maxBitDepth {
			continue
		}
		filtered = append(filtered, f)
	}

	if preferredCodec == "" {
		return filtered
	}

	// Move preferred codec formats to the front while preserving original
	// order within each group.
	preferred := make([]protocol.AudioFormat, 0, len(filtered))
	rest := make([]protocol.AudioFormat, 0, len(filtered))
	for _, f := range filtered {
		if f.Codec == preferredCodec {
			preferred = append(preferred, f)
		} else {
			rest = append(rest, f)
		}
	}
	return append(preferred, rest...)
}

// logAdvertisedFormats writes one summary line describing what the player
// is about to send in client/hello — codec set, max rate, max depth, and
// the cap that produced the list. Operators reading the log can answer
// "what did this player say it could do?" without having to inspect server
// traces.
//
// Empty list goes out as a WARNING: the resulting handshake produces only
// a generic negotiation failure, so surfacing the cause player-side saves
// users from chasing the same symptom on the server.
func logAdvertisedFormats(formats []protocol.AudioFormat, maxSampleRate, maxBitDepth int) {
	capDesc := "no cap"
	if maxSampleRate > 0 || maxBitDepth > 0 {
		capDesc = fmt.Sprintf("cap %dHz/%d-bit", maxSampleRate, maxBitDepth)
	}
	if len(formats) == 0 {
		log.Printf("WARNING: advertising 0 supported formats (%s) — handshake will fail; relax the caps", capDesc)
		return
	}
	codecsSeen := make(map[string]struct{}, 3)
	codecsOrdered := make([]string, 0, 3)
	var maxRate, maxDepth int
	for _, f := range formats {
		if _, ok := codecsSeen[f.Codec]; !ok {
			codecsSeen[f.Codec] = struct{}{}
			codecsOrdered = append(codecsOrdered, f.Codec)
		}
		if f.SampleRate > maxRate {
			maxRate = f.SampleRate
		}
		if f.BitDepth > maxDepth {
			maxDepth = f.BitDepth
		}
	}
	log.Printf("Advertising %d supported formats: codecs=[%s] max=%dHz/%d-bit (%s)",
		len(formats), strings.Join(codecsOrdered, ","), maxRate, maxDepth, capDesc)
}

func (r *Receiver) notifyError(err error) {
	if r.config.OnError != nil {
		r.config.OnError(err)
	} else {
		log.Printf("Receiver error: %v", err)
	}
}

func (r *Receiver) Close() error {
	// Send goodbye BEFORE cancelling the context so the message
	// reaches the server while the connection is still alive.
	if r.client != nil {
		r.client.SendGoodbye("shutdown")
	}

	r.cancel()

	if r.client != nil {
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
