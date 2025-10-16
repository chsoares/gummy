# Gummy TODO List

## Current Status
**Last Updated:** 2025-10-16
**Current Phase:** Module System complete, Windows modules pending
**Next Priority:** Testing and expansion

---

## High Priority

### 1. Windows Module Testing
**Status:** URLs defined, modules not implemented
**Files:** `internal/modules.go`
**Estimated Time:** 2-3 hours

**Tasks:**
- [ ] Create WinPEAS module (similar to peas, but for Windows)
- [ ] Create PowerUp module (PowerShell script execution)
- [ ] Create PrivescCheck module (Windows privilege escalation checker)
- [ ] Test on Windows target (requires Windows VM or HTB box)
- [ ] Verify PowerShell script execution works correctly
- [ ] Handle .exe vs .ps1 file types appropriately

**Implementation Notes:**
- Use `RunScript()` for .ps1 files (PowerShell execution)
- Use `RunBinary()` for .exe files (direct execution)
- May need to adjust execution commands for Windows (cmd.exe vs PowerShell)
- Consider adding `-ExecutionPolicy Bypass` for PowerShell scripts

---

### 2. Session Logging
**Status:** Directory structure ready, logging not implemented
**Files:** `internal/session.go`, new file `internal/logger.go`
**Estimated Time:** 3-4 hours

**Tasks:**
- [ ] Create Logger struct with file handle and buffering
- [ ] Hook into Handler's I/O streams
- [ ] Log all stdin/stdout/stderr with timestamps
- [ ] Implement log rotation (size-based or time-based)
- [ ] Add replay capability (optional enhancement)
- [ ] Add command to enable/disable logging per session

**Implementation Notes:**
```go
type SessionLogger struct {
    logFile    *os.File
    session    *SessionInfo
    enabled    bool
    bufferSize int
}

func (l *SessionLogger) LogInput(data []byte) {
    timestamp := time.Now().Format("15:04:05")
    l.logFile.WriteString(fmt.Sprintf("[%s] IN:  %s\n", timestamp, data))
}
```

**Log Format:**
```
[15:04:05] IN:  ls -la
[15:04:06] OUT: total 48
[15:04:06] OUT: drwxr-xr-x  5 user user 4096 Oct 16 15:04 .
[15:04:06] OUT: drwxr-xr-x 12 user user 4096 Oct 16 14:30 ..
```

---

## Medium Priority

### 3. Additional Linux Modules
**Status:** Planning phase
**Files:** `internal/modules.go`
**Estimated Time:** 1-2 hours each

**Suggested Modules:**

#### 3.1. LinEnum Module
- **URL:** `https://raw.githubusercontent.com/rebootuser/LinEnum/master/LinEnum.sh`
- **Description:** Comprehensive Linux enumeration script
- **Category:** Linux
- **Implementation:** Same as LSE (RunScript with URL)

#### 3.2. Unix-Privesc-Check Module
- **URL:** `https://raw.githubusercontent.com/pentestmonkey/unix-privesc-check/master/unix-privesc-check`
- **Description:** Shell script to check for privilege escalation vectors
- **Category:** Linux
- **Implementation:** RunScript with executable permissions

#### 3.3. LinuxExploitSuggester Module
- **URL:** `https://raw.githubusercontent.com/The-Z-Labs/linux-exploit-suggester/master/linux-exploit-suggester.sh`
- **Description:** Suggests kernel exploits based on version
- **Category:** Linux
- **Implementation:** RunScript (was removed, consider re-adding)

#### 3.4. RustScan Module
- **Description:** Fast port scanner (requires uploading binary)
- **URL:** `https://github.com/RustScan/RustScan/releases/download/2.1.1/rustscan_2.1.1_amd64.deb`
- **Category:** Linux
- **Implementation:** More complex, requires binary extraction from .deb
- **Priority:** Low (can use native nmap/nc instead)

#### 3.5. Chisel Module (Pivoting)
- **URL:** `https://github.com/jpillora/chisel/releases/latest/download/chisel_1.9.1_linux_amd64.gz`
- **Description:** Fast TCP/UDP tunnel over HTTP
- **Category:** Linux
- **Implementation:** Download, gunzip, chmod +x, run in background
- **Priority:** Medium (very useful for pivoting)

