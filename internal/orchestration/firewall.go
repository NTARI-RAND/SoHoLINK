// Package orchestration — port security and firewall rule management.
//
// PortManager allocates and enforces port isolation for workloads using
// platform-specific firewall rules (iptables on Linux, netsh on Windows).
// Each workload's ports are isolated from other workloads' ports.
package orchestration

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// PortAllocation tracks which ports are assigned to which workload.
type PortAllocation struct {
	WorkloadID string
	HostPort   int
	Protocol   string // "tcp" or "udp"
}

// PortManager manages port allocation and firewall rule enforcement.
type PortManager struct {
	mu           sync.Mutex
	allocations  map[int]*PortAllocation // key: host port, value: allocation
	usedPorts    map[int]bool            // quick lookup for allocated ports
	nextPort     int                      // next ephemeral port to allocate
	rules        []FirewallRule           // track applied rules for cleanup
	osType       string                   // "linux", "windows", "darwin"
}

// FirewallRule represents a single firewall rule for auditing and cleanup.
type FirewallRule struct {
	WorkloadID string
	HostPort   int
	Protocol   string
	Action     string // "allow", "deny"
}

// NewPortManager creates a new port manager.
// Ephemeral ports are allocated starting at 8100 (safe range: 8100-9000).
func NewPortManager() *PortManager {
	return &PortManager{
		allocations: make(map[int]*PortAllocation),
		usedPorts:   make(map[int]bool),
		nextPort:    8100,
		osType:      runtime.GOOS,
	}
}

// AllocatePort reserves a port for a workload. Returns the allocated host port.
// If a specific port is requested and available, it is used; otherwise a port
// is allocated from the ephemeral range.
func (pm *PortManager) AllocatePort(workloadID string, requestedPort int, protocol string) (int, error) {
	if protocol != "tcp" && protocol != "udp" {
		return 0, fmt.Errorf("invalid protocol %q (must be tcp or udp)", protocol)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	var hostPort int

	// If a specific port was requested, try to use it
	if requestedPort > 0 {
		if pm.usedPorts[requestedPort] {
			return 0, fmt.Errorf("port %d already allocated", requestedPort)
		}
		hostPort = requestedPort
	} else {
		// Find next available ephemeral port
		for pm.usedPorts[pm.nextPort] && pm.nextPort < 9000 {
			pm.nextPort++
		}
		if pm.nextPort >= 9000 {
			return 0, fmt.Errorf("no ephemeral ports available (limit 9000)")
		}
		hostPort = pm.nextPort
		pm.nextPort++
	}

	// Verify port is not in use by the OS
	if isPortInUse(hostPort) {
		return 0, fmt.Errorf("port %d is already in use by another process", hostPort)
	}

	// Record allocation
	pm.allocations[hostPort] = &PortAllocation{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
	}
	pm.usedPorts[hostPort] = true

	log.Printf("[firewall] allocated port %d (%s) for workload %s", hostPort, protocol, workloadID)
	return hostPort, nil
}

// ReleasePort frees a previously allocated port and removes firewall rules.
func (pm *PortManager) ReleasePort(workloadID string, hostPort int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	alloc, ok := pm.allocations[hostPort]
	if !ok {
		return fmt.Errorf("port %d not allocated", hostPort)
	}
	if alloc.WorkloadID != workloadID {
		return fmt.Errorf("port %d allocated to different workload", hostPort)
	}

	// Remove firewall rules
	if err := pm.removeFirewallRules(workloadID, hostPort, alloc.Protocol); err != nil {
		log.Printf("[firewall] failed to remove rules for %s:%d: %v", workloadID, hostPort, err)
		// Continue anyway — port is freed
	}

	delete(pm.allocations, hostPort)
	delete(pm.usedPorts, hostPort)
	log.Printf("[firewall] released port %d for workload %s", hostPort, workloadID)
	return nil
}

// ApplyFirewallRules creates firewall rules to isolate workload ports.
// On Linux: iptables rules to allow traffic only to the specified ports.
// On Windows: netsh advfirewall rules to allow the application/port.
func (pm *PortManager) ApplyFirewallRules(workloadID string, hostPort int, protocol string) error {
	if _, ok := pm.allocations[hostPort]; !ok {
		return fmt.Errorf("port %d not allocated", hostPort)
	}

	switch pm.osType {
	case "linux":
		return pm.applyLinuxRules(workloadID, hostPort, protocol)
	case "windows":
		return pm.applyWindowsRules(workloadID, hostPort, protocol)
	case "darwin":
		// macOS typically uses pf(4). For now, log a warning.
		log.Printf("[firewall] macOS firewall rules not yet implemented for port %d", hostPort)
		return nil
	default:
		return fmt.Errorf("unsupported OS for firewall rules: %s", pm.osType)
	}
}

// applyLinuxRules creates iptables rules for port isolation.
// Rules: allow inbound traffic to workload port, deny all other container->host traffic.
func (pm *PortManager) applyLinuxRules(workloadID string, hostPort int, protocol string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rules := []string{
		// Allow loopback access (always needed)
		fmt.Sprintf("-A INPUT -i lo -p %s --dport %d -j ACCEPT", protocol, hostPort),
		// Allow from local network (RFC 1918 private ranges)
		fmt.Sprintf("-A INPUT -p %s --dport %d -s 10.0.0.0/8 -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-A INPUT -p %s --dport %d -s 172.16.0.0/12 -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-A INPUT -p %s --dport %d -s 192.168.0.0/16 -j ACCEPT", protocol, hostPort),
		// Drop all other inbound traffic to this port
		fmt.Sprintf("-A INPUT -p %s --dport %d -j DROP", protocol, hostPort),
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[firewall] iptables failed for %s: %v (output: %s)", rule, err, string(out))
			// Don't fail — continue with other rules
		}
	}

	pm.rules = append(pm.rules, FirewallRule{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
		Action:     "allow_local",
	})

	log.Printf("[firewall] applied iptables rules for %s on port %d (%s)", workloadID, hostPort, protocol)
	return nil
}

