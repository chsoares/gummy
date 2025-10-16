package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/chsoares/gummy/internal/ui"
)

// Listener handles incoming TCP connections
type Listener struct {
	host           string
	port           int
	listenerIP     string           // IP for payload generation
	listener       net.Listener
	sessionManager *Manager // Gerenciador de múltiplas sessões
	mu             sync.RWMutex     // Protects concurrent access to listener state
	shutdown       bool             // Flag to indicate graceful shutdown
	silent         bool             // Suppress console output (reserved for future use)
}

// New creates a new Listener instance
// Go convention: constructor functions are usually called "New"
func NewListener(host string, port int) *Listener {
	return &Listener{
		host:           host,
		port:           port,
		sessionManager: NewManager(),
		silent:         false,
	}
}

// SetSilent enables/disables console output
func (l *Listener) SetSilent(silent bool) {
	l.silent = silent
}

// SetListenerIP sets the IP address for payload generation
func (l *Listener) SetListenerIP(ip string) {
	l.listenerIP = ip
	l.sessionManager.SetListenerIP(ip)
	l.sessionManager.SetListenerPort(l.port)
}

// GetListenerPort returns the listening port
func (l *Listener) GetListenerPort() int {
	return l.port
}

// Start begins listening for connections
// Returns an error if it fails to start
func (l *Listener) Start() error {
	addr := fmt.Sprintf("%s:%d", l.host, l.port)

	// net.Listen creates a TCP listener
	// "tcp" is the network type, addr is host:port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	l.listener = listener

	// Show the actual IP for payload generation, not the bind address
	displayAddr := addr
	if l.listenerIP != "" {
		displayAddr = fmt.Sprintf("%s:%d", l.listenerIP, l.port)
	}
	fmt.Println(ui.Info(fmt.Sprintf("Listening for connections on %s", displayAddr)))

	// Start accepting connections in a goroutine
	// This is non-blocking, allowing main to continue
	go l.acceptConnections()

	return nil
}

// acceptConnections continuously accepts new connections
func (l *Listener) acceptConnections() {
	for {
		// Accept blocks until a new connection arrives
		conn, err := l.listener.Accept()
		if err != nil {
			// Check if we're shutting down - if so, exit silently
			l.mu.RLock()
			isShutdown := l.shutdown
			l.mu.RUnlock()

			if isShutdown {
				return
			}

			log.Printf("Error accepting connection: %v", err)
			continue
		}

		// Handle each connection in its own goroutine
		// This allows multiple simultaneous connections
		go l.handleConnection(conn)
	}
}

// handleConnection processes a new connection
func (l *Listener) handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	sessionID := generateSessionID()

	// Adiciona sessão ao gerenciador
	l.sessionManager.AddSession(sessionID, conn, remoteAddr)

	// Handle the session's I/O
	// defer ensures cleanup happens when function returns
	defer func() {
		l.sessionManager.RemoveSession(sessionID)
		conn.Close()
	}()

	// Aguarda indefinidamente - a conexão será gerenciada pelo SessionManager
	// Quando uma sessão for usada, o Handler vai controlar a conexão
	// Quando a conexão fechar, o Handler vai detectar e a sessão será removida

	select {} // Bloqueia para sempre - cleanup via defer quando Handler detectar fechamento
}

// GetSessionManager retorna o gerenciador de sessões
func (l *Listener) GetSessionManager() *Manager {
	return l.sessionManager
}

// Stop gracefully shuts down the listener
func (l *Listener) Stop() error {
	// Set shutdown flag before closing to prevent error logging
	l.mu.Lock()
	l.shutdown = true
	l.mu.Unlock()

	if l.listener != nil {
		return l.listener.Close()
	}

	return nil
}

// generateSessionID creates a unique identifier for sessions
func generateSessionID() string {
	// Generate 8 random bytes
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback if crypto/rand fails (very rare)
		log.Printf("Warning: crypto/rand failed, using fallback ID")
		return fmt.Sprintf("session-%d", len(bytes))
	}
	
	// Convert to hex string (16 characters)
	return hex.EncodeToString(bytes)
}