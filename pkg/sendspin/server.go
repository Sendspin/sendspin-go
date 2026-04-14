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
	"github.com/Sendspin/sendspin-go/pkg/audio"
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
}

type Server struct {
	config   ServerConfig
	serverID string

	upgrader websocket.Upgrader

	httpServer *http.Server
	mux        *http.ServeMux

	clients   map[string]*client
	clientsMu sync.RWMutex

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

type client struct {
	ID           string
	Name         string
	Conn         *websocket.Conn
	Roles        []string
	Capabilities *protocol.PlayerV1Support

	State  string
	Volume int
	Muted  bool

	Codec       string
	OpusEncoder *server.OpusEncoder
	Resampler   *audio.Resampler // non-nil only when source rate != 48kHz

	sendChan chan interface{}
	done     chan struct{}

	mu sync.RWMutex
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
				// TODO: For production, implement proper origin validation.
				// Currently permissive for local-network deployments.
				return true
			},
		},
		clients:    make(map[string]*client),
		clockStart: time.Now(),
		stopChan:   make(chan struct{}),
	}

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

	s.wg.Wait()
	log.Printf("Server stopped cleanly")

	return nil
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
}

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

func (s *Server) generateAndSendChunk() {
	currentTime := s.getClockMicros()
	playbackTime := currentTime + (BufferAheadMs * 1000)

	chunkSamples := (s.audioSource.SampleRate() * ChunkDurationMs) / 1000
	totalSamples := chunkSamples * s.audioSource.Channels()

	samples := make([]int32, totalSamples)
	n, err := s.audioSource.Read(samples)
	if err != nil {
		s.consecutiveReadErrs++
		// Log every error for the first few, then throttle
		if s.consecutiveReadErrs <= 3 || s.consecutiveReadErrs%50 == 0 {
			log.Printf("Error reading audio source (%d consecutive): %v", s.consecutiveReadErrs, err)
		}
		// After 1 second of failures (50 ticks at 20ms), notify clients
		if s.consecutiveReadErrs == 50 {
			log.Printf("Audio source failed for 1s, sending stream/end to all clients")
			s.notifyStreamEnd()
		}
		return
	}
	s.consecutiveReadErrs = 0

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		var audioData []byte
		var encodeErr error

		c.mu.RLock()
		codec := c.Codec
		opusEncoder := c.OpusEncoder
		resampler := c.Resampler
		c.mu.RUnlock()

		switch codec {
		case "opus":
			if opusEncoder != nil {
				samplesToEncode := samples[:n]

				// Resample when source rate != 48kHz (Opus is locked to 48kHz)
				if resampler != nil {
					outputSamples := resampler.OutputSamplesNeeded(len(samplesToEncode))
					resampled := make([]int32, outputSamples)
					samplesWritten := resampler.Resample(samplesToEncode, resampled)
					samplesToEncode = resampled[:samplesWritten]
				}

				samples16 := convertToInt16(samplesToEncode)
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

		chunk := createAudioChunk(playbackTime, audioData)

		if err := s.sendBinary(c, chunk); err != nil {
			if s.config.Debug {
				log.Printf("Error sending audio to %s: %v", c.Name, err)
			}
		}
	}
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
		done:         make(chan struct{}),
	}

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

	activeRoles := s.activateRoles(hello.SupportedRoles)
	serverHello := protocol.ServerHello{
		ServerID:         s.serverID,
		Name:             s.config.Name,
		Version:          ProtocolVersion,
		ActiveRoles:      activeRoles,
		ConnectionReason: "playback",
	}

	if err := s.sendMessage(c, "server/hello", serverHello); err != nil {
		log.Printf("Error sending server hello: %v", err)
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.clientWriter(c)
	}()

	if s.hasRole(c, "player") {
		s.addClientToStream(c)
	}

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

func (s *Server) clientWriter(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg := <-c.sendChan:
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

		case <-c.done:
			return
		}
	}
}

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

// handleClientState applies a client's player state update per spec.
func (s *Server) handleClientState(c *client, payload interface{}) {
	stateData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var stateMsg protocol.ClientStateMessage
	if err := json.Unmarshal(stateData, &stateMsg); err != nil {
		return
	}

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
	// Connection close happens in handleConnection's read loop once this returns.
}

