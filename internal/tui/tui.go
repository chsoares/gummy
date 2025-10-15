package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chsoares/gummy/internal/session"
)

// View represents different screens in the TUI
type View int

const (
	ViewSessionList View = iota
	ViewShell
)

// tickMsg is sent on every tick to refresh the UI
type tickMsg time.Time

// shellOutputMsg contains new output from the shell
type shellOutputMsg string

// Model is the main Bubble Tea model
type Model struct {
	manager       *session.Manager
	currentView   View
	sessionCursor int // Which session is selected in the list
	width         int
	height        int
	quitting      bool

	// Shell state
	viewport      viewport.Model
	textarea      textarea.Model
	shellContent  *strings.Builder // All shell output (pointer to avoid copy issues)
	activeSession *session.SessionInfo
	ready         bool // Viewport initialized
}

// NewModel creates a new TUI model
func NewModel(manager *session.Manager) Model {
	ta := textarea.New()
	ta.Placeholder = "Type command here..."
	ta.Focus()
	ta.Prompt = "$ "
	ta.CharLimit = 500
	ta.SetWidth(80)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false

	vp := viewport.New(80, 20)
	vp.SetContent("")

	var sb strings.Builder

	return Model{
		manager:       manager,
		currentView:   ViewSessionList,
		sessionCursor: 0,
		textarea:      ta,
		viewport:      vp,
		shellContent:  &sb,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tickCmd()
}

// tickCmd sends a tick message every 500ms to refresh the UI
func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle shell view input
		if m.currentView == ViewShell {
			switch msg.Type {
			case tea.KeyEsc:
				// Go back to session list
				m.currentView = ViewSessionList
				m.textarea.Reset()
				m.activeSession = nil
				m.shellContent.Reset()
				return m, nil

			case tea.KeyCtrlC:
				// Send Ctrl+C to remote shell
				if m.activeSession != nil {
					m.activeSession.Conn.Write([]byte{3})
				}
				return m, nil

			case tea.KeyEnter:
				// Send command to remote shell
				if m.activeSession != nil {
					command := m.textarea.Value()
					if command != "" {
						m.activeSession.Conn.Write([]byte(command + "\n"))
						m.textarea.Reset()
					}
				}
				return m, nil

			case tea.KeyPgUp, tea.KeyPgDown:
				// Scroll viewport with PgUp/PgDown only (leave arrows for text selection)
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd

			default:
				// Update textarea for typing
				m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			}
		}

		// Session list view input
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.sessionCursor > 0 {
				m.sessionCursor--
			}
		case "down", "j":
			sessionCount := m.manager.GetSessionCount()
			if m.sessionCursor < sessionCount-1 {
				m.sessionCursor++
			}
		case "enter":
			// Get selected session and switch to shell view
			sessions := m.manager.GetAllSessions()
			if m.sessionCursor < len(sessions) {
				m.activeSession = sessions[m.sessionCursor]
				m.currentView = ViewShell
				m.shellContent.Reset()
				m.shellContent.WriteString(fmt.Sprintf("Connected to session %d (%s)\n",
					m.activeSession.NumID, m.activeSession.RemoteIP))
				m.viewport.SetContent(m.shellContent.String())
				m.textarea.Reset()
				m.textarea.Focus()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if m.currentView == ViewShell && !m.ready {
			// Initialize viewport size
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 6 // Leave space for input and header
			m.textarea.SetWidth(msg.Width - 4)
			m.ready = true
		} else if m.currentView == ViewShell {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 6
			m.textarea.SetWidth(msg.Width - 4)
		}

	case tickMsg:
		// Refresh UI every tick and read shell output if in shell view
		if m.currentView == ViewShell && m.activeSession != nil {
			cmd = readShellOutput(m.activeSession)
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case shellOutputMsg:
		// Append shell output and update viewport
		output := string(msg)
		if output != "" {
			m.shellContent.WriteString(output)
			m.viewport.SetContent(m.shellContent.String())
			m.viewport.GotoBottom() // Auto-scroll to bottom
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the UI
func (m Model) View() string {
	if m.quitting {
		return "Goodbye! 👋\n"
	}

	switch m.currentView {
	case ViewSessionList:
		return m.renderSessionList()
	case ViewShell:
		return m.renderShell()
	default:
		return "Unknown view"
	}
}

// renderSessionList shows the list of active sessions
func (m Model) renderSessionList() string {
	s := "\n"
	s += "  🍬 Gummy - Session Manager\n\n"

	sessionCount := m.manager.GetSessionCount()
	if sessionCount == 0 {
		s += "  No active sessions.\n"
		s += "  Waiting for connections...\n\n"
	} else {
		s += "  Active Sessions:\n\n"

		// Get all sessions (we'll need to add a method to Manager)
		sessions := m.manager.GetAllSessions()
		for i, sess := range sessions {
			cursor := " "
			if i == m.sessionCursor {
				cursor = ">"
			}
			s += fmt.Sprintf("  %s [%d] %s - %s\n", cursor, sess.NumID, sess.RemoteIP, sess.Whoami)
		}
		s += "\n"
	}

	s += "\n  Controls:\n"
	s += "  ↑/↓: Navigate  Enter: Connect  q: Quit\n"

	return s
}

// renderShell shows the interactive shell view with viewport
func (m Model) renderShell() string {
	if m.activeSession == nil {
		return "No active session\n"
	}

	// Styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		Padding(0, 1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 1)

	// Header
	header := headerStyle.Render(fmt.Sprintf("🐚 Session %d - %s", m.activeSession.NumID, m.activeSession.RemoteIP))

	// Help text
	help := helpStyle.Render("Esc: Back | Ctrl+C: Interrupt | PgUp/PgDown: Scroll | Shift+Mouse: Select text")

	// Assemble view
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		m.viewport.View(),
		m.textarea.View(),
		help,
	)
}

// readShellOutput reads available data from the shell connection
func readShellOutput(sess *session.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		// Set a short read deadline to avoid blocking
		sess.Conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		defer sess.Conn.SetReadDeadline(time.Time{})

		buffer := make([]byte, 4096)
		n, err := sess.Conn.Read(buffer)
		if err != nil {
			return shellOutputMsg("")
		}

		if n > 0 {
			return shellOutputMsg(string(buffer[:n]))
		}

		return shellOutputMsg("")
	}
}

// Start runs the TUI application
func Start(manager *session.Manager) error {
	p := tea.NewProgram(
		NewModel(manager),
		tea.WithAltScreen(),
		// NOTE: Mouse support disabled to allow native terminal text selection
	)
	_, err := p.Run()
	return err
}
