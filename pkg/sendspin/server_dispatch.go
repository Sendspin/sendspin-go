// ABOUTME: Message dispatch for incoming client control frames
// ABOUTME: Routes client/time, client/state, client/goodbye to handlers
package sendspin

import (
	"encoding/json"
	"log"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

func (s *Server) handleClientMessage(c *ServerClient, data []byte) {
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

func (s *Server) handleTimeSync(c *ServerClient, payload interface{}) {
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

	if err := c.Send("server/time", response); err != nil {
		if s.config.Debug {
			log.Printf("Error sending server/time to %s: %v", c.name, err)
		}
	}
}

// handleClientState applies a client's player state update per spec.
func (s *Server) handleClientState(c *ServerClient, payload interface{}) {
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
		c.state = stateMsg.Player.State
		c.volume = stateMsg.Player.Volume
		c.muted = stateMsg.Player.Muted
		c.mu.Unlock()

		if s.config.Debug {
			log.Printf("Client %s state: %s (vol: %d, muted: %v)", c.name, stateMsg.Player.State, stateMsg.Player.Volume, stateMsg.Player.Muted)
		}
	}
}

func (s *Server) handleClientGoodbye(c *ServerClient, payload interface{}) {
	goodbyeData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var goodbye protocol.ClientGoodbye
	if err := json.Unmarshal(goodbyeData, &goodbye); err != nil {
		return
	}

	log.Printf("Client %s goodbye: %s", c.name, goodbye.Reason)
	// Connection close happens in handleConnection's read loop once this returns.
}
