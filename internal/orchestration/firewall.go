// Package orchestration — zero-trust port security and firewall rule management.
//
// PortManager allocates and enforces port isolation for workloads using
// platform-specific firewall rules (iptables on Linux, netsh on Windows,
// pf(4) on macOS).
//
// Zero Trust model (replaces RFC 1918 implicit subnet trust):
//   - All workload ports are DEFAULT DENY for inbound traffic.
//   - Only loopback and explicitly authorised source IPs are permitted.
//   - Call AuthorizeSourceIP after a connection token is validated to open
//     a port to a specific verified peer IP.
//   - East-west (workload-to-workload) outbound is DEFAULT DENY via OUTPUT
//     chain rules; call ApplyEastWestRules to declare explicit peer access.
package orchestration

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// PortAllocation tracks which ports are assigned to which workload.
type PortAllocation struct {
	WorkloadID    string
	HostPort      int
	Protocol      string   // "tcp" or "udp"
	AuthorizedIPs []string // zero-trust: only these source IPs may connect
}

// FirewallRule represents a single firewall rule for auditing and cleanup.
type FirewallRule struct {
	WorkloadID string
	HostPort   int
	Protocol   string
	Action     string // "allow_loopback", "allow_source", "drop"
}

// EastWestDest identifies a peer workload a workload is allowed to reach.
type EastWestDest struct {
	HostIP   string
	HostPort int
	Protocol string
}

// PortManager manages port allocation and firewall rule enforcement.
type PortManager struct {
	mu          sync.Mutex
	allocations map[int]*PortAllocation // key: host port
	usedPorts   map[int]bool
	nextPort    int // next ephemeral port (range 8100–9000)
	rules       []FirewallRule
	osType      string // "linux", "windows", "darwin"
}

// NewPortManager creates a new PortManager.
// Ephemeral ports are allocated from 8100–8999.
func NewPortManager() *PortManager {
	return &PortManager{
		allocations: make(map[int]*PortAllocation),
		usedPorts:   make(map[int]bool),
		nextPort:    8100,
		osType:      runtime.GOOS,
	}
}

// AllocatePort reserves a port for a workload. If requestedPort > 0 and
// available it is used; otherwise a port is allocated from the ephemeral range.
func (pm *PortManager) AllocatePort(workloadID string, requestedPort int, protocol string) (int, error) {
	if protocol != "tcp" && protocol != "udp" {
		return 0, fmt.Errorf("invalid protocol %q (must be tcp or udp)", protocol)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	var hostPort int
	if requestedPort > 0 {
		if pm.usedPorts[requestedPort] {
			return 0, fmt.Errorf("port %d already allocated", requestedPort)
		}
		hostPort = requestedPort
	} else {
		for pm.usedPorts[pm.nextPort] && pm.nextPort < 9000 {
			pm.nextPort++
		}
		if pm.nextPort >= 9000 {
			return 0, fmt.Errorf("no ephemeral ports available (limit 9000)")
		}
		hostPort = pm.nextPort
		pm.nextPort++
	}

	if isPortInUse(hostPort) {
		return 0, fmt.Errorf("port %d is already in use by another process", hostPort)
	}

	pm.allocations[hostPort] = &PortAllocation{
		WorkloadID:    workloadID,
		HostPort:      hostPort,
		Protocol:      protocol,
		AuthorizedIPs: nil, // default deny until AuthorizeSourceIP is called
	}
	pm.usedPorts[hostPort] = true

	log.Printf("[firewall] allocated port %d (%s) for workload %s", hostPort, protocol, workloadID)
	return hostPort, nil
}

// ReleasePort frees a previously allocated port and removes its firewall rules.
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

	if err := pm.removeFirewallRules(workloadID, hostPort, alloc.Protocol); err != nil {
		log.Printf("[firewall] failed to remove rules for %s:%d: %v", workloadID, hostPort, err)
	}

	delete(pm.allocations, hostPort)
	delete(pm.usedPorts, hostPort)
	log.Printf("[firewall] released port %d for workload %s", hostPort, workloadID)
	return nil
}

