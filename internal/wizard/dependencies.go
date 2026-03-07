package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DependencyTracker tracks and documents system dependencies.
type DependencyTracker struct {
	capabilities *SystemCapabilities
	costProfile  *CostProfile
	pricing      *PricingConfig
}

// NewDependencyTracker creates a new dependency tracker.
func NewDependencyTracker(caps *SystemCapabilities, cost *CostProfile, pricing *PricingConfig) *DependencyTracker {
	return &DependencyTracker{
		capabilities: caps,
		costProfile:  cost,
		pricing:      pricing,
	}
}

// GenerateReport generates a comprehensive dependency report.
func (d *DependencyTracker) GenerateReport() *DependencyReport {
	report := &DependencyReport{
		Timestamp: time.Now(),
		Platform:  d.capabilities.OS,
		Hardware: HardwareInfo{
			CPU:     d.capabilities.CPU,
			Memory:  d.capabilities.Memory,
			Storage: d.capabilities.Storage,
			GPU:     d.capabilities.GPU,
		},
		Hypervisor:   d.capabilities.Hypervisor,
		Network:      d.capabilities.Network,
		Dependencies: d.detectDependencies(),
		CostProfile:  *d.costProfile,
		Pricing:      *d.pricing,
	}

	return report
}

// detectDependencies detects all system dependencies.
func (d *DependencyTracker) detectDependencies() []Dependency {
	deps := []Dependency{}

	// Hypervisor dependency
	deps = append(deps, d.hypervisorDependency())

	// Virtualization dependency
	deps = append(deps, d.virtualizationDependency())

	// Network dependencies
	deps = append(deps, d.networkDependencies()...)

	// Platform-specific dependencies
	deps = append(deps, d.platformDependencies()...)

	// Optional dependencies
	deps = append(deps, d.optionalDependencies()...)

	return deps
}

// hypervisorDependency checks hypervisor status.
func (d *DependencyTracker) hypervisorDependency() Dependency {
	dep := Dependency{
		Name: d.capabilities.Hypervisor.Type,
		Type: "required",
	}

	if d.capabilities.Hypervisor.Installed && d.capabilities.Hypervisor.Enabled {
		dep.Status = "installed"
		dep.Version = d.capabilities.Hypervisor.Version
	} else if d.capabilities.Hypervisor.Installed {
		dep.Status = "installed_not_enabled"
		dep.Version = d.capabilities.Hypervisor.Version
	} else {
		dep.Status = "missing"
	}

	return dep
}

// virtualizationDependency checks virtualization support.
func (d *DependencyTracker) virtualizationDependency() Dependency {
	dep := Dependency{
		Name: "CPU Virtualization",
		Type: "required",
	}

	if d.capabilities.Virtualization.Supported && d.capabilities.Virtualization.Enabled {
		dep.Status = "installed"
		dep.Version = d.capabilities.Virtualization.Technology
	} else if d.capabilities.Virtualization.Supported {
		dep.Status = "supported_not_enabled"
		dep.Version = d.capabilities.Virtualization.Technology
	} else {
		dep.Status = "not_supported"
	}

	return dep
}

// networkDependencies checks network requirements.
func (d *DependencyTracker) networkDependencies() []Dependency {
	deps := []Dependency{}

	// Check for network interfaces
	activeInterfaces := 0
	for _, iface := range d.capabilities.Network.Interfaces {
		if iface.IsUp && !iface.IsLoopback {
			activeInterfaces++
		}
	}

	netDep := Dependency{
		Name:    "Network Interface",
		Type:    "required",
		Version: fmt.Sprintf("%d active interface(s)", activeInterfaces),
	}

	if activeInterfaces > 0 {
		netDep.Status = "installed"
	} else {
		netDep.Status = "missing"
	}

	deps = append(deps, netDep)

	// Check firewall
	fwDep := Dependency{
		Name: "Firewall",
		Type: "optional",
	}

	if d.capabilities.Network.FirewallEnabled {
		fwDep.Status = "installed"
		fwDep.Version = "enabled"
	} else {
		fwDep.Status = "not_installed"
		fwDep.Version = "disabled"
	}

	deps = append(deps, fwDep)

	return deps
}

