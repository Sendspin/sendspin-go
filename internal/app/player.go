// ABOUTME: Main player application orchestration
// ABOUTME: Coordinates all components (connection, audio, UI)
package app

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/Sendspin/sendspin-go/internal/artwork"
	"github.com/Sendspin/sendspin-go/internal/audio"
	"github.com/Sendspin/sendspin-go/internal/client"
	"github.com/Sendspin/sendspin-go/internal/discovery"
	"github.com/Sendspin/sendspin-go/internal/player"
	"github.com/Sendspin/sendspin-go/internal/sync"
	"github.com/Sendspin/sendspin-go/internal/ui"
	"github.com/Sendspin/sendspin-go/internal/version"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Config holds player configuration
type Config struct {
	ServerAddr string
	Port       int
	Name       string
	BufferMs   int
	UseTUI     bool
}

// Player represents the main player application
type Player struct {
	config          Config
	client          *client.Client
	clockSync       *sync.ClockSync
	scheduler       *player.Scheduler
	schedulerCtx    context.Context    // Context for scheduler goroutines
	schedulerCancel context.CancelFunc // Cancel function for scheduler goroutines
	output          *player.Output
	discovery       *discovery.Manager
	decoder         audio.Decoder
	tuiProg         *tea.Program
	volumeCtrl      *ui.VolumeControl
	artwork         *artwork.Downloader
	ctx             context.Context
	cancel          context.CancelFunc
	playerState     string // "idle" or "playing"
}

// New creates a new player
func New(config Config) *Player {
	ctx, cancel := context.WithCancel(context.Background())

	clockSync := sync.NewClockSync()
	sync.SetGlobalClockSync(clockSync) // Make it globally accessible for CurrentMicros()

	artworkDL, err := artwork.NewDownloader()
	if err != nil {
		log.Printf("Failed to create artwork downloader: %v", err)
	}

	return &Player{
		config:      config,
		clockSync:   clockSync,
		output:      player.NewOutput(),
		artwork:     artworkDL,
		ctx:         ctx,
		cancel:      cancel,
		playerState: "idle", // Start in idle state
	}
}

// Start starts the player
func (p *Player) Start() error {
	// Start TUI if enabled
	if p.config.UseTUI {
		p.volumeCtrl = ui.NewVolumeControl()
		tuiProg, err := ui.Run(p.volumeCtrl)
		if err != nil {
			return fmt.Errorf("failed to start TUI: %w", err)
		}
		p.tuiProg = tuiProg
		go p.tuiProg.Run()

		// Start volume control handler
		go p.handleVolumeControl()
	} else {
		log.Printf("TUI disabled - logging to file for debugging")
	}

	// Start discovery if no manual server
	if p.config.ServerAddr == "" {
		p.discovery = discovery.NewManager(discovery.Config{
			ServiceName: p.config.Name,
			Port:        p.config.Port,
		})

		p.discovery.Advertise()
		p.discovery.Browse()

		// Wait for server discovery
		go p.handleDiscovery()
	} else {
		// Connect directly
		if err := p.connect(p.config.ServerAddr); err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
	}

	// Wait for context cancellation
	<-p.ctx.Done()

	return nil
}

// handleDiscovery waits for server discovery
func (p *Player) handleDiscovery() {
	for {
		select {
		case server := <-p.discovery.Servers():
			addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
			log.Printf("Attempting connection to %s", addr)

			if err := p.connect(addr); err != nil {
				log.Printf("Connection failed: %v", err)
				continue
			}
			return

		case <-p.ctx.Done():
			return
		}
	}
}