#### 3.6. Static Binaries Module (Bulk Upload)
- **URLs:**
  - `https://raw.githubusercontent.com/andrew-d/static-binaries/master/binaries/linux/x86_64/socat`
  - `https://raw.githubusercontent.com/andrew-d/static-binaries/master/binaries/linux/x86_64/ncat`
  - `https://github.com/ernw/static-toolbox/releases/download/1.04/nmap-7.91SVN-x86_64-portable.tar.gz`
- **Description:** Upload useful static binaries for limited environments
- **Category:** Misc
- **Implementation:** Similar to privesc module (bulk upload)

---

### 4. SIGWINCH Handler (Terminal Resize)
**Status:** Not implemented
**Files:** `internal/pty.go` or `internal/shell.go`
**Estimated Time:** 2 hours
**Priority:** Medium (nice-to-have)

**Tasks:**
- [ ] Capture SIGWINCH signal in interactive shell mode
- [ ] Get current terminal size on resize event
- [ ] Send `stty rows X cols Y` command to victim
- [ ] Test with tmux/screen environments

**Implementation Notes:**
```go
func (h *Handler) setupResizeHandler() {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGWINCH)

    go func() {
        for range sigChan {
            width, height := getTerminalSize()
            cmd := fmt.Sprintf("stty rows %d cols %d\n", height, width)
            h.conn.Write([]byte(cmd))
        }
    }()
}
```

---

### 5. Module Output Improvements
**Status:** Working but could be enhanced
**Files:** `internal/session.go`, `internal/shell.go`
**Estimated Time:** 2-3 hours

**Enhancement Ideas:**
- [ ] Add color-coding to module output (errors in red, success in green)
- [ ] Implement output filtering (hide certain patterns)
- [ ] Add search functionality in output files
- [ ] Create module output viewer/browser in gummy menu
- [ ] Add module execution history (`run history` command)

---

## Low Priority

### 6. Auto-Reconnect Capability
**Status:** Not started
**Files:** `internal/listener.go`, `internal/session.go`
**Estimated Time:** 4-5 hours

**Tasks:**
- [ ] Store session state (whoami, platform, last commands)
- [ ] Detect disconnection gracefully
- [ ] Spawn reverse shell automatically on reconnect
- [ ] Restore session context (working directory, environment)
- [ ] Notify user of reconnection success/failure

**Challenges:**
- Requires storing session state persistently
- May need payload persistence on victim
- Complex implementation for reliable reconnection

---

### 7. Port Forwarding
**Status:** Not started (NOT PRIORITY - use ligolo instead)
**Files:** New package `internal/portfwd/`
**Estimated Time:** 8-10 hours

