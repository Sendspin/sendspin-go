// ABOUTME: Server TUI for displaying connected clients and stats
// ABOUTME: Real-time server status display using bubbletea
package server

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ServerTUI struct {
	program  *tea.Program
	updates  chan ServerStatus
	quitChan chan struct{} // Signal to stop the server
	stopped  bool
	mu       sync.Mutex
}

type ServerStatus struct {
	Name       string
	Port       int
	Uptime     time.Duration
	Clients    []ClientInfo
	AudioTitle string
}

type ClientInfo struct {
	Name  string
	ID    string
	Codec string
	State string
}

type tuiModel struct {
	status    ServerStatus
	startTime time.Time
	quitting  bool
	quitChan  chan struct{} // Channel to signal server stop
}

type tickMsg time.Time
type statusMsg ServerStatus

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		tickEvery(),
	)
}

func tickEvery() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			select {
			case m.quitChan <- struct{}{}:
			default:
			}
			return m, tea.Quit
		}

	case tickMsg:
		return m, tickEvery()

	case statusMsg:
		m.status = ServerStatus(msg)
		return m, nil
	}

	return m, nil
}

func (m tuiModel) View() string {
	if m.quitting {
		return "Shutting down server...\n"
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	clientHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("220"))

	var b strings.Builder

	b.WriteString(titleStyle.Render("Sendspin Server"))
	b.WriteString("\n\n")

	b.WriteString(headerStyle.Render("Server: "))
	b.WriteString(valueStyle.Render(m.status.Name))
	b.WriteString("\n")

	b.WriteString(headerStyle.Render("Port: "))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%d", m.status.Port)))
	b.WriteString("\n")

	b.WriteString(headerStyle.Render("Uptime: "))
	uptime := time.Since(m.startTime).Round(time.Second)
	b.WriteString(valueStyle.Render(uptime.String()))
	b.WriteString("\n")

	b.WriteString(headerStyle.Render("Playing: "))
	b.WriteString(valueStyle.Render(m.status.AudioTitle))
	b.WriteString("\n\n")

	b.WriteString(clientHeaderStyle.Render(fmt.Sprintf("Connected Clients (%d)", len(m.status.Clients))))
	b.WriteString("\n\n")

	if len(m.status.Clients) == 0 {
		b.WriteString(valueStyle.Render("  No clients connected"))
		b.WriteString("\n")
	} else {
		for _, client := range m.status.Clients {
			b.WriteString(fmt.Sprintf("  • %s", client.Name))
			b.WriteString(valueStyle.Render(fmt.Sprintf(" (%s, %s)", client.Codec, client.State)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render("Press 'q' or Ctrl+C to quit"))

	return b.String()
}

func NewServerTUI(serverName string, port int) *ServerTUI {
	return &ServerTUI{
		updates:  make(chan ServerStatus, 10),
		quitChan: make(chan struct{}, 1),
	}
}

func (t *ServerTUI) Start(serverName string, port int) error {
	m := tuiModel{
		status: ServerStatus{
			Name:       serverName,
			Port:       port,
			AudioTitle: "Initializing...",
			Clients:    []ClientInfo{},
		},
		startTime: time.Now(),
		quitChan:  t.quitChan,
	}

	t.program = tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		for status := range t.updates {
			if t.program != nil {
				t.program.Send(statusMsg(status))
			}
		}
	}()

	_, err := t.program.Run()
	return err
}

func (t *ServerTUI) Update(status ServerStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	select {
	case t.updates <- status:
	default:
	}
}

func (t *ServerTUI) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()

	if t.program != nil {
		t.program.Quit()
	}
	close(t.updates)
}

func (t *ServerTUI) QuitChan() <-chan struct{} {
	return t.quitChan
}