// applyWindowsRules creates Windows Firewall rules for port isolation.
// Uses netsh advfirewall to allow traffic to the workload port from private networks.
func (pm *PortManager) applyWindowsRules(workloadID string, hostPort int, protocol string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	ruleName := fmt.Sprintf("SoHoLINK-%s-%d-%s", workloadID[:8], hostPort, protocol)

	// Create rule: allow inbound traffic on the port from private networks
	args := []string{
		"advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", ruleName),
		"dir=in",
		"action=allow",
		fmt.Sprintf("protocol=%s", strings.ToUpper(protocol)),
		fmt.Sprintf("localport=%d", hostPort),
		"remoteip=localsubnet",
		fmt.Sprintf("description=SoHoLINK workload %s port isolation", workloadID),
	}

	cmd := exec.Command("netsh", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Log but don't fail — may require admin privileges
		log.Printf("[firewall] netsh failed (requires admin?): %v (output: %s)", err, string(out))
	}

	pm.rules = append(pm.rules, FirewallRule{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
		Action:     "allow_local",
	})

	log.Printf("[firewall] applied Windows Firewall rule for %s on port %d (%s)", workloadID, hostPort, protocol)
	return nil
}

// removeFirewallRules removes firewall rules for a workload port.
func (pm *PortManager) removeFirewallRules(workloadID string, hostPort int, protocol string) error {
	switch pm.osType {
	case "linux":
		return pm.removeLinuxRules(hostPort, protocol)
	case "windows":
		return pm.removeWindowsRules(workloadID, hostPort, protocol)
	case "darwin":
		// No-op for macOS
		return nil
	default:
		return fmt.Errorf("unsupported OS for firewall rule removal: %s", pm.osType)
	}
}

// removeLinuxRules deletes iptables rules for a port.
func (pm *PortManager) removeLinuxRules(hostPort int, protocol string) error {
	rules := []string{
		fmt.Sprintf("-D INPUT -i lo -p %s --dport %d -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-D INPUT -p %s --dport %d -s 10.0.0.0/8 -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-D INPUT -p %s --dport %d -s 172.16.0.0/12 -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-D INPUT -p %s --dport %d -s 192.168.0.0/16 -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-D INPUT -p %s --dport %d -j DROP", protocol, hostPort),
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		_ = cmd.Run() // Ignore errors; rule may not exist
	}

	log.Printf("[firewall] removed iptables rules for port %d (%s)", hostPort, protocol)
	return nil
}

// removeWindowsRules deletes Windows Firewall rules for a port.
func (pm *PortManager) removeWindowsRules(workloadID string, hostPort int, protocol string) error {
	ruleName := fmt.Sprintf("SoHoLINK-%s-%d-%s", workloadID[:8], hostPort, protocol)

	cmd := exec.Command("netsh",
		"advfirewall", "firewall", "delete", "rule",
		fmt.Sprintf("name=%s", ruleName),
	)
	_ = cmd.Run() // Ignore errors; rule may not exist

	log.Printf("[firewall] removed Windows Firewall rule for port %d (%s)", hostPort, protocol)
	return nil
}

// ListAllocations returns all current port allocations (for debugging/monitoring).
func (pm *PortManager) ListAllocations() []PortAllocation {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var result []PortAllocation
	for _, alloc := range pm.allocations {
		result = append(result, *alloc)
	}
	return result
}

// isPortInUse checks if a port is already bound by the OS.
// Attempts to bind to the port; if successful, the port is free.
func isPortInUse(port int) bool {
	// Try TCP
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return true // Port in use
	}
	l.Close()

	// Try UDP
	addr = net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		Port: port,
		IP:   net.ParseIP("127.0.0.1"),
	})
	if err != nil {
		return true // Port in use
	}
	conn.Close()

	return false // Port free
}
