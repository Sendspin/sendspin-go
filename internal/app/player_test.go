// ABOUTME: Tests for player application orchestration
// ABOUTME: Tests player creation, configuration, and lifecycle
package app

import (
	"testing"
)

func TestNewPlayer(t *testing.T) {
	config := Config{
		ServerAddr: "localhost:8927",
		Port:       0,
		Name:       "test-player",
		BufferMs:   300,
		UseTUI:     false,
	}

	player := New(config)

	if player == nil {
		t.Fatal("expected player to be created")
	}

	if player.config.ServerAddr != config.ServerAddr {
		t.Errorf("expected ServerAddr %s, got %s", config.ServerAddr, player.config.ServerAddr)
	}

	if player.config.Name != config.Name {
		t.Errorf("expected Name %s, got %s", config.Name, player.config.Name)
	}

	if player.config.BufferMs != config.BufferMs {
		t.Errorf("expected BufferMs %d, got %d", config.BufferMs, player.config.BufferMs)
	}

	if player.playerState != "idle" {
		t.Errorf("expected initial state 'idle', got '%s'", player.playerState)
	}
}

func TestPlayerInitialization(t *testing.T) {
	config := Config{
		ServerAddr: "",
		Port:       0,
		Name:       "test-player",
		BufferMs:   300,
		UseTUI:     false,
	}

	player := New(config)

	if player.clockSync == nil {
		t.Error("clockSync should be initialized")
	}

	if player.output == nil {
		t.Error("output should be initialized")
	}

	if player.ctx == nil {
		t.Error("context should be initialized")
	}

	if player.cancel == nil {
		t.Error("cancel function should be initialized")
	}
}

func TestPlayerWithArtwork(t *testing.T) {
	config := Config{
		ServerAddr: "localhost:8927",
		Port:       0,
		Name:       "test-player",
		BufferMs:   300,
		UseTUI:     false,
	}

	player := New(config)

	if player.artwork == nil {
		t.Error("artwork downloader should be initialized")
	}
}

func TestPlayerStop(t *testing.T) {
	config := Config{
		ServerAddr: "",
		Port:       0,
		Name:       "test-player",
		BufferMs:   300,
		UseTUI:     false,
	}

	player := New(config)

	// Should not panic
	player.Stop()

	// Context should be cancelled
	select {
	case <-player.ctx.Done():
		// Expected
	default:
		t.Error("context should be cancelled after Stop()")
	}
}

func TestConfigDefaults(t *testing.T) {
	config := Config{}

	if config.ServerAddr != "" {
		t.Errorf("expected empty ServerAddr, got %s", config.ServerAddr)
	}

	if config.Port != 0 {
		t.Errorf("expected Port 0, got %d", config.Port)
	}

	if config.Name != "" {
		t.Errorf("expected empty Name, got %s", config.Name)
	}

	if config.BufferMs != 0 {
		t.Errorf("expected BufferMs 0, got %d", config.BufferMs)
	}

	if config.UseTUI {
		t.Error("expected UseTUI false by default")
	}
}

func TestMultiplePlayerInstances(t *testing.T) {
	config1 := Config{
		Name:     "player-1",
		BufferMs: 100,
	}

	config2 := Config{
		Name:     "player-2",
		BufferMs: 200,
	}

	player1 := New(config1)
	player2 := New(config2)

	if player1 == player2 {
		t.Error("expected different player instances")
	}

	if player1.config.Name == player2.config.Name {
		t.Error("expected different player names")
	}

	if player1.config.BufferMs == player2.config.BufferMs {
		t.Error("expected different buffer sizes")
	}

	player1.Stop()

	select {
	case <-player1.ctx.Done():
		// Expected
	default:
		t.Error("player1 context should be cancelled")
	}

	select {
	case <-player2.ctx.Done():
		t.Error("player2 context should still be active")
	default:
		// Expected
	}

	player2.Stop()
}

func TestPlayerStateInitialization(t *testing.T) {
	player := New(Config{})

	if player.playerState != "idle" {
		t.Errorf("expected initial playerState 'idle', got '%s'", player.playerState)
	}
}

func TestPlayerWithTUIDisabled(t *testing.T) {
	config := Config{
		UseTUI: false,
	}

	player := New(config)

	if player.tuiProg != nil {
		t.Error("TUI program should not be initialized when UseTUI is false")
	}

	if player.volumeCtrl != nil {
		t.Error("volume control should not be initialized when UseTUI is false")
	}
}

func TestPlayerClockSyncInitialization(t *testing.T) {
	player := New(Config{})

	if player.clockSync == nil {
		t.Fatal("clockSync should be initialized")
	}

	// ClockSync should start unsynced
	_, quality := player.clockSync.GetStats()
	if quality != 2 { // QualityLost = 2
		t.Errorf("expected initial quality to be QualityLost (2), got %d", quality)
	}
}

func TestPlayerOutputInitialization(t *testing.T) {
	player := New(Config{})

	if player.output == nil {
		t.Fatal("output should be initialized")
	}

	if player.volume != 100 {
		t.Errorf("expected default volume 100, got %d", player.volume)
	}

	if player.muted {
		t.Error("expected output to not be muted by default")
	}
}
