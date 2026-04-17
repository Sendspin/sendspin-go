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

func NewModel(volCtrl *VolumeControl, transportCtrl *TransportControl) Model {
	return Model{
		volume:        100,
		state:         "idle",
		playbackState: "idle",
		volumeCtrl:    volCtrl,
		transportCtrl: transportCtrl,
	}
}

func Run(volCtrl *VolumeControl, transportCtrl *TransportControl) (*tea.Program, error) {
	p := tea.NewProgram(NewModel(volCtrl, transportCtrl), tea.WithAltScreen())
	return p, nil
}
