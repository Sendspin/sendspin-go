// ABOUTME: mDNS service discovery for Sendspin Protocol
// ABOUTME: Handles both advertisement (server-initiated) and browsing (client-initiated)
package discovery

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/hashicorp/mdns"
)

// silentLogger discards hashicorp/mdns internal logs
var silentLogger = log.New(io.Discard, "", 0)

// Config holds discovery configuration
type Config struct {
	ServiceName string
	Port        int
	ServerMode  bool // If true, advertise as _sendspin-server._tcp, otherwise _sendspin._tcp
}

// Manager handles mDNS operations
type Manager struct {
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	servers chan *ServerInfo
}

// ServerInfo describes a discovered server
type ServerInfo struct {
	Name string
	Host string
	Port int
}

// NewManager creates a discovery manager
func NewManager(config Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		servers: make(chan *ServerInfo, 10),
	}
}

// Advertise advertises this player via mDNS
func (m *Manager) Advertise() error {
	ips, err := getLocalIPs()
	if err != nil {
		return fmt.Errorf("failed to get local IPs: %w", err)
	}

	// Choose service type based on mode
	serviceType := "_sendspin._tcp"
	if m.config.ServerMode {
		serviceType = "_sendspin-server._tcp"
	}

	service, err := mdns.NewMDNSService(
		m.config.ServiceName,
		serviceType,
		"",
		"",
		m.config.Port,
		ips,
		[]string{"path=/sendspin"},
	)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service, Logger: silentLogger})
	if err != nil {
		return fmt.Errorf("failed to create mdns server: %w", err)
	}

	log.Printf("Advertising mDNS service: %s on port %d (type: %s)", m.config.ServiceName, m.config.Port, serviceType)

	go func() {
		<-m.ctx.Done()
		server.Shutdown()
	}()

	return nil
}

// Browse searches for Sendspin servers
func (m *Manager) Browse() error {
	go m.browseLoop()
	return nil
}

// browseLoop continuously browses for servers
func (m *Manager) browseLoop() {
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		entries := make(chan *mdns.ServiceEntry, 10)

		go func() {
			for entry := range entries {
				server := &ServerInfo{
					Name: entry.Name,
					Host: entry.AddrV4.String(),
					Port: entry.Port,
				}

				log.Printf("Discovered server: %s at %s:%d", server.Name, server.Host, server.Port)

				select {
				case m.servers <- server:
				case <-m.ctx.Done():
					return
				}
			}
		}()

		params := &mdns.QueryParam{
			Service: "_sendspin-server._tcp",
			Domain:  "local",
			Timeout: 3,
			Entries: entries,
			Logger:  silentLogger,
		}

		mdns.Query(params)
		close(entries)
	}
}

// Servers returns the channel of discovered servers
func (m *Manager) Servers() <-chan *ServerInfo {
	return m.servers
}

// Stop stops the discovery manager
func (m *Manager) Stop() {
	m.cancel()
}

// getLocalIPs returns local IP addresses
func getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP)
				}
			}
		}
	}

	return ips, nil
}
