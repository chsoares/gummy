package statusbar

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/chsoares/gummy/internal/session"
	"golang.org/x/term"
)

// StatusBar renders a persistent status bar at the bottom of the terminal
type StatusBar struct {
	manager *session.Manager
	width   int
	height  int
	running bool
	stopCh  chan struct{}
}

// New creates a new status bar
func New(manager *session.Manager) *StatusBar {
	width, height, _ := term.GetSize(0)
	if width == 0 {
		width = 80 // Fallback
	}
	if height == 0 {
		height = 24 // Fallback
	}

	return &StatusBar{
		manager: manager,
		width:   width,
		height:  height,
		stopCh:  make(chan struct{}),
	}
}

// Start begins rendering the status bar at the bottom
func (sb *StatusBar) Start() {
	sb.running = true

	// Print a newline to ensure we have space at bottom
	fmt.Println()

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

	// Clear the status bar line
	fmt.Print("\033[s")    // Save cursor
	fmt.Print("\033[1A")   // Move up one line
	fmt.Print("\r\033[K")  // Clear line
	fmt.Print("\033[u")    // Restore cursor
}

// render draws the status bar
func (sb *StatusBar) render() {
	// Get terminal dimensions
	width, height, _ := term.GetSize(0)
	if width > 0 {
		sb.width = width
	}
	if height > 0 {
		sb.height = height
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

	// Render on the line above the prompt
	// Use ANSI codes to save position, move up one line, render, and restore
	fmt.Print("\033[s")          // Save cursor position
	fmt.Print("\033[1A")         // Move up one line
	fmt.Print("\r")              // Go to start of line
	fmt.Print(styledBar)         // Draw the bar
	fmt.Print("\033[u")          // Restore cursor position
}

// Update forces an immediate update of the status bar
func (sb *StatusBar) Update() {
	if sb.running {
		sb.render()
	}
}
