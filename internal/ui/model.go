// ABOUTME: Bubbletea model for player TUI
// ABOUTME: Defines application state and update logic
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio/output"
	"github.com/Sendspin/sendspin-go/pkg/sendspin"
	"github.com/Sendspin/sendspin-go/pkg/sync"
	tea "github.com/charmbracelet/bubbletea"
)

type uiMode int

const (
	modeNormal uiMode = iota
	modeDevicePicker
)

// transientExpireMsg is fired after a transient banner's lifetime to clear
// it. Bubbletea's scheduler guarantees at-most-once delivery.
type transientExpireMsg struct{ nonce uint64 }

// Model represents the TUI state
type Model struct {
	// Connection
	connected  bool
	serverName string

	// Sync
	syncRTT     int64
	syncQuality sync.Quality

	// Stream
	codec      string
	sampleRate int
	channels   int
	bitDepth   int

	// Metadata
	title       string
	artist      string
	album       string
	artworkPath string

	// Playback
	state         string
	playbackState string // "playing", "paused", "stopped", "idle", "reconnecting"
	volume        int
	muted         bool

	// Stats
	received    int64
	played      int64
	dropped     int64
	bufferDepth int

	// Debug
	showDebug  bool
	goroutines int

	// Dimensions
	width  int
	height int

	// Controls
	volumeCtrl    *VolumeControl
	transportCtrl *TransportControl

	// Audio device selection
	audioDevice string // current device driving playback; shown in status row
	configPath  string // where picker saves audio_device; empty disables save

	// Modal state
	mode   uiMode
	picker devicePickerState

	// Transient banner shown in the controls area (e.g. "Saved. Restart to apply.")
	transientMsg   string
	transientNonce uint64 // incremented per banner so stale expire ticks are ignored

	// listPlaybackDevices is indirected so tests can stub it.
	listPlaybackDevices func() ([]output.PlaybackDevice, error)
	// writeConfigKey persists a key to the config file. Indirected for tests.
	writeConfigKey func(path, key, value string) error
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case StatusMsg:
		m.applyStatus(msg)
	case transientExpireMsg:
		if msg.nonce == m.transientNonce {
			m.transientMsg = ""
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.mode == modeDevicePicker {
		// Modal takes over the viewport; the normal view is suspended. A
		// minimal header keeps context ("which player am I configuring?").
		return m.renderHeader() + renderPicker(m.picker, m.width)
	}

	s := ""
	s += m.renderHeader()
	s += m.renderStreamInfo()
	s += m.renderControls()
	s += m.renderStats()

	if m.showDebug {
		s += m.renderDebug()
	}

	s += m.renderHelp()

	return s
}

func (m Model) renderHeader() string {
	connStatus := "Disconnected"
	if m.connected {
		connStatus = fmt.Sprintf("Connected to %s", m.serverName)
	}

	stateDisplay := playbackStateDisplay(m.playbackState)

	syncDisplay := "Sync: \u2717 Lost"
	switch m.syncQuality {
	case sync.QualityGood:
		syncDisplay = fmt.Sprintf("Sync: \u2713 Good (RTT: %.1fms)", float64(m.syncRTT)/1000.0)
	case sync.QualityDegraded:
		syncDisplay = "Sync: \u26a0 Degraded"
	}

	// Use terminal width for responsive layout
	width := m.width
	if width < 60 {
		width = 60 // Minimum width
	}
	innerWidth := width - 4 // Account for borders

	titleWidth := width - 20 // Space for "┌─ Sendspin Player " prefix
	title := "\u250c\u2500 Sendspin Player " + repeatString("\u2500", titleWidth) + "\u2510\n"

	statusLine := fmt.Sprintf("\u2502 Status: %-*s \u2502\n", innerWidth-9, truncate(connStatus, innerWidth-9))

	// State + Sync on same line
	statePrefix := fmt.Sprintf("State:  %s", stateDisplay)
	// Calculate available space: innerWidth minus "State:  <state>" minus spacing minus sync display
	stateSyncLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth,
		fmt.Sprintf("%-30s %s", statePrefix, syncDisplay))

	separator := "\u251c" + repeatString("\u2500", width-2) + "\u2524\n"

	return title + statusLine + stateSyncLine + separator
}

