package wizard

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// WizardStep tests
// ---------------------------------------------------------------------------

func TestWizardStepString(t *testing.T) {
	tests := []struct {
		step WizardStep
		want string
	}{
		{StepWelcome, "Welcome"},
		{StepDetection, "Detection"},
		{StepCostMeasurement, "Cost Measurement"},
		{StepDepreciation, "Depreciation"},
		{StepPricing, "Pricing"},
		{StepIdentity, "Identity"},
		{StepNetwork, "Network"},
		{StepDependencies, "Dependencies"},
		{StepPolicies, "Policies"},
		{StepReview, "Review"},
		{StepComplete, "Complete"},
		{WizardStep(999), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.step.String()
			if got != tt.want {
				t.Errorf("WizardStep(%d).String() = %q, want %q", tt.step, got, tt.want)
			}
		})
	}
}

func TestWizardStepProgress(t *testing.T) {
	tests := []struct {
		step        WizardStep
		wantCurrent int
		wantTotal   int
	}{
		{StepWelcome, 1, 11},
		{StepDetection, 2, 11},
		{StepComplete, 11, 11},
		{StepPricing, 5, 11},
	}
	for _, tt := range tests {
		t.Run(tt.step.String(), func(t *testing.T) {
			current, total := tt.step.Progress()
			if current != tt.wantCurrent || total != tt.wantTotal {
				t.Errorf("Progress() = (%d, %d), want (%d, %d)", current, total, tt.wantCurrent, tt.wantTotal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CalculateAvailableResources tests
// ---------------------------------------------------------------------------

func TestCalculateAvailableResources_Basic(t *testing.T) {
	caps := &SystemCapabilities{
		CPU:    CPUInfo{Cores: 8, Threads: 16},
		Memory: MemoryInfo{TotalGB: 32},
		Storage: StorageInfo{TotalGB: 500},
	}

	alloc := caps.CalculateAvailableResources()

	if alloc.TotalCPUCores != 8 {
		t.Errorf("TotalCPUCores = %d, want 8", alloc.TotalCPUCores)
	}
	if alloc.AllocatableCores != 4 {
		t.Errorf("AllocatableCores = %d, want 4", alloc.AllocatableCores)
	}
	if alloc.ReservedCores != 4 {
		t.Errorf("ReservedCores = %d, want 4", alloc.ReservedCores)
	}
	if alloc.TotalMemoryGB != 32 {
		t.Errorf("TotalMemoryGB = %d, want 32", alloc.TotalMemoryGB)
	}
	if alloc.AllocatableMemoryGB != 16 {
		t.Errorf("AllocatableMemoryGB = %d, want 16", alloc.AllocatableMemoryGB)
	}
	// Storage >= 400GB: reserve 200GB
	if alloc.ReservedStorageGB != 200 {
		t.Errorf("ReservedStorageGB = %d, want 200", alloc.ReservedStorageGB)
	}
	if alloc.AllocatableStorageGB != 300 {
		t.Errorf("AllocatableStorageGB = %d, want 300", alloc.AllocatableStorageGB)
	}
}

func TestCalculateAvailableResources_SmallStorage(t *testing.T) {
	caps := &SystemCapabilities{
		CPU:     CPUInfo{Cores: 4, Threads: 8},
		Memory:  MemoryInfo{TotalGB: 16},
		Storage: StorageInfo{TotalGB: 300},
	}

	alloc := caps.CalculateAvailableResources()

	// Storage < 400GB: reserve 50%
	if alloc.ReservedStorageGB != 150 {
		t.Errorf("ReservedStorageGB = %d, want 150", alloc.ReservedStorageGB)
	}
	if alloc.AllocatableStorageGB != 150 {
		t.Errorf("AllocatableStorageGB = %d, want 150", alloc.AllocatableStorageGB)
	}
}

func TestCalculateAvailableResources_MaxVMs(t *testing.T) {
	tests := []struct {
		name     string
		cores    int
		memoryGB float64
		wantMax  int
	}{
		{"limited by CPU", 8, 64, 1},      // 4 allocatable cores / 4 = 1 VM
		{"limited by memory", 16, 16, 2},  // 8 allocatable cores / 4 = 2, 8GB mem / 4 = 2 → min = 2
		{"large system", 64, 256, 8},      // 32 alloc cores / 4 = 8, 128GB mem / 4 = 32 → min = 8
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := &SystemCapabilities{
				CPU:     CPUInfo{Cores: tt.cores, Threads: tt.cores * 2},
				Memory:  MemoryInfo{TotalGB: tt.memoryGB},
				Storage: StorageInfo{TotalGB: 1000},
			}
			alloc := caps.CalculateAvailableResources()
			if alloc.MaxVMs != tt.wantMax {
				t.Errorf("MaxVMs = %d, want %d", alloc.MaxVMs, tt.wantMax)
			}
		})
	}
}

func TestCalculateAvailableResources_GPU(t *testing.T) {
	capsNoGPU := &SystemCapabilities{
		CPU:     CPUInfo{Cores: 8, Threads: 16},
		Memory:  MemoryInfo{TotalGB: 32},
		Storage: StorageInfo{TotalGB: 500},
	}
	allocNoGPU := capsNoGPU.CalculateAvailableResources()
	if allocNoGPU.HasGPU {
		t.Error("HasGPU should be false when GPU is nil")
	}

	capsWithGPU := &SystemCapabilities{
		CPU:     CPUInfo{Cores: 8, Threads: 16},
		Memory:  MemoryInfo{TotalGB: 32},
		Storage: StorageInfo{TotalGB: 500},
		GPU:     &GPUInfo{Model: "RTX 4090"},
	}
	allocWithGPU := capsWithGPU.CalculateAvailableResources()
	if !allocWithGPU.HasGPU {
		t.Error("HasGPU should be true when GPU is present")
	}
	if allocWithGPU.GPUAllocatable {
		t.Error("GPUAllocatable should default to false")
	}
}

// ---------------------------------------------------------------------------
// ValidateProviderCapability tests
// ---------------------------------------------------------------------------

func TestValidateProviderCapability(t *testing.T) {
	tests := []struct {
		name    string
		caps    SystemCapabilities
		wantErr string
	}{
		{
			name: "no virtualization support",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: false},
			},
			wantErr: "does not support virtualization",
		},
		{
			name: "virtualization not enabled",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: false},
			},
			wantErr: "not enabled",
		},
		{
			name: "no hypervisor",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: true},
				Hypervisor:     HypervisorInfo{Installed: false},
			},
			wantErr: "no hypervisor",
		},
		{
			name: "too few cores",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: true},
				Hypervisor:     HypervisorInfo{Installed: true},
				CPU:            CPUInfo{Cores: 2},
			},
			wantErr: "minimum 4 CPU cores",
		},
		{
			name: "too little RAM",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: true},
				Hypervisor:     HypervisorInfo{Installed: true},
				CPU:            CPUInfo{Cores: 4},
				Memory:         MemoryInfo{TotalGB: 4},
			},
			wantErr: "minimum 8 GB RAM",
		},
		{
			name: "too little storage",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: true},
				Hypervisor:     HypervisorInfo{Installed: true},
				CPU:            CPUInfo{Cores: 4},
				Memory:         MemoryInfo{TotalGB: 16},
				Storage:        StorageInfo{AvailableGB: 50},
			},
			wantErr: "minimum 100 GB",
		},
		{
			name: "valid system",
			caps: SystemCapabilities{
				Virtualization: VirtualizationInfo{Supported: true, Enabled: true},
				Hypervisor:     HypervisorInfo{Installed: true},
				CPU:            CPUInfo{Cores: 8},
				Memory:         MemoryInfo{TotalGB: 32},
				Storage:        StorageInfo{AvailableGB: 500},
			},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.caps.ValidateProviderCapability()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CostCalculator tests
// ---------------------------------------------------------------------------

func testCaps() *SystemCapabilities {
	return &SystemCapabilities{
		CPU:     CPUInfo{Cores: 8, Threads: 16, FrequencyMHz: 3600},
		Memory:  MemoryInfo{TotalGB: 32, AvailableGB: 20},
		Storage: StorageInfo{TotalGB: 500, AvailableGB: 300},
	}
}

func TestNewCostCalculator(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)
	if cc == nil {
		t.Fatal("NewCostCalculator returned nil")
	}
	if cc.profile == nil {
		t.Fatal("profile should not be nil")
	}
}

