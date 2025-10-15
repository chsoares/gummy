package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chsoares/gummy/internal/listener"
	"github.com/chsoares/gummy/internal/statusbar"
	"github.com/chsoares/gummy/internal/tui"
	"github.com/chsoares/gummy/internal/ui"
)

// Config holds the application configuration
type Config struct {
	Port       int
	Host       string
	LogLevel   string
	UseTUI     bool
	UseStatusBar bool
}

func main() {
	// Parse command-line flags
	// flag package is Go's standard way to handle CLI arguments
	config := parseFlags()

	// Setup logging - minimal output like Penelope
	log.SetFlags(0)

	// Show banner
	fmt.Println(ui.Banner())
	//fmt.Println()

	// Initialize listener
	l := listener.New(config.Host, config.Port)

	// Start listening for connections
	if err := l.Start(); err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Failed to start listener: %v", err)))
		os.Exit(1)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start session manager menu - this will block
	go func() {
		<-sigChan
		fmt.Println() // Nova linha antes do goodbye
		if err := l.Stop(); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Error stopping listener: %v", err)))
		}
		fmt.Println(ui.Success("Goodbye!"))
		os.Exit(0)
	}()

	// Choose UI mode
	if config.UseTUI {
		// TUI mode (experimental) - enable silent mode
		l.GetSessionManager().SetSilent(true)
		if err := tui.Start(l.GetSessionManager()); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("TUI error: %v", err)))
			os.Exit(1)
		}
	} else {
		// CLI mode with optional status bar
		var bar *statusbar.StatusBar
		if config.UseStatusBar {
			bar = statusbar.New(l.GetSessionManager())
			bar.Start()
			defer bar.Stop()
		} else {
			fmt.Println(ui.HelpInfo("Type 'help' for available commands"))
			fmt.Println()
		}

		l.GetSessionManager().StartMenu()
	}
}

// parseFlags parses command-line arguments
// Go convention: unexported functions start with lowercase
func parseFlags() *Config {
	config := &Config{}

	// flag.IntVar binds the flag to a variable
	// Usage: -port 4444 or --port 4444
	flag.IntVar(&config.Port, "port", 4444, "Port to listen on")
	flag.IntVar(&config.Port, "p", 4444, "Port to listen on (shorthand)")
	
	flag.StringVar(&config.Host, "host", "0.0.0.0", "Host to bind to")
	flag.StringVar(&config.Host, "h", "0.0.0.0", "Host to bind to (shorthand)")

	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	flag.BoolVar(&config.UseTUI, "tui", false, "Use TUI mode (experimental)")
	flag.BoolVar(&config.UseStatusBar, "statusbar", false, "Show status bar (experimental, has issues)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Gummy - Advanced Shell Handler for CTFs\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	return config
}