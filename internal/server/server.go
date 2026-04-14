// ABOUTME: Main server implementation for Sendspin Protocol
// ABOUTME: Manages WebSocket connections, client state, and audio streaming
package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/internal/discovery"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Protocol constants
	ProtocolVersion = 1

	// Message type for binary audio chunks
	// Per spec: Player role binary messages use IDs 4-7 (bits 000001xx), slot 0 is audio
	AudioChunkMessageType = 4
)

// Config holds server configuration
type Config struct {
	Port       int
	Name       string
	EnableMDNS bool
	Debug      bool
	UseTUI     bool
	AudioFile  string // Path to audio file to stream (MP3, FLAC, WAV). Empty = test tone
}

// Server represents the Sendspin server
type Server struct {
	config   Config
	serverID string

	// WebSocket upgrader
	upgrader websocket.Upgrader

	// HTTP server
	httpServer *http.Server
	mux        *http.ServeMux

	// Client management
	clients   map[string]*Client
	clientsMu sync.RWMutex

	// Server clock (monotonic microseconds)
	clockStart time.Time

	// Audio streaming
	audioEngine *AudioEngine

	// mDNS discovery
	mdnsManager *discovery.Manager

	// TUI
	tui       *ServerTUI
	startTime time.Time

	// Control
	stopChan   chan struct{}
	stopOnce   sync.Once // Ensure Stop() is only called once
	shutdownMu sync.RWMutex
	isShutdown bool
	wg         sync.WaitGroup
}

// Client represents a connected client
type Client struct {
	ID           string
	Name         string
	Conn         *websocket.Conn
	Roles        []string
	Capabilities *protocol.PlayerV1Support

	// State
	State  string
	Volume int
	Muted  bool

	// Negotiated codec for this client
	Codec       string       // "pcm" or "opus" (flac falls back to pcm)
	OpusEncoder *OpusEncoder // Opus encoder (if using opus codec)
	Resampler   *Resampler   // Resampler for Opus (if source rate != 48kHz)

	// Output channel for messages
	sendChan chan interface{}
	done     chan struct{}

	mu sync.RWMutex
}

func New(config Config) *Server {
	mux := http.NewServeMux()

	return &Server{
		config:   config,
		serverID: uuid.New().String(),
		mux:      mux,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// TODO: For production deployment, implement proper origin validation
				// Currently allows all origins for local network deployments
				// This server is designed for trusted local networks only
				origin := r.Header.Get("Origin")
				if origin == "" {
					// Allow non-browser clients (no Origin header)
					return true
				}
				// Accept localhost origins for development
				if origin == "http://localhost" || origin == "http://127.0.0.1" {
					return true
				}
				// For production: implement allowlist-based validation
				log.Printf("Warning: accepting WebSocket from origin: %s", origin)
				return true
			},
		},
		clients:    make(map[string]*Client),
		clockStart: time.Now(),
		startTime:  time.Now(),
		stopChan:   make(chan struct{}),
	}
}