func TestSetElectricityRate(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)
	cc.SetElectricityRate(0.12)

	profile := cc.GetCostProfile()
	if profile.ElectricityRatePerKWh != 0.12 {
		t.Errorf("ElectricityRatePerKWh = %f, want 0.12", profile.ElectricityRatePerKWh)
	}
	if profile.PowerCostPerHour <= 0 {
		t.Errorf("PowerCostPerHour should be > 0, got %f", profile.PowerCostPerHour)
	}
	if profile.BasePowerWatts <= 0 {
		t.Errorf("BasePowerWatts should be > 0, got %f", profile.BasePowerWatts)
	}
	if profile.LoadPowerWatts <= 0 {
		t.Errorf("LoadPowerWatts should be > 0, got %f", profile.LoadPowerWatts)
	}
}

func TestSetCoolingCost(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)

	cc.SetCoolingCost(true, 0.05)
	if cc.GetCostProfile().CoolingCostPerHour != 0.05 {
		t.Errorf("CoolingCostPerHour = %f, want 0.05", cc.GetCostProfile().CoolingCostPerHour)
	}

	cc.SetCoolingCost(false, 0.05)
	if cc.GetCostProfile().CoolingCostPerHour != 0.0 {
		t.Errorf("CoolingCostPerHour should be 0 when hasExtra=false, got %f", cc.GetCostProfile().CoolingCostPerHour)
	}
}

