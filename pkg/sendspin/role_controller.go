// ABOUTME: ControllerGroupRole handles client/command messages
// ABOUTME: Owns group repeat/shuffle state and pushes controller state to clients
package sendspin

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// ControllerConfig configures the ControllerGroupRole.
type ControllerConfig struct {
	// SupportedCommands lists the media commands this server supports
	// (e.g., "next", "previous", "play", "pause", "repeat_all", "shuffle").
	SupportedCommands []string

	// OnCommand is called when a client sends a controller command.
	// May be nil if the caller only wants to push capabilities.
	OnCommand func(client *ServerClient, command string)

	// Repeat is the group's initial repeat mode ("off", "one", "all").
	// Empty defaults to "off".
	Repeat string

	// Shuffle is the group's initial shuffle state.
	Shuffle bool

	// ClockMicros returns the server's current clock in microseconds. When
	// set, repeat/shuffle changes are mirrored into a metadata server/state
	// for backward compatibility with v1 clients that read those fields from
	// the metadata role (per spec#81 legacy support). When nil, no mirror is
	// sent and only controller-role clients receive updates.
	ClockMicros func() int64
}

// ControllerGroupRole handles the "controller" role family. It pushes
// controller state (supported commands plus repeat/shuffle) to clients
// that advertise the controller role on join, dispatches incoming
// client/command payloads to an OnCommand callback, and owns the group's
// repeat/shuffle state. Changing that state via SetRepeat / SetShuffle
// broadcasts a fresh controller server/state to all controller members.
type ControllerGroupRole struct {
	config ControllerConfig

	mu      sync.Mutex
	repeat  string
	shuffle bool
	group   *Group
}

// NewControllerRole creates a ControllerGroupRole with the given config.
func NewControllerRole(config ControllerConfig) *ControllerGroupRole {
	repeat := config.Repeat
	if repeat == "" {
		repeat = "off"
	}
	return &ControllerGroupRole{
		config:  config,
		repeat:  repeat,
		shuffle: config.Shuffle,
	}
}

func (r *ControllerGroupRole) Role() string { return "controller" }

// attachToGroup wires the owning Group into the role so state changes can
// be broadcast to members. Called by Group.RegisterRole via the unexported
// attach-to-group hook; the unexported name keeps the injection internal.
func (r *ControllerGroupRole) attachToGroup(g *Group) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.group = g
}

// Repeat returns the current group repeat mode ("off", "one", "all").
func (r *ControllerGroupRole) Repeat() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.repeat
}

// Shuffle returns the current group shuffle state.
func (r *ControllerGroupRole) Shuffle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shuffle
}

// SetRepeat updates the group's repeat mode and broadcasts the new
// controller state to members. A no-op (no broadcast) if the mode is
// unchanged.
func (r *ControllerGroupRole) SetRepeat(mode string) {
	r.mu.Lock()
	if r.repeat == mode {
		r.mu.Unlock()
		return
	}
	r.repeat = mode
	r.mu.Unlock()
	r.broadcastState()
}

// SetShuffle updates the group's shuffle state and broadcasts the new
// controller state to members. A no-op (no broadcast) if unchanged.
func (r *ControllerGroupRole) SetShuffle(on bool) {
	r.mu.Lock()
	if r.shuffle == on {
		r.mu.Unlock()
		return
	}
	r.shuffle = on
	r.mu.Unlock()
	r.broadcastState()
}

// OnClientJoin sends controller state (supported commands plus the current
// repeat/shuffle) to clients that advertise the controller role. Clients
// without the controller role are skipped.
func (r *ControllerGroupRole) OnClientJoin(c *ServerClient) {
	if !c.HasRole("controller") {
		return
	}

	if err := c.Send("server/state", r.controllerStateMessage()); err != nil {
		log.Printf("ControllerRole: failed to send state to %s: %v", c.Name(), err)
	}
}

func (r *ControllerGroupRole) OnClientLeave(id string, name string) {}

// controllerStateMessage builds the controller server/state snapshot from
// the current repeat/shuffle state.
func (r *ControllerGroupRole) controllerStateMessage() protocol.ServerStateMessage {
	r.mu.Lock()
	repeat, shuffle := r.repeat, r.shuffle
	r.mu.Unlock()

	return protocol.ServerStateMessage{
		Controller: &protocol.ControllerState{
			SupportedCommands: r.config.SupportedCommands,
			Repeat:            repeat,
			Shuffle:           shuffle,
		},
	}
}

// broadcastState pushes the current controller state to every controller
// member of the group. Metadata-only (v1) clients receive a mirrored
// metadata server/state instead when ClockMicros is configured.
func (r *ControllerGroupRole) broadcastState() {
	r.mu.Lock()
	g := r.group
	repeat, shuffle := r.repeat, r.shuffle
	r.mu.Unlock()

	if g == nil {
		return
	}

	ctrlState := protocol.ServerStateMessage{
		Controller: &protocol.ControllerState{
			SupportedCommands: r.config.SupportedCommands,
			Repeat:            repeat,
			Shuffle:           shuffle,
		},
	}

	// Build the legacy metadata mirror once if a clock is available.
	var mdState *protocol.ServerStateMessage
	if r.config.ClockMicros != nil {
		rp, sh := repeat, shuffle
		mdState = &protocol.ServerStateMessage{
			Metadata: &protocol.MetadataState{
				Timestamp: r.config.ClockMicros(),
				Repeat:    &rp,
				Shuffle:   &sh,
			},
		}
	}

	for _, c := range g.Clients() {
		switch {
		case c.HasRole("controller"):
			if err := c.Send("server/state", ctrlState); err != nil {
				log.Printf("ControllerRole: failed to broadcast state to %s: %v", c.Name(), err)
			}
		case mdState != nil && c.HasRole("metadata"):
			if err := c.Send("server/state", *mdState); err != nil {
				log.Printf("ControllerRole: failed to mirror state to %s: %v", c.Name(), err)
			}
		}
	}
}

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
