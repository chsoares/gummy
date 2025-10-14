package session

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chsoares/gummy/internal/shell"
	"golang.org/x/term"
)

// Manager gerencia múltiplas sessões de reverse shell
type Manager struct {
	sessions    map[string]*SessionInfo // Mapa de sessões ativas
	mu          sync.RWMutex            // Proteção concorrente
	nextID      int                     // Próximo ID numérico
	activeConn  net.Conn                // Conexão atualmente ativa (se houver)
	menuActive  bool                    // Se estamos no menu principal
}

// SessionInfo contém informações sobre uma sessão
type SessionInfo struct {
	ID       string    // ID único da sessão (hex)
	NumID    int       // ID numérico para facilitar uso
	Conn     net.Conn  // Conexão TCP
	RemoteIP string    // IP da vítima
	Handler  *shell.Handler // Shell handler
	Active   bool      // Se está sendo usada atualmente
}

// NewManager cria um novo gerenciador de sessões
func NewManager() *Manager {
	return &Manager{
		sessions:   make(map[string]*SessionInfo),
		nextID:     1,
		menuActive: true,
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
		Handler:  handler,
		Active:   false,
	}

	m.sessions[id] = session
	m.nextID++

	if m.menuActive {
		// Se estivermos no menu, quebrar a linha atual, mostrar notificação e novo prompt
		fmt.Printf("\rSession %d opened (%s -> %s)\ngummy> ", session.NumID, remoteIP, conn.LocalAddr().String())
	} else {
		// Se não estivermos no menu, só mostrar a notificação
		fmt.Printf("Session %d opened (%s -> %s)\n", session.NumID, remoteIP, conn.LocalAddr().String())
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

	fmt.Printf("Session %d closed\n", session.NumID)
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
		fmt.Println("No active sessions.")
		return
	}

	fmt.Println("\nActive Sessions:")
	fmt.Println("ID   Remote Address")
	fmt.Println("--   --------------")

	// Ordenar por NumID para exibição consistente
	var sessions []*SessionInfo
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].NumID < sessions[j].NumID
	})

	for _, session := range sessions {
		marker := " "
		if session.Active {
			marker = "*"
		}
		fmt.Printf("%s%-3d %s\n", marker, session.NumID, session.RemoteIP)
	}
	fmt.Println()
}

// UseSession ativa uma sessão específica
func (m *Manager) UseSession(numID int) error {
	m.mu.Lock()

	var targetSession *SessionInfo
	for _, session := range m.sessions {
		if session.NumID == numID {
			targetSession = session
			break
		}
	}

	if targetSession == nil {
		m.mu.Unlock()
		return fmt.Errorf("session %d not found", numID)
	}

	// Desativa sessão anterior
	for _, session := range m.sessions {
		session.Active = false
	}

	targetSession.Active = true
	m.activeConn = targetSession.Conn
	m.menuActive = false

	m.mu.Unlock()

	fmt.Printf("Using session %d (%s)\n", targetSession.NumID, targetSession.RemoteIP)
	fmt.Println("Press F12 to return to menu")

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
		fmt.Printf("\n") // Nova linha para separar
		m.showMenu()
	} else {
		fmt.Println("No active sessions.")
	}

	return err
}

// StartMenu inicia o loop do menu principal
func (m *Manager) StartMenu() {
	fmt.Println("Gummy Multi-Session Handler")
	fmt.Println("Type 'help' for available commands")

	scanner := bufio.NewScanner(os.Stdin)

	for {
		// Só mostra prompt e lê se estivermos no menu
		if m.menuActive {
			fmt.Print("gummy> ")

			if !scanner.Scan() {
				break
			}

			command := strings.TrimSpace(scanner.Text())
			if command == "" {
				continue
			}

			m.handleCommand(command)
		}
		// Se não estivermos no menu, aguarda até voltar
		// Isso será modificado quando UseSession retornar
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
			fmt.Println("Usage: use <session_id>")
			return
		}
		numID, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Printf("Invalid session ID: %s\n", parts[1])
			return
		}
		m.UseSession(numID)
	case "exit", "quit", "q":
		fmt.Println("Goodbye!")
		os.Exit(0)
	case "clear", "cls":
		fmt.Print("\033[2J\033[H")
	default:
		fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", parts[0])
	}
}

// showMenu mostra o menu principal com sessões ativas
func (m *Manager) showMenu() {
	fmt.Printf("\n--- Gummy Session Menu ---\n")
	m.ListSessions()
	fmt.Println("Commands: sessions, use <id>, help, exit")
}

// showHelp mostra ajuda dos comandos
func (m *Manager) showHelp() {
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  sessions, list, ls    - List active sessions")
	fmt.Println("  use <id>             - Use session with given ID")
	fmt.Println("  help, h              - Show this help")
	fmt.Println("  clear, cls           - Clear screen")
	fmt.Println("  exit, quit, q        - Exit Gummy")
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