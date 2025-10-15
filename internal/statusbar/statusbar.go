package statusbar

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/chsoares/gummy/internal/session"
	"golang.org/x/term"
)

// StatusBar renders a persistent status bar at the top of the terminal
type StatusBar struct {
	manager *session.Manager
	width   int
	running bool
	stopCh  chan struct{}
}

// New creates a new status bar
func New(manager *session.Manager) *StatusBar {
	width, _, _ := term.GetSize(0)
	if width == 0 {
		width = 80 // Fallback
	}

	return &StatusBar{
		manager: manager,
		width:   width,
		stopCh:  make(chan struct{}),
	}
}

// Start begins rendering the status bar at the bottom
func (sb *StatusBar) Start() {
	sb.running = true

	// Initial render
	sb.render()

	// Update every second in background
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if sb.running {
					sb.render()
				}
			case <-sb.stopCh:
				return
			}
		}
	}()
}

// Stop stops the status bar
func (sb *StatusBar) Stop() {
	sb.running = false
	close(sb.stopCh)

	// Clear the status bar at bottom
	_, height, _ := term.GetSize(0)
	if height > 0 {
		fmt.Print("\033[s")                      // Save cursor
		fmt.Printf("\033[%d;1H", height)         // Move to bottom line
		fmt.Print("\033[2K")                     // Clear line
		fmt.Print("\033[u")                      // Restore cursor
	}
}

// render draws the status bar
func (sb *StatusBar) render() {
	// Get terminal dimensions
	width, height, _ := term.GetSize(0)
	if width > 0 {
		sb.width = width
	}
	if height == 0 {
		height = 24
	}

	// Get session info
	sessionCount := sb.manager.GetSessionCount()
	activeSessionInfo := ""

	sessions := sb.manager.GetAllSessions()
	for _, sess := range sessions {
		if sess.Active {
			activeSessionInfo = fmt.Sprintf(" | Active: #%d (%s)", sess.NumID, sess.RemoteIP)
			break
		}
	}

	// Build status content
	leftContent := fmt.Sprintf("🍬 Gummy | Sessions: %d%s", sessionCount, activeSessionInfo)
	rightContent := time.Now().Format("15:04:05")

	// Calculate spacing
	contentLen := lipgloss.Width(leftContent) + lipgloss.Width(rightContent)
	spacingLen := sb.width - contentLen
	if spacingLen < 1 {
		spacingLen = 1
	}
	spacing := strings.Repeat(" ", spacingLen)

	// Style the bar
	barStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).  // Purple background
		Foreground(lipgloss.Color("230")). // Light text
		Bold(true).
		Width(sb.width)

	statusLine := leftContent + spacing + rightContent
	styledBar := barStyle.Render(statusLine)

	// Render at bottom of screen
	// Don't use save/restore cursor as it doesn't work well with readline
	// Instead, use alternate save/restore and move cursor back to where readline expects it

	fmt.Print("\0337")                       // Save cursor (DEC method)
	fmt.Printf("\033[%d;1H", height)         // Move to last line
	fmt.Print(styledBar)                     // Draw the bar
	fmt.Print("\0338")                       // Restore cursor (DEC method)
}

// Update forces an immediate update of the status bar
func (sb *StatusBar) Update() {
	if sb.running {
		sb.render()
	}
}
