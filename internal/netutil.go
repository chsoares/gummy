package internal

import (
	"fmt"
	"net"

	"github.com/chsoares/gummy/internal/ui"
)

// GetIPFromInterface resolves an IP address from a network interface name
// Returns the first non-loopback IPv4 address found on the interface
func GetIPFromInterface(ifaceName string) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", fmt.Errorf("interface '%s' not found: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("failed to get addresses for interface '%s': %w", ifaceName, err)
	}

	for _, addr := range addrs {
		// Check if it's an IP address (not IP network)
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// We want IPv4 addresses only
		ip := ipNet.IP.To4()
		if ip == nil {
			continue
		}

		// Skip loopback
		if ip.IsLoopback() {
			continue
		}

		return ip.String(), nil
	}

	return "", fmt.Errorf("no valid IPv4 address found on interface '%s'", ifaceName)
}

// IsValidIP checks if a string is a valid IP address
func IsValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// ListInterfaces returns a list of all network interfaces with their IPs
func ListInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	var result []string
	for _, iface := range ifaces {
		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}

			result = append(result, fmt.Sprintf("%s: %s", iface.Name, ip.String()))
		}
	}

	return result, nil
}

// FormatInterfaceList formats the interface list for display with Gummy styling
func FormatInterfaceList() string {
	ifaces, err := ListInterfaces()
	if err != nil {
		return ui.Error(fmt.Sprintf("Error listing interfaces: %v", err))
	}

	if len(ifaces) == 0 {
		return ui.Warning("No network interfaces found")
	}

	// Build lines for box
	var lines []string
	for _, iface := range ifaces {
		lines = append(lines, ui.Command(fmt.Sprintf("  %s", iface)))
	}

	return ui.BoxWithTitlePadded(fmt.Sprintf("%s Available Interfaces", ui.SymbolGem), lines, 8)
}