func (s *Server) Start() error {
	if s.config.UseTUI {
		s.tui = NewServerTUI(s.config.Name, s.config.Port)

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.tui.Start(s.config.Name, s.config.Port)
		}()

		// Give TUI time to initialize
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("Server starting: %s (ID: %s)", s.config.Name, s.serverID)

	audioEngine, err := NewAudioEngine(s)
	if err != nil {
		return fmt.Errorf("failed to create audio engine: %w", err)
	}
	s.audioEngine = audioEngine

	if s.config.EnableMDNS {
		s.mdnsManager = discovery.NewManager(discovery.Config{
			ServiceName: s.config.Name,
			Port:        s.config.Port,
			ServerMode:  true, // Advertise as server
		})

		if err := s.mdnsManager.Advertise(); err != nil {
			log.Printf("Failed to start mDNS advertisement: %v", err)
		} else {
			log.Printf("mDNS advertisement started")
		}
	}

	s.mux.HandleFunc("/sendspin", s.handleWebSocket)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.audioEngine.Start()
	}()

	addr := fmt.Sprintf(":%d", s.config.Port)
	log.Printf("WebSocket server listening on %s", addr)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	var serverErr error
	var tuiQuitChan <-chan struct{}
	if s.tui != nil {
		tuiQuitChan = s.tui.QuitChan()
	}

	select {
	case <-s.stopChan:
		log.Printf("Server shutting down...")
	case <-tuiQuitChan:
		log.Printf("TUI quit requested, shutting down...")
	case err := <-errChan:
		log.Printf("HTTP server error: %v", err)
		serverErr = err
		// Fall through to cleanup
	}

	s.shutdownMu.Lock()
	s.isShutdown = true
	s.shutdownMu.Unlock()

	if s.tui != nil {
		s.tui.Stop()
	}

	s.audioEngine.Stop()

	if s.mdnsManager != nil {
		s.mdnsManager.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	s.wg.Wait()
	log.Printf("Server stopped cleanly")

	if serverErr != nil {
		return fmt.Errorf("HTTP server failed: %w", serverErr)
	}
	return nil
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
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

	if s.config.Debug {
		log.Printf("[DEBUG] New connection, waiting for handshake")
	}

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

	if hello.ClientID == "" {
		log.Printf("Client hello missing ClientID")
		return
	}
	if hello.Name == "" {
		log.Printf("Client hello missing Name")
		return
	}
	if len(hello.ClientID) > 256 || len(hello.Name) > 256 || len(hello.SupportedRoles) > 20 {
		log.Printf("Client hello fields exceed size limits")
		return
	}

	log.Printf("Client hello: %s (ID: %s, Roles: %v)", hello.Name, hello.ClientID, hello.SupportedRoles)

	client := &Client{
		ID:           hello.ClientID,
		Name:         hello.Name,
		Conn:         conn,
		Roles:        hello.SupportedRoles,
		Capabilities: hello.PlayerV1Support,
		State:        "idle",
		Volume:       100,
		Muted:        false,
		sendChan:     make(chan interface{}, 100),
		done:         make(chan struct{}),
	}

	s.clientsMu.Lock()
	if existingClient, exists := s.clients[hello.ClientID]; exists {
		s.clientsMu.Unlock()
		log.Printf("Client ID %s already connected (name: %s), rejecting duplicate", hello.ClientID, existingClient.Name)

		// Send error message to client
		errorMsg := protocol.Message{
			Type: "server/error",
			Payload: map[string]string{
				"error":   "duplicate_client_id",
				"message": "Client ID already connected",
			},
		}
		if data, err := json.Marshal(errorMsg); err == nil {
			conn.WriteMessage(websocket.TextMessage, data)
		}
		return
	}

	s.clients[client.ID] = client
	s.clientsMu.Unlock()

	s.updateTUI()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client.ID)
		s.clientsMu.Unlock()
		close(client.done)
		log.Printf("Client disconnected: %s", client.Name)

		s.updateTUI()
	}()

	serverHello := protocol.ServerHello{
		ServerID:         s.serverID,
		Name:             s.config.Name,
		Version:          ProtocolVersion,
		ActiveRoles:      s.activateRoles(hello.SupportedRoles),
		ConnectionReason: "playback",
	}

	if err := s.sendMessage(client, "server/hello", serverHello); err != nil {
		log.Printf("Error sending server hello: %v", err)
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.clientWriter(client)
	}()

	if s.hasRole(client, "player") {
		s.audioEngine.AddClient(client)
		defer s.audioEngine.RemoveClient(client)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleClientMessage(client, data)
	}
}

func (s *Server) clientWriter(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg := <-client.sendChan:
			switch v := msg.(type) {
			case []byte:
				client.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := client.Conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					log.Printf("Error writing binary message: %v", err)
					return
				}
			default:
				data, err := json.Marshal(v)
				if err != nil {
					log.Printf("Error marshaling message: %v", err)
					continue
				}
				client.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
					log.Printf("Error writing text message: %v", err)
					return
				}
			}

		case <-ticker.C:
			if err := client.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}

		case <-client.done:
			return
		}
	}
}

