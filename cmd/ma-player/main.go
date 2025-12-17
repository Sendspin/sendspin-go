// ABOUTME: Entry point for Music Assistant compatible player
// ABOUTME: Uses internal/ stack with legacy protocol support
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sendspin/sendspin-go/internal/app"
)

var (
	serverAddr = flag.String("server", "", "Music Assistant server address (e.g., 192.168.1.100:8097)")
	port       = flag.Int("port", 8927, "Port for mDNS advertisement")
	name       = flag.String("name", "", "Player friendly name (default: hostname-ma-player)")
	bufferMs   = flag.Int("buffer-ms", 150, "Jitter buffer size in milliseconds")
	logFile    = flag.String("log-file", "ma-player.log", "Log file path")
	noTUI      = flag.Bool("no-tui", false, "Disable TUI, use streaming logs instead")
	streamLogs = flag.Bool("stream-logs", false, "Alias for -no-tui")
)

func main() {
	flag.Parse()

	// Determine if we should use TUI or streaming logs
	useTUI := !(*noTUI || *streamLogs)

	// Set up logging
	f, err := os.OpenFile(*logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
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
		playerName = fmt.Sprintf("%s-ma-player", hostname)
	}

	log.Printf("Starting Music Assistant Player: %s", playerName)

	config := app.Config{
		ServerAddr: *serverAddr,
		Port:       *port,
		Name:       playerName,
		BufferMs:   *bufferMs,
		UseTUI:     useTUI,
	}

	player := app.New(config)

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("Shutdown signal received")
		player.Stop()
	}()

	if err := player.Start(); err != nil {
		log.Fatalf("Player failed: %v", err)
	}

	log.Printf("Player stopped")
}
