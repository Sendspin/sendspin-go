// ABOUTME: High-level Server API for Sendspin streaming
// ABOUTME: Wraps server components into a simple, user-friendly interface
package sendspin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sendspin/sendspin-go/internal/discovery"
	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	ProtocolVersion = 1

	// Binary message type IDs per spec (bits 7-2 for role, bits 1-0 for slot)
	// Player role: 000001xx (4-7), slot 0 = 4
	AudioChunkMessageType = 4

	DefaultSampleRate = 192000
	DefaultChannels   = 2
	DefaultBitDepth   = 24

	ChunkDurationMs = 20  // 20ms chunks
	BufferAheadMs   = 500 // Send audio 500ms ahead
)

type ServerConfig struct {
	// Port to listen on (default: 8927)
	Port int

	Name string

	// Audio source to stream (required)
	Source AudioSource

	// EnableMDNS enables mDNS service advertisement (default: true)
	EnableMDNS bool

	Debug bool

	// DiscoverClients enables server-initiated discovery: browse for
	// clients advertising _sendspin._tcp and dial out to them.
	// See https://www.sendspin-audio.com/spec/ — "server-initiated" mode.
	DiscoverClients bool

	// SupportedRoles lists the role families this server activates.
	// When nil, defaults to ["player", "metadata"] for backward compat.
	// Roles registered via Group.RegisterRole are also activated
	// regardless of this list.
	SupportedRoles []string
}

type Server struct {
	config   ServerConfig
	serverID string

	upgrader websocket.Upgrader

	httpServer *http.Server
	mux        *http.ServeMux
	boundAddr  atomic.Pointer[net.TCPAddr]

	clients      map[string]*ServerClient
	clientsMu    sync.RWMutex
	defaultGroup *Group

	clockStart time.Time // monotonic microseconds origin

	audioSource         AudioSource
	consecutiveReadErrs int

	mdnsManager *discovery.Manager

	// server-initiated discovery dialer cancel
	dialerCancel context.CancelFunc

	stopChan   chan struct{}
	stopOnce   sync.Once
	shutdownMu sync.RWMutex
	isShutdown bool
	wg         sync.WaitGroup
}

type ClientInfo struct {
	ID     string
	Name   string
	State  string
	Volume int
	Muted  bool
	Codec  string
}

func NewServer(config ServerConfig) (*Server, error) {
	// Port == 0 is honored as "let the OS pick an ephemeral port". The
	// CLI / config-file layers supply 8927 as the documented default
	// before reaching this point; library callers that omit Port get
	// ephemeral binding (and can read it back via Server.Addr after
	// Start). This is the substitution-free behavior tests rely on for
	// flake-free Port: 0 binding.
	if config.Name == "" {
		config.Name = "Sendspin Server"
	}
	if config.Source == nil {
		return nil, fmt.Errorf("audio source is required")
	}
	if config.SupportedRoles == nil {
		config.SupportedRoles = []string{"player", "metadata"}
	}

	mux := http.NewServeMux()

	s := &Server{
		config:      config,
		serverID:    uuid.New().String(),
		mux:         mux,
		audioSource: config.Source,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// TODO: For production, implement proper origin validation.
				// Currently permissive for local-network deployments.
				return true
			},
		},
		clients:    make(map[string]*ServerClient),
		clockStart: time.Now(),
		stopChan:   make(chan struct{}),
	}

	s.defaultGroup = NewGroup(s.serverID)

	s.defaultGroup.RegisterRole(NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) {
			return s.audioSource.Metadata()
		},
		ClockMicros: s.getClockMicros,
	}))

	s.defaultGroup.RegisterRole(NewPlayerRole(PlayerRoleConfig{
		SampleRate: s.audioSource.SampleRate(),
		Channels:   s.audioSource.Channels(),
		BitDepth:   DefaultBitDepth,
		NewEncoder: func(sampleRate, channels, chunkSamples int) (*server.OpusEncoder, error) {
			return server.NewOpusEncoder(sampleRate, channels, chunkSamples)
		},
		NewFLACEncoder: func(sampleRate, channels, bitDepth, blockSize int) (*server.FLACEncoder, error) {
			return server.NewFLACEncoder(sampleRate, channels, bitDepth, blockSize)
		},
	}))

	return s, nil
}

