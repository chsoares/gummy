// Package main defines the entry point of the program.
package main

import (
	"io"
	"log"
	"net"
	"sync"
	"time"
)

//
// ──────────────────────────────────────────────────────────────
//   CORE: SessionManager and networking logic
// ──────────────────────────────────────────────────────────────
//

// Session represents a single active connection.
type Session struct {
	ID        int
	Conn      net.Conn
	Address   string
	StartTime time.Time
}

// SessionEventType defines what kind of event occurred.
type SessionEventType int

const (
	EventNewSession SessionEventType = iota
	EventClosedSession
)

// SessionEvent carries information about a change in session state.
type SessionEvent struct {
	Type    SessionEventType
	Session *Session
}

// SessionManager manages multiple concurrent sessions.
type SessionManager struct {
	Sessions map[int]*Session
	Mutex    sync.Mutex
	NextID   int
	Events   chan SessionEvent
}

// NewSessionManager initializes a new SessionManager instance.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		Sessions: make(map[int]*Session),
		NextID:   0,
		Events:   make(chan SessionEvent, 10),
	}
}

// Add registers a new session and returns it.
func (m *SessionManager) Add(conn net.Conn) *Session {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	m.NextID++
	id := m.NextID

	s := &Session{
		ID:        id,
		Conn:      conn,
		Address:   conn.RemoteAddr().String(),
		StartTime: time.Now(),
	}

	m.Sessions[id] = s

	// Emit event (non-blocking)
	select {
	case m.Events <- SessionEvent{Type: EventNewSession, Session: s}:
	default:
	}

	return s
}

// Remove deletes a session by ID and notifies observers.
func (m *SessionManager) Remove(id int) {
	m.Mutex.Lock()
	s, ok := m.Sessions[id]
	if ok {
		delete(m.Sessions, id)
	}
	m.Mutex.Unlock()

	if ok {
		select {
		case m.Events <- SessionEvent{Type: EventClosedSession, Session: s}:
		default:
		}
	}
}

// handleConnection runs for each client and echoes back data.
func handleConnection(manager *SessionManager, s *Session) {
	defer func() {
		s.Conn.Close()
		manager.Remove(s.ID)
	}()
	_, _ = io.Copy(s.Conn, s.Conn)
}

//
// ──────────────────────────────────────────────────────────────
//   MAIN ENTRY POINT
// ──────────────────────────────────────────────────────────────
//

func main() {
	manager := NewSessionManager()

	// Run listener concurrently
	go func() {
		listener, err := net.Listen("tcp", ":4444")
		if err != nil {
			log.Fatal("Failed to start listener:", err)
		}
		defer listener.Close()
		log.Println(" Listening on :4444 ...")

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Println("Error accepting connection:", err)
				continue
			}
			session := manager.Add(conn)
			go handleConnection(manager, session)
		}
	}()

	// Start Bubble Tea TUI
	startUI(manager)
}
