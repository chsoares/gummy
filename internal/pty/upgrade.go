package pty

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PTYUpgrader gerencia upgrade de shells raw para PTY
type PTYUpgrader struct {
	conn      net.Conn
	sessionID string
}

// NewPTYUpgrader cria um novo upgrader de PTY
func NewPTYUpgrader(conn net.Conn, sessionID string) *PTYUpgrader {
	return &PTYUpgrader{
		conn:      conn,
		sessionID: sessionID,
	}
}

// TryUpgrade tenta fazer upgrade da shell para PTY
func (p *PTYUpgrader) TryUpgrade() error {
	// Detecta shell type primeiro (silencioso)
	shellType, err := p.detectShell()
	if err != nil {
		return fmt.Errorf("failed to detect shell: %w", err)
	}

	// Tenta upgrade baseado no shell type
	switch shellType {
	case "bash", "sh", "dash":
		return p.upgradeBashShell()
	case "python", "python3":
		return p.upgradePythonShell()
	default:
		return p.upgradeGenericShell()
	}
}

// detectShell tenta detectar o tipo de shell
func (p *PTYUpgrader) detectShell() (string, error) {
	// Envia comando para detectar shell
	commands := []string{
		"echo $0",           // Detecta shell atual
		"which python3",     // Verifica python3
		"which python",      // Verifica python
		"ps -p $$",          // Mostra processo atual
	}

	for _, cmd := range commands {
		p.conn.Write([]byte(cmd + "\n"))
		time.Sleep(100 * time.Millisecond)
	}

	// Lê resposta
	buffer := make([]byte, 4096)
	p.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := p.conn.Read(buffer)
	if err != nil {
		return "unknown", err
	}
	p.conn.SetReadDeadline(time.Time{})

	response := strings.ToLower(string(buffer[:n]))

	// Analisa resposta para detectar shell
	if strings.Contains(response, "python3") {
		return "python3", nil
	}
	if strings.Contains(response, "python") {
		return "python", nil
	}
	if strings.Contains(response, "bash") {
		return "bash", nil
	}
	if strings.Contains(response, "/sh") {
		return "sh", nil
	}

	return "bash", nil // Default fallback
}

// upgradePythonShell faz upgrade usando Python PTY
func (p *PTYUpgrader) upgradePythonShell() error {
	// Comandos PTY upgrade com Python (silenciosos)
	pythonCommands := []string{
		// Primeiro, tenta python3
		"python3 -c \"import pty; pty.spawn('/bin/bash')\"",
		// Fallback para python2
		"python -c \"import pty; pty.spawn('/bin/bash')\"",
	}

	for _, cmd := range pythonCommands {
		p.conn.Write([]byte(cmd + "\r\n"))
		time.Sleep(200 * time.Millisecond)

		// Testa se PTY está funcionando
		if p.testPTY() {
			return p.completePTYSetup()
		}
	}

	return fmt.Errorf("python PTY upgrade failed")
}

// upgradeBashShell faz upgrade usando recursos nativos do bash
func (p *PTYUpgrader) upgradeBashShell() error {
	// Comandos para criar script PTY upgrade (silenciosos)
	scriptCommands := []string{
		// Cria script temporário
		"echo '#!/bin/bash' > /tmp/pty_upgrade.sh",
		"echo 'script -qc /bin/bash /dev/null' >> /tmp/pty_upgrade.sh",
		"chmod +x /tmp/pty_upgrade.sh",
		"/tmp/pty_upgrade.sh",
	}

	for _, cmd := range scriptCommands {
		p.conn.Write([]byte(cmd + "\r\n"))
		time.Sleep(150 * time.Millisecond)
	}

	// Testa se funcionou
	if p.testPTY() {
		return p.completePTYSetup()
	}

	return fmt.Errorf("bash script PTY upgrade failed")
}

// upgradeGenericShell tenta métodos genéricos de upgrade
func (p *PTYUpgrader) upgradeGenericShell() error {
	// Lista de comandos PTY upgrade alternativos (silenciosos)
	genericCommands := []string{
		// Script command (disponível na maioria dos sistemas)
		"script -qc /bin/bash /dev/null",
		// Socat (se disponível)
		"socat - EXEC:'/bin/bash',pty,stderr,setsid,sigint,sane",
		// Busybox (em sistemas embarcados)
		"busybox script -qc /bin/bash /dev/null",
	}

	for _, cmd := range genericCommands {
		p.conn.Write([]byte(cmd + "\r\n"))
		time.Sleep(200 * time.Millisecond)

		if p.testPTY() {
			return p.completePTYSetup()
		}
	}

	return fmt.Errorf("generic PTY upgrade failed - raw shell will be used")
}

// testPTY testa se PTY upgrade foi bem-sucedido
func (p *PTYUpgrader) testPTY() bool {
	// Envia comando de teste
	testCmd := "stty -echo; echo PTY_TEST_OK; stty echo\n"
	p.conn.Write([]byte(testCmd))

	// Aguarda resposta
	buffer := make([]byte, 1024)
	p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := p.conn.Read(buffer)
	p.conn.SetReadDeadline(time.Time{})

	if err != nil {
		return false
	}

	response := string(buffer[:n])
	return strings.Contains(response, "PTY_TEST_OK")
}

// completePTYSetup completa configuração PTY
func (p *PTYUpgrader) completePTYSetup() error {
	// Obter dimensões do terminal local
	width, height := p.getTerminalSize()

	// Limpar output anterior enviando vários enters
	p.conn.Write([]byte("\n\n\n"))
	time.Sleep(100 * time.Millisecond)

	// Configurar PTY na shell remota (silenciosamente)
	setupCommands := []string{
		fmt.Sprintf("stty rows %d cols %d", height, width),
		"export TERM=xterm-256color",
		"export SHELL=/bin/bash",
		"stty echo", // IMPORTANTE: Manter echo habilitado para shells interativas
		"clear",     // Limpar tela
	}

	for _, cmd := range setupCommands {
		p.conn.Write([]byte(cmd + "\r\n"))
		time.Sleep(30 * time.Millisecond)
	}

	// Aguarda comandos terminarem
	time.Sleep(200 * time.Millisecond)

	return nil
}

// getTerminalSize obtém dimensões do terminal local
func (p *PTYUpgrader) getTerminalSize() (int, int) {
	// Usa stty para obter dimensões do terminal local
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return 80, 24 // Default fallback
	}

	var height, width int
	fmt.Sscanf(string(output), "%d %d", &height, &width)

	if width == 0 || height == 0 {
		return 80, 24 // Default fallback
	}

	return width, height
}

// SetupResizeHandler configura handler para redimensionamento
func (p *PTYUpgrader) SetupResizeHandler() {
	// TODO: Implementar SIGWINCH handler para redimensionamento automático
	// Por enquanto, dimensões são fixas no upgrade
}