// ApplyFirewallRules creates zero-trust firewall rules for a workload port.
// The rules default-deny all inbound except loopback. Call AuthorizeSourceIP
// after token verification to open the port to a specific peer.
//
// On Linux: iptables INPUT chain rules.
// On Windows: netsh advfirewall rules.
// On macOS: logged warning (pf rules TBD).
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
		return pm.applyDarwinRules(workloadID, hostPort, protocol)
	default:
		return fmt.Errorf("unsupported OS for firewall rules: %s", pm.osType)
	}
}

// AuthorizeSourceIP adds a firewall rule allowing a specific source IP to
// reach workloadID's port. Call this after verifying a ConnectionToken from
// the peer — this is the zero-trust "explicit allow" step.
func (pm *PortManager) AuthorizeSourceIP(workloadID string, hostPort int, sourceIP string, protocol string) error {
	pm.mu.Lock()
	alloc, ok := pm.allocations[hostPort]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("port %d not allocated", hostPort)
	}
	if alloc.WorkloadID != workloadID {
		pm.mu.Unlock()
		return fmt.Errorf("port %d not owned by workload %s", hostPort, workloadID)
	}
	alloc.AuthorizedIPs = append(alloc.AuthorizedIPs, sourceIP)
	pm.mu.Unlock()

	switch pm.osType {
	case "linux":
		return pm.insertLinuxSourceAllow(hostPort, sourceIP, protocol)
	case "windows":
		return pm.insertWindowsSourceAllow(workloadID, hostPort, sourceIP, protocol)
	case "darwin":
		return pm.insertDarwinSourceAllow(hostPort, sourceIP, protocol)
	default:
		log.Printf("[firewall] AuthorizeSourceIP: no-op on %s for port %d source %s", pm.osType, hostPort, sourceIP)
		return nil
	}
}

// ApplyEastWestRules adds OUTPUT chain rules to restrict outbound traffic from
// the workload's UID. Default deny outbound; allow only declared peer destinations.
//
// This enforces micro-segmentation: workloads on the same host cannot reach
// each other's ports unless explicitly declared in AllowedPeers.
//
// workloadUID is the host UID under which the workload runs (e.g. 65534 for nobody).
// allowedDests is the list of peer (hostIP, hostPort, protocol) tuples to allow.
func (pm *PortManager) ApplyEastWestRules(workloadID string, workloadUID int, allowedDests []EastWestDest) error {
	if pm.osType != "linux" {
		log.Printf("[firewall] east-west OUTPUT rules: no-op on %s (Linux only)", pm.osType)
		return nil
	}

	// Insert ACCEPT rules for each declared peer BEFORE the default DROP.
	for _, dest := range allowedDests {
		rule := fmt.Sprintf("-I OUTPUT -m owner --uid-owner %d -p %s -d %s --dport %d -j ACCEPT",
			workloadUID, dest.Protocol, dest.HostIP, dest.HostPort)
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[firewall] east-west ACCEPT rule failed: %v (output: %s)", err, string(out))
		}
	}

	// Default deny outbound for this UID (appended after the ACCEPT rules above).
	dropRule := fmt.Sprintf("-A OUTPUT -m owner --uid-owner %d -j DROP", workloadUID)
	cmd := exec.Command("iptables", strings.Fields(dropRule)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[firewall] east-west DROP rule failed: %v (output: %s)", err, string(out))
	}

	log.Printf("[firewall] east-west rules applied for workload %s (uid %d, %d allowed peers)",
		workloadID, workloadUID, len(allowedDests))
	return nil
}

// ReleaseEastWestRules removes the OUTPUT chain rules for a workload UID.
func (pm *PortManager) ReleaseEastWestRules(workloadUID int, allowedDests []EastWestDest) {
	if pm.osType != "linux" {
		return
	}

	for _, dest := range allowedDests {
		rule := fmt.Sprintf("-D OUTPUT -m owner --uid-owner %d -p %s -d %s --dport %d -j ACCEPT",
			workloadUID, dest.Protocol, dest.HostIP, dest.HostPort)
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		_ = cmd.Run()
	}

	dropRule := fmt.Sprintf("-D OUTPUT -m owner --uid-owner %d -j DROP", workloadUID)
	cmd := exec.Command("iptables", strings.Fields(dropRule)...)
	_ = cmd.Run()
}

