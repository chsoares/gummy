package ui

import (
	"fmt"
	"sync"
	"time"
)

// Spinner shows an inline animated spinner
type Spinner struct {
	frames    []string
	index     int
	stopCh    chan struct{}
	messageCh chan string
	message   string
	stopped   bool
	wg        sync.WaitGroup
}

// NewSpinner creates a new spinner
func NewSpinner() *Spinner {
	return &Spinner{
		frames:    []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		index:     0,
		stopCh:    make(chan struct{}),
		messageCh: make(chan string, 10),
	}
}

// Start begins the spinner animation inline
func (s *Spinner) Start(message string) {
	s.message = message
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Clear line, move to start, and redraw with current message
				fmt.Printf("\r\033[K%s%s %s%s", ColorYellow, s.frames[s.index], s.message, ColorReset)
				s.index = (s.index + 1) % len(s.frames)
			case newMsg := <-s.messageCh:
				// Update message
				s.message = newMsg
			case <-s.stopCh:
				// Clear the spinner line completely
				fmt.Print("\r\033[K")
				return
			}
		}
	}()
}

// Update changes the spinner message dynamically
func (s *Spinner) Update(message string) {
	select {
	case s.messageCh <- message:
	default:
		// Channel full, skip update
	}
}

// Stop stops the spinner (safe to call multiple times)
func (s *Spinner) Stop() {
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.wg.Wait() // Wait for goroutine to finish and clear the line
}
