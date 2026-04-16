// ABOUTME: MetadataGroupRole pushes track metadata to joining clients
// ABOUTME: Sends server/state with metadata snapshot on client join
package sendspin

import (
	"log"

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
// that advertise the metadata role when they join.
type MetadataGroupRole struct {
	config MetadataConfig
}

// NewMetadataRole creates a MetadataGroupRole with the given config.
func NewMetadataRole(config MetadataConfig) *MetadataGroupRole {
	return &MetadataGroupRole{config: config}
}

func (r *MetadataGroupRole) Role() string { return "metadata" }

// OnClientJoin sends the current metadata snapshot to clients that
// advertise the metadata role.
func (r *MetadataGroupRole) OnClientJoin(c *ServerClient) {
	if !c.HasRole("metadata") {
		return
	}

	if r.config.GetMetadata == nil {
		return
	}

	title, artist, album := r.config.GetMetadata()

	var clockMicros int64
	if r.config.ClockMicros != nil {
		clockMicros = r.config.ClockMicros()
	}

	state := protocol.ServerStateMessage{
		Metadata: &protocol.MetadataState{
			Timestamp: clockMicros,
			Title:     strPtr(title),
			Artist:    strPtr(artist),
			Album:     strPtr(album),
		},
	}

	if err := c.Send("server/state", state); err != nil {
		log.Printf("MetadataRole: failed to send state to %s: %v", c.Name(), err)
	}
}

func (r *MetadataGroupRole) OnClientLeave(id string, name string) {}