func TestSetDepreciation(t *testing.T) {
	tests := []struct {
		name         string
		cost         float64
		lifespan     float64
		wantPositive bool
	}{
		{"normal", 2000, 5, true},
		{"zero lifespan", 2000, 0, false},
		{"zero cost", 0, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := testCaps()
			cc := NewCostCalculator(caps)
			cc.SetDepreciation(tt.cost, tt.lifespan)

			dep := cc.GetCostProfile().DepreciationPerHour
			if tt.wantPositive && dep <= 0 {
				t.Errorf("DepreciationPerHour should be > 0, got %f", dep)
			}
			if !tt.wantPositive && dep != 0 {
				t.Errorf("DepreciationPerHour should be 0, got %f", dep)
			}

			// Verify calculation: cost / (lifespan * 365 * 24)
			if tt.wantPositive {
				expected := tt.cost / (tt.lifespan * 365 * 24)
				if math.Abs(dep-expected) > 0.0001 {
					t.Errorf("DepreciationPerHour = %f, want ~%f", dep, expected)
				}
			}
		})
	}
}

func TestCalculateTotalCost(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)

	cc.SetElectricityRate(0.12)
	cc.SetCoolingCost(true, 0.03)
	cc.SetDepreciation(2000, 5)

	total := cc.CalculateTotalCost()
	profile := cc.GetCostProfile()

	expected := profile.PowerCostPerHour + profile.CoolingCostPerHour + profile.DepreciationPerHour
	if math.Abs(total-expected) > 0.0001 {
		t.Errorf("CalculateTotalCost() = %f, want %f", total, expected)
	}
	if total != profile.TotalCostPerHour {
		t.Errorf("profile.TotalCostPerHour = %f, should match returned %f", profile.TotalCostPerHour, total)
	}
}

func TestSuggestPricing(t *testing.T) {
	tests := []struct {
		margin   float64
		wantMode string
	}{
		{10.0, "cost-recovery"},
		{30.0, "competitive"},
		{50.0, "premium"},
		{25.0, "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.wantMode, func(t *testing.T) {
			caps := testCaps()
			cc := NewCostCalculator(caps)
			cc.SetElectricityRate(0.12)
			cc.SetDepreciation(2000, 5)
			cc.CalculateTotalCost()

			pricing := cc.SuggestPricing(tt.margin)
			if pricing == nil {
				t.Fatal("SuggestPricing returned nil")
			}
			if pricing.PriceMode != tt.wantMode {
				t.Errorf("PriceMode = %q, want %q", pricing.PriceMode, tt.wantMode)
			}
			if pricing.Currency != "USD" {
				t.Errorf("Currency = %q, want USD", pricing.Currency)
			}
			if pricing.ProfitMarginPercent != tt.margin {
				t.Errorf("ProfitMarginPercent = %f, want %f", pricing.ProfitMarginPercent, tt.margin)
			}
			if pricing.PerVMPerHour < 0 {
				t.Errorf("PerVMPerHour should be >= 0, got %f", pricing.PerVMPerHour)
			}
		})
	}
}

func TestCompareToMarket(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)

	rates, err := cc.CompareToMarket(0.10)
	if err != nil {
		t.Fatalf("CompareToMarket error: %v", err)
	}
	if rates.Min >= rates.Max {
		t.Errorf("Min (%f) should be < Max (%f)", rates.Min, rates.Max)
	}
	if rates.Count != 25 {
		t.Errorf("Count = %d, want 25", rates.Count)
	}
}

