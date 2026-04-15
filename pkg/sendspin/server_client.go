// ABOUTME: ServerClient represents one accepted connection on a sendspin Server
// ABOUTME: Exposes ID/Name/Roles/Send surface for the Group/GroupRole layer
package sendspin

import (
	"fmt"
	"strings"
	"sync"

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
