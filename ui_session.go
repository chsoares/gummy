// ui_session.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//
// ──────────────────────────────────────────────
//   Session View — Terminal interativo
// ──────────────────────────────────────────────
//

// SessionViewModel represents the second screen (per-session view)
type SessionViewModel struct {
	session  *Session
	viewport viewport.Model
	input    textinput.Model
	outputCh chan string // channel for new data from connection
	exit     bool
}

// Internal messages
type newOutputMsg string

// Constructor
func NewSessionViewModel(s *Session) *SessionViewModel {
	vp := viewport.New(60, 15)
	vp.SetContent("Connected to session #" + fmt.Sprint(s.ID))

	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.Focus()

	m := &SessionViewModel{
		session:  s,
		viewport: vp,
		input:    ti,
		outputCh: make(chan string, 10),
	}

	// Start reading from the connection
	go m.readFromConn()

	return m
}

//
// ──────────────────────────────────────────────
//   I/O: read and write to the connection
// ──────────────────────────────────────────────
//

// Goroutine that continuously reads data from the connection
func (m *SessionViewModel) readFromConn() {
	reader := bufio.NewReader(m.session.Conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				m.outputCh <- "\n[connection closed]"
				close(m.outputCh)
				return
			}
			m.outputCh <- fmt.Sprintf("\n[error: %v]", err)
			return
		}
		m.outputCh <- line
	}
}

// Sends input text to the connection
func (m *SessionViewModel) sendCommand(cmd string) {
	if cmd == "" {
		return
	}
	// Ensure newline at the end
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	m.session.Conn.Write([]byte(cmd))
}

//
// ──────────────────────────────────────────────
//   Bubble Tea lifecycle
// ──────────────────────────────────────────────
//

func (m *SessionViewModel) Init() tea.Cmd {
	// Start listening for output messages
	return m.waitForOutput()
}

// Custom command to bridge outputCh to Bubble Tea
func (m *SessionViewModel) waitForOutput() tea.Cmd {
	return func() tea.Msg {
		if msg, ok := <-m.outputCh; ok {
			return newOutputMsg(msg)
		}
		return nil
	}
}

func (m *SessionViewModel) Update(msg tea.Msg) (*SessionViewModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.exit = true
			return m, nil
		case "enter":
			cmd := m.input.Value()
			m.sendCommand(cmd)
			m.input.SetValue("")
			return m, nil
		}

	case newOutputMsg:
		m.viewport.SetContent(m.viewport.View() + string(msg))
		m.viewport.GotoBottom()
		return m, m.waitForOutput()
	}

	// Update subcomponents
	var cmds []tea.Cmd
	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *SessionViewModel) View() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("99")).
		Padding(1, 2)

	return style.Render(
		fmt.Sprintf("Session #%d — %s\n\n%s\n\n> %s\n\n[Esc to return]",
			m.session.ID,
			m.session.Address,
			m.viewport.View(),
			m.input.View(),
		),
	)
}
