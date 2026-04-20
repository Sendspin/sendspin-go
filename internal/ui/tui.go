// ABOUTME: TUI initialization and control
// ABOUTME: Wraps bubbletea program for player UI
package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type VolumeControl struct {
	Changes chan VolumeChangeMsg
	Quit    chan QuitMsg
}

func NewVolumeControl() *VolumeControl {
	return &VolumeControl{
		Changes: make(chan VolumeChangeMsg, 10),
		Quit:    make(chan QuitMsg, 1),
	}
}

type TransportMsg struct {
	Command string // "play", "pause", "toggle", "next", "previous", "reconnect"
}

type TransportControl struct {
	Commands chan TransportMsg
}

func NewTransportControl() *TransportControl {
	return &TransportControl{
		Commands: make(chan TransportMsg, 10),
	}
}

// Config bundles everything the TUI needs from main.go at construction.
// Extending by adding fields is intentionally cheap — the alternative was
// growing the Run() / NewModel() positional argument list every time a new
// TUI feature needs a dependency.
type Config struct {
	VolumeCtrl    *VolumeControl
	TransportCtrl *TransportControl
	// ConfigPath is where WriteStringKey persists settings like audio_device.
	// Empty disables the Save action in modals that would write to disk.
	ConfigPath string
	// AudioDevice is the device currently driving playback, shown in the
	// status line and used to seed the picker's highlighted row.
	AudioDevice string
}

func NewModel(cfg Config) Model {
	return Model{
		volume:        100,
		state:         "idle",
		playbackState: "idle",
		volumeCtrl:    cfg.VolumeCtrl,
		transportCtrl: cfg.TransportCtrl,
		configPath:    cfg.ConfigPath,
		audioDevice:   cfg.AudioDevice,
	}
}

func Run(cfg Config) (*tea.Program, error) {
	p := tea.NewProgram(NewModel(cfg), tea.WithAltScreen())
	return p, nil
}
