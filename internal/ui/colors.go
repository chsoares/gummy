package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ANSI color codes inspirados nos scripts ezpz
const (
	// Colors
	ColorReset    = "\033[0m"
	ColorBold     = "\033[1m"
	ColorDim      = "\033[2m"

	// Foreground colors
	ColorBlack    = "\033[30m"
	ColorRed      = "\033[31m"
	ColorGreen    = "\033[32m"
	ColorYellow   = "\033[33m"
	ColorBlue     = "\033[34m"
	ColorMagenta  = "\033[35m"
	ColorCyan     = "\033[36m"
	ColorWhite    = "\033[37m"

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
	SymbolDroplet     = "󰗣"    // Main gummy theme
	SymbolTarget      = "󰓾"    // Target/session
	SymbolFire        = ""    // Received shell
	SymbolGem         = ""    // Active sessions header
	SymbolSkull       = ""    // Session died
	SymbolCommand     = ""   // Commands/arrows
	SymbolInfo        = ""   // Information
	SymbolCheck       = ""   // Information
	SymbolDownload    = ""   // Information
	SymbolUpload      = ""   // Information
	SymbolError       = ""   // Information
	SymbolWarning     = ""   // Information
)

// Themed color functions inspired by ezpz scripts
func Header(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorYellow, SymbolInfo, text, ColorReset)
}

func Info(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorCyan, SymbolInfo, text, ColorReset)
}

func Success(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorCyan, SymbolCheck, text, ColorReset)
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

// Session status indicators
func SessionActive(text string) string {
	return fmt.Sprintf("%s%s %s%s", ColorGreen, SymbolTarget, text, ColorReset)
}

func SessionInactive(text string) string {
	return fmt.Sprintf("%s %s%s", ColorReset, text, ColorReset)
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
		Foreground(lipgloss.Color("magenta")).
		Padding(0, 1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("cyan")).
		Padding(0, 1)

	droplet := SymbolDroplet
	title := fmt.Sprintf("%s %s", droplet, subtitle)

	return boxStyle.Render(titleStyle.Render(title))
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
	return fmt.Sprintf("%s%s Session %d (%s) closed%s",
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