func TestCompareToAWS(t *testing.T) {
	tests := []struct {
		name             string
		cores            int
		memGB            float64
		wantInstanceType string
	}{
		{"large system", 16, 64, "m5.2xlarge"},
		{"medium system", 8, 32, "t3.xlarge"}, // 4 alloc cores, 16GB alloc mem → t3.xlarge
		{"small system", 4, 8, "t3.large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := &SystemCapabilities{
				CPU:     CPUInfo{Cores: tt.cores, Threads: tt.cores * 2},
				Memory:  MemoryInfo{TotalGB: tt.memGB},
				Storage: StorageInfo{TotalGB: 500},
			}
			cc := NewCostCalculator(caps)
			cc.SetElectricityRate(0.12)
			cc.CalculateTotalCost()

			cmp := cc.CompareToAWS(0.10)
			if cmp.InstanceType != tt.wantInstanceType {
				t.Errorf("InstanceType = %q, want %q", cmp.InstanceType, tt.wantInstanceType)
			}
			if cmp.YourPrice != 0.10 {
				t.Errorf("YourPrice = %f, want 0.10", cmp.YourPrice)
			}
		})
	}
}

func TestFormatCostBreakdown(t *testing.T) {
	caps := testCaps()
	cc := NewCostCalculator(caps)
	cc.SetElectricityRate(0.12)
	cc.SetCoolingCost(true, 0.02)
	cc.SetDepreciation(2000, 5)
	cc.CalculateTotalCost()

	output := cc.FormatCostBreakdown()
	if !strings.Contains(output, "Cost Breakdown") {
		t.Error("FormatCostBreakdown should contain 'Cost Breakdown'")
	}
	if !strings.Contains(output, "Power:") {
		t.Error("FormatCostBreakdown should contain 'Power:'")
	}
	if !strings.Contains(output, "Total:") {
		t.Error("FormatCostBreakdown should contain 'Total:'")
	}
}

func TestEstimatePowerFromUsage(t *testing.T) {
	tests := []struct {
		name       string
		cpuPercent float64
		memPercent float64
		hasGPU     bool
		wantMin    float64
	}{
		{"idle no gpu", 0, 0, false, 50},
		{"full load no gpu", 100, 100, false, 50},
		{"with gpu", 50, 50, true, 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			power := EstimatePowerFromUsage(tt.cpuPercent, tt.memPercent, tt.hasGPU)
			if power < tt.wantMin {
				t.Errorf("power = %f, want >= %f", power, tt.wantMin)
			}
		})
	}
}

func TestEstimateCoolingCost(t *testing.T) {
	// No GPU = no extra cooling
	capsNoGPU := testCaps()
	cc := NewCostCalculator(capsNoGPU)
	cost := cc.EstimateCoolingCost(0.12)
	if cost != 0.0 {
		t.Errorf("EstimateCoolingCost without GPU should be 0, got %f", cost)
	}

	// With GPU
	capsGPU := testCaps()
	capsGPU.GPU = &GPUInfo{Model: "RTX 4090"}
	ccGPU := NewCostCalculator(capsGPU)
	costGPU := ccGPU.EstimateCoolingCost(0.12)
	if costGPU <= 0 {
		t.Errorf("EstimateCoolingCost with GPU should be > 0, got %f", costGPU)
	}
}

// ---------------------------------------------------------------------------
// ConfigGenerator tests
// ---------------------------------------------------------------------------

func TestConfigGeneratorSetBaseDir(t *testing.T) {
	cfg := &WizardConfig{Mode: "provider"}
	caps := testCaps()
	gen := NewConfigGenerator(cfg, caps)

	tmpDir := t.TempDir()
	gen.SetBaseDir(tmpDir)

	if gen.GetBaseDir() != tmpDir {
		t.Errorf("GetBaseDir() = %q, want %q", gen.GetBaseDir(), tmpDir)
	}
}