func (s *Server) addClientToStream(c *client) {
	codec := s.negotiateCodec(c)

	var opusEncoder *server.OpusEncoder
	var resampler *audio.Resampler
	sourceRate := s.audioSource.SampleRate()

	switch codec {
	case "opus":
		// Opus requires 48kHz — create resampler if source rate differs
		if sourceRate != 48000 {
			resampler = audio.NewResampler(sourceRate, 48000, s.audioSource.Channels())
			log.Printf("Created resampler: %dHz -> 48kHz for Opus (client: %s)", sourceRate, c.Name)
		}

		opusChunkSamples := (48000 * ChunkDurationMs) / 1000
		encoder, err := server.NewOpusEncoder(48000, s.audioSource.Channels(), opusChunkSamples)
		if err != nil {
			log.Printf("Failed to create Opus encoder for %s, falling back to PCM: %v", c.Name, err)
			codec = "pcm"
			resampler = nil
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
	c.Resampler = resampler
	c.mu.Unlock()

	log.Printf("Added client %s with codec %s", c.Name, codec)

	// For Opus, report 48kHz to the client since that's what it will decode
	// (the resampler runs server-side before encoding).
	streamSampleRate := s.audioSource.SampleRate()
	streamBitDepth := DefaultBitDepth
	if codec == "opus" {
		streamSampleRate = 48000
		streamBitDepth = 16
	}

	streamStart := protocol.StreamStart{
		Player: &protocol.StreamStartPlayer{
			Codec:      codec,
			SampleRate: streamSampleRate,
			Channels:   s.audioSource.Channels(),
			BitDepth:   streamBitDepth,
		},
	}

	s.sendMessage(c, "stream/start", streamStart)

	// server/state carries the initial metadata snapshot per spec.
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

	// group/update is required by spec even when we host a single implicit group.
	groupID := s.serverID
	playbackState := "playing"
	groupUpdate := protocol.GroupUpdate{
		GroupID:       &groupID,
		PlaybackState: &playbackState,
	}

	s.sendMessage(c, "group/update", groupUpdate)
}

func (s *Server) notifyStreamEnd() {
	streamEnd := protocol.StreamEnd{
		Roles: []string{"player"},
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		if s.hasRole(c, "player") {
			s.sendMessage(c, "stream/end", streamEnd)
		}
	}
}

func (s *Server) removeClient(c *client) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	c.mu.Lock()
	if c.OpusEncoder != nil {
		c.OpusEncoder.Close()
		c.OpusEncoder = nil
	}
	c.Resampler = nil
	c.mu.Unlock()

	delete(s.clients, c.ID)
	close(c.done)
}

func strPtr(s string) *string {
	return &s
}

// negotiateCodec picks PCM at the source's native rate when advertised
// (lossless hi-res), then falls through to Opus for bandwidth savings, then
// PCM as a last resort.
func (s *Server) negotiateCodec(c *client) string {
	if c.Capabilities == nil {
		return "pcm"
	}

	sourceRate := s.audioSource.SampleRate()

	for _, format := range c.Capabilities.SupportedFormats {
		if format.Codec == "pcm" && format.SampleRate == sourceRate && format.BitDepth == DefaultBitDepth {
			return "pcm"
		}
	}

	for _, format := range c.Capabilities.SupportedFormats {
		if format.Codec == "opus" {
			return "opus"
		}
	}

	return "pcm"
}

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

func (s *Server) sendBinary(c *client, data []byte) error {
	select {
	case c.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// getClockMicros returns server uptime in microseconds (monotonic, not wall clock).
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

// activateRoles filters a client's advertised role list down to the roles this
// server actually implements, keeping only the first version of each role family
// so "player@v1" wins over a later "player@v2" entry in the same hello.
//
// Implemented: player (audio streaming), metadata (track info via server/state).
// Not implemented: visualizer, artwork, controller.
func (s *Server) activateRoles(supportedRoles []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(supportedRoles))

	for _, role := range supportedRoles {
		// Extract role family (e.g., "player" from "player@v1")
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

// createAudioChunk packs timestamp + payload into a Sendspin binary frame:
// [1 byte message type][8 byte big-endian timestamp (µs)][audio bytes].
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
