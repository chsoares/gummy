package transfer

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chsoares/gummy/internal/ui"
)

// Transferer handles file upload/download operations
type Transferer struct {
	conn      net.Conn
	sessionID string
}

// Config holds transfer configuration
type Config struct {
	ChunkSize int // Size of each chunk in bytes
	Timeout   time.Duration
}

// DefaultConfig returns default transfer configuration
func DefaultConfig() Config {
	return Config{
		ChunkSize: 4096, // 4KB chunks
		Timeout:   30 * time.Second,
	}
}

// New creates a new Transferer instance
func New(conn net.Conn, sessionID string) *Transferer {
	return &Transferer{
		conn:      conn,
		sessionID: sessionID,
	}
}

// Upload sends a local file to the remote system
// localPath: path to local file
// remotePath: destination path on remote system
func (t *Transferer) Upload(localPath, remotePath string) error {
	// Read local file
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	fileSize := len(data)
	fmt.Println(ui.Uploading(fmt.Sprintf("Uploading %s (%s)...", filepath.Base(localPath), formatSize(fileSize))))

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Calculate MD5 checksum for verification
	hash := md5.Sum(data)
	checksum := hex.EncodeToString(hash[:])

	// Send file in chunks
	config := DefaultConfig()
	chunks := splitIntoChunks(encoded, config.ChunkSize)
	totalChunks := len(chunks)

	// Create remote file and prepare for writing
	setupCommands := []string{
		fmt.Sprintf("rm -f %s.b64 2>/dev/null", remotePath), // Clean any previous temp file
		fmt.Sprintf("touch %s.b64", remotePath),              // Create temp base64 file
	}

	for _, cmd := range setupCommands {
		if err := t.sendCommand(cmd); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
	}

	// Send chunks with progress
	for i, chunk := range chunks {
		// Append chunk to remote file
		cmd := fmt.Sprintf("echo '%s' >> %s.b64", chunk, remotePath)
		if err := t.sendCommand(cmd); err != nil {
			return fmt.Errorf("failed to send chunk %d: %w", i, err)
		}

		// Show progress
		t.showProgress(i+1, totalChunks, fileSize)
	}

	fmt.Println() // New line after progress bar

	// Decode base64 and save final file
	decodeCmd := fmt.Sprintf("base64 -d %s.b64 > %s && rm %s.b64", remotePath, remotePath, remotePath)
	if err := t.sendCommand(decodeCmd); err != nil {
		return fmt.Errorf("failed to decode file: %w", err)
	}

	// Verify checksum (optional - best effort)
	remoteChecksum, err := t.getRemoteMD5(remotePath)
	if err == nil && remoteChecksum == checksum {
		fmt.Println(ui.TransferComplete(fmt.Sprintf("Upload complete! (MD5: %s)", checksum[:8])))
	} else {
		fmt.Println(ui.TransferComplete("Upload complete!"))
	}

	return nil
}

