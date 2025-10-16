package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ANSI color codes inspirados nos scripts ezpz
const (
	// Colors
	ColorReset = "\033[0m"
	ColorBold  = "\033[1m"
	ColorDim   = "\033[2m"

	// Foreground colors
	ColorBlack   = "\033[30m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"

	// Bright colors
	ColorBrightBlack   = "\033[90m"
	ColorBrightRed     = "\033[91m"
	ColorBrightGreen   = "\033[92m"
	ColorBrightYellow  = "\033[93m"
	ColorBrightBlue    = "\033[94m"
	ColorBrightMagenta = "\033[95m"
	ColorBrightCyan    = "\033[96m"
	ColorBrightWhite   = "\033[97m"

	// Symbols (nerdfont only) - simplified set
	SymbolDroplet  = "󰗣" // Main gummy theme
	SymbolTarget   = "󰓾" // Target/session
	SymbolFire     = "" // Received shell
	SymbolGem      = "" // Active sessions header
	SymbolSkull    = "" // Session died
	SymbolCommand  = "" // Commands/arrows
	SymbolInfo     = "" // Information
	SymbolCheck    = "" // Information
	SymbolDownload = "" // Information
	SymbolUpload   = "" // Information
	SymbolError    = "" // Information
	SymbolWarning  = "" // Information
)

// Themed color functions inspired by ezpz scripts
func Header(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolInfo, text, ColorReset)
}

func Info(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorCyan, SymbolInfo, text, ColorReset)
}

func Success(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolCheck, text, ColorReset)
}

func Error(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorRed, SymbolError, text, ColorReset)
}

func Warning(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorMagenta, SymbolWarning, text, ColorReset)
}

func Command(text string) string {
	return fmt.Sprintf("%s%s", ColorReset, text)
}

func Title(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolGem, text, ColorReset)
}

func Question(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorBlue, SymbolCommand, text, ColorReset)
}

// CommandHelp for help text
func CommandHelp(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorBlue, SymbolCommand, text, ColorReset)
}

// HelpInfo for command instructions
func HelpInfo(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorBlue, SymbolCommand, text, ColorReset)
}

// TableHeader for table column headers (no symbol, just colored text)
func TableHeader(text string) string {
	return fmt.Sprintf("%s%s%s", ColorBlue, text, ColorReset)
}

// Session status indicators
func SessionActive(text string) string {
	return fmt.Sprintf("%s%s%s", ColorYellow, text, ColorReset)
}

func SessionInactive(text string) string {
	return fmt.Sprintf("%s%s", text, ColorReset)
}

// Banner function inspired by gum style
func Banner() string {
	// Define styles with Lipgloss
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("5")). // Magenta
		Bold(true).
		Padding(0, 1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1)

	droplet := SymbolDroplet
	title := fmt.Sprintf("gummy shell %s", droplet)

	return boxStyle.Render(titleStyle.Render(title))
}

// Subtitle banner for specific contexts
func SubBanner(subtitle string) string {
	// Define styles with Lipgloss
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("5")).
		Padding(0, 1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1)

	droplet := SymbolDroplet
	title := fmt.Sprintf("%s %s", droplet, subtitle)

	return boxStyle.Render(titleStyle.Render(title))
}

// SectionHeader creates a bordered header for sections (list, help, etc)
func SectionHeader(title string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("5")). // Magenta
		Padding(0, 1)

	return style.Render(title)
}

// BoxWithTitle creates a box with title and content lines (like dwrm help output)
func BoxWithTitle(title string, lines []string) string {
	// Title style
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		Bold(true)

	// Box style with border
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("5")). // Magenta border
		Padding(0, 1)

	// Join all content lines
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)

	return boxStyle.Render(content)
}

// BoxWithTitlePadded creates a box with title and content lines with custom padding
func BoxWithTitlePadded(title string, lines []string, paddingRight int) string {
	// Title style
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		Bold(true)

	// Box style with border and custom padding
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("5")). // Magenta border
		Padding(0, paddingRight)

	// Join all content lines
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)

	return boxStyle.Render(content)
}

// Prompt with gummy theme
func Prompt() string {
	return fmt.Sprintf("%s%s%s gummy%s%s ❯ %s", ColorMagenta, SymbolDroplet, ColorBold, ColorReset, ColorBrightMagenta, ColorReset)
}

// PromptWithSession shows prompt with selected session number
func PromptWithSession(sessionID int) string {
	return fmt.Sprintf("%s%s%s gummy [%d]%s%s ❯ %s", ColorMagenta, SymbolDroplet, ColorBold, sessionID, ColorReset, ColorBrightMagenta, ColorReset)
}

// PTY status indicators
func PTYSuccess() string {
	return fmt.Sprintf("%s%s PTY upgrade successful%s", ColorGreen, SymbolInfo, ColorReset)
}

func PTYFailed() string {
	return fmt.Sprintf("%s%s PTY upgrade failed - using raw shell%s", ColorYellow, SymbolInfo, ColorReset)
}

