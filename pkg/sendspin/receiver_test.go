// ABOUTME: Tests for Receiver type construction and basic lifecycle
package sendspin

import (
	"testing"
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
