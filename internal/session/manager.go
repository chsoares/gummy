package session

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

	"github.com/chsoares/gummy/internal/payloads"
	"github.com/chsoares/gummy/internal/shell"
	"github.com/chsoares/gummy/internal/transfer"
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
	ID       string         // ID único da sessão (hex)
	NumID    int            // ID numérico para facilitar uso
	Conn     net.Conn       // Conexão TCP
	RemoteIP string         // IP da vítima
	Whoami   string         // user@host da vítima
	Handler  *shell.Handler // Shell handler
	Active   bool           // Se está sendo usada atualmente
}

// GummyCompleter implements readline.AutoCompleter for smart path completion
type GummyCompleter struct {
	manager *Manager
}

// Do implements the AutoCompleter interface
func (c *GummyCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])
	trimmed := strings.TrimLeft(lineStr, " \t")

	commands := []string{"upload", "download", "list", "use", "shell", "kill", "help", "exit", "clear"}

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

	handler := shell.NewHandler(conn, id)

	// Configure callback para quando conexão fechar
	handler.SetCloseCallback(func(sessionID string) {
		m.RemoveSession(sessionID)
	})

	session := &SessionInfo{
		ID:       id,
		NumID:    m.nextID,
		Conn:     conn,
		RemoteIP: remoteIP,
		Whoami:   "detecting...",
		Handler:  handler,
		Active:   false,
	}

	m.sessions[id] = session
	m.nextID++

	// Detecta whoami em background
	go m.detectWhoami(session)

	// Inicia monitoramento da sessão
	go m.monitorSession(session)

	// Only print if not in silent mode
	if !m.silent {
		if m.menuActive {
			// Se estivermos no menu, quebrar a linha atual, mostrar notificação e novo prompt
			fmt.Printf("\r%s\n%s", ui.SessionOpened(session.NumID, remoteIP), ui.Prompt())
		} else {
			// Se não estivermos no menu, só mostrar a notificação
			fmt.Println(ui.SessionOpened(session.NumID, remoteIP))
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
			fmt.Println("No active sessions.")
		}
	}
}

// ListSessions mostra todas as sessões ativas
func (m *Manager) ListSessions() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.sessions) == 0 {
		fmt.Println(ui.Info("No active sessions."))
		return
	}

	// Collect all session lines
	var lines []string
	lines = append(lines, ui.TableHeader("id  remote address     whoami"))

	// Ordenar por NumID para exibição consistente
	var sessions []*SessionInfo
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].NumID < sessions[j].NumID
	})

	for _, session := range sessions {
		sessionLine := fmt.Sprintf("%-3d %-18s %s", session.NumID, session.RemoteIP, session.Whoami)
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

// detectWhoami detecta user@host da sessão em background
func (m *Manager) detectWhoami(session *SessionInfo) {
	// Aguarda um pouco para a shell se estabilizar
	time.Sleep(800 * time.Millisecond)

	// Comando mais direto e confiável
	whoamiCmd := "echo $(whoami)@$(hostname)\n"
	_, err := session.Conn.Write([]byte(whoamiCmd))
	if err != nil {
		session.Whoami = "unknown"
		return
	}

	// Define timeout maior para leitura
	session.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Lê toda a resposta em múltiplas tentativas
	allData := ""
	buffer := make([]byte, 1024)

	for i := 0; i < 10; i++ { // máximo 10 tentativas
		n, err := session.Conn.Read(buffer)
		if err != nil {
			if i > 0 { // Se já lemos algo, pode ter terminado
				break
			}
			session.Whoami = "unknown"
			return
		}

		allData += string(buffer[:n])

		// Se temos pelo menos uma linha completa, processa
		if strings.Contains(allData, "\n") {
			lines := strings.Split(allData, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				// Procura por user@host sem comandos
				if strings.Contains(line, "@") &&
					!strings.Contains(line, "echo") &&
					!strings.Contains(line, "whoami") &&
					!strings.Contains(line, "hostname") &&
					!strings.Contains(line, "$") {

					// Limpa a linha
					cleaned := strings.ReplaceAll(line, "\r", "")
					cleaned = strings.TrimSpace(cleaned)

					// Valida formato básico: palavra@palavra
					parts := strings.Split(cleaned, "@")
					if len(parts) == 2 && len(parts[0]) > 0 && len(parts[1]) > 0 && len(cleaned) < 50 {
						session.Whoami = cleaned

						// Drena o restante rapidamente
						go func() {
							time.Sleep(100 * time.Millisecond)
							drainBuffer := make([]byte, 2048)
							session.Conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
							for {
								n, err := session.Conn.Read(drainBuffer)
								if err != nil || n == 0 {
									break
								}
							}
							session.Conn.SetReadDeadline(time.Time{})
						}()

						return
					}
				}
			}
		}

		// Pequena pausa entre tentativas
		time.Sleep(100 * time.Millisecond)
	}

	// Remove timeout
	session.Conn.SetReadDeadline(time.Time{})
	session.Whoami = "unknown"
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

	fmt.Println(ui.Info("Entering interactive shell. Press F12 to return to menu"))

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
		fmt.Println(ui.Info("No active sessions."))
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
			fmt.Println(ui.Error("Usage: use <session_id>"))
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
			fmt.Println(ui.Error("Usage: kill <session_id>"))
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
			fmt.Println(ui.Error("Usage: upload <local_path> [remote_path]"))
			return
		}
		remotePath := ""
		if len(parts) >= 3 {
			remotePath = parts[2]
		}
		m.handleUpload(parts[1], remotePath)
	case "download":
		if len(parts) < 2 {
			fmt.Println(ui.Error("Usage: download <remote_path> [local_path]"))
			return
		}
		localPath := ""
		if len(parts) >= 3 {
			localPath = parts[2]
		}
		m.handleDownload(parts[1], localPath)
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
	lines = append(lines, ui.Command("ssh                          - Generate SSH connection //TODO"))
	lines = append(lines, ui.Command("winrm                        - Generate WinRM connection //TODO"))
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
	lines = append(lines, ui.Command("run <module>                 - Run a module //TODO"))
	lines = append(lines, ui.Command("spawn                        - Spawn new session //TODO"))
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
	t := transfer.New(m.selectedSession.Conn, m.selectedSession.ID)

	// Create context with cancel for ESC handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching for ESC key in background
	go transfer.WatchForCancel(ctx, cancel)

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
	t := transfer.New(m.selectedSession.Conn, m.selectedSession.ID)

	// Create context with cancel for ESC handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching for ESC key in background
	go transfer.WatchForCancel(ctx, cancel)

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
	gen := payloads.NewReverseShellGenerator(ip, port)

	// Bash payloads
	fmt.Println(ui.CommandHelp("Bash"))
	fmt.Println(gen.GenerateBash())
	fmt.Println(gen.GenerateBashBase64())
	// PowerShell payload
	fmt.Println(ui.CommandHelp("PowerShell"))
	fmt.Println(gen.GeneratePowerShell())
}
