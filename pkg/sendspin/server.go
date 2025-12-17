// ABOUTME: High-level Server API for Sendspin streaming
// ABOUTME: Wraps server components into a simple, user-friendly interface
package sendspin

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
	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// ProtocolVersion is the version of the Sendspin protocol we implement
	ProtocolVersion = 1

	// Binary message type IDs per spec (bits 7-2 for role, bits 1-0 for slot)
	// Player role: 000001xx (4-7), slot 0 = 4
	AudioChunkMessageType = 4

	// Audio format constants
	DefaultSampleRate = 192000
	DefaultChannels   = 2
	DefaultBitDepth   = 24

	// Chunk timing
	ChunkDurationMs = 20  // 20ms chunks
	BufferAheadMs   = 500 // Send audio 500ms ahead
)

// ServerConfig configures a Sendspin server
type ServerConfig struct {
	// Port to listen on (default: 8927)
	Port int

	// Name of the server for identification
	Name string

	// Audio source to stream (required)
	Source AudioSource

	// EnableMDNS enables mDNS service advertisement (default: true)
	EnableMDNS bool

	// Debug enables debug logging
	Debug bool
}

// Server represents a Sendspin streaming server
type Server struct {
	config   ServerConfig
	serverID string

	// WebSocket upgrader
	upgrader websocket.Upgrader

	// HTTP server
	httpServer *http.Server
	mux        *http.ServeMux

	// Client management
	clients   map[string]*client
	clientsMu sync.RWMutex

	// Server clock (monotonic microseconds)
	clockStart time.Time

	// Audio streaming
	audioSource AudioSource

	// mDNS discovery
	mdnsManager *discovery.Manager

	// Control
	stopChan   chan struct{}
	stopOnce   sync.Once
	shutdownMu sync.RWMutex
	isShutdown bool
	wg         sync.WaitGroup
}

// client represents a connected client (internal)
type client struct {
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
	Codec       string
	OpusEncoder *server.OpusEncoder

	// Output channel for messages
	sendChan chan interface{}

	mu sync.RWMutex
}

// ClientInfo represents information about a connected client
type ClientInfo struct {
	ID     string
	Name   string
	State  string
	Volume int
	Muted  bool
	Codec  string
}

// NewServer creates a new Sendspin server
func NewServer(config ServerConfig) (*Server, error) {
	// Validate config
	if config.Port == 0 {
		config.Port = 8927
	}
	if config.Name == "" {
		config.Name = "Sendspin Server"
	}
	if config.Source == nil {
		return nil, fmt.Errorf("audio source is required")
	}

	mux := http.NewServeMux()

	s := &Server{
		config:      config,
		serverID:    uuid.New().String(),
		mux:         mux,
		audioSource: config.Source,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// For local network deployments, accept all origins
				// TODO: For production, implement proper origin validation
				return true
			},
		},
		clients:    make(map[string]*client),
		clockStart: time.Now(),
		stopChan:   make(chan struct{}),
	}

	return s, nil
}

// Start starts the server and begins streaming
func (s *Server) Start() error {
	log.Printf("Server starting: %s (ID: %s)", s.config.Name, s.serverID)
	log.Printf("Audio source: %dHz/%dbit/%dch",
		s.audioSource.SampleRate(),
		DefaultBitDepth,
		s.audioSource.Channels())

	// Start mDNS advertisement if enabled
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

	// Set up HTTP handlers
	s.mux.HandleFunc("/sendspin", s.handleWebSocket)

	// Start audio streaming
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.streamAudio()
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

	// Wait for stop signal or server error
	select {
	case <-s.stopChan:
		log.Printf("Server shutting down...")
	case err := <-errChan:
		log.Printf("HTTP server error: %v", err)
		return err
	}

	// Mark server as shutting down
	s.shutdownMu.Lock()
	s.isShutdown = true
	s.shutdownMu.Unlock()

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

	// Close audio source
	if err := s.audioSource.Close(); err != nil {
		log.Printf("Error closing audio source: %v", err)
	}

	s.wg.Wait()
	log.Printf("Server stopped cleanly")

	return nil
}

// Stop stops the server
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
}