// connect establishes connection to server
func (p *Player) connect(serverAddr string) error {
	clientID := uuid.New().String()

	clientConfig := client.Config{
		ServerAddr: serverAddr,
		ClientID:   clientID,
		Name:       p.config.Name,
		Version:    1,
		DeviceInfo: protocol.DeviceInfo{
			ProductName:     version.Product,
			Manufacturer:    version.Manufacturer,
			SoftwareVersion: version.Version,
		},
		PlayerSupport: protocol.PlayerSupport{
			// Hi-res formats first (MA provider picks first supported format)
			SupportedFormats: []protocol.AudioFormat{
				// PCM hi-res - highest quality first
				{Codec: "pcm", Channels: 2, SampleRate: 192000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 176400, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 96000, BitDepth: 24},
				{Codec: "pcm", Channels: 2, SampleRate: 88200, BitDepth: 24},
				// PCM standard quality
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
				// Opus fallback (48kHz only - Opus spec requirement)
				{Codec: "opus", Channels: 2, SampleRate: 48000, BitDepth: 16},
			},
			BufferCapacity:    1048576,
			SupportedCommands: []string{"volume", "mute"},
		},
		MetadataSupport: protocol.MetadataSupport{
			SupportPictureFormats: []string{"jpeg", "png", "webp"},
			MediaWidth:            600, // Request 600px artwork (good balance)
			MediaHeight:           600,
		},
		ArtworkSupport: protocol.ArtworkSupport{
			Channels: []protocol.ArtworkChannel{
				{Source: "album", Format: "jpeg", MediaWidth: 600, MediaHeight: 600},
			},
		},
		VisualizerSupport: protocol.VisualizerSupport{
			BufferCapacity: 1048576, // 1MB buffer for visualization data
		},
	}

	p.client = client.NewClient(clientConfig)

	if err := p.client.Connect(); err != nil {
		return err
	}

	log.Printf("Connected to server: %s", serverAddr)

	// Update TUI
	connected := true
	p.updateTUI(ui.StatusMsg{
		Connected:  &connected,
		ServerName: serverAddr,
	})

	// Perform initial clock sync before starting audio handlers
	if err := p.performInitialSync(); err != nil {
		log.Printf("Initial clock sync failed: %v", err)
	}

	// Update TUI with sync status
	rtt, quality := p.clockSync.GetStats()
	p.updateTUI(ui.StatusMsg{
		SyncOffset:  0, // No longer tracking offset, using loop-origin method
		SyncRTT:     rtt,
		SyncQuality: quality,
	})

	// Start component goroutines
	go p.handleAudioChunks()
	go p.handleControls()
	go p.handleStreamStart()
	go p.handleMetadata()
	go p.handleSessionUpdates()
	go p.clockSyncLoop()
	go p.statsUpdateLoop()

	return nil
}

// performInitialSync does multiple sync rounds before audio starts
func (p *Player) performInitialSync() error {
	log.Printf("Performing initial clock synchronization...")

	// Do 5 quick sync rounds to establish server loop origin
	for i := 0; i < 5; i++ {
		t1 := time.Now().UnixMicro() // Client send time (Unix µs)
		p.client.SendTimeSync(t1)

		// Wait for response with timeout
		select {
		case resp := <-p.client.TimeSyncResp:
			t4 := time.Now().UnixMicro() // Client receive time (Unix µs)
			p.clockSync.ProcessSyncResponse(resp.ClientTransmitted, resp.ServerReceived, resp.ServerTransmitted, t4)

		case <-time.After(500 * time.Millisecond):
			log.Printf("Initial sync round %d timeout", i+1)
		}

		// Brief pause between syncs
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
			// Drain any stale responses before sending new request
			for {
				select {
				case <-p.client.TimeSyncResp:
					log.Printf("Discarded stale time sync response")
				default:
					goto sendRequest
				}
			}

		sendRequest:
			t1 := time.Now().UnixMicro() // Client send time (Unix µs)
			p.client.SendTimeSync(t1)

		case resp := <-p.client.TimeSyncResp:
			// Process response asynchronously when it arrives
			t4 := time.Now().UnixMicro() // Client receive time (Unix µs)
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
			// Check if player field exists
			if start.Player == nil {
				log.Printf("Received stream/start with no player info")
				continue
			}

			log.Printf("Stream starting: %s %dHz %dch %dbit",
				start.Player.Codec, start.Player.SampleRate, start.Player.Channels, start.Player.BitDepth)

			// Update TUI with stream info
			p.updateTUI(ui.StatusMsg{
				Codec:      start.Player.Codec,
				SampleRate: start.Player.SampleRate,
				Channels:   start.Player.Channels,
				BitDepth:   start.Player.BitDepth,
			})

			format := audio.Format{
				Codec:      start.Player.Codec,
				SampleRate: start.Player.SampleRate,
				Channels:   start.Player.Channels,
				BitDepth:   start.Player.BitDepth,
			}

			// Initialize decoder
			decoder, err := audio.NewDecoder(format)
			if err != nil {
				log.Printf("Failed to create decoder: %v", err)
				continue
			}
			p.decoder = decoder

			// Initialize output
			if err := p.output.Initialize(format); err != nil {
				log.Printf("Failed to initialize output: %v", err)
				continue
			}

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
			p.scheduler = player.NewScheduler(p.clockSync, p.config.BufferMs)
			go p.scheduler.Run()
			go p.handleScheduledAudio(p.schedulerCtx)

		case <-p.ctx.Done():
			return
		}
	}
}