// Session management
func SessionOpened(id int, addr string) string {
	return fmt.Sprintf("%s%s Reverse shell received on session %d (%s)%s",
		ColorYellow, SymbolFire, id, addr, ColorReset)
}

func SessionClosed(id int, addr string) string {
	return fmt.Sprintf("%s%s Session %d (%s) closed!%s",
		ColorRed, SymbolSkull, id, addr, ColorReset)
}

func UsingSession(id int, addr string) string {
	return fmt.Sprintf("%s%s Using session %d (%s)%s",
		ColorYellow, SymbolTarget, id, addr, ColorReset)
}

func ReturningToMenu() string {
	return fmt.Sprintf("\r\n%s%s Exiting interactive shell%s\r\n",
		ColorCyan, SymbolInfo, ColorReset)
}

// File transfer operations
func Downloading(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolDownload, text, ColorReset)
}

func Uploading(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolUpload, text, ColorReset)
}

// Module execution mode indicators (with emojis as placeholders)
func ExecutionModeSymbol(mode string) string {
	switch mode {
	case "memory":
		return "" // In-memory execution (will be replaced with nerd font symbol)
	case "disk-cleanup":
		return "󰃢" // Disk with cleanup (will be replaced with nerd font symbol)
	case "disk-no-cleanup":
		return "" // Disk without cleanup (will be replaced with nerd font symbol)
	default:
		return "" // Unknown mode
	}
}

// Module execution mode legend (gray text like confirmation hints)
func ExecutionModeLegend() string {
	legendStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return legendStyle.Render(fmt.Sprintf("\nExecution mode: %s In-memory  %s Disk + cleanup  %s Disk without cleanup",
		ExecutionModeSymbol("memory"),
		ExecutionModeSymbol("disk-cleanup"),
		ExecutionModeSymbol("disk-no-cleanup")))
}

// confirmModel is the Bubble Tea model for confirmation (based on gum)
type confirmModel struct {
	prompt       string
	affirmative  string
	negative     string
	quitting     bool
	showHelp     bool
	help         help.Model
	keys         confirmKeymap
	confirmation bool

	// styles matching gum flags
	promptStyle     lipgloss.Style
	selectedStyle   lipgloss.Style
	unselectedStyle lipgloss.Style
}

type confirmKeymap struct {
	Abort       key.Binding
	Quit        key.Binding
	Negative    key.Binding
	Affirmative key.Binding
	Toggle      key.Binding
	Submit      key.Binding
}

func (k confirmKeymap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Submit, k.Affirmative, k.Negative}
}

func (k confirmKeymap) FullHelp() [][]key.Binding { return nil }

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Abort):
			m.confirmation = false
			return m, tea.Interrupt
		case key.Matches(msg, m.keys.Quit):
			m.confirmation = false
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Negative):
			m.confirmation = false
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Toggle):
			m.confirmation = !m.confirmation
		case key.Matches(msg, m.keys.Submit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Affirmative):
			m.quitting = true
			m.confirmation = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.quitting {
		return ""
	}

	var aff, neg string

	if m.confirmation {
		aff = m.selectedStyle.Render(m.affirmative)
		neg = m.unselectedStyle.Render(m.negative)
	} else {
		aff = m.unselectedStyle.Render(m.affirmative)
		neg = m.selectedStyle.Render(m.negative)
	}

	parts := []string{
		m.promptStyle.Render(m.prompt),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, aff, neg),
	}

	if m.showHelp {
		parts = append(parts, "", m.help.View(m.keys))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Wrap everything in a bordered box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("5")). // Magenta border
		Padding(0, 1)

	return boxStyle.Render(content)
}

// Confirm prompts user for yes/no confirmation (exact gum implementation)
// Returns true if user confirms, false otherwise
func Confirm(message string) bool {
	// Match gum default keymap
	keys := confirmKeymap{
		Abort: key.NewBinding(
			key.WithKeys("ctrl+c"),
		),
		Quit: key.NewBinding(
			key.WithKeys("esc"),
		),
		Negative: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("n", "No"),
		),
		Affirmative: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "Yes"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("left", "h", "right", "l", "tab"),
			key.WithHelp("←/→", "toggle"),
		),
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
	}

	// Match gum styles: --prompt.foreground 6 --selected.background 5 --selected.foreground 255 --unselected.background 235
	m := confirmModel{
		prompt:       message,
		affirmative:  "Yes",
		negative:     "No",
		confirmation: true, // Start with Yes selected
		showHelp:     true,
		help:         help.New(),
		keys:         keys,
		promptStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("6")), // Cyan
		selectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("5")).
			Bold(true).
			Width(12).              // Width of 12 for better centering (divisible by both 2 and 3)
			Align(lipgloss.Center). // Center text in button
			MarginRight(2),         // Space between buttons
		unselectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Background(lipgloss.Color("235")). // Light gray background
			Width(12).                         // Width of 12 for better centering (divisible by both 2 and 3)
			Align(lipgloss.Center).            // Center text in button
			MarginRight(2),                    // Space between buttons
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return false
	}

	return finalModel.(confirmModel).confirmation
}