// Clients returns information about all connected clients
func (s *Server) Clients() []ClientInfo {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	clients := make([]ClientInfo, 0, len(s.clients))
	for _, c := range s.clients {
		c.mu.RLock()
		clients = append(clients, ClientInfo{
			ID:     c.ID,
			Name:   c.Name,
			State:  c.State,
			Volume: c.Volume,
			Muted:  c.Muted,
			Codec:  c.Codec,
		})
		c.mu.RUnlock()
	}

	return clients
}

// streamAudio generates and sends audio chunks to clients
func (s *Server) streamAudio() {
	log.Printf("Audio streaming started")

	ticker := time.NewTicker(time.Duration(ChunkDurationMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.generateAndSendChunk()
		case <-s.stopChan:
			log.Printf("Audio streaming stopping")
			return
		}
	}
}

// generateAndSendChunk generates a chunk of audio and sends it to all clients
func (s *Server) generateAndSendChunk() {
	// Get current timestamp + buffer ahead time
	currentTime := s.getClockMicros()
	playbackTime := currentTime + (BufferAheadMs * 1000)

	// Calculate chunk size based on source sample rate
	chunkSamples := (s.audioSource.SampleRate() * ChunkDurationMs) / 1000
	totalSamples := chunkSamples * s.audioSource.Channels()

	// Read audio samples from source
	samples := make([]int32, totalSamples)
	n, err := s.audioSource.Read(samples)
	if err != nil {
		log.Printf("Error reading audio source: %v", err)
		return
	}

	// Send to all clients
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		var audioData []byte
		var encodeErr error

		c.mu.RLock()
		codec := c.Codec
		opusEncoder := c.OpusEncoder
		c.mu.RUnlock()

		// Encode based on client's negotiated codec
		switch codec {
		case "opus":
			if opusEncoder != nil {
				samples16 := convertToInt16(samples[:n])
				audioData, encodeErr = opusEncoder.Encode(samples16)
				if encodeErr != nil {
					log.Printf("Opus encode error for %s: %v", c.Name, encodeErr)
					continue
				}
			} else {
				continue
			}
		case "pcm":
			audioData = encodePCM(samples[:n])
		default:
			audioData = encodePCM(samples[:n])
		}

		// Create binary message
		chunk := createAudioChunk(playbackTime, audioData)

		if err := s.sendBinary(c, chunk); err != nil {
			if s.config.Debug {
				log.Printf("Error sending audio to %s: %v", c.Name, err)
			}
		}
	}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	log.Printf("New WebSocket connection from %s", r.RemoteAddr)
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

	if hello.ClientID == "" || hello.Name == "" {
		log.Printf("Client hello missing required fields")
		return
	}

	log.Printf("Client hello: %s (ID: %s, Roles: %v)", hello.Name, hello.ClientID, hello.SupportedRoles)

	// Create client
	c := &client{
		ID:           hello.ClientID,
		Name:         hello.Name,
		Conn:         conn,
		Roles:        hello.SupportedRoles,
		Capabilities: hello.PlayerV1Support,
		State:        "synchronized",
		Volume:       100,
		Muted:        false,
		sendChan:     make(chan interface{}, 100),
	}

	// Check for duplicate and register
	s.clientsMu.Lock()
	if _, exists := s.clients[hello.ClientID]; exists {
		s.clientsMu.Unlock()
		log.Printf("Client ID %s already connected, rejecting duplicate", hello.ClientID)
		return
	}
	s.clients[c.ID] = c
	s.clientsMu.Unlock()

	defer func() {
		s.removeClient(c)
		log.Printf("Client disconnected: %s", c.Name)
	}()

	// Send server/hello with active roles per spec
	activeRoles := s.activateRoles(hello.SupportedRoles)
	serverHello := protocol.ServerHello{
		ServerID:         s.serverID,
		Name:             s.config.Name,
		Version:          ProtocolVersion,
		ActiveRoles:      activeRoles,
		ConnectionReason: "playback", // We're a streaming server
	}

	if err := s.sendMessage(c, "server/hello", serverHello); err != nil {
		log.Printf("Error sending server hello: %v", err)
		return
	}

	// Start writer goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.clientWriter(c)
	}()

	// Start stream for player clients
	if s.hasRole(c, "player") {
		s.addClientToStream(c)
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

		s.handleClientMessage(c, data)
	}
}

