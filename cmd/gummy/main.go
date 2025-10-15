package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chsoares/gummy/internal/listener"
	"github.com/chsoares/gummy/internal/netutil"
	"github.com/chsoares/gummy/internal/ui"
)

// Config holds the application configuration
type Config struct {
	Port      int
	Host      string
	Interface string
	IP        string // Resolved IP (from interface or direct)
	LogLevel  string
}

func main() {
	// Parse command-line flags
	// flag package is Go's standard way to handle CLI arguments
	config := parseFlags()

	// Setup logging - minimal output like Penelope
	log.SetFlags(0)

	// Print banner first
	fmt.Println(ui.Banner())
	fmt.Println(ui.HelpInfo("Type 'help' for available commands"))
	fmt.Println()

	// Initialize listener with resolved IP
	l := listener.New(config.Host, config.Port)
	l.SetListenerIP(config.IP) // Set the IP for payload generation

	// Start listening for connections
	if err := l.Start(); err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Failed to start listener: %v", err)))
		os.Exit(1)
	}

	// Setup signal handling - only for cleanup, not for exit
	// Exit is only via exit/quit/q commands
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM) // Only SIGTERM, not SIGINT (Ctrl+C)

	go func() {
		<-sigChan
		fmt.Println()
		if err := l.Stop(); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Error stopping listener: %v", err)))
		}
		fmt.Println(ui.Success("Goodbye!"))
		os.Exit(0)
	}()

	l.GetSessionManager().StartMenu()
}

// parseFlags parses command-line arguments
// Go convention: unexported functions start with lowercase
func parseFlags() *Config {
	config := &Config{}

	var interfaceFlag string
	var ipFlag string

	// flag.IntVar binds the flag to a variable
	flag.IntVar(&config.Port, "port", 4444, "Port to listen on")
	flag.IntVar(&config.Port, "p", 4444, "Port to listen on (shorthand)")

	flag.StringVar(&interfaceFlag, "interface", "", "Network interface to bind to (e.g., eth0, eno1)")
	flag.StringVar(&interfaceFlag, "i", "", "Network interface to bind to (shorthand)")

	flag.StringVar(&ipFlag, "ip", "", "IP address to bind to (alternative to -i)")

	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Gummy - Advanced Shell Handler for CTFs\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s -i <interface> -p <port>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s -ip <address> -p <port>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n%s\n", netutil.FormatInterfaceList())
	}

	flag.Parse()

	// Validate that either interface or IP is provided
	if interfaceFlag == "" && ipFlag == "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", ui.Error("Error: Either -i <interface> or -ip <address> is required"))
		flag.Usage()
		os.Exit(1)
	}

	// Both flags provided - error
	if interfaceFlag != "" && ipFlag != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", ui.Error("Error: Cannot specify both -i and -ip flags"))
		flag.Usage()
		os.Exit(1)
	}

	// Resolve IP from interface
	if interfaceFlag != "" {
		ip, err := netutil.GetIPFromInterface(interfaceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n\n", ui.Error(fmt.Sprintf("Error: %v", err)))
			fmt.Fprintf(os.Stderr, "%s\n", netutil.FormatInterfaceList())
			os.Exit(1)
		}
		config.IP = ip
		config.Interface = interfaceFlag
		config.Host = ip // Bind to the specific interface IP
	} else {
		// Validate IP address
		if !netutil.IsValidIP(ipFlag) {
			fmt.Fprintf(os.Stderr, "%s\n", ui.Error(fmt.Sprintf("Error: Invalid IP address: %s", ipFlag)))
			os.Exit(1)
		}
		config.IP = ipFlag
		config.Host = ipFlag // Bind to specific IP
	}

	return config
}