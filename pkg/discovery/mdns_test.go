// ABOUTME: Tests for mDNS service discovery
// ABOUTME: Validates Manager creation, configuration, and lifecycle
package discovery

import (
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	config := Config{
		ServiceName: "test-service",
		Port:        8080,
		ServerMode:  false,
	}

	manager := NewManager(config)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}

	if manager.config.ServiceName != "test-service" {
		t.Errorf("Expected ServiceName 'test-service', got '%s'", manager.config.ServiceName)
	}

	if manager.config.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", manager.config.Port)
	}

	if manager.config.ServerMode != false {
		t.Errorf("Expected ServerMode false, got %v", manager.config.ServerMode)
	}

	if manager.servers == nil {
		t.Error("servers channel should not be nil")
	}

	if manager.ctx == nil {
		t.Error("ctx should not be nil")
	}

	if manager.cancel == nil {
		t.Error("cancel should not be nil")
	}

	manager.Stop()
}

func TestManagerServerMode(t *testing.T) {
	config := Config{
		ServiceName: "test-server",
		Port:        9090,
		ServerMode:  true,
	}

	manager := NewManager(config)
	defer manager.Stop()

	if !manager.config.ServerMode {
		t.Error("Expected ServerMode to be true")
	}
}

func TestManagerServersChannel(t *testing.T) {
	config := Config{
		ServiceName: "test",
		Port:        8080,
		ServerMode:  false,
	}

	manager := NewManager(config)
	defer manager.Stop()

	serversChan := manager.Servers()

	if serversChan == nil {
		t.Fatal("Servers() returned nil channel")
	}

	if serversChan != manager.servers {
		t.Error("Servers() should return the manager's servers channel")
	}
}

func TestManagerStop(t *testing.T) {
	config := Config{
		ServiceName: "test",
		Port:        8080,
		ServerMode:  false,
	}

	manager := NewManager(config)

	manager.Stop()

	select {
	case <-manager.ctx.Done():
		// Expected - context should be cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled after Stop()")
	}
}

func TestGetLocalIPs(t *testing.T) {
	ips, err := getLocalIPs()

	if err != nil {
		t.Fatalf("getLocalIPs failed: %v", err)
	}

	// We should have at least one non-loopback IPv4 address on most systems
	// This test may be environment-dependent, so we just verify it doesn't crash
	// and returns a non-nil slice
	if ips == nil {
		t.Error("getLocalIPs returned nil slice")
	}

	for _, ip := range ips {
		if ip.To4() == nil {
			t.Errorf("getLocalIPs returned non-IPv4 address: %v", ip)
		}
		if ip.IsLoopback() {
			t.Errorf("getLocalIPs returned loopback address: %v", ip)
		}
	}
}

func TestServerInfo(t *testing.T) {
	info := &ServerInfo{
		Name: "test-server",
		Host: "192.168.1.100",
		Port: 8080,
	}

	if info.Name != "test-server" {
		t.Errorf("Expected Name 'test-server', got '%s'", info.Name)
	}

	if info.Host != "192.168.1.100" {
		t.Errorf("Expected Host '192.168.1.100', got '%s'", info.Host)
	}

	if info.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", info.Port)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "client mode",
			config: Config{
				ServiceName: "client",
				Port:        8080,
				ServerMode:  false,
			},
		},
		{
			name: "server mode",
			config: Config{
				ServiceName: "server",
				Port:        9090,
				ServerMode:  true,
			},
		},
		{
			name: "different port",
			config: Config{
				ServiceName: "test",
				Port:        12345,
				ServerMode:  false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(tt.config)
			defer manager.Stop()

			if manager.config.ServiceName != tt.config.ServiceName {
				t.Errorf("ServiceName: expected %s, got %s", tt.config.ServiceName, manager.config.ServiceName)
			}
			if manager.config.Port != tt.config.Port {
				t.Errorf("Port: expected %d, got %d", tt.config.Port, manager.config.Port)
			}
			if manager.config.ServerMode != tt.config.ServerMode {
				t.Errorf("ServerMode: expected %v, got %v", tt.config.ServerMode, manager.config.ServerMode)
			}
		})
	}
}