// applyLinuxRules creates zero-trust iptables rules:
//   - Allow loopback inbound
//   - Drop all other inbound (no RFC 1918 blanket allow)
//
// Source IPs are opened individually via AuthorizeSourceIP / insertLinuxSourceAllow.
func (pm *PortManager) applyLinuxRules(workloadID string, hostPort int, protocol string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rules := []string{
		// Allow loopback (required for health checks and local inter-process comms)
		fmt.Sprintf("-A INPUT -i lo -p %s --dport %d -j ACCEPT", protocol, hostPort),
		// Default deny: drop everything else on this port
		fmt.Sprintf("-A INPUT -p %s --dport %d -j DROP", protocol, hostPort),
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[firewall] iptables rule failed: %s — %v (output: %s)", rule, err, string(out))
		}
	}

	pm.rules = append(pm.rules, FirewallRule{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
		Action:     "allow_loopback",
	})

	log.Printf("[firewall] zero-trust iptables rules applied for %s port %d (%s)", workloadID, hostPort, protocol)
	return nil
}

// insertLinuxSourceAllow inserts a specific source-IP ACCEPT rule immediately
// before the existing DROP rule, so authorised peers can reach the port.
func (pm *PortManager) insertLinuxSourceAllow(hostPort int, sourceIP string, protocol string) error {
	// -I inserts before existing rules; place before the DROP
	rule := fmt.Sprintf("-I INPUT -p %s --dport %d -s %s -j ACCEPT", protocol, hostPort, sourceIP)
	cmd := exec.Command("iptables", strings.Fields(rule)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables source-allow insert: %v (output: %s)", err, string(out))
	}
	log.Printf("[firewall] authorised source %s → port %d (%s)", sourceIP, hostPort, protocol)
	return nil
}

// applyWindowsRules creates a Windows Firewall rule that only allows loopback
// (127.0.0.1) by default. Source IPs are opened via insertWindowsSourceAllow.
func (pm *PortManager) applyWindowsRules(workloadID string, hostPort int, protocol string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Base rule: allow loopback only
	ruleName := fmt.Sprintf("SoHoLINK-%s-%d-%s-loopback", workloadID[:8], hostPort, protocol)
	args := []string{
		"advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", ruleName),
		"dir=in", "action=allow",
		fmt.Sprintf("protocol=%s", strings.ToUpper(protocol)),
		fmt.Sprintf("localport=%d", hostPort),
		"remoteip=127.0.0.1",
		fmt.Sprintf("description=SoHoLINK zero-trust loopback for workload %s", workloadID),
	}
	cmd := exec.Command("netsh", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[firewall] netsh loopback rule failed (requires admin?): %v (output: %s)", err, string(out))
	}

	// Block-all rule for any other source on this port
	blockName := fmt.Sprintf("SoHoLINK-%s-%d-%s-block", workloadID[:8], hostPort, protocol)
	blockArgs := []string{
		"advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", blockName),
		"dir=in", "action=block",
		fmt.Sprintf("protocol=%s", strings.ToUpper(protocol)),
		fmt.Sprintf("localport=%d", hostPort),
		"remoteip=any",
		fmt.Sprintf("description=SoHoLINK default-deny for workload %s", workloadID),
	}
	blockCmd := exec.Command("netsh", blockArgs...)
	if out, err := blockCmd.CombinedOutput(); err != nil {
		log.Printf("[firewall] netsh default-deny rule failed: %v (output: %s)", err, string(out))
	}

	pm.rules = append(pm.rules, FirewallRule{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
		Action:     "allow_loopback",
	})

	log.Printf("[firewall] zero-trust Windows Firewall rules applied for %s port %d (%s)", workloadID, hostPort, protocol)
	return nil
}

