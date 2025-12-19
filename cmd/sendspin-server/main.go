// ABOUTME: Entry point for Sendspin Protocol server
// ABOUTME: Thin CLI wrapper around pkg/sendspin.Server with TUI support
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/sendspin"
)

var (
	port      = flag.Int("port", 8927, "WebSocket server port")
	name      = flag.String("name", "", "Server friendly name (default: hostname-sendspin-server)")
	logFile   = flag.String("log-file", "sendspin-server.log", "Log file path")
	debug     = flag.Bool("debug", false, "Enable debug logging")
	noMDNS    = flag.Bool("no-mdns", false, "Disable mDNS advertisement")
	noTUI     = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	audioFile = flag.String("audio", "", "Audio file to stream (MP3, FLAC, WAV). If not specified, plays test tone")
)

func main() {
	flag.Parse()

	// Determine if we should use TUI or streaming logs
	useTUI := !*noTUI

	// Set up logging
	f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer f.Close()

	if useTUI {
		// TUI mode: log only to file
		log.SetOutput(f)
	} else {
		// Streaming logs mode: log to both stdout and file
		multiWriter := io.MultiWriter(os.Stdout, f)
		log.SetOutput(multiWriter)
	}

	// Determine server name
	serverName := *name
	if serverName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		serverName = fmt.Sprintf("%s-sendspin-server", hostname)
	}

	if !useTUI {
		log.Printf("Starting Sendspin Server: %s on port %d", serverName, *port)
		if *debug {
			log.Printf("Debug logging enabled")
		}
		log.Printf("Logging to: %s", *logFile)
		log.Printf("Press Ctrl-C to stop")
	}

	// Create audio source
	var source sendspin.AudioSource
	if *audioFile == "" {
		// Use test tone
		source = sendspin.NewTestTone(sendspin.DefaultSampleRate, sendspin.DefaultChannels)
	} else {
		// Use file source (from internal package for now)
		// Note: This uses internal/server.NewAudioSource until file sources are migrated to pkg/
		internalSource, err := server.NewAudioSource(*audioFile)
		if err != nil {
			log.Fatalf("Failed to create audio source: %v", err)
		}
		source = internalSource
	}

	// Create server config
	config := sendspin.ServerConfig{
		Port:       *port,
		Name:       serverName,
		Source:     source,
		EnableMDNS: !*noMDNS,
		Debug:      *debug,
	}

	srv, err := sendspin.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Set up TUI if enabled
	var tui *server.ServerTUI
	var tuiDone chan struct{}
	if useTUI {
		tui = server.NewServerTUI(serverName, *port)
		tuiDone = make(chan struct{})

		// Start TUI in goroutine
		go func() {
			defer close(tuiDone)
			if err := tui.Start(serverName, *port); err != nil {
				log.Printf("TUI error: %v", err)
			}
		}()

		// Give TUI time to initialize
		time.Sleep(100 * time.Millisecond)

		// Start TUI update loop
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					updateTUI(tui, srv, source, serverName, *port)
				case <-tuiDone:
					return
				}
			}
		}()
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Handle TUI quit
	var tuiQuitChan <-chan struct{}
	if tui != nil {
		tuiQuitChan = tui.QuitChan()
	}

	// Start server in goroutine
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Start()
	}()

	// Wait for shutdown signal, TUI quit, or server error
	select {
	case sig := <-sigChan:
		log.Printf("\n\n=== Received %v signal, shutting down gracefully... ===\n", sig)
	case <-tuiQuitChan:
		log.Printf("TUI quit requested, shutting down...")
	case err := <-serverDone:
		if err != nil {
			log.Printf("Server error: %v", err)
		}
		// Server stopped, proceed to cleanup
	}

	// Stop server
	srv.Stop()

	// Stop TUI
	if tui != nil {
		tui.Stop()
		<-tuiDone
	}

	// Wait for server to finish
	if err := <-serverDone; err != nil {
		log.Fatalf("Server error: %v", err)
	}

	log.Printf("Server stopped")
}

// updateTUI sends current server state to the TUI
func updateTUI(tui *server.ServerTUI, srv *sendspin.Server, source sendspin.AudioSource, serverName string, port int) {
	// Get client info from server
	clients := srv.Clients()

	// Convert to TUI ClientInfo format
	tuiClients := make([]server.ClientInfo, len(clients))
	for i, c := range clients {
		tuiClients[i] = server.ClientInfo{
			Name:  c.Name,
			ID:    c.ID,
			Codec: c.Codec,
			State: c.State,
		}
	}

	// Get audio metadata
	title, artist, _ := source.Metadata()
	audioTitle := title
	if artist != "" && artist != "Unknown Artist" {
		audioTitle = artist + " - " + title
	}

	// Send update to TUI
	tui.Update(server.ServerStatus{
		Name:       serverName,
		Port:       port,
		Clients:    tuiClients,
		AudioTitle: audioTitle,
	})
}