func (s *Server) Start() error {
	log.Printf("Server starting: %s (ID: %s)", s.config.Name, s.serverID)
	log.Printf("Audio source: %dHz/%dbit/%dch",
		s.audioSource.SampleRate(),
		DefaultBitDepth,
		s.audioSource.Channels())

	if s.config.EnableMDNS {
		s.mdnsManager = discovery.NewManager(discovery.Config{
			ServiceName: s.config.Name,
			Port:        s.config.Port,
			ServerMode:  true,
		})

		if err := s.mdnsManager.Advertise(); err != nil {
			log.Printf("Failed to start mDNS advertisement: %v", err)
		} else {
			log.Printf("mDNS advertisement started")
		}
	}

	if s.config.DiscoverClients {
		if s.mdnsManager == nil {
			// If mDNS isn't running for advertising, start a manager just for browsing.
			s.mdnsManager = discovery.NewManager(discovery.Config{
				ServiceName: s.config.Name,
				Port:        s.config.Port,
				ServerMode:  true,
			})
		}

		if err := s.mdnsManager.BrowseClients(); err != nil {
			log.Printf("Failed to start client discovery: %v", err)
		} else {
			log.Printf("Browsing for clients advertising _sendspin._tcp")

			dialCtx, cancel := context.WithCancel(context.Background())
			s.dialerCancel = cancel

			dialer := newClientDialer(s.mdnsManager.Clients(), func(ctx context.Context, info *discovery.ClientInfo) error {
				return dialAndHandle(ctx, info, s.handleConnection)
			})

			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				dialer.run(dialCtx)
			}()
		}
	}

	s.mux.HandleFunc("/sendspin", s.handleWebSocket)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.streamAudio()
	}()

	addr := fmt.Sprintf(":%d", s.config.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	tcpAddr, _ := listener.Addr().(*net.TCPAddr)
	s.boundAddr.Store(tcpAddr)
	log.Printf("WebSocket server listening on %s", listener.Addr())

	s.httpServer = &http.Server{
		Handler: s.mux,
	}

	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	select {
	case <-s.stopChan:
		log.Printf("Server shutting down...")
	case err := <-errChan:
		log.Printf("HTTP server error: %v", err)
		return err
	}

	s.shutdownMu.Lock()
	s.isShutdown = true
	s.shutdownMu.Unlock()

	// Cancel in-flight client dials before stopping mDNS so they observe
	// context cancellation ahead of the discovery channel closing.
	if s.dialerCancel != nil {
		s.dialerCancel()
	}

	if s.mdnsManager != nil {
		s.mdnsManager.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	if err := s.audioSource.Close(); err != nil {
		log.Printf("Error closing audio source: %v", err)
	}

	if s.defaultGroup != nil {
		s.defaultGroup.Close()
	}

	s.wg.Wait()
	log.Printf("Server stopped cleanly")

	return nil
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
}

// getClockMicros returns server uptime in microseconds (monotonic, not wall clock).
func (s *Server) getClockMicros() int64 {
	return time.Since(s.clockStart).Microseconds()
}

// Group returns the server's default playback group. For M2 there is
// exactly one implicit group per Server; the accessor exists so future
// GroupRole implementations (M3) can subscribe to its event bus.
func (s *Server) Group() *Group {
	return s.defaultGroup
}

// Addr returns the network address the server is listening on, or nil if
// Start has not yet bound a listener. Useful in tests that configure
// Port: 0 (OS-assigned ephemeral port) and need to know the actual port
// after Start runs.
func (s *Server) Addr() net.Addr {
	if a := s.boundAddr.Load(); a != nil {
		return a
	}
	return nil
}

func (s *Server) Clients() []ClientInfo {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	clients := make([]ClientInfo, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, ClientInfo{
			ID:     c.ID(),
			Name:   c.Name(),
			State:  c.State(),
			Volume: c.Volume(),
			Muted:  c.Muted(),
			Codec:  c.Codec(),
		})
	}

	return clients
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	log.Printf("New WebSocket connection from %s", r.RemoteAddr)
	s.handleConnection(conn)
}