// platformDependencies returns platform-specific dependencies.
func (d *DependencyTracker) platformDependencies() []Dependency {
	deps := []Dependency{}

	switch d.capabilities.OS.Platform {
	case "windows":
		deps = append(deps, d.windowsDependencies()...)
	case "linux":
		deps = append(deps, d.linuxDependencies()...)
	case "darwin":
		deps = append(deps, d.macOSDependencies()...)
	}

	return deps
}

// windowsDependencies returns Windows-specific dependencies.
func (d *DependencyTracker) windowsDependencies() []Dependency {
	deps := []Dependency{}

	// Virtual Switch (required for Hyper-V networking)
	vSwitchDep := Dependency{
		Name:     "Hyper-V Virtual Switch",
		Type:     "required",
		Platform: "windows",
	}

	// Check if hypervisor has virtual switch feature
	hasVSwitch := false
	for _, feature := range d.capabilities.Hypervisor.Features {
		if feature == "virtual_switch" {
			hasVSwitch = true
			break
		}
	}

	if hasVSwitch {
		vSwitchDep.Status = "installed"
	} else {
		vSwitchDep.Status = "missing"
	}

	deps = append(deps, vSwitchDep)

	// Windows Firewall
	fwDep := Dependency{
		Name:     "Windows Firewall",
		Type:     "optional",
		Platform: "windows",
	}

	if d.capabilities.Network.FirewallEnabled {
		fwDep.Status = "installed"
		fwDep.Version = "active"
	} else {
		fwDep.Status = "installed"
		fwDep.Version = "inactive"
	}

	deps = append(deps, fwDep)

	return deps
}

// linuxDependencies returns Linux-specific dependencies.
func (d *DependencyTracker) linuxDependencies() []Dependency {
	deps := []Dependency{}

	// libvirt (common KVM management tool)
	libvirtDep := Dependency{
		Name:     "libvirt",
		Type:     "optional",
		Platform: "linux",
		Status:   "not_checked",
	}

	deps = append(deps, libvirtDep)

	// AppArmor (security module)
	apparmorDep := Dependency{
		Name:     "AppArmor",
		Type:     "optional",
		Platform: "linux",
		Status:   "not_checked",
	}

	deps = append(deps, apparmorDep)

	// iptables/firewalld
	fwDep := Dependency{
		Name:     "iptables/firewalld",
		Type:     "optional",
		Platform: "linux",
	}

	if d.capabilities.Network.FirewallEnabled {
		fwDep.Status = "installed"
	} else {
		fwDep.Status = "not_installed"
	}

	deps = append(deps, fwDep)

	return deps
}

// macOSDependencies returns macOS-specific dependencies.
func (d *DependencyTracker) macOSDependencies() []Dependency {
	deps := []Dependency{}

	// Virtualization.framework (macOS native virtualization)
	virtDep := Dependency{
		Name:     "Virtualization.framework",
		Type:     "required",
		Platform: "darwin",
		Status:   "not_checked",
	}

	deps = append(deps, virtDep)

	return deps
}

// optionalDependencies returns optional dependencies.
func (d *DependencyTracker) optionalDependencies() []Dependency {
	deps := []Dependency{}

	// GPU compute (optional)
	if d.capabilities.GPU != nil {
		gpuDep := Dependency{
			Name:    "GPU Compute",
			Type:    "optional",
			Status:  "installed",
			Version: d.capabilities.GPU.Model,
		}
		deps = append(deps, gpuDep)
	}

	return deps
}

// SaveToFile saves the dependency report to a JSON file.
func (r *DependencyReport) SaveToFile(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// LoadFromFile loads a dependency report from a JSON file.
func LoadDependencyReportFromFile(path string) (*DependencyReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var report DependencyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report: %w", err)
	}

	return &report, nil
}

