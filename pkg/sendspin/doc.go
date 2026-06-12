// ABOUTME: High-level Sendspin library API
// ABOUTME: Provides simple Player and Server APIs for most use cases
// Package sendspin provides high-level APIs for Sendspin audio streaming.
//
// This is the main entry point for most library users, providing:
//   - Player: Connect to servers and play synchronized audio
//   - Server: Serve audio to multiple clients
//   - AudioSource: Interface for custom audio sources
//
// For lower-level control, see the audio, protocol, sync, and discovery packages.
//
// Example Player:
//
//	player, err := sendspin.NewPlayer(sendspin.PlayerConfig{
//	    ServerAddr: "localhost:8927",
//	    PlayerName: "Living Room",
//	    Volume:     80,
//	})
//	err = player.Connect()
//	err = player.Play()
//
// Example Server:
//
//	source := sendspin.NewTestTone(48000, 2) // or implement AudioSource
//	server, err := sendspin.NewServer(sendspin.ServerConfig{
//	    Port:   8927,
//	    Source: source,
//	})
//	err = server.Start()
package sendspin