func (s *Server) handleConnection(conn *websocket.Conn) {
	defer conn.Close()
	conn.SetReadLimit(1 << 20) // 1MB

	s.shutdownMu.RLock()
	if s.isShutdown {
		s.shutdownMu.RUnlock()
		log.Printf("Rejecting connection during shutdown")
		return
	}
	s.shutdownMu.RUnlock()

	_, data, err := conn.ReadMessage()
	if err != nil {
		log.Printf("Error reading hello: %v", err)
		return
	}

	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return
	}

	if msg.Type != "client/hello" {
		log.Printf("Expected client/hello, got %s", msg.Type)
		return
	}

	helloData, err := json.Marshal(msg.Payload)
	if err != nil {
		log.Printf("Error marshaling hello payload: %v", err)
		return
	}

	var hello protocol.ClientHello
	if err := json.Unmarshal(helloData, &hello); err != nil {
		log.Printf("Error unmarshaling client hello: %v", err)
		return
	}

	if hello.ClientID == "" || hello.Name == "" {
		log.Printf("Client hello missing required fields")
		return
	}
	if len(hello.ClientID) > 256 || len(hello.Name) > 256 || len(hello.SupportedRoles) > 20 {
		log.Printf("Client hello fields exceed size limits")
		return
	}

	log.Printf("Client hello: %s (ID: %s, Roles: %v)", hello.Name, hello.ClientID, hello.SupportedRoles)

	c := &ServerClient{
		id:           hello.ClientID,
		name:         hello.Name,
		conn:         conn,
		roles:        hello.SupportedRoles,
		capabilities: hello.PlayerV1Support,
		state:        "synchronized",
		volume:       100,
		muted:        false,
		sendChan:     make(chan interface{}, 100),
		done:         make(chan struct{}),
	}

	s.clientsMu.Lock()
	if _, exists := s.clients[hello.ClientID]; exists {
		s.clientsMu.Unlock()
		log.Printf("Client ID %s already connected, rejecting duplicate", hello.ClientID)
		return
	}
	s.clients[c.id] = c
	s.clientsMu.Unlock()

	defer func() {
		s.removeClient(c)
		log.Printf("Client disconnected: %s", c.name)
	}()

	activeRoles := s.activateRoles(hello.SupportedRoles)
	serverHello := protocol.ServerHello{
		ServerID:         s.serverID,
		Name:             s.config.Name,
		Version:          ProtocolVersion,
		ActiveRoles:      activeRoles,
		ConnectionReason: "playback",
	}

	if err := c.Send("server/hello", serverHello); err != nil {
		log.Printf("Error sending server hello: %v", err)
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.clientWriter(c)
	}()

	// Add to group AFTER server/hello and writer start so that:
	// 1. server/hello is the first message the client receives
	// 2. The writer goroutine is running to deliver group/update
	//    (from addClient) and role-dispatched messages (stream/start,
	//    server/state) via sendChan.
	s.defaultGroup.addClient(c)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleClientMessage(c, data)
	}
}

func (s *Server) clientWriter(c *ServerClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg := <-c.sendChan:
			switch v := msg.(type) {
			case []byte:
				c.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := c.conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					return
				}
			default:
				data, err := json.Marshal(v)
				if err != nil {
					continue
				}
				c.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}

		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

func (s *Server) removeClient(c *ServerClient) {
	c.mu.Lock()
	if c.opusEncoder != nil {
		c.opusEncoder.Close()
		c.opusEncoder = nil
	}
	if c.flacEncoder != nil {
		c.flacEncoder.Close()
		c.flacEncoder = nil
	}
	c.resampler = nil
	c.mu.Unlock()

	s.clientsMu.Lock()
	delete(s.clients, c.id)
	s.clientsMu.Unlock()

	s.defaultGroup.removeClient(c)

	close(c.done)
}

// activateRoles filters a client's advertised role list down to the roles
// this server supports (via config + registered GroupRoles), keeping only
// the first version of each role family so "player@v1" wins over a later
// "player@v2" entry in the same hello.
func (s *Server) activateRoles(supportedRoles []string) []string {
	// Build the set of role families this server supports.
	allowed := make(map[string]bool)
	for _, family := range s.config.SupportedRoles {
		allowed[family] = true
	}

	// Roles registered on the Group are also allowed.
	if s.defaultGroup != nil {
		s.defaultGroup.mu.RLock()
		for family := range s.defaultGroup.roles {
			allowed[family] = true
		}
		s.defaultGroup.mu.RUnlock()
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(supportedRoles))

	for _, role := range supportedRoles {
		family := role
		if idx := strings.Index(role, "@"); idx > 0 {
			family = role[:idx]
		}

		if seen[family] {
			continue
		}

		if allowed[family] {
			seen[family] = true
			result = append(result, role)
		}
	}

	return result
}
