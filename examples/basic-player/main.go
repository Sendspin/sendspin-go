// ABOUTME: Basic Sendspin player example
// ABOUTME: Demonstrates how to connect to a server and play audio
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sendspin/sendspin-go/pkg/sendspin"
)

func main() {
	serverAddr := flag.String("server", "localhost:8927", "Sendspin server address")
	playerName := flag.String("name", "Basic Player", "Player name")
	volume := flag.Int("volume", 80, "Initial volume (0-100)")
	flag.Parse()

	config := sendspin.PlayerConfig{
		ServerAddr: *serverAddr,
		PlayerName: *playerName,
		Volume:     *volume,
		BufferMs:   500, // 500ms playback buffer
		DeviceInfo: sendspin.DeviceInfo{
			ProductName:     "Sendspin Example Player",
			Manufacturer:    "Sendspin",
			SoftwareVersion: "1.0.0",
		},
		OnMetadata: func(meta sendspin.Metadata) {
			log.Printf("Now playing: %s - %s (%s)", meta.Artist, meta.Title, meta.Album)
		},
		OnStateChange: func(state sendspin.PlayerState) {
			log.Printf("State changed: %s (volume: %d, muted: %v)", state.State, state.Volume, state.Muted)
		},
		OnError: func(err error) {
			log.Printf("Error: %v", err)
		},
	}

	player, err := sendspin.NewPlayer(config)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer player.Close()

	log.Printf("Connecting to %s...", *serverAddr)
	if err := player.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	log.Printf("Connected! Starting playback...")

	if err := player.Play(); err != nil {
		log.Fatalf("Failed to start playback: %v", err)
	}

	go func() {
		for {
			status := player.Status()
			stats := player.Stats()
			if status.Connected {
				log.Printf("Status: %s | %s %dHz %dch %dbit | Buffer: %dms | RTT: %dμs",
					status.State,
					status.Codec,
					status.SampleRate,
					status.Channels,
					status.BitDepth,
					stats.BufferDepth,
					stats.SyncRTT)
			}
			// Sleep for 5 seconds
			select {
			case <-make(chan struct{}):
			}
		}
	}()

	// Wait for interrupt signal
	fmt.Println("\nPress Ctrl+C to stop playback")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down...")
}
