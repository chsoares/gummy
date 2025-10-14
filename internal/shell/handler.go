package shell

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chsoares/gummy/internal/pty"
	"github.com/chsoares/gummy/internal/ui"
	"golang.org/x/term"
)

// Handler gerencia uma sessão de reverse shell
// A vítima já enviou uma shell conectada, nós fazemos relay do I/O
type Handler struct {
	conn         net.Conn // Conexão com a vítima (que já tem shell rodando)
	sessionID    string   // ID da sessão para logs
	originalTerm *term.State // Estado original do terminal para restaurar
	onClose      func(string) // Callback quando conexão fechar
}

// NewHandler cria um novo handler para reverse shell
// Diferente do anterior: não executamos comandos localmente,
// fazemos relay entre usuário local e shell remota da vítima
func NewHandler(conn net.Conn, sessionID string) *Handler {
	return &Handler{
		conn:         conn,
		sessionID:    sessionID,
		originalTerm: nil,
		onClose:      nil,
	}
}

// SetCloseCallback define callback para quando conexão fechar
func (h *Handler) SetCloseCallback(callback func(string)) {
	h.onClose = callback
}

// Start inicia o relay interativo entre usuário local e shell remota
// Esta é a função principal que conecta stdin/stdout local com a conexão remota
func (h *Handler) Start() error {
	// Configura terminal em modo raw para evitar echo duplo
	if err := h.setupRawMode(); err != nil {
		fmt.Printf("Warning: failed to setup raw mode: %v\n", err)
	}

	// Garante que o terminal será restaurado ao sair
	defer h.restoreTerminal()

	// Configura handler para Ctrl+C para restaurar terminal
	h.setupSignalHandler()

	// Testa se a conexão está realmente viva
	if !h.isConnectionAlive() {
		return fmt.Errorf("connection is dead")
	}

	// Configura timeout para detectar shells inativas
	h.conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Tenta detectar se é uma shell interativa lendo banner/prompt inicial
	initialBuffer := make([]byte, 4096)
	n, err := h.conn.Read(initialBuffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout normal para nc -e - não mostra nada
		} else {
			return fmt.Errorf("failed to read from remote shell: %w", err)
		}
	} else {
		// Mostra output inicial da vítima (prompt, banner, etc.)
		os.Stdout.Write(initialBuffer[:n])
	}

	// Remove timeout após conectar
	h.conn.SetReadDeadline(time.Time{})

	// Tenta upgrade PTY antes de iniciar I/O relay
	h.attemptPTYUpgrade()

	// Drain adicional para garantir shell limpa
	h.drainBeforeInteractive()

	// Inicia goroutines para relay bidirecional de I/O
	errorChan := make(chan error, 2)

	// Goroutine 1: Local stdin → Remote connection (nossos comandos → vítima)
	go h.relayLocalToRemote(errorChan)

	// Goroutine 2: Remote connection → Local stdout (output da vítima → nós)
	go h.relayRemoteToLocal(errorChan)

	// Aguarda até uma das goroutines terminar (erro ou EOF)
	err = <-errorChan

	// Notifica que conexão fechou se callback foi definido
	if h.onClose != nil && err != nil && err != io.EOF {
		h.onClose(h.sessionID)
	}

	return err
}

// relayLocalToRemote lê do stdin local e envia para a shell remota
// Usuário digita comando → enviado para vítima
func (h *Handler) relayLocalToRemote(errorChan chan error) {
	buffer := make([]byte, 4096)

	for {
		n, err := os.Stdin.Read(buffer)
		if err != nil {
			errorChan <- fmt.Errorf("local to remote relay error: %w", err)
			return
		}

		data := buffer[:n]

		// Intercepta F12 (\x1b[24~) para voltar ao menu
		if h.containsF12(data) {
			fmt.Print(ui.ReturningToMenu())
			errorChan <- io.EOF
			return
		}

		// Envia dados normalmente para shell remota
		_, writeErr := h.conn.Write(data)
		if writeErr != nil {
			errorChan <- fmt.Errorf("write to remote error: %w", writeErr)
			return
		}
	}
}

