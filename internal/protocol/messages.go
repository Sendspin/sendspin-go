// ABOUTME: DEPRECATED - Use pkg/protocol instead
// ABOUTME: Legacy message types kept for backward compatibility tests
//
// Deprecated: This package is deprecated. Use github.com/Sendspin/sendspin-go/pkg/protocol instead.
// All new code should import pkg/protocol which contains spec-aligned message types.
package protocol

type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type ClientHello struct {
	ClientID          string             `json:"client_id"`
	Name              string             `json:"name"`
	Version           int                `json:"version"`
	SupportedRoles    []string           `json:"supported_roles"`
	DeviceInfo        *DeviceInfo        `json:"device_info,omitempty"`
	PlayerSupport     *PlayerSupport     `json:"player_support,omitempty"`
	MetadataSupport   *MetadataSupport   `json:"metadata_support,omitempty"`
	VisualizerSupport *VisualizerSupport `json:"visualizer_support,omitempty"`
}

type DeviceInfo struct {
	ProductName     string `json:"product_name"`
	Manufacturer    string `json:"manufacturer"`
	SoftwareVersion string `json:"software_version"`
}

type PlayerSupport struct {
	// Spec fields (newer)
	SupportFormats    []AudioFormat `json:"support_formats,omitempty"`
	BufferCapacity    int           `json:"buffer_capacity,omitempty"`
	SupportedCommands []string      `json:"supported_commands,omitempty"`

	// Legacy fields (Music Assistant compatibility - uses separate arrays)
	SupportCodecs      []string `json:"support_codecs,omitempty"`
	SupportChannels    []int    `json:"support_channels,omitempty"`
	SupportSampleRates []int    `json:"support_sample_rates,omitempty"`
	SupportBitDepth    []int    `json:"support_bit_depth,omitempty"`
}

type AudioFormat struct {
	Codec      string `json:"codec"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sample_rate"`
	BitDepth   int    `json:"bit_depth"`
}

type MetadataSupport struct {
	SupportPictureFormats []string `json:"support_picture_formats"`
	MediaWidth            int      `json:"media_width,omitempty"`
	MediaHeight           int      `json:"media_height,omitempty"`
}

type VisualizerSupport struct {
	BufferCapacity int `json:"buffer_capacity,omitempty"`
	// FFT details - to be determined by spec
}

type ServerHello struct {
	ServerID         string   `json:"server_id"`
	Name             string   `json:"name"`
	Version          int      `json:"version"`
	ActiveRoles      []string `json:"active_roles"`
	ConnectionReason string   `json:"connection_reason"`
}

// ClientState reports the player's current state (sent as client/state message)
type ClientState struct {
	State  string `json:"state"`  // "synchronized" or "error" (per spec)
	Volume int    `json:"volume"` // 0-100
	Muted  bool   `json:"muted"`  // All fields are required
}

type ServerCommand struct {
	Command string `json:"command"`
	Volume  int    `json:"volume,omitempty"`
	Mute    bool   `json:"mute,omitempty"`
}

type StreamStartPlayer struct {
	Codec       string `json:"codec"`
	SampleRate  int    `json:"sample_rate"`
	Channels    int    `json:"channels"`
	BitDepth    int    `json:"bit_depth"`
	CodecHeader string `json:"codec_header,omitempty"` // Base64-encoded
}

type StreamStart struct {
	Player *StreamStartPlayer `json:"player,omitempty"`
}

type StreamMetadata struct {
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	ArtworkURL string `json:"artwork_url,omitempty"`
}

type SessionMetadata struct {
	Title         string  `json:"title,omitempty"`
	Artist        string  `json:"artist,omitempty"`
	Album         string  `json:"album,omitempty"`
	AlbumArtist   string  `json:"album_artist,omitempty"`
	ArtworkURL    string  `json:"artwork_url,omitempty"`
	Track         int     `json:"track,omitempty"`
	TrackDuration int     `json:"track_duration,omitempty"`
	Year          int     `json:"year,omitempty"`
	PlaybackSpeed float64 `json:"playback_speed,omitempty"`
	Repeat        string  `json:"repeat,omitempty"`
	Shuffle       bool    `json:"shuffle,omitempty"`
	Timestamp     int64   `json:"timestamp,omitempty"`
}

type SessionUpdate struct {
	GroupID       string           `json:"group_id"`
	PlaybackState string           `json:"playback_state,omitempty"` // "playing" or "idle"
	Metadata      *SessionMetadata `json:"metadata,omitempty"`
}

type ClientTime struct {
	ClientTransmitted int64 `json:"client_transmitted"` // Client timestamp in microseconds
}

type ServerTime struct {
	ClientTransmitted int64 `json:"client_transmitted"` // Echoed client timestamp
	ServerReceived    int64 `json:"server_received"`    // Server receive timestamp
	ServerTransmitted int64 `json:"server_transmitted"` // Server send timestamp
}
