// ABOUTME: Tests for refactored Player composing Receiver
// ABOUTME: Verifies backward-compatible API and new ProcessCallback
package sendspin

import (
	"testing"
)

func TestNewPlayer_Defaults(t *testing.T) {
	player, err := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Player",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if player == nil {
		t.Fatal("expected non-nil player")
	}
	if player.receiver != nil {
		t.Error("receiver should be nil before Connect")
	}
}

func TestNewPlayer_ProcessCallbackStored(t *testing.T) {
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr:      "localhost:8927",
		PlayerName:      "Test Player",
		ProcessCallback: func(samples []int32) {},
	})
	if player.config.ProcessCallback == nil {
		t.Error("expected ProcessCallback to be stored in config")
	}
}

func TestPlayer_StatusBeforeConnect(t *testing.T) {
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Player",
		Volume:     80,
	})

	status := player.Status()
	if status.Volume != 80 {
		t.Errorf("expected volume 80, got %d", status.Volume)
	}
	if status.Connected {
		t.Error("expected not connected before Connect()")
	}
	if status.State != "idle" {
		t.Errorf("expected state idle, got %s", status.State)
	}
}