// ExportMarkdown exports the dependency report as markdown.
func (r *DependencyReport) ExportMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# SoHoLINK Dependency Report\n\n")
	sb.WriteString(fmt.Sprintf("**Generated:** %s\n\n", r.Timestamp.Format(time.RFC3339)))

	// Platform
	sb.WriteString("## Platform\n\n")
	sb.WriteString(fmt.Sprintf("- **OS:** %s\n", r.Platform.Distribution))
	sb.WriteString(fmt.Sprintf("- **Version:** %s\n", r.Platform.Version))
	sb.WriteString(fmt.Sprintf("- **Architecture:** %s\n", r.Platform.Architecture))
	sb.WriteString(fmt.Sprintf("- **Kernel:** %s\n\n", r.Platform.Kernel))

	// Hardware
	sb.WriteString("## Hardware\n\n")
	sb.WriteString("### CPU\n")
	sb.WriteString(fmt.Sprintf("- **Model:** %s\n", r.Hardware.CPU.Model))
	sb.WriteString(fmt.Sprintf("- **Cores:** %d physical, %d threads\n", r.Hardware.CPU.Cores, r.Hardware.CPU.Threads))
	sb.WriteString(fmt.Sprintf("- **Frequency:** %.2f MHz\n", r.Hardware.CPU.FrequencyMHz))
	sb.WriteString(fmt.Sprintf("- **Virtualization:** %s\n\n", r.Hardware.CPU.VirtualizationTech))

	sb.WriteString("### Memory\n")
	sb.WriteString(fmt.Sprintf("- **Total:** %.1f GB\n", r.Hardware.Memory.TotalGB))
	sb.WriteString(fmt.Sprintf("- **Available:** %.1f GB\n", r.Hardware.Memory.AvailableGB))
	sb.WriteString(fmt.Sprintf("- **Used:** %.1f%%\n\n", r.Hardware.Memory.UsedPercent))

	sb.WriteString("### Storage\n")
	sb.WriteString(fmt.Sprintf("- **Total:** %.1f GB\n", r.Hardware.Storage.TotalGB))
	sb.WriteString(fmt.Sprintf("- **Available:** %.1f GB\n", r.Hardware.Storage.AvailableGB))
	sb.WriteString(fmt.Sprintf("- **Filesystem:** %s\n", r.Hardware.Storage.Filesystem))
	sb.WriteString(fmt.Sprintf("- **Type:** %s\n\n", r.Hardware.Storage.DriveType))

	if r.Hardware.GPU != nil {
		sb.WriteString("### GPU\n")
		sb.WriteString(fmt.Sprintf("- **Model:** %s\n", r.Hardware.GPU.Model))
		sb.WriteString(fmt.Sprintf("- **Vendor:** %s\n", r.Hardware.GPU.Vendor))
		if r.Hardware.GPU.VRAMGb > 0 {
			sb.WriteString(fmt.Sprintf("- **VRAM:** %d GB\n", r.Hardware.GPU.VRAMGb))
		}
		sb.WriteString("\n")
	}

	// Hypervisor
	sb.WriteString("## Hypervisor\n\n")
	sb.WriteString(fmt.Sprintf("- **Type:** %s\n", r.Hypervisor.Type))
	sb.WriteString(fmt.Sprintf("- **Installed:** %v\n", r.Hypervisor.Installed))
	sb.WriteString(fmt.Sprintf("- **Enabled:** %v\n", r.Hypervisor.Enabled))
	if r.Hypervisor.Version != "" {
		sb.WriteString(fmt.Sprintf("- **Version:** %s\n", r.Hypervisor.Version))
	}
	if len(r.Hypervisor.Features) > 0 {
		sb.WriteString(fmt.Sprintf("- **Features:** %s\n", strings.Join(r.Hypervisor.Features, ", ")))
	}
	sb.WriteString("\n")

	// Network
	sb.WriteString("## Network\n\n")
	sb.WriteString(fmt.Sprintf("- **Bandwidth:** %d Mbps\n", r.Network.BandwidthMbps))
	sb.WriteString(fmt.Sprintf("- **Firewall:** %v\n", r.Network.FirewallEnabled))
	sb.WriteString(fmt.Sprintf("- **Interfaces:** %d\n\n", len(r.Network.Interfaces)))

	// Dependencies
	sb.WriteString("## Dependencies\n\n")
	sb.WriteString("| Name | Type | Status | Version |\n")
	sb.WriteString("|------|------|--------|----------|\n")

	for _, dep := range r.Dependencies {
		statusEmoji := dependencyStatusEmoji(dep.Status)
		sb.WriteString(fmt.Sprintf("| %s | %s | %s %s | %s |\n",
			dep.Name,
			dep.Type,
			statusEmoji,
			dep.Status,
			dep.Version,
		))
	}
	sb.WriteString("\n")

	// Cost Profile
	sb.WriteString("## Cost Profile\n\n")
	sb.WriteString(fmt.Sprintf("- **Electricity Rate:** $%.3f/kWh\n", r.CostProfile.ElectricityRatePerKWh))
	sb.WriteString(fmt.Sprintf("- **Base Power:** %.0f W\n", r.CostProfile.BasePowerWatts))
	sb.WriteString(fmt.Sprintf("- **Load Power:** %.0f W\n", r.CostProfile.LoadPowerWatts))
	sb.WriteString(fmt.Sprintf("- **Power Cost:** $%.3f/hour\n", r.CostProfile.PowerCostPerHour))

	if r.CostProfile.HasExtraCooling {
		sb.WriteString(fmt.Sprintf("- **Cooling Cost:** $%.3f/hour\n", r.CostProfile.CoolingCostPerHour))
	}

	if r.CostProfile.DepreciationPerHour > 0 {
		sb.WriteString(fmt.Sprintf("- **Depreciation:** $%.3f/hour\n", r.CostProfile.DepreciationPerHour))
		sb.WriteString(fmt.Sprintf("  - Hardware Cost: $%.2f\n", r.CostProfile.HardwareCost))
		sb.WriteString(fmt.Sprintf("  - Lifespan: %.0f years\n", r.CostProfile.HardwareLifespanYears))
	}

	sb.WriteString(fmt.Sprintf("- **Total Cost:** $%.3f/hour ($%.2f/month)\n\n",
		r.CostProfile.TotalCostPerHour,
		r.CostProfile.TotalCostPerHour*24*30,
	))

	// Pricing
	sb.WriteString("## Pricing Configuration\n\n")
	sb.WriteString(fmt.Sprintf("- **Price per VM:** $%.3f/hour ($%.2f/month)\n",
		r.Pricing.PerVMPerHour,
		r.Pricing.PerVMPerHour*24*30,
	))
	sb.WriteString(fmt.Sprintf("- **Currency:** %s\n", r.Pricing.Currency))
	sb.WriteString(fmt.Sprintf("- **Profit Margin:** %.0f%%\n", r.Pricing.ProfitMarginPercent))
	sb.WriteString(fmt.Sprintf("- **Price Mode:** %s\n\n", r.Pricing.PriceMode))

	// Footer
	sb.WriteString("---\n\n")
	sb.WriteString("*Generated by SoHoLINK Deployment Wizard*\n")

	return sb.String()
}