func TestConfigGeneratorGenerate(t *testing.T) {
	tmpDir := t.TempDir()

	caps := testCaps()
	caps.Hypervisor = HypervisorInfo{Type: "hyper-v", Version: "10.0"}
	caps.OS = OSInfo{Platform: "windows", Distribution: "Windows 11"}

	cfg := &WizardConfig{
		Mode: "provider",
		Resources: ResourceAllocation{
			TotalCPUCores:        8,
			AllocatableCores:     4,
			TotalMemoryGB:        32,
			AllocatableMemoryGB:  16,
			TotalStorageGB:       500,
			AllocatableStorageGB: 300,
			MaxVMs:               4,
		},
		Pricing: PricingConfig{
			PerVMPerHour:        0.05,
			Currency:            "USD",
			ProfitMarginPercent: 30,
			PriceMode:           "competitive",
		},
		CostProfile: CostProfile{
			ElectricityRatePerKWh: 0.12,
		},
		NetworkMode: "public",
		Policies: PolicyConfig{
			MaxVMsPerCustomer:   2,
			MaxCPUCoresPerVM:    4,
			MaxMemoryPerVMGB:    8,
			MaxStoragePerVMGB:   100,
			MinContractLeadTime: "1h",
			MaxContractDuration: "720h",
			RequireSignatures:   true,
		},
	}

	gen := NewConfigGenerator(cfg, caps)
	gen.SetBaseDir(tmpDir)

	err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Verify directory structure was created
	expectedDirs := []string{"identity", "data", "logs", "vm-storage"}
	for _, dir := range expectedDirs {
		path := filepath.Join(tmpDir, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q should be a directory", dir)
		}
	}

	// Verify config file exists
	configPath := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.yaml should exist: %v", err)
	}

	// Verify identity files exist
	identityFiles := []string{"private.pem", "public.pem", "did.txt"}
	for _, f := range identityFiles {
		path := filepath.Join(tmpDir, "identity", f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("identity/%s should exist: %v", f, err)
		}
	}

	// Verify DID format
	didBytes, err := os.ReadFile(filepath.Join(tmpDir, "identity", "did.txt"))
	if err != nil {
		t.Fatalf("failed to read did.txt: %v", err)
	}
	didStr := strings.TrimSpace(string(didBytes))
	if !strings.HasPrefix(didStr, "did:soholink:") {
		t.Errorf("DID should start with 'did:soholink:', got %q", didStr)
	}

	// Verify wizard config paths were set
	wc := gen.GetWizardConfig()
	if wc.ConfigPath == "" {
		t.Error("ConfigPath should be set after Generate()")
	}
	if wc.IdentityPath == "" {
		t.Error("IdentityPath should be set after Generate()")
	}
	if wc.DependencyReport == "" {
		t.Error("DependencyReport should be set after Generate()")
	}
}

func TestConfigGeneratorValidateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	caps := testCaps()
	caps.Hypervisor = HypervisorInfo{Type: "hyper-v", Version: "10.0"}
	caps.OS = OSInfo{Platform: "windows"}

	cfg := &WizardConfig{
		Mode:        "provider",
		NetworkMode: "public",
		Resources:   ResourceAllocation{MaxVMs: 2},
		Pricing:     PricingConfig{Currency: "USD"},
		Policies:    PolicyConfig{MinContractLeadTime: "1h", MaxContractDuration: "720h"},
	}

	gen := NewConfigGenerator(cfg, caps)
	gen.SetBaseDir(tmpDir)

	// Generate first
	if err := gen.Generate(); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Validate should succeed
	if err := gen.ValidateConfig(); err != nil {
		t.Errorf("ValidateConfig() should succeed after Generate(), got: %v", err)
	}
}

func TestConfigGeneratorGenerateSummary(t *testing.T) {
	tmpDir := t.TempDir()
	caps := testCaps()
	caps.Hypervisor = HypervisorInfo{Type: "hyper-v", Version: "10.0"}
	caps.OS = OSInfo{Platform: "windows"}

	cfg := &WizardConfig{
		Mode:        "provider",
		NetworkMode: "public",
		Resources:   ResourceAllocation{MaxVMs: 2, TotalCPUCores: 8, AllocatableCores: 4},
		Pricing:     PricingConfig{PerVMPerHour: 0.05, Currency: "USD", PriceMode: "competitive", ProfitMarginPercent: 30},
		Policies:    PolicyConfig{MinContractLeadTime: "1h", MaxContractDuration: "720h"},
	}

	gen := NewConfigGenerator(cfg, caps)
	gen.SetBaseDir(tmpDir)

	if err := gen.Generate(); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	summary := gen.GenerateSummary()
	if !strings.Contains(summary, "PROVIDER") {
		t.Error("summary should contain mode")
	}
	if !strings.Contains(summary, "Configuration Summary") {
		t.Error("summary should contain 'Configuration Summary'")
	}
}
