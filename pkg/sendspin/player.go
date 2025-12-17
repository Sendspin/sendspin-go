// ABOUTME: High-level Player API for Sendspin streaming
// ABOUTME: Provides simple interface for connecting to servers and playing audio
package sendspin

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/audio/output"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
	"github.com/google/uuid"
)

// PlayerConfig holds player configuration
type PlayerConfig struct {
	// ServerAddr is the server address (host:port)
	ServerAddr string

	// PlayerName is the display name for this player
	PlayerName string

	// Volume is the initial volume (0-100)
	Volume int

	// BufferMs is the playback buffer size in milliseconds (default: 500)
	BufferMs int

	// DeviceInfo provides device identification
	DeviceInfo DeviceInfo

	// OnMetadata is called when metadata is received
	OnMetadata func(Metadata)

	// OnStateChange is called when playback state changes
	OnStateChange func(PlayerState)

	// OnError is called when errors occur
	OnError func(error)
}

// DeviceInfo describes the player device
type DeviceInfo struct {
	ProductName     string
	Manufacturer    string
	SoftwareVersion string
}

// Metadata contains track information
type Metadata struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	ArtworkURL  string
	Track       int
	Year        int
	Duration    int // seconds
}

// PlayerState describes the current state
type PlayerState struct {
	State      string // "idle", "playing", "paused"
	Volume     int
	Muted      bool
	Codec      string
	SampleRate int
	Channels   int
	BitDepth   int
	Connected  bool
}

// PlayerStats contains playback statistics
type PlayerStats struct {
	Received    int64
	Played      int64
	Dropped     int64
	BufferDepth int // milliseconds
	SyncRTT     int64
	SyncQuality sync.Quality
}

// Player provides high-level audio playback from Resonate servers
type Player struct {
	config PlayerConfig

	// Components
	client    *protocol.Client
	clockSync *sync.ClockSync
	scheduler *Scheduler
	output    output.Output
	decoder   decode.Decoder

	// State
	state           PlayerState
	ctx             context.Context
	cancel          context.CancelFunc
	serverAddr      string
	schedulerCtx    context.Context    // Context for scheduler goroutines
	schedulerCancel context.CancelFunc // Cancel function for scheduler goroutines
}

// NewPlayer creates a new player with the given configuration
func NewPlayer(config PlayerConfig) (*Player, error) {
	// Set defaults
	if config.Volume == 0 {
		config.Volume = 100
	}
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

	// Create clock sync
	clockSync := sync.NewClockSync()
	sync.SetGlobalClockSync(clockSync)

	// Output will be created based on bit depth when stream starts
	// (oto for 16-bit, malgo for 24-bit)

	player := &Player{
		config:     config,
		clockSync:  clockSync,
		output:     nil, // Created when format is known
		ctx:        ctx,
		cancel:     cancel,
		serverAddr: config.ServerAddr,
		state: PlayerState{
			State:     "idle",
			Volume:    config.Volume,
			Muted:     false,
			Connected: false,
		},
	}

	return player, nil
}

// Connect establishes connection to the server and performs initial setup
func (p *Player) Connect() error {
	clientID := uuid.New().String()

	// Configure protocol client with spec-compliant formats
	clientConfig := protocol.Config{
		ServerAddr: p.serverAddr,
		ClientID:   clientID,
		Name:       p.config.PlayerName,
		Version:    1,
		DeviceInfo: protocol.DeviceInfo{
			ProductName:     p.config.DeviceInfo.ProductName,
			Manufacturer:    p.config.DeviceInfo.Manufacturer,
			SoftwareVersion: p.config.DeviceInfo.SoftwareVersion,
		},
		PlayerV1Support: protocol.PlayerV1Support{
			// Per spec: supported_formats in priority order
			SupportedFormats: []protocol.AudioFormat{
				// PCM hi-res - highest quality first
				{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 176400, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 96000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 88200, BitDepth: 24},
				// PCM standard quality
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
				// Opus fallback
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

	p.client = protocol.NewClient(clientConfig)

	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	log.Printf("Connected to server: %s", p.serverAddr)
	p.state.Connected = true
	p.notifyStateChange()

	// Perform initial clock sync
	if err := p.performInitialSync(); err != nil {
		log.Printf("Initial clock sync failed: %v", err)
	}

	// Start component goroutines
	go p.handleStreamStart()
	go p.handleStreamClear()
	go p.handleStreamEnd()
	go p.handleAudioChunks()
	go p.handleControls()
	go p.handleServerState()
	go p.handleGroupUpdates()
	go p.clockSyncLoop()

	return nil
}

// performInitialSync does multiple sync rounds before audio starts
func (p *Player) performInitialSync() error {
	log.Printf("Performing initial clock synchronization...")

	for i := 0; i < 5; i++ {
		t1 := time.Now().UnixMicro()
		p.client.SendTimeSync(t1)

		select {
		case resp := <-p.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			p.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)

		case <-time.After(500 * time.Millisecond):
			log.Printf("Initial sync round %d timeout", i+1)
		}

		time.Sleep(100 * time.Millisecond)
	}

	rtt, quality := p.clockSync.GetStats()
	log.Printf("Initial clock sync complete: rtt=%dμs, quality=%v", rtt, quality)

	return nil
}

// clockSyncLoop continuously syncs clock
func (p *Player) clockSyncLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Drain stale responses
			for {
				select {
				case <-p.client.TimeSyncResp:
					log.Printf("Discarded stale time sync response")
				default:
					goto sendRequest
				}
			}

		sendRequest:
			t1 := time.Now().UnixMicro()
			p.client.SendTimeSync(t1)

		case resp := <-p.client.TimeSyncResp:
			t4 := time.Now().UnixMicro()
			p.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)

		case <-p.ctx.Done():
			return
		}
	}
}

