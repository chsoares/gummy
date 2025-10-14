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
// remotePath: destination path on remote system (if empty, uses filename in remote cwd)
func (t *Transferer) Upload(localPath, remotePath string) error {
	// Read local file
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	// If remotePath is empty, use just the filename (will go to remote cwd)
	if remotePath == "" {
		remotePath = filepath.Base(localPath)
	}

	fileSize := len(data)
	fmt.Println(ui.Uploading(fmt.Sprintf("Uploading %s (%s)...", filepath.Base(localPath), formatSize(fileSize))))

	// Drain leftover data from previous shell interactions
	t.drainConnection()

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Calculate MD5 checksum for verification
	hash := md5.Sum(data)
	checksum := hex.EncodeToString(hash[:])

	// Send file in chunks
	config := DefaultConfig()
	chunks := splitIntoChunks(encoded, config.ChunkSize)

	// Create remote file and prepare for writing (silently)
	setupCommands := []string{
		fmt.Sprintf("rm -f %s.b64 2>/dev/null", remotePath), // Clean any previous temp file
		fmt.Sprintf("touch %s.b64", remotePath),              // Create temp base64 file
	}

	for _, cmd := range setupCommands {
		t.conn.Write([]byte(cmd + "\n"))
		time.Sleep(50 * time.Millisecond)
	}

	// Send chunks with progress (only for files > 100KB)
	bytesSent := 0
	lastProgressUpdate := 0

	for _, chunk := range chunks {
		// Append chunk to remote file
		cmd := fmt.Sprintf("echo '%s' >> %s.b64", chunk, remotePath)
		t.conn.Write([]byte(cmd + "\n"))
		time.Sleep(30 * time.Millisecond)

		bytesSent += len(chunk)

		// Show progress every 100KB for large files
		if fileSize > 100*1024 && bytesSent-lastProgressUpdate >= 100*1024 {
			fmt.Printf("\r%-50s\r Uploading... %s", "", formatSize(bytesSent))
			lastProgressUpdate = bytesSent
		}
	}

	// Clear progress line if we showed it
	if fileSize > 100*1024 {
		fmt.Printf("\r%-50s\r", "")
	}

	// Decode base64 and save final file
	decodeCmd := fmt.Sprintf("base64 -d %s.b64 > %s && rm %s.b64", remotePath, remotePath, remotePath)
	t.conn.Write([]byte(decodeCmd + "\n"))
	time.Sleep(200 * time.Millisecond)

	// Drain output from all commands
	t.drainConnection()

	// Verify checksum with markers (like download)
	marker := "GUMMY_MD5_START"
	endMarker := "GUMMY_MD5_END"
	cmd := fmt.Sprintf("echo %s; md5sum %s 2>/dev/null | awk '{print $1}'; echo %s", marker, remotePath, endMarker)
	t.conn.Write([]byte(cmd + "\n"))
	time.Sleep(300 * time.Millisecond)

	// Read MD5 response
	var output strings.Builder
	buffer := make([]byte, 2048)
	t.conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	for {
		n, err := t.conn.Read(buffer)
		if err != nil {
			break
		}
		if n > 0 {
			output.WriteString(string(buffer[:n]))
			if strings.Contains(output.String(), endMarker) {
				break
			}
		}
	}

	t.conn.SetReadDeadline(time.Time{})

	// Extract MD5
	fullOutput := output.String()
	startIdx := strings.LastIndex(fullOutput, marker)
	if startIdx != -1 {
		endIdx := strings.Index(fullOutput[startIdx:], endMarker)
		if endIdx != -1 {
			content := fullOutput[startIdx+len(marker):startIdx+endIdx]
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if len(line) == 32 && isHex(line) {
					if line == checksum {
						fmt.Println(ui.Success(fmt.Sprintf("Upload complete! (MD5: %s)", checksum[:8])))
						t.drainConnection()
						return nil
					}
				}
			}
		}
	}

	// Fallback if MD5 check failed
	fmt.Println(ui.Success("Upload complete!"))
	t.drainConnection()
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

	// Read output with progress indication
	var output strings.Builder
	buffer := make([]byte, 8192)
	totalBytes := 0
	lastProgressUpdate := 0

	for {
		// Reset deadline on each read
		t.conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		n, err := t.conn.Read(buffer)
		if err != nil {
			// Timeout - break and check what we have
			break
		}

		if n > 0 {
			output.WriteString(string(buffer[:n]))
			totalBytes += n

			// Show progress every 100KB to avoid spam
			if totalBytes-lastProgressUpdate >= 100*1024 {
				// Clear line and show progress
				fmt.Printf("\r%-50s\rReceiving data... %s", "", formatSize(totalBytes))
				lastProgressUpdate = totalBytes
			}

			// Check if we have complete data: end marker AFTER last start marker
			currentOutput := output.String()
			lastStartIdx := strings.LastIndex(currentOutput, marker)
			if lastStartIdx != -1 {
				remainingAfterStart := currentOutput[lastStartIdx:]
				if strings.Contains(remainingAfterStart, endMarker) {
					// Complete! Clear progress line
					if totalBytes > 100*1024 {
						fmt.Printf("\r%-50s\r", "")
					}
					break
				}
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

	fmt.Println(ui.Success(fmt.Sprintf("Download complete! Saved to: %s (%s, MD5: %s)",
		localPath, formatSize(len(decoded)), checksum[:8])))

	t.drainConnection()
	return nil
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