// handleAudioChunks decodes and schedules audio
func (p *Player) handleAudioChunks() {
	chunkCount := 0
	for {
		select {
		case chunk := <-p.client.AudioChunks:
			chunkCount++

			// Detailed logging for first few chunks
			if chunkCount <= 5 {
				serverNow := sync.ServerMicrosNow()
				timeDiff := chunk.Timestamp - serverNow
				log.Printf("Received audio chunk #%d: chunk_ts=%d, server_now=%d, diff=%dμs (%.1fms)",
					chunkCount, chunk.Timestamp, serverNow, timeDiff, float64(timeDiff)/1000.0)
			}

			if p.decoder == nil || p.scheduler == nil {
				log.Printf("Skipping chunk: decoder=%v, scheduler=%v", p.decoder != nil, p.scheduler != nil)
				continue
			}

			// Decode
			pcm, err := p.decoder.Decode(chunk.Data)
			if err != nil {
				log.Printf("Decode error: %v", err)
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
			if err := p.output.Play(buf); err != nil {
				log.Printf("Playback error: %v", err)
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
				p.output.SetVolume(cmd.Volume)
				p.client.SendState(protocol.ClientState{
					State:  p.playerState,
					Volume: cmd.Volume,
					Muted:  p.output.IsMuted(),
				})

			case "mute":
				p.output.SetMuted(cmd.Mute)
				p.client.SendState(protocol.ClientState{
					State:  p.playerState,
					Volume: p.output.GetVolume(),
					Muted:  cmd.Mute,
				})
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// handleMetadata updates UI with track info
func (p *Player) handleMetadata() {
	for {
		select {
		case meta := <-p.client.Metadata:
			log.Printf("Metadata: %s - %s (%s)", meta.Artist, meta.Title, meta.Album)

			// Update TUI with metadata
			p.updateTUI(ui.StatusMsg{
				Title:  meta.Title,
				Artist: meta.Artist,
				Album:  meta.Album,
			})

		case <-p.ctx.Done():
			return
		}
	}
}

// handleSessionUpdates processes session updates and extracts metadata
func (p *Player) handleSessionUpdates() {
	for {
		select {
		case update := <-p.client.SessionUpdate:
			if update.Metadata != nil {
				log.Printf("Session metadata: %s - %s (%s)",
					update.Metadata.Artist, update.Metadata.Title, update.Metadata.Album)

				// Download artwork if available
				var artworkPath string
				if update.Metadata.ArtworkURL != "" && p.artwork != nil {
					path, err := p.artwork.Download(update.Metadata.ArtworkURL)
					if err != nil {
						log.Printf("Failed to download artwork: %v", err)
					} else {
						artworkPath = path
					}
				}

				// Update TUI with metadata from session
				p.updateTUI(ui.StatusMsg{
					Title:       update.Metadata.Title,
					Artist:      update.Metadata.Artist,
					Album:       update.Metadata.Album,
					ArtworkPath: artworkPath,
				})
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// handleVolumeControl processes volume changes from TUI
func (p *Player) handleVolumeControl() {
	if p.volumeCtrl == nil {
		return
	}

	for {
		select {
		case vol := <-p.volumeCtrl.Changes:
			log.Printf("Volume change: %d%%, muted=%v", vol.Volume, vol.Muted)

			// Apply to output
			if p.output != nil {
				p.output.SetVolume(vol.Volume)
				p.output.SetMuted(vol.Muted)
			}

			// Send state to server
			if p.client != nil {
				p.client.SendState(protocol.ClientState{
					State:  p.playerState,
					Volume: vol.Volume,
					Muted:  vol.Muted,
				})
			}

		case <-p.volumeCtrl.Quit:
			log.Printf("Received quit signal from TUI")
			p.Stop()
			return

		case <-p.ctx.Done():
			return
		}
	}
}

// statsUpdateLoop periodically updates TUI with playback statistics
func (p *Player) statsUpdateLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Use a slower ticker for expensive runtime stats to avoid GC pauses
	runtimeStatsTicker := time.NewTicker(2 * time.Second)
	defer runtimeStatsTicker.Stop()

	var lastGoroutines int
	var lastMemAlloc, lastMemSys uint64

	for {
		select {
		case <-runtimeStatsTicker.C:
			// Collect runtime stats less frequently (every 2 seconds)
			// to avoid GC pauses from ReadMemStats
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			lastGoroutines = runtime.NumGoroutine()
			lastMemAlloc = m.Alloc
			lastMemSys = m.Sys

		case <-ticker.C:
			msg := ui.StatusMsg{
				Goroutines: lastGoroutines,
				MemAlloc:   lastMemAlloc,
				MemSys:     lastMemSys,
			}

			// Add scheduler stats if available
			if p.scheduler != nil {
				stats := p.scheduler.Stats()
				bufferDepth := p.scheduler.BufferDepth()

				msg.Received = stats.Received
				msg.Played = stats.Played
				msg.Dropped = stats.Dropped
				msg.BufferDepth = bufferDepth
			}

			p.updateTUI(msg)

		case <-p.ctx.Done():
			return
		}
	}
}

// Stop stops the player
func (p *Player) Stop() {
	p.cancel()

	if p.client != nil {
		p.client.Close()
	}

	if p.output != nil {
		p.output.Close()
	}
}

// updateTUI sends a status update to the TUI if enabled
func (p *Player) updateTUI(msg ui.StatusMsg) {
	if p.tuiProg != nil {
		p.tuiProg.Send(msg)
	}
}
