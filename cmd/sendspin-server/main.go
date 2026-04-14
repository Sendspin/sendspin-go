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
	port            = flag.Int("port", 8927, "WebSocket server port")
	name            = flag.String("name", "", "Server friendly name (default: hostname-sendspin-server)")
	logFile         = flag.String("log-file", "sendspin-server.log", "Log file path")
	debug           = flag.Bool("debug", false, "Enable debug logging")
	noMDNS          = flag.Bool("no-mdns", false, "Disable mDNS advertisement")
	noTUI           = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	audioFile       = flag.String("audio", "", "Audio source to stream (MP3, FLAC, HTTP URL, HLS). Default: test tone")
	discoverClients = flag.Bool("discover-clients", false, "Enable server-initiated discovery: browse _sendspin._tcp and dial out to clients")
)

func main() {
	flag.Parse()

	useTUI := !*noTUI

	f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer f.Close()

	if useTUI {
		// Log to file only when TUI is running; otherwise the log would stomp the TUI
		log.SetOutput(f)
	} else {
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	}

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
	}

	var source sendspin.AudioSource
	if *audioFile == "" {
		source = sendspin.NewTestTone(sendspin.DefaultSampleRate, sendspin.DefaultChannels)
	} else {
		internalSource, err := server.NewAudioSource(*audioFile)
		if err != nil {
			log.Fatalf("Failed to create audio source: %v", err)
		}
		source = internalSource
	}

	srv, err := sendspin.NewServer(sendspin.ServerConfig{
		Port:            *port,
		Name:            serverName,
		Source:          source,
		EnableMDNS:      !*noMDNS,
		Debug:           *debug,
		DiscoverClients: *discoverClients,
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	var tui *server.ServerTUI
	var tuiDone chan struct{}
	if useTUI {
		tui = server.NewServerTUI(serverName, *port)
		tuiDone = make(chan struct{})

		go func() {
			defer close(tuiDone)
			if err := tui.Start(serverName, *port); err != nil {
				log.Printf("TUI error: %v", err)
			}
		}()

		time.Sleep(100 * time.Millisecond)

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

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var tuiQuitChan <-chan struct{}
	if tui != nil {
		tuiQuitChan = tui.QuitChan()
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Start()
	}()

	serverStopped := false
	select {
	case sig := <-sigChan:
		log.Printf("Received %v, shutting down...", sig)
	case <-tuiQuitChan:
		log.Printf("TUI quit requested, shutting down...")
	case err := <-serverDone:
		serverStopped = true
		if err != nil {
			log.Printf("Server error: %v", err)
		}
	}

	srv.Stop()

	if tui != nil {
		tui.Stop()
		<-tuiDone
	}

	if !serverStopped {
		if err := <-serverDone; err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}

	log.Printf("Server stopped")
}

func updateTUI(tui *server.ServerTUI, srv *sendspin.Server, source sendspin.AudioSource, serverName string, port int) {
	clients := srv.Clients()

	tuiClients := make([]server.ClientInfo, len(clients))
	for i, c := range clients {
		tuiClients[i] = server.ClientInfo{
			Name:  c.Name,
			ID:    c.ID,
			Codec: c.Codec,
			State: c.State,
		}
	}

	title, artist, _ := source.Metadata()
	audioTitle := title
	if artist != "" && artist != "Unknown Artist" {
		audioTitle = artist + " - " + title
	}

	tui.Update(server.ServerStatus{
		Name:       serverName,
		Port:       port,
		Clients:    tuiClients,
		AudioTitle: audioTitle,
	})
}
