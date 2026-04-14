// ABOUTME: Tests for Receiver type construction and basic lifecycle
package sendspin

import (
	"fmt"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
)

func TestNewReceiver_Defaults(t *testing.T) {
	config := ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test Receiver",
	}

	r, err := NewReceiver(config)
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer r.Close()

	if r == nil {
		t.Fatal("Expected non-nil Receiver")
	}

	if r.clockSync == nil {
		t.Error("Expected clockSync to be initialized")
	}

	if r.Output() == nil {
		t.Error("Expected Output() channel to be non-nil")
	}

	if r.config.BufferMs != 500 {
		t.Errorf("Expected default BufferMs=500, got %d", r.config.BufferMs)
	}

	if r.config.DeviceInfo.ProductName == "" {
		t.Error("Expected default DeviceInfo.ProductName to be set")
	}

	if r.config.DeviceInfo.Manufacturer == "" {
		t.Error("Expected default DeviceInfo.Manufacturer to be set")
	}

	if r.config.DeviceInfo.SoftwareVersion == "" {
		t.Error("Expected default DeviceInfo.SoftwareVersion to be set")
	}
}

func TestNewReceiver_RequiresServerAddr(t *testing.T) {
	config := ReceiverConfig{
		PlayerName: "Test Receiver",
		// ServerAddr intentionally omitted
	}

	r, err := NewReceiver(config)
	if err == nil {
		t.Error("Expected error when ServerAddr is empty")
		if r != nil {
			r.Close()
		}
	}

	if r != nil {
		t.Error("Expected nil Receiver on error")
	}
}

func TestReceiver_Connect_BadAddress(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:99999",
		PlayerName: "Test",
	})

	err := recv.Connect()
	if err == nil {
		t.Fatal("expected connection error for bad address")
	}
}

func TestReceiver_ClockSyncIsOwnInstance(t *testing.T) {
	config1 := ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Receiver One",
	}
	config2 := ReceiverConfig{
		ServerAddr: "localhost:8928",
		PlayerName: "Receiver Two",
	}

	r1, err := NewReceiver(config1)
	if err != nil {
		t.Fatalf("NewReceiver r1 failed: %v", err)
	}
	defer r1.Close()

	r2, err := NewReceiver(config2)
	if err != nil {
		t.Fatalf("NewReceiver r2 failed: %v", err)
	}
	defer r2.Close()

	if r1.ClockSync() == r2.ClockSync() {
		t.Error("Expected each Receiver to have its own ClockSync instance")
	}
}

func TestReceiver_CloseBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	// Should not panic
	err := recv.Close()
	if err != nil {
		t.Fatalf("unexpected error closing unconnected receiver: %v", err)
	}
}

func TestReceiver_StatsBeforeConnect(t *testing.T) {
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
	})

	stats := recv.Stats()
	if stats.Received != 0 || stats.Played != 0 || stats.Dropped != 0 {
		t.Error("expected zero stats before connect")
	}
}

func TestReceiver_OnStreamStartCallback(t *testing.T) {
	var receivedFormat audio.Format
	_, err := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		OnStreamStart: func(f audio.Format) {
			receivedFormat = f
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Callback is stored but not invoked until stream starts
	if receivedFormat.Codec != "" {
		t.Error("expected empty format before stream start")
	}
}

func TestReceiver_CustomDecoderFactory(t *testing.T) {
	factoryCalled := false
	recv, _ := NewReceiver(ReceiverConfig{
		ServerAddr: "localhost:8927",
		PlayerName: "Test",
		DecoderFactory: func(f audio.Format) (decode.Decoder, error) {
			factoryCalled = true
			return nil, fmt.Errorf("test decoder")
		},
	})

	if recv.config.DecoderFactory == nil {
		t.Error("expected DecoderFactory to be stored")
	}
	if factoryCalled {
		t.Error("factory should not be called before connect")
	}
}
