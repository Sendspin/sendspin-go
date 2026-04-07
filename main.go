// ABOUTME: Entry point for Sendspin Protocol player
// ABOUTME: Parses CLI flags and starts the player application
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Sendspin/sendspin-go/internal/discovery"
	internalsync "github.com/Sendspin/sendspin-go/internal/sync"
	"github.com/Sendspin/sendspin-go/internal/ui"
	"github.com/Sendspin/sendspin-go/internal/version"
	"github.com/Sendspin/sendspin-go/pkg/sendspin"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	serverAddr = flag.String("server", "", "Manual server address (skip mDNS)")
	port       = flag.Int("port", 8927, "Port for mDNS advertisement")
	name       = flag.String("name", "", "Player friendly name (default: hostname-sendspin-player)")
	bufferMs   = flag.Int("buffer-ms", 150, "Jitter buffer size in milliseconds")
	logFile    = flag.String("log-file", "sendspin-player.log", "Log file path")
	noTUI      = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	streamLogs = flag.Bool("stream-logs", false, "Alias for -no-tui")
)

func main() {
	flag.Parse()

	// Determine if we should use TUI or streaming logs
	useTUI := !(*noTUI || *streamLogs)

	// Set up logging
	f, err := os.OpenFile(*logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer func() { _ = f.Close() }()

	if useTUI {
		// TUI mode: log only to file
		log.SetOutput(f)
	} else {
		// Streaming logs mode: log to both stdout and file
		multiWriter := io.MultiWriter(os.Stdout, f)
		log.SetOutput(multiWriter)
	}

	// Determine player name
	playerName := *name
	if playerName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		playerName = fmt.Sprintf("%s-sendspin-player", hostname)
	}

	// Handle shutdown signals early so discovery can use sigChan
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if !useTUI {
		log.Printf("Starting Sendspin Player: %s", playerName)
		log.Printf("TUI disabled - logging to file for debugging")
	}

	// TUI setup
	var tuiProg *tea.Program
	var volumeCtrl *ui.VolumeControl

	if useTUI {
		volumeCtrl = ui.NewVolumeControl()
		tuiProg, err = ui.Run(volumeCtrl)
		if err != nil {
			log.Fatalf("Failed to start TUI: %v", err)
		}
		go tuiProg.Run()
	}

	// Helper to update TUI
	updateTUI := func(msg ui.StatusMsg) {
		if tuiProg != nil {
			tuiProg.Send(msg)
		}
	}

	// Handle server discovery if no manual server specified
	var serverAddress string
	if *serverAddr == "" {
		log.Printf("Searching for servers via mDNS (press Ctrl+C to quit)...")
		disc := discovery.NewManager(discovery.Config{
			ServiceName: playerName,
			Port:        *port,
		})
		disc.Advertise()
		disc.Browse()

		// Wait for server discovery or shutdown
		select {
		case server := <-disc.Servers():
			serverAddress = fmt.Sprintf("%s:%d", server.Host, server.Port)
			log.Printf("Discovered server at %s", serverAddress)
		case sig := <-sigChan:
			log.Printf("Received %v during discovery, shutting down", sig)
			return
		}
	} else {
		serverAddress = *serverAddr
	}

	// Create player with callbacks for TUI
	config := sendspin.PlayerConfig{
		ServerAddr: serverAddress,
		PlayerName: playerName,
		Volume:     100,
		BufferMs:   *bufferMs,
		DeviceInfo: sendspin.DeviceInfo{
			ProductName:     version.Product,
			Manufacturer:    version.Manufacturer,
			SoftwareVersion: version.Version,
		},
		OnStateChange: func(state sendspin.PlayerState) {
			updateTUI(ui.StatusMsg{
				Codec:      state.Codec,
				SampleRate: state.SampleRate,
				Channels:   state.Channels,
				BitDepth:   state.BitDepth,
			})
			if state.Connected {
				connected := true
				updateTUI(ui.StatusMsg{
					Connected:  &connected,
					ServerName: serverAddress,
				})
			}
		},
		OnMetadata: func(meta sendspin.Metadata) {
			updateTUI(ui.StatusMsg{
				Title:  meta.Title,
				Artist: meta.Artist,
				Album:  meta.Album,
			})
		},
		OnError: func(err error) {
			log.Printf("Player error: %v", err)
		},
	}

	player, err := sendspin.NewPlayer(config)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}

	// Connect to server
	if err := player.Connect(); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}

	log.Printf("Connected to server: %s", serverAddress)

	// Start volume control handler if TUI is enabled
	if volumeCtrl != nil {
		go handleVolumeControl(player, volumeCtrl)
	}

	// Start stats update loop for TUI
	if tuiProg != nil {
		go statsUpdateLoop(player, updateTUI)
	}

	// Wait for quit signal from TUI or OS
	if volumeCtrl != nil {
		select {
		case <-volumeCtrl.Quit:
			log.Printf("Received quit signal from TUI")
		case <-sigChan:
			log.Printf("Shutdown signal received")
		}
	} else {
		<-sigChan
		log.Printf("Shutdown signal received")
	}

	// Close player
	if err := player.Close(); err != nil {
		log.Printf("Error closing player: %v", err)
	}

	log.Printf("Player stopped")
}

// handleVolumeControl processes volume changes from TUI
func handleVolumeControl(player *sendspin.Player, volumeCtrl *ui.VolumeControl) {
	for {
		select {
		case vol := <-volumeCtrl.Changes:
			log.Printf("Volume change: %d%%, muted=%v", vol.Volume, vol.Muted)
			player.SetVolume(vol.Volume)
			player.Mute(vol.Muted)
		case <-volumeCtrl.Quit:
			return
		}
	}
}

// statsUpdateLoop periodically updates TUI with playback statistics
func statsUpdateLoop(player *sendspin.Player, updateTUI func(ui.StatusMsg)) {
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
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			lastGoroutines = runtime.NumGoroutine()
			lastMemAlloc = m.Alloc
			lastMemSys = m.Sys

		case <-ticker.C:
			stats := player.Stats()

			// Convert pkg/sync.Quality to internal/sync.Quality
			var syncQuality internalsync.Quality
			switch stats.SyncQuality {
			case 0: // QualityGood
				syncQuality = internalsync.QualityGood
			case 1: // QualityDegraded
				syncQuality = internalsync.QualityDegraded
			case 2: // QualityLost
				syncQuality = internalsync.QualityLost
			}

			updateTUI(ui.StatusMsg{
				Received:    stats.Received,
				Played:      stats.Played,
				Dropped:     stats.Dropped,
				BufferDepth: stats.BufferDepth,
				SyncRTT:     stats.SyncRTT,
				SyncQuality: syncQuality,
				Goroutines:  lastGoroutines,
				MemAlloc:    lastMemAlloc,
				MemSys:      lastMemSys,
			})
		}
	}
}
