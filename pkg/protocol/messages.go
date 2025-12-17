// ABOUTME: Sendspin Protocol message type definitions
// ABOUTME: Defines structs for all message types per the Sendspin spec
package protocol

// Message is the top-level wrapper for all protocol messages
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// ClientHello is sent by clients to initiate the handshake
// Per spec: roles use versioned format like "player@v1"
type ClientHello struct {
	ClientID       string      `json:"client_id"`
	Name           string      `json:"name"`
	Version        int         `json:"version"`
	SupportedRoles []string    `json:"supported_roles"`
	DeviceInfo     *DeviceInfo `json:"device_info,omitempty"`
	// Per spec: support objects use versioned keys like "player@v1_support"
	PlayerV1Support     *PlayerV1Support     `json:"player@v1_support,omitempty"`
	ArtworkV1Support    *ArtworkV1Support    `json:"artwork@v1_support,omitempty"`
	VisualizerV1Support *VisualizerV1Support `json:"visualizer@v1_support,omitempty"`

	// Legacy support fields for Music Assistant backward compatibility
	// Uses unversioned keys like "player_support" instead of "player@v1_support"
	PlayerSupport     *PlayerSupport     `json:"player_support,omitempty"`
	MetadataSupport   *MetadataSupport   `json:"metadata_support,omitempty"`
	ArtworkSupport    *ArtworkSupport    `json:"artwork_support,omitempty"`
	VisualizerSupport *VisualizerSupport `json:"visualizer_support,omitempty"`
}

// DeviceInfo contains device identification
type DeviceInfo struct {
	ProductName     string `json:"product_name"`
	Manufacturer    string `json:"manufacturer"`
	SoftwareVersion string `json:"software_version"`
}

// PlayerV1Support describes player@v1 capabilities per spec
type PlayerV1Support struct {
	SupportedFormats  []AudioFormat `json:"supported_formats"`
	BufferCapacity    int           `json:"buffer_capacity"`
	SupportedCommands []string      `json:"supported_commands"`

	// Legacy fields for Music Assistant backward compatibility
	// MA uses separate arrays instead of AudioFormat objects
	SupportCodecs      []string `json:"support_codecs,omitempty"`
	SupportChannels    []int    `json:"support_channels,omitempty"`
	SupportSampleRates []int    `json:"support_sample_rates,omitempty"`
	SupportBitDepth    []int    `json:"support_bit_depth,omitempty"`
}

// ArtworkV1Support describes artwork@v1 capabilities per spec
type ArtworkV1Support struct {
	Channels []ArtworkChannel `json:"channels"`
}

// ArtworkChannel describes a single artwork channel
type ArtworkChannel struct {
	Source      string `json:"source"` // "album", "artist", or "none"
	Format      string `json:"format"` // "jpeg", "png", or "bmp"
	MediaWidth  int    `json:"media_width"`
	MediaHeight int    `json:"media_height"`
}

// VisualizerV1Support describes visualizer@v1 capabilities per spec
type VisualizerV1Support struct {
	BufferCapacity int `json:"buffer_capacity"`
}

// AudioFormat describes a supported audio format
type AudioFormat struct {
	Codec      string `json:"codec"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sample_rate"`
	BitDepth   int    `json:"bit_depth"`
}

// ServerHello is the server's response to client/hello
type ServerHello struct {
	ServerID         string   `json:"server_id"`
	Name             string   `json:"name"`
	Version          int      `json:"version"`
	ActiveRoles      []string `json:"active_roles"`
	ConnectionReason string   `json:"connection_reason"` // "discovery" or "playback"
}

// ClientStateMessage is sent as client/state with role-specific objects
type ClientStateMessage struct {
	Player *PlayerState `json:"player,omitempty"`
}

// PlayerState reports the player's current state per spec
type PlayerState struct {
	State  string `json:"state"`            // "synchronized" or "error"
	Volume int    `json:"volume,omitempty"` // 0-100, if volume command supported
	Muted  bool   `json:"muted,omitempty"`  // if mute command supported
}

// ServerCommandMessage is sent as server/command with role-specific objects
type ServerCommandMessage struct {
	Player *PlayerCommand `json:"player,omitempty"`
}

// PlayerCommand is a control command for the player
type PlayerCommand struct {
	Command string `json:"command"` // "volume" or "mute"
	Volume  int    `json:"volume,omitempty"`
	Mute    bool   `json:"mute,omitempty"`
}

// StreamStartPlayer contains the audio format details
type StreamStartPlayer struct {
	Codec       string `json:"codec"`
	SampleRate  int    `json:"sample_rate"`
	Channels    int    `json:"channels"`
	BitDepth    int    `json:"bit_depth"`
	CodecHeader string `json:"codec_header,omitempty"` // Base64-encoded
}

// StreamStart notifies the client of stream format (nested structure)
type StreamStart struct {
	Player *StreamStartPlayer `json:"player,omitempty"`
}