// insertWindowsSourceAllow adds a Windows Firewall rule allowing a specific
// source IP to reach the workload port.
func (pm *PortManager) insertWindowsSourceAllow(workloadID string, hostPort int, sourceIP string, protocol string) error {
	ruleName := fmt.Sprintf("SoHoLINK-%s-%d-%s-src-%s", workloadID[:8], hostPort, protocol, strings.ReplaceAll(sourceIP, ".", "_"))
	args := []string{
		"advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", ruleName),
		"dir=in", "action=allow",
		fmt.Sprintf("protocol=%s", strings.ToUpper(protocol)),
		fmt.Sprintf("localport=%d", hostPort),
		fmt.Sprintf("remoteip=%s", sourceIP),
		fmt.Sprintf("description=SoHoLINK authorised source for workload %s", workloadID),
	}
	cmd := exec.Command("netsh", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh source-allow rule: %v (output: %s)", err, string(out))
	}
	log.Printf("[firewall] authorised source %s → port %d (%s) on Windows", sourceIP, hostPort, protocol)
	return nil
}

// removeFirewallRules removes firewall rules for a workload port on cleanup.
func (pm *PortManager) removeFirewallRules(workloadID string, hostPort int, protocol string) error {
	switch pm.osType {
	case "linux":
		return pm.removeLinuxRules(hostPort, protocol)
	case "windows":
		return pm.removeWindowsRules(workloadID, hostPort, protocol)
	case "darwin":
		return pm.removeDarwinRules(workloadID, hostPort, protocol)
	default:
		return nil
	}
}

// removeLinuxRules deletes iptables rules for a port (loopback allow + DROP).
func (pm *PortManager) removeLinuxRules(hostPort int, protocol string) error {
	rules := []string{
		fmt.Sprintf("-D INPUT -i lo -p %s --dport %d -j ACCEPT", protocol, hostPort),
		fmt.Sprintf("-D INPUT -p %s --dport %d -j DROP", protocol, hostPort),
	}
	for _, rule := range rules {
		cmd := exec.Command("iptables", strings.Fields(rule)...)
		_ = cmd.Run()
	}
	log.Printf("[firewall] removed iptables rules for port %d (%s)", hostPort, protocol)
	return nil
}

// removeWindowsRules deletes Windows Firewall rules for a port.
func (pm *PortManager) removeWindowsRules(workloadID string, hostPort int, protocol string) error {
	for _, suffix := range []string{"loopback", "block"} {
		ruleName := fmt.Sprintf("SoHoLINK-%s-%d-%s-%s", workloadID[:8], hostPort, protocol, suffix)
		cmd := exec.Command("netsh",
			"advfirewall", "firewall", "delete", "rule",
			fmt.Sprintf("name=%s", ruleName),
		)
		_ = cmd.Run()
	}
	log.Printf("[firewall] removed Windows Firewall rules for port %d (%s)", hostPort, protocol)
	return nil
}

// ListAllocations returns all current port allocations (for monitoring/debugging).
func (pm *PortManager) ListAllocations() []PortAllocation {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	result := make([]PortAllocation, 0, len(pm.allocations))
	for _, alloc := range pm.allocations {
		result = append(result, *alloc)
	}
	return result
}

// ── macOS pf(4) rules ─────────────────────────────────────────────────────────
//
// pf anchor files are written to /var/tmp/soholink/ (world-writable tmp dir
// that survives process restart without requiring /etc write access).
// pfctl commands require the process to run as root or have the
// com.apple.security.network.server entitlement on macOS 10.15+.

const pfAnchorDir = "/var/tmp/soholink"

func pfAnchorFile(workloadID string, hostPort int) string {
	return filepath.Join(pfAnchorDir, fmt.Sprintf("%s-%d.conf", workloadID[:8], hostPort))
}

func pfAnchorName(workloadID string, hostPort int) string {
	return fmt.Sprintf("soholink/%s-%d", workloadID[:8], hostPort)
}

