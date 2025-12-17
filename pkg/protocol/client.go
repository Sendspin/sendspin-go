// ABOUTME: WebSocket client for Sendspin Protocol communication
// ABOUTME: Handles connection, handshake, and message routing
package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// BinaryMessageHeaderSize is the size of binary message header (type byte + timestamp)
	BinaryMessageHeaderSize = 1 + 8 // 9 bytes: 1 byte type + 8 byte timestamp

	// AudioChunkMessageType is the binary message type ID for audio chunks
	// Per spec: Player role binary messages use IDs 4-7 (bits 000001xx), slot 0 is audio
	AudioChunkMessageType = 4
)

// Config holds client configuration
type Config struct {
	ServerAddr          string
	ClientID            string
	Name                string
	Version             int
	DeviceInfo          DeviceInfo
	PlayerV1Support     PlayerV1Support
	ArtworkV1Support    *ArtworkV1Support
	VisualizerV1Support *VisualizerV1Support
}

// Client represents a WebSocket client
type Client struct {
	config Config
	conn   *websocket.Conn
	mu     sync.RWMutex

	// Message channels
	AudioChunks  chan AudioChunk
	ControlMsgs  chan PlayerCommand
	TimeSyncResp chan ServerTime
	StreamStart  chan StreamStart
	StreamClear  chan StreamClear
	StreamEnd    chan StreamEnd
	ServerState  chan ServerStateMessage
	GroupUpdate  chan GroupUpdate

	// State
	connected bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// AudioChunk represents a timestamped audio frame
type AudioChunk struct {
	Timestamp int64  // Microseconds, server clock
	Data      []byte // Encoded audio
}

// NewClient creates a new WebSocket client
func NewClient(config Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		config:       config,
		AudioChunks:  make(chan AudioChunk, 100),
		ControlMsgs:  make(chan PlayerCommand, 10),
		TimeSyncResp: make(chan ServerTime, 10),
		StreamStart:  make(chan StreamStart, 1),
		StreamClear:  make(chan StreamClear, 10),
		StreamEnd:    make(chan StreamEnd, 1),
		ServerState:  make(chan ServerStateMessage, 10),
		GroupUpdate:  make(chan GroupUpdate, 10),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Connect establishes WebSocket connection and performs handshake
func (c *Client) Connect() error {
	u := url.URL{Scheme: "ws", Host: c.config.ServerAddr, Path: "/sendspin"}
	log.Printf("Connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	// Perform handshake
	if err := c.handshake(); err != nil {
		c.Close()
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Start message reader
	go c.readMessages()

	return nil
}

// handshake performs the protocol handshake
func (c *Client) handshake() error {
	// Build versioned role list per spec
	roles := []string{"player@v1", "metadata@v1"}
	if c.config.ArtworkV1Support != nil {
		roles = append(roles, "artwork@v1")
	}
	if c.config.VisualizerV1Support != nil {
		roles = append(roles, "visualizer@v1")
	}

	// Send client/hello with versioned roles
	hello := ClientHello{
		ClientID:            c.config.ClientID,
		Name:                c.config.Name,
		Version:             c.config.Version,
		SupportedRoles:      roles,
		DeviceInfo:          &c.config.DeviceInfo,
		PlayerV1Support:     &c.config.PlayerV1Support,
		ArtworkV1Support:    c.config.ArtworkV1Support,
		VisualizerV1Support: c.config.VisualizerV1Support,
	}

	msg := Message{
		Type:    "client/hello",
		Payload: hello,
	}

	// Debug: Log the hello message
	helloJSON, _ := json.MarshalIndent(msg, "", "  ")
	log.Printf("Sending client/hello:\n%s", string(helloJSON))

	if err := c.sendJSON(msg); err != nil {
		return fmt.Errorf("failed to send client/hello: %w", err)
	}

	// Wait for server/hello (with timeout)
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read server/hello: %w", err)
	}
	c.conn.SetReadDeadline(time.Time{}) // Clear deadline

	var serverMsg Message
	if err := json.Unmarshal(data, &serverMsg); err != nil {
		return fmt.Errorf("failed to parse server/hello: %w", err)
	}

	if serverMsg.Type != "server/hello" {
		return fmt.Errorf("expected server/hello, got %s", serverMsg.Type)
	}

	log.Printf("Handshake complete with server")

	// Send initial state per spec (client/state with nested player object)
	state := ClientStateMessage{
		Player: &PlayerState{
			State:  "synchronized",
			Volume: 100,
			Muted:  false,
		},
	}

	stateMsg := Message{
		Type:    "client/state",
		Payload: state,
	}

	if err := c.sendJSON(stateMsg); err != nil {
		return fmt.Errorf("failed to send initial state: %w", err)
	}

	return nil
}

// sendJSON sends a JSON message
func (c *Client) sendJSON(msg Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	return c.conn.WriteJSON(msg)
}

// readMessages reads and routes incoming messages
func (c *Client) readMessages() {
	defer c.Close()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		if messageType == websocket.BinaryMessage {
			c.handleBinaryMessage(data)
		} else if messageType == websocket.TextMessage {
			c.handleJSONMessage(data)
		} else {
			log.Printf("Unknown WebSocket message type: %d", messageType)
		}
	}
}

// handleBinaryMessage handles audio chunks
func (c *Client) handleBinaryMessage(data []byte) {
	if len(data) < BinaryMessageHeaderSize {
		log.Printf("Invalid binary message: too short")
		return
	}

	msgType := data[0]
	if msgType != AudioChunkMessageType {
		log.Printf("Unknown binary message type: %d", msgType)
		return
	}

	timestamp := int64(binary.BigEndian.Uint64(data[1:BinaryMessageHeaderSize]))
	audioData := data[BinaryMessageHeaderSize:]

	chunk := AudioChunk{
		Timestamp: timestamp,
		Data:      audioData,
	}

	select {
	case c.AudioChunks <- chunk:
	case <-c.ctx.Done():
	}
}

// handleJSONMessage routes JSON messages per spec
func (c *Client) handleJSONMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Failed to parse JSON message: %v", err)
		return
	}

	payloadBytes, _ := json.Marshal(msg.Payload)

	switch msg.Type {
	case "server/command":
		var cmdMsg ServerCommandMessage
		if err := json.Unmarshal(payloadBytes, &cmdMsg); err != nil {
			log.Printf("Failed to parse server/command: %v", err)
			return
		}
		if cmdMsg.Player != nil {
			select {
			case c.ControlMsgs <- *cmdMsg.Player:
			case <-c.ctx.Done():
			}
		}

	case "server/time":
		var timeMsg ServerTime
		if err := json.Unmarshal(payloadBytes, &timeMsg); err != nil {
			log.Printf("Failed to parse server/time: %v", err)
			return
		}
		select {
		case c.TimeSyncResp <- timeMsg:
		case <-c.ctx.Done():
		}

	case "stream/start":
		var start StreamStart
		if err := json.Unmarshal(payloadBytes, &start); err != nil {
			log.Printf("Failed to parse stream/start: %v", err)
			return
		}
		select {
		case c.StreamStart <- start:
		case <-c.ctx.Done():
		}

	case "stream/clear":
		var clear StreamClear
		if err := json.Unmarshal(payloadBytes, &clear); err != nil {
			log.Printf("Failed to parse stream/clear: %v", err)
			return
		}
		select {
		case c.StreamClear <- clear:
		case <-c.ctx.Done():
		}

	case "stream/end":
		var end StreamEnd
		if err := json.Unmarshal(payloadBytes, &end); err != nil {
			log.Printf("Failed to parse stream/end: %v", err)
			return
		}
		select {
		case c.StreamEnd <- end:
		case <-c.ctx.Done():
		}

	case "server/state":
		var state ServerStateMessage
		if err := json.Unmarshal(payloadBytes, &state); err != nil {
			log.Printf("Failed to parse server/state: %v", err)
			return
		}
		if state.Metadata != nil {
			log.Printf("Metadata: %v - %v (%v)",
				derefString(state.Metadata.Artist),
				derefString(state.Metadata.Title),
				derefString(state.Metadata.Album))
		}
		select {
		case c.ServerState <- state:
		case <-time.After(100 * time.Millisecond):
			log.Printf("Server state channel full, dropping message")
		}

	case "group/update":
		var update GroupUpdate
		if err := json.Unmarshal(payloadBytes, &update); err != nil {
			log.Printf("Failed to parse group/update: %v", err)
			return
		}
		log.Printf("Group update: id=%v, state=%v",
			derefString(update.GroupID),
			derefString(update.PlaybackState))
		select {
		case c.GroupUpdate <- update:
		case <-time.After(100 * time.Millisecond):
			log.Printf("Group update channel full, dropping message")
		}

	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

// derefString safely dereferences a string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SendState sends a client/state message per spec
func (c *Client) SendState(state PlayerState) error {
	msg := Message{
		Type: "client/state",
		Payload: ClientStateMessage{
			Player: &state,
		},
	}
	return c.sendJSON(msg)
}

// SendGoodbye sends a client/goodbye message before disconnecting
func (c *Client) SendGoodbye(reason string) error {
	msg := Message{
		Type: "client/goodbye",
		Payload: ClientGoodbye{
			Reason: reason,
		},
	}
	return c.sendJSON(msg)
}

// SendTimeSync sends a client/time message
func (c *Client) SendTimeSync(t1 int64) error {
	msg := Message{
		Type: "client/time",
		Payload: ClientTime{
			ClientTransmitted: t1,
		},
	}
	return c.sendJSON(msg)
}

// Close closes the connection
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		c.connected = false
		c.cancel()
		c.conn.Close()
		log.Printf("Connection closed")
	}
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}
