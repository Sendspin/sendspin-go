// ABOUTME: ServerConn wraps a WebSocket for server-side typed message sending
// ABOUTME: CGO-free alternative to sendspin.ServerClient for adapters and tools
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ServerConn wraps an accepted WebSocket connection with typed Send and
// SendBinary methods plus a background writer goroutine. It is the
// lightweight, CGO-free counterpart to sendspin.ServerClient — use it
// when you need typed protocol I/O without the full server runtime
// (e.g., conformance adapters, CLI tools, test harnesses).
//
// The zero value is not usable — construct via NewServerConn.
type ServerConn struct {
	conn     *websocket.Conn
	id       string
	name     string
	sendChan chan interface{}
	done     chan struct{}
	once     sync.Once
}

// NewServerConn wraps an existing WebSocket connection and starts a
// background writer goroutine. Call Close() when done to stop the
// writer. Does NOT close the underlying WebSocket — the caller owns
// that lifecycle.
func NewServerConn(conn *websocket.Conn, id, name string) *ServerConn {
	sc := &ServerConn{
		conn:     conn,
		id:       id,
		name:     name,
		sendChan: make(chan interface{}, 100),
		done:     make(chan struct{}),
	}
	go sc.runWriter()
	return sc
}

// ID returns the identifier passed to NewServerConn.
func (sc *ServerConn) ID() string { return sc.id }

// Name returns the name passed to NewServerConn.
func (sc *ServerConn) Name() string { return sc.name }

// Send enqueues a typed control message (JSON envelope) for
// transmission. Returns an error immediately if the send buffer is
// full. The message is wrapped in a {"type": msgType, "payload": ...}
// envelope by the writer goroutine.
func (sc *ServerConn) Send(msgType string, payload interface{}) error {
	msg := Message{
		Type:    msgType,
		Payload: payload,
	}
	select {
	case sc.sendChan <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendBinary enqueues a raw binary frame for transmission. Returns an
// error immediately if the send buffer is full.
func (sc *ServerConn) SendBinary(data []byte) error {
	select {
	case sc.sendChan <- data:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// Close stops the writer goroutine. Safe to call multiple times. Does
// NOT close the underlying WebSocket connection.
func (sc *ServerConn) Close() {
	sc.once.Do(func() {
		close(sc.done)
	})
}

func (sc *ServerConn) runWriter() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	const writeDeadline = 10 * time.Second

	for {
		select {
		case msg := <-sc.sendChan:
			switch v := msg.(type) {
			case []byte:
				sc.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := sc.conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					return
				}
			default:
				data, err := json.Marshal(v)
				if err != nil {
					continue
				}
				sc.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := sc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}

		case <-ticker.C:
			if err := sc.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}

		case <-sc.done:
			return
		}
	}
}

// CreateAudioChunk packs timestamp + payload into a Sendspin binary audio
// frame: [1 byte message type][8 byte big-endian timestamp (µs)][audio bytes].
func CreateAudioChunk(timestamp int64, audioData []byte) []byte {
	chunk := make([]byte, BinaryMessageHeaderSize+len(audioData))
	chunk[0] = AudioChunkMessageType
	binary.BigEndian.PutUint64(chunk[1:BinaryMessageHeaderSize], uint64(timestamp))
	copy(chunk[BinaryMessageHeaderSize:], audioData)
	return chunk
}

// CreateArtworkChunk packs an artwork frame: [1 byte message type][8 byte
// timestamp (µs)][image bytes]. Channel is 0-3, mapping to the artwork
// channel message types (8-11).
func CreateArtworkChunk(channel int, timestamp int64, imageData []byte) []byte {
	chunk := make([]byte, BinaryMessageHeaderSize+len(imageData))
	chunk[0] = byte(ArtworkChannel0MessageType + channel)
	binary.BigEndian.PutUint64(chunk[1:BinaryMessageHeaderSize], uint64(timestamp))
	copy(chunk[BinaryMessageHeaderSize:], imageData)
	return chunk
}