func (m Model) renderStreamInfo() string {
	width := m.width
	if width < 60 {
		width = 60
	}
	innerWidth := width - 4

	if !m.connected || m.codec == "" {
		return fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "No stream")
	}

	s := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "Now Playing:")
	if m.title != "" {
		metaWidth := innerWidth - 10 // Account for "  Track:  " prefix
		s += fmt.Sprintf("\u2502   Track:  %-*s \u2502\n", innerWidth-10, truncate(m.title, metaWidth))
		s += fmt.Sprintf("\u2502   Artist: %-*s \u2502\n", innerWidth-10, truncate(m.artist, metaWidth))
		s += fmt.Sprintf("\u2502   Album:  %-*s \u2502\n", innerWidth-10, truncate(m.album, metaWidth))
		if m.artworkPath != "" {
			s += fmt.Sprintf("\u2502   Art:    %-*s \u2502\n", innerWidth-10, truncate(m.artworkPath, metaWidth))
		}
	} else {
		s += fmt.Sprintf("\u2502   %-*s \u2502\n", innerWidth-3, "(No metadata)")
	}

	s += fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "")
	formatStr := formatCodecDisplay(m.codec, m.sampleRate, m.bitDepth)
	s += fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, formatStr)

	return s
}

func (m Model) renderControls() string {
	width := m.width
	if width < 60 {
		width = 60
	}
	innerWidth := width - 4

	muteIcon := ""
	if m.muted {
		muteIcon = " \U0001f507"
	}

	volumeBar := renderBar(m.volume, 100, 20)

	s := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "")
	volumeStr := fmt.Sprintf("Volume: [%s] %d%%%s", volumeBar, m.volume, muteIcon)
	s += fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, volumeStr)

	bufferStr := fmt.Sprintf("Buffer: %dms", m.bufferDepth)
	s += fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, bufferStr)

	// Output device status row. Uses the hotkey helper so the 'O' rendering
	// stays in sync with the help line that advertises the trigger.
	outputName := m.audioDevice
	if outputName == "" {
		outputName = "(miniaudio default)"
	}
	outputLine := hotkey('o', "Output: "+truncate(outputName, innerWidth-10))
	s += renderLineWithANSI(innerWidth, outputLine)

	if m.transientMsg != "" {
		s += renderLineWithANSI(innerWidth, "  "+m.transientMsg)
	}

	return s
}

// renderLineWithANSI emits a bordered row whose contents contain ANSI
// escape sequences. fmt's padding verbs count those escape bytes toward
// width, leading to short rows; this helper pads using visible-column
// count instead so every border stays aligned.
func renderLineWithANSI(innerWidth int, content string) string {
	pad := innerWidth - displayLen(content)
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("\u2502 %s%s \u2502\n", content, strings.Repeat(" ", pad))
}

func (m Model) renderStats() string {
	width := m.width
	if width < 60 {
		width = 60
	}
	innerWidth := width - 4

	separator := "\u251c" + repeatString("\u2500", width-2) + "\u2524\n"
	statsStr := fmt.Sprintf("Stats:  RX: %d  Played: %d  Dropped: %d", m.received, m.played, m.dropped)
	statsLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, statsStr)
	emptyLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "")

	return separator + statsLine + emptyLine
}

func (m Model) renderHelp() string {
	width := m.width
	if width < 60 {
		width = 60
	}
	innerWidth := width - 4

	// Uniform hotkey rendering — every trigger character is reverse-
	// highlighted inline via hotkey(). Separators are two spaces; we rely
	// on renderLineWithANSI for correct padding because hotkey() emits
	// escape codes that fmt would miscount.
	parts := []string{
		hotkey(KeySpace, "Play/Pause"),
		hotkey('n', "Next"),
		hotkey('p', "Prev"),
		hotkey(KeyUpDown, "Volume"),
		hotkey('m', "Mute"),
		hotkey('o', "Output"),
		hotkey('q', "Quit"),
	}
	helpStr := strings.Join(parts, "  ")
	helpLine := renderLineWithANSI(innerWidth, helpStr)
	bottom := "\u2514" + repeatString("\u2500", width-2) + "\u2518\n"

	return helpLine + bottom
}