**Note:** Consider using [ligolo-ng](https://github.com/nicocha30/ligolo-ng) for pivoting instead. It's a specialized tool that does this much better.

**If implementing:**
- [ ] Local port forwarding (listen locally, forward through victim)
- [ ] Remote port forwarding (listen on victim, forward to local)
- [ ] Dynamic port allocation
- [ ] Multiple concurrent forwards
- [ ] Integration with existing session management

---

## Module URL Reference

All URLs are stored in `internal/modules.go` for easy updates:

### Linux
```go
URL_LINPEAS = "https://github.com/peass-ng/PEASS-ng/releases/latest/download/linpeas.sh"
URL_LSE     = "https://github.com/chsoares/linux-smart-enumeration/raw/refs/heads/master/lse.sh"
URL_DEEPCE  = "https://raw.githubusercontent.com/stealthcopter/deepce/refs/heads/main/deepce.sh"
URL_PSPY64  = "https://github.com/DominicBreuker/pspy/releases/download/v1.2.1/pspy64"
URL_PSPY32  = "https://github.com/DominicBreuker/pspy/releases/download/v1.2.1/pspy32"
```

### Windows
```go
URL_WINPEAS      = "https://github.com/peass-ng/PEASS-ng/releases/latest/download/winPEASany.exe"
URL_POWERUP      = "https://raw.githubusercontent.com/PowerShellEmpire/PowerTools/master/PowerUp/PowerUp.ps1"
URL_PRIVESCCHECK = "https://raw.githubusercontent.com/itm4n/PrivescCheck/refs/heads/master/PrivescCheck.ps1"
URL_LAZAGNE      = "https://github.com/AlessandroZ/LaZagne/releases/latest/download/LaZagne.exe"
URL_SHARPUP      = "https://github.com/r3motecontrol/Ghostpack-CompiledBinaries/blob/master/SharpUp.exe"
URL_POWERVIEW    = "https://github.com/PowerShellMafia/PowerSploit/raw/refs/heads/master/Recon/PowerView.ps1"
```

---

## Testing Checklist

### Module System Testing
- [x] peas module (LinPEAS) - Tested on Linux
- [x] lse module (Linux Smart Enumeration) - Tested on Linux
- [x] pspy module (Process monitoring) - Tested on Linux
- [x] privesc module (Bulk upload) - Tested on Linux
- [x] sh module (Custom scripts) - Tested on Linux
- [ ] Windows modules - Requires Windows target
- [ ] Module execution with various argument combinations
- [ ] Module timeout behavior (pspy 5min timeout)
- [ ] Module output in separate terminal
- [ ] Module cleanup (shred files after execution)

### Platform Detection Testing
- [x] Linux detection (uname -s)
- [x] Platform stored in SessionInfo
- [ ] Windows detection via privesc module
- [ ] macOS detection (if applicable)

### Error Handling Testing
- [x] Invalid module names
- [x] Missing required arguments
- [x] Network errors during download
- [x] Upload failures
- [ ] Execution errors (permission denied, etc.)
- [ ] Timeout handling for long-running modules

---

## Future Enhancements (Ideas)

### 1. Module Marketplace / Plugin System
- Allow users to add custom modules via config file
- Module metadata (author, version, description)
- Module dependency management
- Update check for modules

### 2. Web Dashboard
- Web interface for session management
- Real-time output streaming via WebSockets
- Module execution history visualization
- File browser for session directories

### 3. Persistence Mechanisms
- Automatic persistence setup on victim
- Cron jobs, systemd services, registry keys
- Multiple persistence methods per platform
- Automatic cleanup on demand

### 4. Post-Exploitation Framework
- Password dumping modules (mimikatz, lazagne)
- Lateral movement helpers
- Domain enumeration (BloodHound, PowerView)
- Credential harvesting

### 5. AI-Assisted Analysis
- ChatGPT integration for output analysis (like Penelope's -a flag)
- Automatic vulnerability identification
- Suggested next steps based on enumeration results
- Natural language queries ("find SUID binaries")

---

## Notes

- All modules follow the same interface: `Run(session *SessionInfo, args []string) error`
- Modules are registered in `GetModuleRegistry()` function
- Category order is explicit: Linux, Windows, Misc, Custom
- Output always goes to `~/.gummy/YYYY_MM_DD/IP_user_hostname/scripts/`
- Module execution is non-blocking (runs in goroutine)
- Timeouts prevent zombie processes (5min default for binaries)
- Security: All temp files are cleaned up with `shred` (falls back to `rm`)

---

## Questions to Consider

1. **Module versioning:** Should we track module script versions?
2. **Module caching:** Cache downloaded scripts locally to avoid re-downloading?
3. **Module execution queue:** Allow queuing multiple modules?
4. **Module dependencies:** Handle modules that depend on others?
5. **Module output formats:** JSON output for programmatic parsing?
6. **Module permissions:** Restrict certain modules to certain users?

---

## Contribution Guidelines

If adding new modules:
1. Add URL constant in `internal/modules.go` (lines 94-108)
2. Create module struct implementing Module interface
3. Register in `GetModuleRegistry()` (lines 26-34)
4. Test thoroughly with various targets
5. Update CLAUDE.md and this TODO.md
6. Commit with descriptive message

**Module Template:**
```go
type NewModule struct{}

func (m *NewModule) Name() string        { return "modname" }
func (m *NewModule) Category() string    { return "Linux" } // or Windows, Misc, Custom
func (m *NewModule) Description() string { return "Short description" }

func (m *NewModule) Run(session *SessionInfo, args []string) error {
    // For shell scripts:
    return session.RunScript(URL_SCRIPT, args)

    // For binaries:
    return session.RunBinary(URL_BINARY, args)
}
```