// writePFAnchor writes the pf anchor conf file for a workload port.
// extraAllows contains additional source IPs to allow (inserted before the
// default-block rule so order is preserved on reload).
func writePFAnchor(workloadID string, hostPort int, protocol string, extraAllows []string) error {
	if err := os.MkdirAll(pfAnchorDir, 0700); err != nil {
		return fmt.Errorf("pf: mkdir anchor dir: %w", err)
	}

	var sb strings.Builder
	// Allow loopback
	sb.WriteString(fmt.Sprintf("pass in quick proto %s from 127.0.0.1 to any port %d\n", protocol, hostPort))
	// Allow explicitly authorised source IPs
	for _, ip := range extraAllows {
		sb.WriteString(fmt.Sprintf("pass in quick proto %s from %s to any port %d\n", protocol, ip, hostPort))
	}
	// Default deny for everything else on this port
	sb.WriteString(fmt.Sprintf("block drop in quick proto %s from any to any port %d\n", protocol, hostPort))

	return os.WriteFile(pfAnchorFile(workloadID, hostPort), []byte(sb.String()), 0600)
}

// applyDarwinRules creates a pf anchor for a workload port: allow loopback,
// deny all other inbound. Requires pfctl (root or entitlement).
func (pm *PortManager) applyDarwinRules(workloadID string, hostPort int, protocol string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := writePFAnchor(workloadID, hostPort, protocol, nil); err != nil {
		log.Printf("[firewall] pf: failed to write anchor file: %v", err)
		return err
	}

	anchorName := pfAnchorName(workloadID, hostPort)
	anchorFile := pfAnchorFile(workloadID, hostPort)

	// Load the per-workload anchor rules via the privilege helper (or direct exec).
	_ = pfctlLoadAnchor(anchorName, anchorFile)

	// Ensure pf is enabled (best-effort; may already be on).
	pfctlEnable()

	pm.rules = append(pm.rules, FirewallRule{
		WorkloadID: workloadID,
		HostPort:   hostPort,
		Protocol:   protocol,
		Action:     "allow_loopback",
	})

	log.Printf("[firewall] pf anchor applied for %s port %d (%s)", workloadID, hostPort, protocol)
	return nil
}

// insertDarwinSourceAllow adds a pass rule for sourceIP by rewriting and
// reloading the anchor file.
func (pm *PortManager) insertDarwinSourceAllow(hostPort int, sourceIP string, protocol string) error {
	// Find the workload owning this port
	pm.mu.Lock()
	alloc, ok := pm.allocations[hostPort]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("pf: port %d not allocated", hostPort)
	}
	workloadID := alloc.WorkloadID
	authorizedIPs := make([]string, len(alloc.AuthorizedIPs))
	copy(authorizedIPs, alloc.AuthorizedIPs)
	pm.mu.Unlock()

	if err := writePFAnchor(workloadID, hostPort, protocol, authorizedIPs); err != nil {
		return fmt.Errorf("pf: rewrite anchor: %w", err)
	}

	anchorName := pfAnchorName(workloadID, hostPort)
	anchorFile := pfAnchorFile(workloadID, hostPort)

	if err := pfctlLoadAnchor(anchorName, anchorFile); err != nil {
		return fmt.Errorf("pfctl reload: %w", err)
	}

	log.Printf("[firewall] pf: authorised source %s → port %d (%s)", sourceIP, hostPort, protocol)
	return nil
}

// removeDarwinRules flushes the pf anchor for a workload port and deletes
// the anchor file.
func (pm *PortManager) removeDarwinRules(workloadID string, hostPort int, _ string) error {
	anchorName := pfAnchorName(workloadID, hostPort)

	// Flush all rules in the anchor via the privilege helper (or direct exec).
	if err := pfctlFlushAnchor(anchorName); err != nil {
		log.Printf("[firewall] pfctl flush anchor failed: %v", err)
	}

	// Remove the anchor conf file
	_ = os.Remove(pfAnchorFile(workloadID, hostPort))

	log.Printf("[firewall] pf anchor removed for %s port %d", workloadID, hostPort)
	return nil
}

// isPortInUse checks if a port is already bound by the OS.
func isPortInUse(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	l.Close()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port, IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return true
	}
	conn.Close()
	return false
}
