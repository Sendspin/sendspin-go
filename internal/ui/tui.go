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

func NewModel(volCtrl *VolumeControl) Model {
	return Model{
		volume:     100,
		state:      "idle",
		volumeCtrl: volCtrl,
	}
}

func Run(volCtrl *VolumeControl) (*tea.Program, error) {
	p := tea.NewProgram(NewModel(volCtrl), tea.WithAltScreen())
	return p, nil
}