func (m Model) renderDebug() string {
	width := m.width
	if width < 60 {
		width = 60
	}
	innerWidth := width - 4

	debugTitle := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, "DEBUG:")
	goroutineStr := fmt.Sprintf("  Goroutines: %d", m.goroutines)
	goroutineLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, goroutineStr)
	playbackStr := fmt.Sprintf("  Playback: %s", m.playbackState)
	playbackLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, playbackStr)
	codecStr := fmt.Sprintf("  Preferred codec: %s", m.codec)
	codecLine := fmt.Sprintf("\u2502 %-*s \u2502\n", innerWidth, codecStr)

	return debugTitle + goroutineLine + playbackLine + codecLine
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeDevicePicker {
		return m.handlePickerMode(msg)
	}
	switch msg.String() {
	case "o":
		return m.openDevicePicker()
	case "q", "ctrl+c":
		if m.volumeCtrl != nil {
			select {
			case m.volumeCtrl.Quit <- QuitMsg{}:
			default:
				// Channel full, skip
			}
		}
		return m, tea.Quit
	case "up":
		if m.volume < 100 {
			m.volume += 5
			if m.volume > 100 {
				m.volume = 100
			}
			if m.volumeCtrl != nil {
				select {
				case m.volumeCtrl.Changes <- VolumeChangeMsg{Volume: m.volume, Muted: m.muted}:
				default:
					// Channel full, skip
				}
			}
		}
	case "down":
		if m.volume > 0 {
			m.volume -= 5
			if m.volume < 0 {
				m.volume = 0
			}
			if m.volumeCtrl != nil {
				select {
				case m.volumeCtrl.Changes <- VolumeChangeMsg{Volume: m.volume, Muted: m.muted}:
				default:
					// Channel full, skip
				}
			}
		}
	case "m":
		m.muted = !m.muted
		// Send volume change to player via channel
		if m.volumeCtrl != nil {
			select {
			case m.volumeCtrl.Changes <- VolumeChangeMsg{Volume: m.volume, Muted: m.muted}:
			default:
				// Channel full, skip
			}
		}
	case " ":
		if m.transportCtrl != nil {
			select {
			case m.transportCtrl.Commands <- TransportMsg{Command: "toggle"}:
			default:
			}
		}
	case "n":
		if m.transportCtrl != nil {
			select {
			case m.transportCtrl.Commands <- TransportMsg{Command: "next"}:
			default:
			}
		}
	case "p":
		if m.transportCtrl != nil {
			select {
			case m.transportCtrl.Commands <- TransportMsg{Command: "previous"}:
			default:
			}
		}
	case "r":
		if m.transportCtrl != nil {
			select {
			case m.transportCtrl.Commands <- TransportMsg{Command: "reconnect"}:
			default:
			}
		}
	case "d":
		m.showDebug = !m.showDebug
	}

	return m, nil
}

func (m *Model) applyStatus(msg StatusMsg) {
	if msg.Connected != nil {
		m.connected = *msg.Connected
	}
	if msg.ServerName != "" {
		m.serverName = msg.ServerName
	}
	if msg.PlaybackState != "" {
		m.playbackState = msg.PlaybackState
	}
	// Sync stats are always applied when sent
	if msg.SyncRTT != 0 {
		m.syncRTT = msg.SyncRTT
		m.syncQuality = msg.SyncQuality
	}
	if msg.Codec != "" {
		m.codec = msg.Codec
		m.sampleRate = msg.SampleRate
		m.channels = msg.Channels
		m.bitDepth = msg.BitDepth
	}
	if msg.Title != "" {
		m.title = msg.Title
		m.artist = msg.Artist
		m.album = msg.Album
	}
	if msg.ArtworkPath != "" {
		m.artworkPath = msg.ArtworkPath
	}
	// Volume is always applied when explicitly sent (can be 0 for silent)
	// We rely on caller not sending Volume=0 in messages unless it's intentional
	if msg.Volume != 0 {
		m.volume = msg.Volume
	}
	// Always apply stats - they can legitimately be zero
	m.received = msg.Received
	m.played = msg.Played
	m.dropped = msg.Dropped
	m.bufferDepth = msg.BufferDepth
	m.goroutines = msg.Goroutines
}

type StatusMsg struct {
	Connected     *bool
	ServerName    string
	PlaybackState string
	SyncRTT       int64
	SyncQuality   sync.Quality
	Codec         string
	SampleRate    int
	Channels      int
	BitDepth      int
	Title         string
	Artist        string
	Album         string
	ArtworkPath   string
	Volume        int
	Received      int64
	Played        int64
	Dropped       int64
	BufferDepth   int
	Goroutines    int
}

type VolumeChangeMsg struct {
	Volume int
	Muted  bool
}

type QuitMsg struct{}

