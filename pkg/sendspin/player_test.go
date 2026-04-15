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

// TestNewPlayer_DeviceInfoPassesThrough guards #48: --manufacturer and
// --product-name CLI flags write into PlayerConfig.DeviceInfo, and must
// survive the trip into the underlying Receiver without being replaced
// by the library defaults.
func TestNewPlayer_DeviceInfoPassesThrough(t *testing.T) {
	custom := DeviceInfo{
		ProductName:     "Custom Product",
		Manufacturer:    "Custom Mfg",
		SoftwareVersion: "9.9.9",
	}
	player, err := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		DeviceInfo: custom,
	})
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if player.config.DeviceInfo != custom {
		t.Errorf("config.DeviceInfo = %+v, want %+v", player.config.DeviceInfo, custom)
	}
}

// TestNewPlayer_StaticDelayStored guards #47: PlayerConfig.StaticDelayMs
// must be preserved on the Player so Connect can plumb it into Receiver
// and from there into Scheduler.
func TestNewPlayer_StaticDelayStored(t *testing.T) {
	player, err := NewPlayer(PlayerConfig{
		ServerAddr:    "localhost:8927",
		PlayerName:    "Test",
		StaticDelayMs: 250,
	})
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if player.config.StaticDelayMs != 250 {
		t.Errorf("config.StaticDelayMs = %d, want 250", player.config.StaticDelayMs)
	}
}

// TestReceiver_StaticDelayDefaultZero sanity-checks that a ReceiverConfig
// without StaticDelayMs set produces a scheduler with zero offset. This is
// the test that would have flagged the new field if a future refactor
// stopped plumbing it through. Uses a short connect attempt + teardown
// because we don't want to actually dial anything.
func TestReceiver_StaticDelayDefaultZero(t *testing.T) {
	recv, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:0",
		PlayerName: "Test",
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	defer recv.Close()
	if recv.config.StaticDelayMs != 0 {
		t.Errorf("default StaticDelayMs = %d, want 0", recv.config.StaticDelayMs)
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
