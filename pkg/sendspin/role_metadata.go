// ABOUTME: MetadataGroupRole pushes track metadata to joining clients
// ABOUTME: Sends server/state with metadata snapshot on join, transition, or on demand
package sendspin

import (
	"log"
	"sync"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// MetadataConfig configures the MetadataGroupRole.
type MetadataConfig struct {
	// GetMetadata returns the current track metadata (title, artist, album).
	GetMetadata func() (title, artist, album string)

	// ClockMicros returns the server's current clock in microseconds.
	ClockMicros func() int64
}

// MetadataGroupRole handles the "metadata" role family. It pushes a
// server/state message with the current track metadata to clients
// that advertise the metadata role: on join, on stopped→playing
// transitions, and on demand via BroadcastMetadata.
type MetadataGroupRole struct {
	config MetadataConfig

	mu    sync.RWMutex
	group *Group
}

// NewMetadataRole creates a MetadataGroupRole with the given config.
// The role must be registered on a Group via Group.RegisterRole; the
// group reference is captured so OnPlaybackStateChanged and
// BroadcastMetadata can iterate the group's attached clients.
func NewMetadataRole(config MetadataConfig) *MetadataGroupRole {
	return &MetadataGroupRole{config: config}
}

// attachToGroup is called by Group.RegisterRole to give the role a
// back-reference for client iteration. Unexported because role authors
// must register via Group.RegisterRole rather than calling this directly.
func (r *MetadataGroupRole) attachToGroup(g *Group) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.group = g
}

// attachedGroup returns the group this role was registered on, or nil
// if it has not yet been registered.
func (r *MetadataGroupRole) attachedGroup() *Group {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.group
}

func (r *MetadataGroupRole) Role() string { return "metadata" }

// snapshotMessage builds the current server/state metadata snapshot.
// Returns nil if no GetMetadata callback is configured.
func (r *MetadataGroupRole) snapshotMessage() *protocol.ServerStateMessage {
	if r.config.GetMetadata == nil {
		return nil
	}

	title, artist, album := r.config.GetMetadata()

	var clockMicros int64
	if r.config.ClockMicros != nil {
		clockMicros = r.config.ClockMicros()
	}

	return &protocol.ServerStateMessage{
		Metadata: &protocol.MetadataState{
			Timestamp: clockMicros,
			Title:     strPtr(title),
			Artist:    strPtr(artist),
			Album:     strPtr(album),
		},
	}
}

// OnClientJoin sends the current metadata snapshot to clients that
// advertise the metadata role.
func (r *MetadataGroupRole) OnClientJoin(c *ServerClient) {
	if !c.HasRole("metadata") {
		return
	}

	state := r.snapshotMessage()
	if state == nil {
		return
	}

	if err := c.Send("server/state", *state); err != nil {
		log.Printf("MetadataRole: failed to send state to %s: %v", c.Name(), err)
	}
}

func (r *MetadataGroupRole) OnClientLeave(id string, name string) {}

// OnPlaybackStateChanged broadcasts the current metadata snapshot to
// every attached metadata-role client when the group transitions to
// "playing" from a non-playing state. This closes the gap where a
// client connects while the group is stopped (and thus receives only
// the empty-state on join), then never sees metadata when playback
// subsequently begins.
//
// Other transitions (playing→paused, playing→stopped) are intentionally
// not re-broadcast — the existing client state is already correct for
// "the same track is paused/stopped".
func (r *MetadataGroupRole) OnPlaybackStateChanged(oldState, newState string) {
	if newState != "playing" || oldState == "playing" {
		return
	}
	r.broadcast()
}

// BroadcastMetadata pushes the current metadata snapshot to every
// attached metadata-role client. Use this when the server's metadata
// source has mutated and connected clients need to see the new value
// (e.g., HLS playlist advance, controller-driven track change).
//
// Safe to call concurrently with the group's own event dispatch — uses
// Group.Clients which returns a snapshot copy.
func (r *MetadataGroupRole) BroadcastMetadata() {
	r.broadcast()
}

// broadcast sends the snapshot to every metadata-role client in the
// attached group. Logs and continues on per-client send errors so one
// dropped client does not stall the rest.
func (r *MetadataGroupRole) broadcast() {
	g := r.attachedGroup()
	if g == nil {
		return
	}
	state := r.snapshotMessage()
	if state == nil {
		return
	}
	for _, c := range g.Clients() {
		if !c.HasRole("metadata") {
			continue
		}
		if err := c.Send("server/state", *state); err != nil {
			log.Printf("MetadataRole: broadcast to %s failed: %v", c.Name(), err)
		}
	}
}