// dependencyStatusEmoji returns an emoji for dependency status.
func dependencyStatusEmoji(status string) string {
	switch status {
	case "installed":
		return "✅"
	case "missing":
		return "❌"
	case "not_installed":
		return "⚠️"
	case "not_applicable":
		return "➖"
	case "not_checked":
		return "❓"
	case "supported_not_enabled":
		return "⚠️"
	case "installed_not_enabled":
		return "⚠️"
	default:
		return "❓"
	}
}

// ExportHTML exports the dependency report as HTML.
func (r *DependencyReport) ExportHTML() string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>SoHoLINK Dependency Report</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 900px; margin: 40px auto; padding: 20px; }
        h1 { color: #333; border-bottom: 2px solid #007bff; padding-bottom: 10px; }
        h2 { color: #555; margin-top: 30px; }
        table { width: 100%; border-collapse: collapse; margin: 20px 0; }
        th, td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background-color: #f8f9fa; font-weight: bold; }
        .status-ok { color: green; }
        .status-missing { color: red; }
        .status-warning { color: orange; }
        .info-grid { display: grid; grid-template-columns: 200px 1fr; gap: 10px; margin: 20px 0; }
        .info-label { font-weight: bold; }
        .footer { margin-top: 40px; padding-top: 20px; border-top: 1px solid #ddd; color: #888; }
    </style>
</head>
<body>
`)

	sb.WriteString(fmt.Sprintf("    <h1>SoHoLINK Dependency Report</h1>\n"))
	sb.WriteString(fmt.Sprintf("    <p><strong>Generated:</strong> %s</p>\n", r.Timestamp.Format(time.RFC1123)))

	// Platform section
	sb.WriteString("    <h2>Platform</h2>\n")
	sb.WriteString("    <div class=\"info-grid\">\n")
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Operating System:</div><div>%s</div>\n", r.Platform.Distribution))
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Version:</div><div>%s</div>\n", r.Platform.Version))
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Architecture:</div><div>%s</div>\n", r.Platform.Architecture))
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Kernel:</div><div>%s</div>\n", r.Platform.Kernel))
	sb.WriteString("    </div>\n")

	// Hardware section
	sb.WriteString("    <h2>Hardware</h2>\n")
	sb.WriteString("    <h3>CPU</h3>\n")
	sb.WriteString("    <div class=\"info-grid\">\n")
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Model:</div><div>%s</div>\n", r.Hardware.CPU.Model))
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Cores:</div><div>%d physical, %d threads</div>\n", r.Hardware.CPU.Cores, r.Hardware.CPU.Threads))
	sb.WriteString(fmt.Sprintf("        <div class=\"info-label\">Virtualization:</div><div>%s</div>\n", r.Hardware.CPU.VirtualizationTech))
	sb.WriteString("    </div>\n")

	// Dependencies table
	sb.WriteString("    <h2>Dependencies</h2>\n")
	sb.WriteString("    <table>\n")
	sb.WriteString("        <tr><th>Name</th><th>Type</th><th>Status</th><th>Version</th></tr>\n")

	for _, dep := range r.Dependencies {
		statusClass := "status-ok"
		if dep.Status == "missing" {
			statusClass = "status-missing"
		} else if strings.Contains(dep.Status, "not") || strings.Contains(dep.Status, "warning") {
			statusClass = "status-warning"
		}

		sb.WriteString(fmt.Sprintf("        <tr><td>%s</td><td>%s</td><td class=\"%s\">%s</td><td>%s</td></tr>\n",
			dep.Name,
			dep.Type,
			statusClass,
			dep.Status,
			dep.Version,
		))
	}

	sb.WriteString("    </table>\n")

	// Footer
	sb.WriteString("    <div class=\"footer\">\n")
	sb.WriteString("        <p><em>Generated by SoHoLINK Deployment Wizard</em></p>\n")
	sb.WriteString("    </div>\n")

	sb.WriteString("</body>\n</html>")

	return sb.String()
}

// ValidateDependencies checks if all required dependencies are met.
func (r *DependencyReport) ValidateDependencies() (bool, []string) {
	var issues []string
	allOK := true

	for _, dep := range r.Dependencies {
		if dep.Type == "required" {
			if dep.Status == "missing" || dep.Status == "not_supported" || dep.Status == "supported_not_enabled" {
				allOK = false
				issues = append(issues, fmt.Sprintf("%s: %s", dep.Name, dep.Status))
			}
		}
	}

	return allOK, issues
}

// GetSummary returns a brief summary of the system.
func (r *DependencyReport) GetSummary() string {
	alloc := &ResourceAllocation{
		TotalCPUCores:  r.Hardware.CPU.Cores,
		TotalMemoryGB:  int(r.Hardware.Memory.TotalGB),
		TotalStorageGB: int(r.Hardware.Storage.TotalGB),
	}

	// Simple allocation (same logic as detection.go)
	alloc.AllocatableCores = alloc.TotalCPUCores / 2
	alloc.AllocatableMemoryGB = alloc.TotalMemoryGB / 2

	maxVMs := alloc.AllocatableCores / 4
	if maxVMsByMem := alloc.AllocatableMemoryGB / 4; maxVMsByMem < maxVMs {
		maxVMs = maxVMsByMem
	}

	return fmt.Sprintf("%s | %d cores | %.1f GB RAM | %s | Max %d VMs",
		r.Platform.Distribution,
		r.Hardware.CPU.Cores,
		r.Hardware.Memory.TotalGB,
		r.Hypervisor.Type,
		maxVMs,
	)
}
