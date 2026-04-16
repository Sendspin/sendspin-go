// ABOUTME: ServerClient represents one accepted connection on a sendspin Server
// ABOUTME: Exposes ID/Name/Roles/Send surface for the Group/GroupRole layer
package sendspin

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/internal/server"
	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
)

// ServerClient represents one accepted WebSocket connection on a Server.
// It owns the client's negotiated roles, capabilities, playback state,
// per-codec encoder state, and the outbound send channel.
//
// Exposed accessors (ID, Name, Roles, HasRole, Send, SendBinary) are the
// stable surface the future Group/GroupRole layer depends on. Internal
// fields remain unexported so the server package can mutate them through
// the mutex without leaking that detail to callers.
type ServerClient struct {
	id           string
	name         string
	conn         *websocket.Conn
	roles        []string
	capabilities *protocol.PlayerV1Support

	state  string
	volume int
	muted  bool

	codec       string
	opusEncoder *server.OpusEncoder
	resampler   *audio.Resampler // non-nil only when source rate != 48kHz

	sendChan chan interface{}
	done     chan struct{}

	mu sync.RWMutex
}

// ID returns the client-supplied unique identifier from client/hello.
func (c *ServerClient) ID() string { return c.id }

// Name returns the human-friendly name from client/hello.
func (c *ServerClient) Name() string { return c.name }

// Roles returns the client's advertised role list as a fresh slice.
// Callers may mutate the returned slice without affecting the ServerClient.
func (c *ServerClient) Roles() []string {
	out := make([]string, len(c.roles))
	copy(out, c.roles)
	return out
}

// HasRole reports whether this client advertised a given role family.
// It matches both exact ("player") and versioned ("player@v1") forms so
// callers can query by family without tracking versions.
func (c *ServerClient) HasRole(role string) bool {
	for _, r := range c.roles {
		if r == role || strings.HasPrefix(r, role+"@") {
			return true
		}
	}
	return false
}

// Send enqueues a typed control message for transmission. Returns an
// error immediately if the client's send buffer is full rather than
// blocking — callers decide whether to drop or disconnect.
//
// A nil return means the message was enqueued, not that it was delivered:
// after the client's writer goroutine has exited (e.g., post-disconnect),
// enqueued messages are dropped silently. Callers should treat this as
// best-effort once they've observed a client leaving.
func (c *ServerClient) Send(msgType string, payload interface{}) error {
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

// SendBinary enqueues a raw binary frame (e.g., an audio chunk) for
// transmission. Same non-blocking semantics as Send.
//
// A nil return means the frame was enqueued, not that it was delivered:
// after the client's writer goroutine has exited (e.g., post-disconnect),
// enqueued frames are dropped silently. Callers should treat this as
// best-effort once they've observed a client leaving.
func (c *ServerClient) SendBinary(data []byte) error {
	select {
	case c.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// State returns the client's current playback state ("synchronized",
// "playing", "paused"). Safe to call concurrently with state updates.
func (c *ServerClient) State() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Volume returns the client's reported volume (0-100). Safe to call
// concurrently with state updates.
func (c *ServerClient) Volume() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.volume
}

// Muted returns the client's mute state. Safe to call concurrently with
// state updates.
func (c *ServerClient) Muted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.muted
}

// Codec returns the currently-negotiated codec name ("pcm", "opus", "").
// Returns the empty string before stream negotiation completes. Safe to
// call concurrently with state updates.
func (c *ServerClient) Codec() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.codec
}

// NewServerClientFromConn wraps an existing WebSocket connection in a
// ServerClient and starts a background writer goroutine. This is the
// entry point for code that accepts its own WebSocket connections
// (e.g., conformance adapters) and wants to use the typed Send/SendBinary
// API without constructing a full Server.
//
// The caller is responsible for reading from the connection; the writer
// goroutine handles outbound messages via Send/SendBinary. Call Close()
// when done to stop the writer and release resources.
func NewServerClientFromConn(conn *websocket.Conn, id, name string, roles []string, capabilities *protocol.PlayerV1Support) *ServerClient {
	c := &ServerClient{
		id:           id,
		name:         name,
		conn:         conn,
		roles:        roles,
		capabilities: capabilities,
		state:        "synchronized",
		volume:       100,
		sendChan:     make(chan interface{}, 100),
		done:         make(chan struct{}),
	}
	go c.runWriter()
	return c
}

// runWriter drains the sendChan and writes messages to the WebSocket.
// Exits when the done channel is closed (via Close). This is the
// standalone equivalent of Server.clientWriter for ServerClients
// created via NewServerClientFromConn.
func (c *ServerClient) runWriter() {
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

// Close stops the writer goroutine and signals that this ServerClient
// is done. Safe to call multiple times. Does NOT close the underlying
// WebSocket connection — the caller owns that lifecycle.
func (c *ServerClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		// Already closed
	default:
		close(c.done)
	}
}