// clientWriter sends messages to the client
func (s *Server) clientWriter(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg, ok := <-c.sendChan:
			if !ok {
				return
			}

			switch v := msg.(type) {
			case []byte:
				c.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := c.Conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					return
				}
			default:
				data, err := json.Marshal(v)
				if err != nil {
					continue
				}
				c.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := c.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}

		case <-ticker.C:
			if err := c.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}

// handleClientMessage processes messages from clients
func (s *Server) handleClientMessage(c *client, data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return
	}

	switch msg.Type {
	case "client/time":
		s.handleTimeSync(c, msg.Payload)
	case "client/state":
		s.handleClientState(c, msg.Payload)
	case "client/goodbye":
		s.handleClientGoodbye(c, msg.Payload)
	default:
		if s.config.Debug {
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
}

// handleTimeSync responds to time synchronization requests
func (s *Server) handleTimeSync(c *client, payload interface{}) {
	serverRecv := s.getClockMicros()

	timeData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var clientTime protocol.ClientTime
	if err := json.Unmarshal(timeData, &clientTime); err != nil {
		return
	}

	serverSend := s.getClockMicros()

	response := protocol.ServerTime{
		ClientTransmitted: clientTime.ClientTransmitted,
		ServerReceived:    serverRecv,
		ServerTransmitted: serverSend,
	}

	s.sendMessage(c, "server/time", response)
}

// handleClientState handles state updates from clients per spec
func (s *Server) handleClientState(c *client, payload interface{}) {
	stateData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var stateMsg protocol.ClientStateMessage
	if err := json.Unmarshal(stateData, &stateMsg); err != nil {
		return
	}

	// Handle player state
	if stateMsg.Player != nil {
		c.mu.Lock()
		c.State = stateMsg.Player.State
		c.Volume = stateMsg.Player.Volume
		c.Muted = stateMsg.Player.Muted
		c.mu.Unlock()

		if s.config.Debug {
			log.Printf("Client %s state: %s (vol: %d, muted: %v)", c.Name, stateMsg.Player.State, stateMsg.Player.Volume, stateMsg.Player.Muted)
		}
	}
}

// handleClientGoodbye handles graceful disconnect from clients
func (s *Server) handleClientGoodbye(c *client, payload interface{}) {
	goodbyeData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var goodbye protocol.ClientGoodbye
	if err := json.Unmarshal(goodbyeData, &goodbye); err != nil {
		return
	}

	log.Printf("Client %s goodbye: %s", c.Name, goodbye.Reason)
	// Connection will be closed after message handling
}

// addClientToStream adds a client to receive audio
func (s *Server) addClientToStream(c *client) {
	// Negotiate codec
	codec := s.negotiateCodec(c)

	// Create encoder if needed
	var opusEncoder *server.OpusEncoder
	chunkSamples := (s.audioSource.SampleRate() * ChunkDurationMs) / 1000

	switch codec {
	case "opus":
		encoder, err := server.NewOpusEncoder(s.audioSource.SampleRate(), s.audioSource.Channels(), chunkSamples)
		if err != nil {
			log.Printf("Failed to create Opus encoder for %s, falling back to PCM: %v", c.Name, err)
			codec = "pcm"
		} else {
			opusEncoder = encoder
		}
	case "flac":
		log.Printf("FLAC streaming not supported for %s, using PCM", c.Name)
		codec = "pcm"
	}

	c.mu.Lock()
	c.Codec = codec
	c.OpusEncoder = opusEncoder
	c.mu.Unlock()

	log.Printf("Added client %s with codec %s", c.Name, codec)

	// Send stream/start message
	streamStart := protocol.StreamStart{
		Player: &protocol.StreamStartPlayer{
			Codec:      codec,
			SampleRate: s.audioSource.SampleRate(),
			Channels:   s.audioSource.Channels(),
			BitDepth:   DefaultBitDepth,
		},
	}

	s.sendMessage(c, "stream/start", streamStart)

	// Send metadata via server/state per spec
	title, artist, album := s.audioSource.Metadata()
	serverState := protocol.ServerStateMessage{
		Metadata: &protocol.MetadataState{
			Timestamp: s.getClockMicros(),
			Title:     strPtr(title),
			Artist:    strPtr(artist),
			Album:     strPtr(album),
		},
	}

	s.sendMessage(c, "server/state", serverState)

	// Send group/update per spec
	groupID := s.serverID
	playbackState := "playing"
	groupUpdate := protocol.GroupUpdate{
		GroupID:       &groupID,
		PlaybackState: &playbackState,
	}

	s.sendMessage(c, "group/update", groupUpdate)
}

// removeClient removes a client
func (s *Server) removeClient(c *client) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	c.mu.Lock()
	if c.OpusEncoder != nil {
		c.OpusEncoder.Close()
		c.OpusEncoder = nil
	}
	c.mu.Unlock()

	delete(s.clients, c.ID)
	close(c.sendChan)
}

