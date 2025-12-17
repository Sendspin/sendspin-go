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
	Capabilities *protocol.PlayerSupport

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

	mu sync.RWMutex
}

// New creates a new server instance
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

// Start starts the server
func (s *Server) Start() error {
	// Start TUI if enabled
	if s.config.UseTUI {
		s.tui = NewServerTUI(s.config.Name, s.config.Port)

		// Start TUI in a goroutine
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.tui.Start(s.config.Name, s.config.Port)
		}()

		// Give TUI time to initialize
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("Server starting: %s (ID: %s)", s.config.Name, s.serverID)

	// Create audio engine
	audioEngine, err := NewAudioEngine(s)
	if err != nil {
		return fmt.Errorf("failed to create audio engine: %w", err)
	}
	s.audioEngine = audioEngine

	// Start mDNS advertisement if enabled
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

	// Set up HTTP handlers
	s.mux.HandleFunc("/sendspin", s.handleWebSocket)

	// Start audio streaming
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.audioEngine.Start()
	}()

	// Start HTTP server
	addr := fmt.Sprintf(":%d", s.config.Port)
	log.Printf("WebSocket server listening on %s", addr)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	// Run server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for stop signal, TUI quit, or server error
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

	// Mark server as shutting down to reject new connections
	s.shutdownMu.Lock()
	s.isShutdown = true
	s.shutdownMu.Unlock()

	// Stop TUI first so it can display shutdown message
	if s.tui != nil {
		s.tui.Stop()
	}

	// Stop audio engine
	s.audioEngine.Stop()

	// Stop mDNS
	if s.mdnsManager != nil {
		s.mdnsManager.Stop()
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	s.wg.Wait()
	log.Printf("Server stopped cleanly")

	// Return server error if one occurred
	if serverErr != nil {
		return fmt.Errorf("HTTP server failed: %w", serverErr)
	}
	return nil
}

// Stop stops the server
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	log.Printf("New WebSocket connection from %s", r.RemoteAddr)

	// Handle the connection
	s.handleConnection(conn)
}

// handleConnection manages a client connection
func (s *Server) handleConnection(conn *websocket.Conn) {
	defer conn.Close()

	// Check if server is shutting down
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

	// Wait for client/hello
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

	// Parse client hello
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

	// Validate client hello
	if hello.ClientID == "" {
		log.Printf("Client hello missing ClientID")
		return
	}
	if hello.Name == "" {
		log.Printf("Client hello missing Name")
		return
	}

	log.Printf("Client hello: %s (ID: %s, Roles: %v)", hello.Name, hello.ClientID, hello.SupportedRoles)

	// Create client before acquiring lock
	client := &Client{
		ID:           hello.ClientID,
		Name:         hello.Name,
		Conn:         conn,
		Roles:        hello.SupportedRoles,
		Capabilities: hello.PlayerSupport,
		State:        "idle",
		Volume:       100,
		Muted:        false,
		sendChan:     make(chan interface{}, 100),
	}

	// Check for duplicate client ID and register atomically
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

	// Register client
	s.clients[client.ID] = client
	s.clientsMu.Unlock()

	// Update TUI with new client
	s.updateTUI()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client.ID)
		s.clientsMu.Unlock()
		close(client.sendChan)
		log.Printf("Client disconnected: %s", client.Name)

		// Update TUI after client disconnect
		s.updateTUI()
	}()

	// Send server/hello
	serverHello := protocol.ServerHello{
		ServerID: s.serverID,
		Name:     s.config.Name,
		Version:  ProtocolVersion,
	}

	if err := s.sendMessage(client, "server/hello", serverHello); err != nil {
		log.Printf("Error sending server hello: %v", err)
		return
	}

	// Start writer goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.clientWriter(client)
	}()

	// Start stream for this client if it's a player
	if s.hasRole(client, "player") {
		s.audioEngine.AddClient(client)
		defer s.audioEngine.RemoveClient(client)
	}

	// Read messages from client
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

// clientWriter sends messages to the client
func (s *Server) clientWriter(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg, ok := <-client.sendChan:
			if !ok {
				return
			}

			// Determine message type
			switch v := msg.(type) {
			case []byte:
				// Binary message
				client.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := client.Conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					log.Printf("Error writing binary message: %v", err)
					return
				}
			default:
				// JSON message
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
			// Send ping
			if err := client.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}

// handleClientMessage processes messages from clients
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

// handleTimeSync responds to time synchronization requests
func (s *Server) handleTimeSync(client *Client, payload interface{}) {
	// Capture receive time as early as possible
	serverRecv := s.getClockMicros()

	// Parse client time
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

	// Capture transmit time before queueing message
	// Note: This timestamp represents the queue time, not the actual wire time.
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

// handleClientState handles state updates from clients.
// Supports both legacy "player/update" payloads and spec-style "client/state" payloads.
func (s *Server) handleClientState(client *Client, payload interface{}) {
	stateData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling state payload: %v", err)
		return
	}

	// Spec-style wrapper: { "player": { ... } }
	var wrapped struct {
		Player *protocol.ClientState `json:"player,omitempty"`
	}
	if err := json.Unmarshal(stateData, &wrapped); err == nil && wrapped.Player != nil {
		s.applyClientState(client, *wrapped.Player)
		return
	}

	// Legacy payload: { "state": "...", "volume": ..., "muted": ... }
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

// sendMessage sends a JSON message to a client
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

// sendBinary sends binary data to a client
func (s *Server) sendBinary(client *Client, data []byte) error {
	select {
	case client.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// getClockMicros returns the server clock in microseconds
func (s *Server) getClockMicros() int64 {
	return time.Since(s.clockStart).Microseconds()
}

// hasRole checks if a client has a specific role
func (s *Server) hasRole(client *Client, role string) bool {
	for _, r := range client.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// CreateAudioChunk creates a binary audio chunk message
func CreateAudioChunk(timestamp int64, audioData []byte) []byte {
	// Binary format: [message_type:1][timestamp:8][audio_data:N]
	chunk := make([]byte, 1+8+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:9], uint64(timestamp))
	copy(chunk[9:], audioData)
	return chunk
}
