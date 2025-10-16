package internal

import (
	"fmt"
	"os"
	"os/exec"
)

// Terminal emulator configuration
type terminalConfig struct {
	name      string
	flag      string
	extraArgs []string
}

// terminalEmulators list ordered by priority
var terminalEmulators = []terminalConfig{
	// Modern terminals (PRIORITY!)
	{"kitty", "-e", nil},                             // Kitty - GPU accelerated, user's terminal
	{"ghostty", "-e", nil},                           // Ghostty - Mitchell's new terminal
	{"foot", "-e", nil},                              // Foot - Wayland native
	{"alacritty", "-e", nil},                         // Alacritty - GPU accelerated
	{"wezterm", "start", []string{"--", "sh", "-c"}}, // WezTerm - Lua config

	// Traditional terminals
	{"gnome-terminal", "--", nil},
	{"konsole", "-e", nil},
	{"xfce4-terminal", "-e", nil},
	{"mate-terminal", "-e", nil},
	{"terminator", "-x", nil},
	{"xterm", "-e", nil},
	{"urxvt", "-e", nil},
	{"st", "-e", nil}, // Suckless terminal
}

// OpenTerminal opens a new terminal window with the given command
func OpenTerminal(command string) error {
	var terminal string
	var config terminalConfig

	// Find first available terminal
	for _, t := range terminalEmulators {
		if _, err := exec.LookPath(t.name); err == nil {
			terminal = t.name
			config = t
			break
		}
	}

	if terminal == "" {
		return fmt.Errorf("no terminal emulator found. Install: kitty, alacritty, foot, or gnome-terminal")
	}

	// Build command arguments
	var args []string

	// WezTerm has different structure
	if terminal == "wezterm" {
		args = append(args, "start", "--")
		args = append(args, "sh", "-c", command)
	} else {
		if config.flag != "" {
			args = append(args, config.flag)
		}
		if config.extraArgs != nil {
			args = append(args, config.extraArgs...)
		}
		args = append(args, "sh", "-c", command)
	}

	// fmt.Println(ui.Info(fmt.Sprintf("Opening %s terminal...", terminal)))

	cmd := exec.Command(terminal, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", terminal, err)
	}

	return nil
}