// Download retrieves a file from the remote system
// remotePath: path to remote file
// localPath: destination path on local system (if empty, saves to current directory)
func (t *Transferer) Download(remotePath, localPath string) error {
	// If localPath is empty, save to current directory with same filename
	if localPath == "" {
		localPath = filepath.Base(remotePath)
	}

	fmt.Println(ui.Downloading(fmt.Sprintf("Downloading %s...", filepath.Base(remotePath))))

	// Drain leftover data from previous shell interactions
	t.drainConnection()

	// Use unique markers
	marker := "GUMMY_B64_START"
	endMarker := "GUMMY_B64_END"

	// Send command with markers
	cmd := fmt.Sprintf("echo %s; base64 -w 0 %s 2>/dev/null; echo; echo %s", marker, remotePath, endMarker)
	t.conn.Write([]byte(cmd + "\n"))

	time.Sleep(500 * time.Millisecond)

	// Read output until end marker
	var output strings.Builder
	buffer := make([]byte, 8192)
	t.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	foundEnd := false
	for !foundEnd {
		n, err := t.conn.Read(buffer)
		if err != nil {
			if strings.Contains(output.String(), endMarker) {
				break
			}
			return fmt.Errorf("timeout reading file")
		}

		if n > 0 {
			chunk := string(buffer[:n])
			output.WriteString(chunk)

			if strings.Contains(chunk, endMarker) {
				foundEnd = true
				break
			}
		}
	}

	t.conn.SetReadDeadline(time.Time{})

	fullOutput := output.String()

	// Find markers (use LastIndex to skip command echo)
	startIdx := strings.LastIndex(fullOutput, marker)
	if startIdx == -1 {
		return fmt.Errorf("file not found: %s", remotePath)
	}

	endIdx := strings.Index(fullOutput[startIdx:], endMarker)
	if endIdx == -1 {
		return fmt.Errorf("incomplete download")
	}
	endIdx += startIdx

	// Extract base64 content
	content := fullOutput[startIdx+len(marker):endIdx]

	// Clean and join base64 lines
	lines := strings.Split(content, "\n")
	var base64Lines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 0 {
			base64Lines = append(base64Lines, line)
		}
	}

	if len(base64Lines) == 0 {
		return fmt.Errorf("file is empty: %s", remotePath)
	}

	base64Data := strings.Join(base64Lines, "")

	// Decode
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("failed to decode: %w", err)
	}

	// Save
	if err := os.WriteFile(localPath, decoded, 0644); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	// Checksum
	hash := md5.Sum(decoded)
	checksum := hex.EncodeToString(hash[:])

	fmt.Println(ui.TransferComplete(fmt.Sprintf("Download complete! Saved to: %s (%s, MD5: %s)",
		localPath, formatSize(len(decoded)), checksum[:8])))

	t.drainConnection()
	return nil
}

// sendCommand sends a command to remote shell
func (t *Transferer) sendCommand(cmd string) error {
	_, err := t.conn.Write([]byte(cmd + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}
	time.Sleep(50 * time.Millisecond) // Small delay for command processing
	return nil
}

// getRemoteMD5 gets MD5 checksum of remote file
func (t *Transferer) getRemoteMD5(remotePath string) (string, error) {
	// Try md5sum command
	cmd := fmt.Sprintf("md5sum %s 2>/dev/null | awk '{print $1}'", remotePath)
	t.conn.Write([]byte(cmd + "\n"))
	time.Sleep(200 * time.Millisecond)

	// Read response (best effort)
	buffer := make([]byte, 1024)
	t.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := t.conn.Read(buffer)
	t.conn.SetReadDeadline(time.Time{})

	if err != nil {
		return "", err
	}

	response := strings.TrimSpace(string(buffer[:n]))
	lines := strings.Split(response, "\n")

	// Find line with MD5 hash (32 hex chars)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 32 && isHex(line) {
			return line, nil
		}
	}

	return "", fmt.Errorf("could not extract MD5")
}

// drainConnection drains any pending data from connection
// This is CRITICAL before file transfer to remove leftover shell output
func (t *Transferer) drainConnection() {
	buffer := make([]byte, 4096)
	t.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))

	for {
		_, err := t.conn.Read(buffer)
		if err != nil {
			break // Timeout means buffer is clean
		}
	}

	t.conn.SetReadDeadline(time.Time{})
}

// isBase64Like checks if a string looks like base64 data
func isBase64Like(s string) bool {
	if len(s) < 10 {
		return false
	}
	// Base64 only contains: A-Z, a-z, 0-9, +, /, =
	for _, ch := range s {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '+' || ch == '/' || ch == '=') {
			return false
		}
	}
	return true
}

// showProgress displays upload progress bar
func (t *Transferer) showProgress(current, total, fileSize int) {
	percentage := float64(current) / float64(total) * 100
	barWidth := 40
	filled := int(percentage / 100 * float64(barWidth))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r [%s] %3.0f%% (%d/%d chunks)", bar, percentage, current, total)
}

// splitIntoChunks splits a string into chunks of specified size
func splitIntoChunks(s string, chunkSize int) []string {
	var chunks []string
	for i := 0; i < len(s); i += chunkSize {
		end := i + chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// formatSize formats bytes into human-readable size
func formatSize(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// isHex checks if string is valid hexadecimal
func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// DrainOutput drains any pending output from connection
func (t *Transferer) DrainOutput() {
	buffer := make([]byte, 4096)
	t.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

	for {
		_, err := t.conn.Read(buffer)
		if err != nil {
			break
		}
	}

	t.conn.SetReadDeadline(time.Time{})
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
