// ABOUTME: Entry point for Sendspin Protocol player
// ABOUTME: Parses CLI flags and starts the player application
package main

import (
	"context"
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
	"github.com/Sendspin/sendspin-go/internal/ui"
	"github.com/Sendspin/sendspin-go/internal/version"
	"github.com/Sendspin/sendspin-go/pkg/sendspin"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	serverAddr     = flag.String("server", "", "Manual server address (skip mDNS)")
	port           = flag.Int("port", 8927, "Port for mDNS advertisement")
	name           = flag.String("name", "", "Player friendly name (default: hostname-sendspin-player)")
	bufferMs       = flag.Int("buffer-ms", 150, "Jitter buffer size in milliseconds")
	staticDelayMs  = flag.Int("static-delay-ms", 0, "Static playback delay in milliseconds for hardware latency compensation")
	logFile        = flag.String("log-file", "sendspin-player.log", "Log file path")
	noTUI          = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	streamLogs     = flag.Bool("stream-logs", false, "Alias for -no-tui")
	productName    = flag.String("product-name", "", "Override the product name sent in device_info (default: compiled-in identity)")
	manufacturer   = flag.String("manufacturer", "", "Override the manufacturer sent in device_info (default: compiled-in identity)")
	noReconnect    = flag.Bool("no-reconnect", false, "Disable automatic reconnect on connection loss")
	daemon         = flag.Bool("daemon", false, "Daemon mode: log to stdout only (journalctl-friendly), no TUI, no log file")
	preferredCodec = flag.String("preferred-codec", "", "Preferred codec: pcm (default), opus, or flac")
	bufferCapacity = flag.Int("buffer-capacity", 1048576, "Buffer capacity in bytes advertised to server (default: 1MB)")
)

func main() {
	flag.Parse()

	// Use TUI if not explicitly disabled; -stream-logs and -daemon both imply -no-tui
	useTUI := !(*noTUI || *streamLogs || *daemon)

	if *daemon {
		// Daemon mode: log to stdout only. systemd/journalctl captures stdout
		// and adds its own timestamps, so we keep ours for grep-ability.
		log.SetOutput(os.Stdout)
	} else {
		f, err := os.OpenFile(*logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		defer func() { _ = f.Close() }()

		if useTUI {
			// Log to file only when TUI is running; otherwise the log would stomp the TUI
			log.SetOutput(f)
		} else {
			log.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}

	playerName := *name
	if playerName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		playerName = fmt.Sprintf("%s-sendspin-player", hostname)
	}

	// Set up sigChan before discovery so the select loop can catch Ctrl+C during browsing
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if !useTUI {
		log.Printf("Starting Sendspin Player: %s (version %s)", playerName, version.Version)
		if *daemon {
			log.Printf("Daemon mode: logging to stdout only")
		}
	}

	var tuiProg *tea.Program
	var volumeCtrl *ui.VolumeControl
	var transportCtrl *ui.TransportControl

	if useTUI {
		volumeCtrl = ui.NewVolumeControl()
		transportCtrl = ui.NewTransportControl()
		var err error
		tuiProg, err = ui.Run(volumeCtrl, transportCtrl)
		if err != nil {
			log.Fatalf("Failed to start TUI: %v", err)
		}
		go tuiProg.Run()
	}

	updateTUI := func(msg ui.StatusMsg) {
		if tuiProg != nil {
			tuiProg.Send(msg)
		}
	}

	var serverAddress string
	var disc *discovery.Manager
	if *serverAddr == "" {
		log.Printf("Searching for servers via mDNS (press Ctrl+C to quit)...")
		disc = discovery.NewManager(discovery.Config{
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

	deviceProduct := version.Product
	if *productName != "" {
		deviceProduct = *productName
	}
	deviceManufacturer := version.Manufacturer
	if *manufacturer != "" {
		deviceManufacturer = *manufacturer
	}

	config := sendspin.PlayerConfig{
		ServerAddr:     serverAddress,
		PlayerName:     playerName,
		Volume:         100,
		BufferMs:       *bufferMs,
		StaticDelayMs:  *staticDelayMs,
		PreferredCodec: *preferredCodec,
		BufferCapacity: *bufferCapacity,
		DeviceInfo: sendspin.DeviceInfo{
			ProductName:     deviceProduct,
			Manufacturer:    deviceManufacturer,
			SoftwareVersion: version.Version,
		},
		OnStateChange: func(state sendspin.PlayerState) {
			updateTUI(ui.StatusMsg{
				Codec:         state.Codec,
				SampleRate:    state.SampleRate,
				Channels:      state.Channels,
				BitDepth:      state.BitDepth,
				PlaybackState: state.State,
			})
			connected := state.Connected
			serverLabel := serverAddress
			if state.State == "reconnecting" {
				serverLabel = "reconnecting..."
			}
			updateTUI(ui.StatusMsg{
				Connected:  &connected,
				ServerName: serverLabel,
			})
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
		Reconnect: sendspin.ReconnectConfig{
			Enabled: !*noReconnect,
		},
	}

	if !*noReconnect && disc != nil {
		config.Reconnect.Rediscover = func(ctx context.Context) (string, error) {
			disc.Browse()
			select {
			case server := <-disc.Servers():
				addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
				log.Printf("Rediscovered server at %s", addr)
				return addr, nil
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
				return "", fmt.Errorf("rediscover timed out")
			}
		}
	}

	player, err := sendspin.NewPlayer(config)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}

	if err := player.Connect(); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}

	log.Printf("Connected to server: %s", serverAddress)

	if volumeCtrl != nil {
		go handleVolumeControl(player, volumeCtrl)
	}

	if transportCtrl != nil {
		go handleTransportControl(player, transportCtrl)
	}

	if tuiProg != nil {
		go statsUpdateLoop(player, updateTUI)
	}

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

	if err := player.Close(); err != nil {
		log.Printf("Error closing player: %v", err)
	}

	log.Printf("Player stopped")
}

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

func handleTransportControl(player *sendspin.Player, ctrl *ui.TransportControl) {
	for cmd := range ctrl.Commands {
		if !player.Status().Connected {
			log.Printf("Transport command %q ignored: not connected", cmd.Command)
			continue
		}
		var err error
		switch cmd.Command {
		case "toggle":
			// Toggle sends "pause" if server is playing, "play" otherwise.
			// The server decides the actual state; we just request it.
			status := player.Status()
			if status.State == "playing" {
				err = player.SendCommand("pause")
			} else {
				err = player.SendCommand("play")
			}
		case "play":
			err = player.SendCommand("play")
		case "pause":
			err = player.SendCommand("pause")
		case "next":
			err = player.SendCommand("next")
		case "previous":
			err = player.SendCommand("previous")
		case "reconnect":
			log.Printf("Manual reconnect requested")
		}
		if err != nil {
			log.Printf("Transport command %q failed: %v", cmd.Command, err)
		}
	}
}

func statsUpdateLoop(player *sendspin.Player, updateTUI func(ui.StatusMsg)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		stats := player.Stats()

		// NumGoroutine is cheap; ReadMemStats removed to avoid stop-the-world pauses
		updateTUI(ui.StatusMsg{
			Received:    stats.Received,
			Played:      stats.Played,
			Dropped:     stats.Dropped,
			BufferDepth: stats.BufferDepth,
			SyncRTT:     stats.SyncRTT,
			SyncQuality: stats.SyncQuality,
			Goroutines:  runtime.NumGoroutine(),
		})
	}
}