func (s *Server) handleClientMessage(client *Client, data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return
	}

	switch msg.Type {
	case "client/time":
		s.handleTimeSync(client, msg.Payload)
	case "player/update":
		s.handleClientState(client, msg.Payload)
	case "client/state":
		s.handleClientState(client, msg.Payload)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

func (s *Server) handleTimeSync(client *Client, payload interface{}) {
	// Capture receive time as early as possible
	serverRecv := s.getClockMicros()

	timeData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling time payload: %v", err)
		return
	}

	var clientTime protocol.ClientTime
	if err := json.Unmarshal(timeData, &clientTime); err != nil {
		log.Printf("Error unmarshaling client time: %v", err)
		return
	}

	// Note: This timestamp is the queue time, not the actual wire time.
	// The message is queued to sendChan and transmitted asynchronously by clientWriter.
	// For more accurate timing, the timestamp would need to be captured immediately
	// before the actual WebSocket write operation.
	serverSend := s.getClockMicros()

	if s.config.Debug {
		log.Printf("[DEBUG] Time sync for %s: t1=%d, t2=%d, t3=%d",
			client.Name, clientTime.ClientTransmitted, serverRecv, serverSend)
	}

	response := protocol.ServerTime{
		ClientTransmitted: clientTime.ClientTransmitted,
		ServerReceived:    serverRecv,
		ServerTransmitted: serverSend,
	}

	if err := s.sendMessage(client, "server/time", response); err != nil {
		log.Printf("Error sending server time: %v", err)
	}
}

// handleClientState accepts both legacy "player/update" and spec-style "client/state" payloads.
func (s *Server) handleClientState(client *Client, payload interface{}) {
	stateData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling state payload: %v", err)
		return
	}

	var wrapped struct {
		Player *protocol.ClientState `json:"player,omitempty"`
	}
	if err := json.Unmarshal(stateData, &wrapped); err == nil && wrapped.Player != nil {
		s.applyClientState(client, *wrapped.Player)
		return
	}

	var state protocol.ClientState
	if err := json.Unmarshal(stateData, &state); err == nil {
		s.applyClientState(client, state)
		return
	}

	log.Printf("Error unmarshaling client state: %s", string(stateData))
}

func (s *Server) applyClientState(client *Client, state protocol.ClientState) {
	client.mu.Lock()
	client.State = state.State
	client.Volume = state.Volume
	client.Muted = state.Muted
	client.mu.Unlock()

	log.Printf("Client %s state: %s (vol: %d, muted: %v)", client.Name, state.State, state.Volume, state.Muted)
}

func (s *Server) sendMessage(client *Client, msgType string, payload interface{}) error {
	msg := protocol.Message{
		Type:    msgType,
		Payload: payload,
	}

	select {
	case client.sendChan <- msg:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

func (s *Server) sendBinary(client *Client, data []byte) error {
	select {
	case client.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

func (s *Server) getClockMicros() int64 {
	return time.Since(s.clockStart).Microseconds()
}

// hasRole checks if a client has a role, accepting both bare ("player") and versioned ("player@1") forms.
func (s *Server) hasRole(client *Client, role string) bool {
	for _, r := range client.Roles {
		if r == role || strings.HasPrefix(r, role+"@") {
			return true
		}
	}
	return false
}

// activateRoles filters to roles this server implements, preserving input order.
func (s *Server) activateRoles(supportedRoles []string) []string {
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

		switch family {
		case "player", "metadata":
			seen[family] = true
			result = append(result, role)
		}
	}

	return result
}

// CreateAudioChunk encodes a binary audio frame: [type:1][timestamp:8][data:N].
func CreateAudioChunk(timestamp int64, audioData []byte) []byte {
	chunk := make([]byte, 1+8+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:9], uint64(timestamp))
	copy(chunk[9:], audioData)
	return chunk
}