// handleStreamStart initializes decoder and output
func (p *Player) handleStreamStart() {
	for {
		select {
		case start := <-p.client.StreamStart:
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

			// Initialize decoder
			var decoder decode.Decoder
			var err error

			switch format.Codec {
			case "pcm":
				decoder, err = decode.NewPCM(format)
			case "opus":
				decoder, err = decode.NewOpus(format)
			case "flac":
				decoder, err = decode.NewFLAC(format)
			case "mp3":
				decoder, err = decode.NewMP3(format)
			default:
				err = fmt.Errorf("unsupported codec: %s", format.Codec)
			}

			if err != nil {
				p.notifyError(fmt.Errorf("failed to create decoder: %w", err))
				continue
			}
			p.decoder = decoder

			// Create appropriate output backend based on bit depth
			// Use oto for 16-bit (Music Assistant compatibility)
			// Use malgo for 24-bit (true hi-res support)
			if p.output == nil {
				if format.BitDepth <= 16 {
					p.output = output.NewOto()
					log.Printf("Using oto backend for %d-bit audio", format.BitDepth)
				} else {
					p.output = output.NewMalgo()
					log.Printf("Using malgo backend for %d-bit audio", format.BitDepth)
				}
			}

			// Initialize output
			if err := p.output.Open(format.SampleRate, format.Channels, format.BitDepth); err != nil {
				p.notifyError(fmt.Errorf("failed to initialize output: %w", err))
				continue
			}

			// Update state
			p.state.Codec = format.Codec
			p.state.SampleRate = format.SampleRate
			p.state.Channels = format.Channels
			p.state.BitDepth = format.BitDepth
			p.state.State = "playing"
			p.notifyStateChange()

			// Stop any existing scheduler goroutines before starting new ones
			if p.schedulerCancel != nil {
				p.schedulerCancel()
			}
			if p.scheduler != nil {
				p.scheduler.Stop()
			}

			// Create new scheduler context
			p.schedulerCtx, p.schedulerCancel = context.WithCancel(p.ctx)

			// Initialize scheduler
			p.scheduler = NewScheduler(p.clockSync, p.config.BufferMs)
			go p.scheduler.Run()
			go p.handleScheduledAudio(p.schedulerCtx)

		case <-p.ctx.Done():
			return
		}
	}
}

// handleAudioChunks decodes and schedules audio
func (p *Player) handleAudioChunks() {
	for {
		select {
		case chunk := <-p.client.AudioChunks:
			if p.decoder == nil || p.scheduler == nil {
				continue
			}

			// Decode
			pcm, err := p.decoder.Decode(chunk.Data)
			if err != nil {
				p.notifyError(fmt.Errorf("decode error: %w", err))
				continue
			}

			// Schedule
			buf := audio.Buffer{
				Timestamp: chunk.Timestamp,
				Samples:   pcm,
			}
			p.scheduler.Schedule(buf)

		case <-p.ctx.Done():
			return
		}
	}
}

// handleScheduledAudio plays scheduled buffers
func (p *Player) handleScheduledAudio(ctx context.Context) {
	for {
		select {
		case buf := <-p.scheduler.Output():
			if err := p.output.Write(buf.Samples); err != nil {
				p.notifyError(fmt.Errorf("playback error: %w", err))
			}

		case <-ctx.Done():
			return
		}
	}
}

