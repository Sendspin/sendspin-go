// ABOUTME: ControllerGroupRole handles client/command messages
// ABOUTME: Pushes controller capabilities to joining clients
package sendspin

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// ControllerConfig configures the ControllerGroupRole.
type ControllerConfig struct {
	// SupportedCommands lists the media commands this server supports
	// (e.g., "next", "previous", "play", "pause").
	SupportedCommands []string

	// OnCommand is called when a client sends a controller command.
	// May be nil if the caller only wants to push capabilities.
	OnCommand func(client *ServerClient, command string)
}

// ControllerGroupRole handles the "controller" role family. It pushes
// controller capabilities (supported commands) to clients that
// advertise the controller role on join, and dispatches incoming
// client/command payloads to an OnCommand callback.
type ControllerGroupRole struct {
	config ControllerConfig
}

// NewControllerRole creates a ControllerGroupRole with the given config.
func NewControllerRole(config ControllerConfig) *ControllerGroupRole {
	return &ControllerGroupRole{config: config}
}

func (r *ControllerGroupRole) Role() string { return "controller" }

// OnClientJoin sends controller state (supported commands) to clients
// that advertise the controller role. Clients without the controller
// role are skipped.
func (r *ControllerGroupRole) OnClientJoin(c *ServerClient) {
	if !c.HasRole("controller") {
		return
	}

	state := protocol.ServerStateMessage{
		Controller: &protocol.ControllerState{
			SupportedCommands: r.config.SupportedCommands,
		},
	}

	if err := c.Send("server/state", state); err != nil {
		log.Printf("ControllerRole: failed to send state to %s: %v", c.Name(), err)
	}
}

func (r *ControllerGroupRole) OnClientLeave(id string, name string) {}

// HandleMessage parses a controller command payload and invokes the
// OnCommand callback.
func (r *ControllerGroupRole) HandleMessage(c *ServerClient, payload json.RawMessage) error {
	var cmd struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return fmt.Errorf("invalid controller command: %w", err)
	}
	if cmd.Command == "" {
		return fmt.Errorf("controller command missing 'command' field")
	}

	if r.config.OnCommand != nil {
		r.config.OnCommand(c, cmd.Command)
	}

	return nil
}