// strPtr returns a pointer to the given string
func strPtr(s string) *string {
	return &s
}

// negotiateCodec selects the best codec based on client capabilities
func (s *Server) negotiateCodec(c *client) string {
	if c.Capabilities == nil {
		return "pcm"
	}

	sourceRate := s.audioSource.SampleRate()

	// Prioritize PCM at native rate
	for _, format := range c.Capabilities.SupportedFormats {
		if format.Codec == "pcm" && format.SampleRate == sourceRate && format.BitDepth == DefaultBitDepth {
			return "pcm"
		}
	}

	// Consider compressed codecs
	for _, format := range c.Capabilities.SupportedFormats {
		if format.Codec == "opus" && sourceRate == 48000 {
			return "opus"
		}
		if format.Codec == "flac" {
			return "flac"
		}
	}

	return "pcm"
}

// sendMessage sends a JSON message to a client
func (s *Server) sendMessage(c *client, msgType string, payload interface{}) error {
	msg := protocol.Message{
		Type:    msgType,
		Payload: payload,
	}

	select {
	case c.sendChan <- msg:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// sendBinary sends binary data to a client
func (s *Server) sendBinary(c *client, data []byte) error {
	select {
	case c.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// getClockMicros returns the server clock in microseconds
func (s *Server) getClockMicros() int64 {
	return time.Since(s.clockStart).Microseconds()
}

// hasRole checks if a client has a specific role (handles versioned roles)
func (s *Server) hasRole(c *client, role string) bool {
	for _, r := range c.Roles {
		// Match exact role or versioned role (e.g., "player" matches "player@v1")
		if r == role || strings.HasPrefix(r, role+"@") {
			return true
		}
	}
	return false
}

// activateRoles returns the active roles based on client's supported roles
// Preserves input order (first occurrence of each role family)
func (s *Server) activateRoles(supportedRoles []string) []string {
	// Track which families we've seen to avoid duplicates
	seen := make(map[string]bool)
	// Result preserves input order
	result := make([]string, 0, len(supportedRoles))

	for _, role := range supportedRoles {
		// Extract role family (e.g., "player" from "player@v1")
		family := role
		if idx := strings.Index(role, "@"); idx > 0 {
			family = role[:idx]
		}

		// Only keep the first (highest priority) version of each role
		if seen[family] {
			continue
		}

		// Check if we support this role family
		switch family {
		case "player", "metadata", "visualizer", "artwork", "controller":
			seen[family] = true
			result = append(result, role)
		}
	}

	return result
}

// createAudioChunk creates a binary audio chunk message
func createAudioChunk(timestamp int64, audioData []byte) []byte {
	chunk := make([]byte, 1+8+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:9], uint64(timestamp))
	copy(chunk[9:], audioData)
	return chunk
}

// convertToInt16 converts int32 samples to int16 (for Opus encoding)
func convertToInt16(samples []int32) []int16 {
	result := make([]int16, len(samples))
	for i, s := range samples {
		result[i] = int16(s >> 8)
	}
	return result
}

// encodePCM encodes int32 samples as 24-bit PCM bytes
func encodePCM(samples []int32) []byte {
	output := make([]byte, len(samples)*3)
	for i, sample := range samples {
		output[i*3] = byte(sample)
		output[i*3+1] = byte(sample >> 8)
		output[i*3+2] = byte(sample >> 16)
	}
	return output
}
