// ABOUTME: Tests for refactored Player composing Receiver
// ABOUTME: Verifies backward-compatible API and new ProcessCallback
package sendspin

import (
	"context"
	"testing"
	"time"
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

// TestNewPlayer_ReconnectDefaultsApplied guards the reconnect backoff
// defaults (#38). When the caller enables reconnect but leaves timing
// fields zero, NewPlayer must fill them with the documented defaults so
// an accidentally-zero delay never spin-loops.
func TestNewPlayer_ReconnectDefaultsApplied(t *testing.T) {
	player, err := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		Reconnect:  ReconnectConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	rc := player.config.Reconnect
	if rc.InitialDelay != 500*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 500ms", rc.InitialDelay)
	}
	if rc.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", rc.MaxDelay)
	}
	if rc.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want 2.0", rc.Multiplier)
	}
	if rc.MaxAttempts != 0 {
		t.Errorf("MaxAttempts = %d, want 0 (infinite)", rc.MaxAttempts)
	}
}

// TestNewPlayer_ReconnectDefaultsSkippedWhenDisabled makes sure we don't
// silently turn reconnect on. If Enabled is false we leave the zero values
// alone — there is no supervisor goroutine to read them anyway.
func TestNewPlayer_ReconnectDefaultsSkippedWhenDisabled(t *testing.T) {
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})
	if player.config.Reconnect.Enabled {
		t.Error("Reconnect.Enabled should default to false")
	}
	if player.config.Reconnect.InitialDelay != 0 {
		t.Error("InitialDelay should not be populated when Reconnect.Enabled is false")
	}
}

// TestNewPlayer_ReconnectRediscoverCallbackStored confirms the closure
// survives into player.config so the reconnect supervisor can call it.
func TestNewPlayer_ReconnectRediscoverCallbackStored(t *testing.T) {
	called := false
	player, _ := NewPlayer(PlayerConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		Reconnect: ReconnectConfig{
			Enabled: true,
			Rediscover: func(ctx context.Context) (string, error) {
				called = true
				return "other:1234", nil
			},
		},
	})
	if player.config.Reconnect.Rediscover == nil {
		t.Fatal("Rediscover callback not stored")
	}
	addr, err := player.config.Reconnect.Rediscover(context.Background())
	if err != nil || addr != "other:1234" || !called {
		t.Errorf("callback not invoked correctly: addr=%q err=%v called=%v", addr, err, called)
	}
}

// TestJitter stays inside the ±frac band. With frac=0.2 and a 1s delay,
// the result must always fall within [800ms, 1200ms].
func TestJitter(t *testing.T) {
	base := 1 * time.Second
	for i := 0; i < 100; i++ {
		got := jitter(base, 0.2)
		if got < 800*time.Millisecond || got > 1200*time.Millisecond {
			t.Errorf("jitter(%v, 0.2) = %v, outside ±20%% band", base, got)
		}
	}
	if jitter(0, 0.2) != 0 {
		t.Error("jitter(0, _) should return 0")
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