func renderBar(value, max, width int) string {
	filled := (value * width) / max
	return strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", width-filled)
}

func truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length-3] + "..."
}

func channelName(channels int) string {
	if channels == 1 {
		return "Mono"
	}
	return "Stereo"
}

func repeatString(s string, count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(s, count)
}

// playbackStateDisplay returns a display string with icon for the given playback state.
func playbackStateDisplay(state string) string {
	switch state {
	case "playing":
		return "\u25b6 Playing"
	case "paused":
		return "\u23f8 Paused"
	case "stopped":
		return "\u23f9 Stopped"
	case "idle":
		return "\u25cf Idle"
	case "reconnecting":
		return "\u21bb Reconnecting..."
	default:
		return "\u25cf Idle"
	}
}

// formatSampleRate returns a human-friendly sample rate string.
func formatSampleRate(rate int) string {
	switch rate {
	case 44100:
		return "44.1kHz"
	case 48000:
		return "48kHz"
	case 88200:
		return "88.2kHz"
	case 96000:
		return "96kHz"
	case 176400:
		return "176.4kHz"
	case 192000:
		return "192kHz"
	default:
		if rate%1000 == 0 {
			return fmt.Sprintf("%dkHz", rate/1000)
		}
		return fmt.Sprintf("%.1fkHz", float64(rate)/1000.0)
	}
}

// formatCodecDisplay returns a rich codec description string.
func formatCodecDisplay(codec string, sampleRate, bitDepth int) string {
	rate := formatSampleRate(sampleRate)
	upper := strings.ToUpper(codec)

	switch strings.ToLower(codec) {
	case "pcm":
		return fmt.Sprintf("%s %s/%dbit lossless", upper, rate, bitDepth)
	case "flac":
		return fmt.Sprintf("%s %s/%dbit lossless", upper, rate, bitDepth)
	case "opus":
		return fmt.Sprintf("%s %s/%dbit", upper, rate, bitDepth)
	default:
		return fmt.Sprintf("%s %s/%dbit", upper, rate, bitDepth)
	}
}

// openDevicePicker transitions the Model into modeDevicePicker, enumerating
// available devices via the injected lister (defaulting to the real
// output.ListPlaybackDevices). The picker's own state tracks enumeration
// errors, so we never fail to enter the modal — the user always gets to
// see *something*, even if it's "couldn't enumerate devices".
func (m Model) openDevicePicker() (tea.Model, tea.Cmd) {
	lister := m.listPlaybackDevices
	if lister == nil {
		lister = output.ListPlaybackDevices
	}
	saveEnabled := m.configPath != ""
	m.picker = newDevicePicker(lister, m.audioDevice, saveEnabled)
	m.mode = modeDevicePicker
	return m, nil
}

// handlePickerMode dispatches a key to the picker's pure state machine and
// executes the requested action. Save writes audio_device to the config
// file and shows a transient "Saved. Restart to apply." banner for 3s.
func (m Model) handlePickerMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, action := m.picker.handlePickerKey(msg.String())
	m.picker = next
	switch action {
	case pickerNoop:
		return m, nil
	case pickerClose, pickerPropagate:
		m.mode = modeNormal
		return m, nil
	case pickerSave:
		chosen := m.picker.selected()
		if chosen == nil {
			m.mode = modeNormal
			return m, nil
		}
		writer := m.writeConfigKey
		if writer == nil {
			writer = sendspin.WriteStringKey
		}
		if chosen.Name != m.audioDevice {
			// No-op when the chosen value already matches what's saved —
			// matches the client_id write-back behavior and avoids
			// churning user-edited config files for no reason.
			if err := writer(m.configPath, "audio_device", chosen.Name); err != nil {
				m.mode = modeNormal
				return m.withTransient(fmt.Sprintf("Save failed: %v", err))
			}
		}
		m.mode = modeNormal
		return m.withTransient(fmt.Sprintf("Saved audio_device=%q. Restart to apply.", chosen.Name))
	}
	return m, nil
}

// withTransient schedules msg to be shown in the status row for ~3 seconds.
// The nonce guards against a later expire firing for a previously-shown
// banner that was already overwritten by a newer one.
func (m Model) withTransient(msg string) (tea.Model, tea.Cmd) {
	m.transientNonce++
	m.transientMsg = msg
	nonce := m.transientNonce
	return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return transientExpireMsg{nonce: nonce}
	})
}