// containsF12 verifica se os dados contém a sequência F12
func (h *Handler) containsF12(data []byte) bool {
	f12Sequence := []byte{0x1b, '[', '2', '4', '~'} // ESC[24~

	// Busca pela sequência F12 nos dados
	for i := 0; i <= len(data)-len(f12Sequence); i++ {
		if i+len(f12Sequence) <= len(data) {
			match := true
			for j := 0; j < len(f12Sequence); j++ {
				if data[i+j] != f12Sequence[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}

	return false
}

// relayRemoteToLocal lê da shell remota e mostra no stdout local
// Output da vítima → mostrado para usuário
func (h *Handler) relayRemoteToLocal(errorChan chan error) {
	// Para melhorar output de raw shells, vamos processar byte a byte
	// e fazer algumas normalizações básicas
	buffer := make([]byte, 4096)
	for {
		n, err := h.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				errorChan <- io.EOF
			} else {
				errorChan <- fmt.Errorf("remote to local relay error: %w", err)
			}
			return
		}

		// Normaliza alguns caracteres problemáticos de raw shells
		output := h.normalizeOutput(buffer[:n])

		_, writeErr := os.Stdout.Write(output)
		if writeErr != nil {
			errorChan <- fmt.Errorf("write to stdout error: %w", writeErr)
			return
		}
	}
}

// normalizeOutput aplica normalizações básicas para melhorar output de raw shells
func (h *Handler) normalizeOutput(data []byte) []byte {
	// Para raw shells básicas, não fazemos muita normalização
	// porque pode quebrar aplicações que dependem de sequências específicas

	// Por enquanto, retorna dados como estão
	// Em futuras versões, podemos adicionar:
	// - Conversão \r\n para \n em algumas situações
	// - Limpeza de sequências de controle malformadas
	// - Etc.

	return data
}

// attemptPTYUpgrade tenta fazer upgrade da shell para PTY
func (h *Handler) attemptPTYUpgrade() {
	upgrader := pty.NewPTYUpgrader(h.conn, h.sessionID)

	err := upgrader.TryUpgrade()
	if err == nil {
		// PTY upgrade bem-sucedido - drenar output de setup
		h.drainSetupOutput()
	}
	// Completamente silencioso - sem output para o usuário
}

// drainSetupOutput drena output dos comandos de setup do PTY
func (h *Handler) drainSetupOutput() {
	// Aguarda um pouco para comandos terminarem
	time.Sleep(500 * time.Millisecond)

	// Drena qualquer output residual dos comandos de setup
	buffer := make([]byte, 4096)
	h.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

	for {
		_, err := h.conn.Read(buffer)
		if err != nil {
			break // Timeout ou fim dos dados
		}
	}

	h.conn.SetReadDeadline(time.Time{})
}

// drainBeforeInteractive drena qualquer comando residual antes da shell interativa
func (h *Handler) drainBeforeInteractive() {
	// Pausa para dar tempo para detecção de whoami terminar
	time.Sleep(1 * time.Second)

	// Envia enter para gerar novo prompt limpo
	h.conn.Write([]byte("\n"))

	// Aguarda o novo prompt aparecer
	time.Sleep(300 * time.Millisecond)

	// Drena apenas comandos antigos, preservando o prompt atual
	buffer := make([]byte, 1024)
	promptBuffer := ""

	h.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

	for {
		n, err := h.conn.Read(buffer)
		if err != nil || n == 0 {
			break
		}

		data := string(buffer[:n])
		promptBuffer += data

		// Se parece com um prompt no final, para de drenar
		lines := strings.Split(promptBuffer, "\n")
		if len(lines) > 0 {
			lastLine := strings.TrimSpace(lines[len(lines)-1])
			// Se termina com $ ou # ou >, provavelmente é um prompt
			if strings.HasSuffix(lastLine, "$") || strings.HasSuffix(lastLine, "#") || strings.HasSuffix(lastLine, ">") {
				// Mostra o prompt atual
				os.Stdout.Write([]byte(lastLine))
				break
			}
		}
	}

	h.conn.SetReadDeadline(time.Time{})
}

// isConnectionAlive testa se a conexão está realmente viva
func (h *Handler) isConnectionAlive() bool {
	// Tenta escrever um byte nulo (não visível) para testar a conexão
	h.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	_, err := h.conn.Write([]byte{})
	h.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return false
	}

	// Testa leitura com timeout muito curto
	h.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buffer := make([]byte, 1)
	_, err = h.conn.Read(buffer)
	h.conn.SetReadDeadline(time.Time{})

	// Se deu timeout, conexão está viva mas sem dados (normal)
	// Se deu EOF ou outro erro de conexão, está morta
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return true // Timeout é normal, conexão viva
		}
		return false // EOF ou erro real
	}

	return true
}

// GetSessionID retorna o ID da sessão
func (h *Handler) GetSessionID() string {
	return h.sessionID
}

// SendCommand envia um comando para a shell remota (útil para automação futura)
func (h *Handler) SendCommand(command string) error {
	_, err := h.conn.Write([]byte(command + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}
	return nil
}

// setupRawMode coloca o terminal local em modo raw
// Isso desabilita echo local e permite controle total da shell remota
func (h *Handler) setupRawMode() error {
	// Verifica se stdin é um terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("stdin is not a terminal")
	}

	// Salva estado original do terminal
	originalState, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to get terminal state: %w", err)
	}
	h.originalTerm = originalState

	// Coloca terminal em modo raw
	// Raw mode: sem echo, sem buffering, caracteres passam direto
	_, err = term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}

	return nil
}

// restoreTerminal restaura o terminal ao estado original
func (h *Handler) restoreTerminal() {
	if h.originalTerm != nil {
		term.Restore(int(os.Stdin.Fd()), h.originalTerm)
	}
}

// setupSignalHandler configura handler para restaurar terminal em caso de Ctrl+C
func (h *Handler) setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		h.restoreTerminal()
		fmt.Printf("\n")
		os.Exit(0)
	}()
}