// handleControls processes server commands
func (p *Player) handleControls() {
	for {
		select {
		case cmd := <-p.client.ControlMsgs:
			switch cmd.Command {
			case "volume":
				p.SetVolume(cmd.Volume)

			case "mute":
				p.Mute(cmd.Mute)
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// handleStreamClear processes stream/clear messages (for seek)
func (p *Player) handleStreamClear() {
	for {
		select {
		case clear := <-p.client.StreamClear:
			log.Printf("Stream clear received for roles: %v", clear.Roles)
			// Clear buffers if player role is specified (or if empty = all roles)
			if len(clear.Roles) == 0 || containsRole(clear.Roles, "player") {
				if p.scheduler != nil {
					p.scheduler.Clear()
				}
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// handleStreamEnd processes stream/end messages
func (p *Player) handleStreamEnd() {
	for {
		select {
		case end := <-p.client.StreamEnd:
			log.Printf("Stream end received for roles: %v", end.Roles)
			// Stop playback if player role is specified (or if empty = all roles)
			if len(end.Roles) == 0 || containsRole(end.Roles, "player") {
				p.state.State = "idle"
				p.notifyStateChange()
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// containsRole checks if a role is in the list
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// handleServerState processes server/state messages (includes metadata)
func (p *Player) handleServerState() {
	for {
		select {
		case state := <-p.client.ServerState:
			if state.Metadata != nil && p.config.OnMetadata != nil {
				meta := state.Metadata
				p.config.OnMetadata(Metadata{
					Title:       derefString(meta.Title),
					Artist:      derefString(meta.Artist),
					Album:       derefString(meta.Album),
					AlbumArtist: derefString(meta.AlbumArtist),
					ArtworkURL:  derefString(meta.ArtworkURL),
					Track:       derefInt(meta.Track),
					Year:        derefInt(meta.Year),
					Duration:    getDurationSeconds(meta.Progress),
				})
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// handleGroupUpdates processes group/update messages
func (p *Player) handleGroupUpdates() {
	for {
		select {
		case update := <-p.client.GroupUpdate:
			if update.PlaybackState != nil {
				log.Printf("Group playback state: %s", *update.PlaybackState)
			}
			if update.GroupID != nil {
				log.Printf("Joined group: %s", *update.GroupID)
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// derefString safely dereferences a string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt safely dereferences an int pointer
func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// getDurationSeconds extracts duration in seconds from progress
func getDurationSeconds(p *protocol.ProgressState) int {
	if p == nil {
		return 0
	}
	return p.TrackDuration / 1000
}

// Play starts or resumes playback
func (p *Player) Play() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}

	p.state.State = "playing"
	p.notifyStateChange()

	// Per spec: player state is "synchronized" or "error"
	return p.client.SendState(protocol.PlayerState{
		State:  "synchronized",
		Volume: p.state.Volume,
		Muted:  p.state.Muted,
	})
}

// Pause pauses playback
func (p *Player) Pause() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}

	p.state.State = "paused"
	p.notifyStateChange()

	// Per spec: player state is "synchronized" or "error"
	return p.client.SendState(protocol.PlayerState{
		State:  "synchronized",
		Volume: p.state.Volume,
		Muted:  p.state.Muted,
	})
}

// Stop stops playback
func (p *Player) Stop() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}

	p.state.State = "idle"
	p.notifyStateChange()

	// Per spec: player state is "synchronized" or "error"
	return p.client.SendState(protocol.PlayerState{
		State:  "synchronized",
		Volume: p.state.Volume,
		Muted:  p.state.Muted,
	})
}

// SetVolume sets the volume (0-100)
func (p *Player) SetVolume(volume int) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}

	p.state.Volume = volume

	// Apply to output
	if oto, ok := p.output.(*output.Oto); ok {
		oto.SetVolume(volume)
	}

	// Send state to server per spec
	if p.client != nil && p.state.Connected {
		p.client.SendState(protocol.PlayerState{
			State:  "synchronized",
			Volume: volume,
			Muted:  p.state.Muted,
		})
	}

	p.notifyStateChange()
	return nil
}

// Mute sets the mute state
func (p *Player) Mute(muted bool) error {
	p.state.Muted = muted

	// Apply to output
	if oto, ok := p.output.(*output.Oto); ok {
		oto.SetMuted(muted)
	}

	// Send state to server per spec
	if p.client != nil && p.state.Connected {
		p.client.SendState(protocol.PlayerState{
			State:  "synchronized",
			Volume: p.state.Volume,
			Muted:  muted,
		})
	}

	p.notifyStateChange()
	return nil
}

// Status returns the current player state
func (p *Player) Status() PlayerState {
	return p.state
}

// Stats returns playback statistics
func (p *Player) Stats() PlayerStats {
	stats := PlayerStats{}

	if p.scheduler != nil {
		s := p.scheduler.Stats()
		stats.Received = s.Received
		stats.Played = s.Played
		stats.Dropped = s.Dropped
		stats.BufferDepth = p.scheduler.BufferDepth()
	}

	if p.clockSync != nil {
		rtt, quality := p.clockSync.GetStats()
		stats.SyncRTT = rtt
		stats.SyncQuality = quality
	}

	return stats
}

// Close closes the player and releases all resources
func (p *Player) Close() error {
	p.cancel()

	if p.client != nil {
		// Send goodbye before closing per spec
		p.client.SendGoodbye("shutdown")
		p.client.Close()
	}

	if p.scheduler != nil {
		p.scheduler.Stop()
	}

	if p.decoder != nil {
		p.decoder.Close()
	}

	if p.output != nil {
		p.output.Close()
	}

	p.state.Connected = false
	p.state.State = "idle"
	p.notifyStateChange()

	return nil
}

// notifyStateChange calls the OnStateChange callback if set
func (p *Player) notifyStateChange() {
	if p.config.OnStateChange != nil {
		p.config.OnStateChange(p.state)
	}
}

// notifyError calls the OnError callback if set
func (p *Player) notifyError(err error) {
	if p.config.OnError != nil {
		p.config.OnError(err)
	} else {
		log.Printf("Player error: %v", err)
	}
}