// ServerStateMessage is sent as server/state with role-specific objects
type ServerStateMessage struct {
	Metadata   *MetadataState   `json:"metadata,omitempty"`
	Controller *ControllerState `json:"controller,omitempty"`
}

// MetadataState contains track metadata per spec (for metadata role)
type MetadataState struct {
	Timestamp   int64          `json:"timestamp"`              // Server clock µs when valid
	Title       *string        `json:"title,omitempty"`        // Track title
	Artist      *string        `json:"artist,omitempty"`       // Primary artist(s)
	AlbumArtist *string        `json:"album_artist,omitempty"` // Album artist(s)
	Album       *string        `json:"album,omitempty"`        // Album name
	ArtworkURL  *string        `json:"artwork_url,omitempty"`  // URL to artwork
	Year        *int           `json:"year,omitempty"`         // Release year YYYY
	Track       *int           `json:"track,omitempty"`        // Track number (1-indexed)
	Progress    *ProgressState `json:"progress,omitempty"`     // Playback progress
	Repeat      *string        `json:"repeat,omitempty"`       // "off", "one", "all"
	Shuffle     *bool          `json:"shuffle,omitempty"`      // Shuffle enabled
}

// ProgressState contains playback progress info per spec
type ProgressState struct {
	TrackProgress int `json:"track_progress"` // Current position in ms
	TrackDuration int `json:"track_duration"` // Total duration in ms (0 = unknown)
	PlaybackSpeed int `json:"playback_speed"` // Speed * 1000 (1000 = normal, 0 = paused)
}

// ControllerState contains controller state per spec
type ControllerState struct {
	SupportedCommands []string `json:"supported_commands"`
	Volume            int      `json:"volume"` // Group volume 0-100
	Muted             bool     `json:"muted"`  // Group mute state
}

// GroupUpdate is sent as group/update per spec
type GroupUpdate struct {
	PlaybackState *string `json:"playback_state,omitempty"` // "playing", "paused", "stopped"
	GroupID       *string `json:"group_id,omitempty"`
	GroupName     *string `json:"group_name,omitempty"`
}

// StreamClear instructs clients to clear buffers (for seek)
type StreamClear struct {
	Roles []string `json:"roles,omitempty"` // Roles to clear: "player", "visualizer"
}

// StreamEnd ends streams for specified roles
type StreamEnd struct {
	Roles []string `json:"roles,omitempty"` // Roles to end (omit = all)
}

// ClientGoodbye is sent before graceful disconnect
type ClientGoodbye struct {
	Reason string `json:"reason"` // "another_server", "shutdown", "restart", "user_request"
}

// ClientTime is sent for clock synchronization
type ClientTime struct {
	ClientTransmitted int64 `json:"client_transmitted"` // Client timestamp in microseconds
}

// ServerTime is the response to client/time
type ServerTime struct {
	ClientTransmitted int64 `json:"client_transmitted"` // Echoed client timestamp
	ServerReceived    int64 `json:"server_received"`    // Server receive timestamp
	ServerTransmitted int64 `json:"server_transmitted"` // Server send timestamp
}

// Legacy types for Music Assistant backward compatibility

// MetadataSupport describes metadata/artwork capabilities (legacy format)
type MetadataSupport struct {
	SupportPictureFormats []string `json:"support_picture_formats"`
	MediaWidth            int      `json:"media_width,omitempty"`
	MediaHeight           int      `json:"media_height,omitempty"`
}

// StreamMetadata contains track information (legacy message type)
type StreamMetadata struct {
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	ArtworkURL string `json:"artwork_url,omitempty"`
}

// SessionMetadata contains track metadata within session updates (legacy format)
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

// SessionUpdate notifies client of session state changes (legacy message type)
type SessionUpdate struct {
	GroupID       string           `json:"group_id"`
	PlaybackState string           `json:"playback_state,omitempty"` // "playing" or "idle"
	Metadata      *SessionMetadata `json:"metadata,omitempty"`
}

// VisualizerSupport is a legacy alias for VisualizerV1Support
type VisualizerSupport = VisualizerV1Support

// ArtworkSupport is a legacy alias for ArtworkV1Support
type ArtworkSupport = ArtworkV1Support

// ClientState is a legacy alias for PlayerState (flat format used with client/state)
type ClientState struct {
	State  string `json:"state"`  // "synchronized" or "error"
	Volume int    `json:"volume"` // 0-100
	Muted  bool   `json:"muted"`
}

// ServerCommand is a legacy flat format for player commands
type ServerCommand struct {
	Command string `json:"command"`
	Volume  int    `json:"volume,omitempty"`
	Mute    bool   `json:"mute,omitempty"`
}

// PlayerSupport is a format for player capabilities (Music Assistant / aiosendspin compatibility)
type PlayerSupport struct {
	SupportedFormats  []AudioFormat `json:"supported_formats,omitempty"`
	BufferCapacity    int           `json:"buffer_capacity,omitempty"`
	SupportedCommands []string      `json:"supported_commands,omitempty"`
}
