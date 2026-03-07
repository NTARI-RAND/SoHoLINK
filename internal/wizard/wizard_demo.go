package wizard

import (
	"fmt"
	"log"
)

// DemoWizardFlow demonstrates the complete wizard flow without UI.
// This shows how all the components work together.
func DemoWizardFlow() error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  SoHoLINK Deployment Wizard Demo")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Step 1: System Detection
	fmt.Println("Step 1: Detecting System Capabilities...")
	fmt.Println()

	caps, err := DetectSystemCapabilities()
	if err != nil {
		return fmt.Errorf("detection failed: %w", err)
	}

	printSystemCapabilities(caps)

	// Validate provider capability
	if err := caps.ValidateProviderCapability(); err != nil {
		log.Printf("⚠️  Warning: %v", err)
		fmt.Println("\nYour system may not be suitable as a provider.")
		fmt.Println("Continue anyway for demo purposes...")
	}

	// Step 2: Calculate Resources
	fmt.Println("\nStep 2: Calculating Available Resources...")
	fmt.Println()

	alloc := caps.CalculateAvailableResources()
	printResourceAllocation(alloc)

	// Step 3: Cost Calculation
	fmt.Println("\nStep 3: Calculating Operating Costs...")
	fmt.Println()

	costCalc := NewCostCalculator(caps)

	// Set electricity rate (user would input this)
	electricityRate := 0.12 // $0.12/kWh
	costCalc.SetElectricityRate(electricityRate)

	// Estimate cooling cost
	hasExtraCooling := caps.GPU != nil
	coolingCost := 0.0
	if hasExtraCooling {
		coolingCost = costCalc.EstimateCoolingCost(electricityRate)
	}
	costCalc.SetCoolingCost(hasExtraCooling, coolingCost)

	// Set depreciation (user would input this)
	hardwareCost := 3500.0     // $3,500 hardware
	lifespan := 5.0            // 5 year lifespan
	costCalc.SetDepreciation(hardwareCost, lifespan)

	// Calculate total cost
	_ = costCalc.CalculateTotalCost()

	fmt.Println(costCalc.FormatCostBreakdown())

	// Step 4: Pricing Suggestions
	fmt.Println("\nStep 4: Suggesting Pricing...")
	fmt.Println()

	// Suggest competitive pricing (30% margin)
	pricing := costCalc.SuggestPricing(30.0)

	fmt.Printf("💰 Suggested Price: $%.3f/hour ($%.2f/month per VM)\n",
		pricing.PerVMPerHour,
		pricing.PerVMPerHour*24*30)
	fmt.Printf("   Profit Margin: %.0f%% (%s tier)\n\n",
		pricing.ProfitMarginPercent,
		pricing.PriceMode)

	// Compare to AWS
	awsComparison := costCalc.CompareToAWS(pricing.PerVMPerHour)
	fmt.Printf("📊 AWS Comparison:\n")
	fmt.Printf("   AWS %s: $%.2f/hour\n", awsComparison.InstanceType, awsComparison.AWSPrice)
	fmt.Printf("   Your Price: $%.3f/hour\n", awsComparison.YourPrice)
	fmt.Printf("   Savings: %.1f%% cheaper than AWS! 🎉\n\n",
		awsComparison.SavingsPercent)

	// Profit estimate
	fmt.Println(costCalc.FormatProfitEstimate(pricing))

	// Step 5: Create Wizard Config
	fmt.Println("\nStep 5: Creating Configuration...")
	fmt.Println()

	wizardCfg := &WizardConfig{
		Mode:      "provider",
		Resources: *alloc,
		Pricing:   *pricing,
		CostProfile: *costCalc.GetCostProfile(),
		NetworkMode: "public",
		AutoAccept:  true,
		Policies: PolicyConfig{
			MaxVMsPerCustomer:      4,
			MaxCPUCoresPerVM:       4,
			MaxMemoryPerVMGB:       8,
			MaxStoragePerVMGB:      100,
			MinContractLeadTime:    "24h",
			MaxContractDuration:    "720h", // 30 days
			RequireSignatures:      true,
			RateLimitingEnabled:    true,
		},
	}

	// Step 6: Generate Configuration Files
	fmt.Println("Step 6: Generating Configuration Files...")
	fmt.Println()

	configGen := NewConfigGenerator(wizardCfg, caps)

	if err := configGen.Generate(); err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	fmt.Printf("✅ Configuration generated successfully!\n\n")
	fmt.Printf("   Base Directory: %s\n", configGen.GetBaseDir())
	fmt.Printf("   Config File:    %s\n", wizardCfg.ConfigPath)
	fmt.Printf("   Identity:       %s\n", wizardCfg.IdentityPath)
	fmt.Printf("   Report:         %s\n\n", wizardCfg.DependencyReport)

	// Step 7: Validate Configuration
	fmt.Println("Step 7: Validating Configuration...")
	fmt.Println()

	if err := configGen.ValidateConfig(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Println("✅ Configuration validated successfully!")

	// Step 8: Generate Dependency Report
	fmt.Println("Step 8: Dependency Report Generated...")
	fmt.Println()

	tracker := NewDependencyTracker(caps, &wizardCfg.CostProfile, &wizardCfg.Pricing)
	report := tracker.GenerateReport()

	allOK, issues := report.ValidateDependencies()
	if allOK {
		fmt.Println("✅ All required dependencies met!")
	} else {
		fmt.Println("⚠️  Some dependencies need attention:")
		for _, issue := range issues {
			fmt.Printf("   • %s\n", issue)
		}
	}

	fmt.Println()
	fmt.Printf("📄 Reports saved:\n")
	fmt.Printf("   • JSON:     %s/dependencies.json\n", configGen.GetBaseDir())
	fmt.Printf("   • Markdown: %s/dependencies.md\n", configGen.GetBaseDir())
	fmt.Printf("   • HTML:     %s/dependencies.html\n\n", configGen.GetBaseDir())

	// Final Summary
	fmt.Println("\n" + configGen.GenerateSummary())

	return nil
}

// printSystemCapabilities prints detected system capabilities.
func printSystemCapabilities(caps *SystemCapabilities) {
	fmt.Printf("✅ Operating System: %s\n", caps.OS.Distribution)
	fmt.Printf("   Architecture: %s\n", caps.OS.Architecture)
	fmt.Printf("   Kernel: %s\n\n", caps.OS.Kernel)

	fmt.Printf("✅ CPU: %s\n", caps.CPU.Model)
	fmt.Printf("   Cores: %d physical, %d threads\n", caps.CPU.Cores, caps.CPU.Threads)
	fmt.Printf("   Frequency: %.0f MHz\n", caps.CPU.FrequencyMHz)
	fmt.Printf("   Virtualization: %s\n\n", caps.CPU.VirtualizationTech)

	fmt.Printf("✅ Memory: %.1f GB total\n", caps.Memory.TotalGB)
	fmt.Printf("   Available: %.1f GB (%.1f%% used)\n\n",
		caps.Memory.AvailableGB,
		caps.Memory.UsedPercent)

	fmt.Printf("✅ Storage: %.1f GB total\n", caps.Storage.TotalGB)
	fmt.Printf("   Available: %.1f GB (%.1f%% used)\n",
		caps.Storage.AvailableGB,
		caps.Storage.UsedPercent)
	fmt.Printf("   Type: %s\n", caps.Storage.DriveType)
	fmt.Printf("   Filesystem: %s\n\n", caps.Storage.Filesystem)

	if caps.GPU != nil {
		fmt.Printf("✅ GPU: %s %s\n", caps.GPU.Vendor, caps.GPU.Model)
		if caps.GPU.DriverVersion != "" {
			fmt.Printf("   Driver: %s\n", caps.GPU.DriverVersion)
		}
		fmt.Println()
	}

	fmt.Printf("✅ Hypervisor: %s\n", caps.Hypervisor.Type)
	if caps.Hypervisor.Installed {
		fmt.Printf("   Status: Installed and %s\n",
			func() string {
				if caps.Hypervisor.Enabled {
					return "enabled ✅"
				}
				return "disabled ⚠️"
			}())
		if caps.Hypervisor.Version != "" {
			fmt.Printf("   Version: %s\n", caps.Hypervisor.Version)
		}
		if len(caps.Hypervisor.Features) > 0 {
			fmt.Printf("   Features: %v\n", caps.Hypervisor.Features)
		}
	} else {
		fmt.Printf("   Status: Not installed ❌\n")
	}
	fmt.Println()

	fmt.Printf("✅ Virtualization: %s\n", caps.Virtualization.Technology)
	if caps.Virtualization.Supported {
		fmt.Printf("   Status: Supported and %s\n",
			func() string {
				if caps.Virtualization.Enabled {
					return "enabled ✅"
				}
				return "not enabled ⚠️"
			}())
	} else {
		fmt.Printf("   Status: Not supported ❌\n")
	}
	fmt.Println()

	fmt.Printf("✅ Network: %d interface(s)\n", len(caps.Network.Interfaces))
	fmt.Printf("   Estimated Bandwidth: %d Mbps\n", caps.Network.BandwidthMbps)
	fmt.Printf("   Firewall: %v\n", caps.Network.FirewallEnabled)
}

// printResourceAllocation prints resource allocation.
func printResourceAllocation(alloc *ResourceAllocation) {
	fmt.Println("Your system can offer to the marketplace:")
	fmt.Println()
	fmt.Printf("  • CPU:     %d cores allocatable (keeping %d for host)\n",
		alloc.AllocatableCores,
		alloc.ReservedCores)
	fmt.Printf("  • Memory:  %d GB allocatable (keeping %d GB for host)\n",
		alloc.AllocatableMemoryGB,
		alloc.ReservedMemoryGB)
	fmt.Printf("  • Storage: %d GB allocatable (keeping %d GB for host)\n",
		alloc.AllocatableStorageGB,
		alloc.ReservedStorageGB)
	fmt.Println()
	fmt.Printf("  Maximum VMs: %d\n", alloc.MaxVMs)
	fmt.Printf("  (Typical: 4 cores, 4 GB RAM, 100 GB storage per VM)\n")

	if alloc.HasGPU {
		fmt.Println()
		fmt.Println("  🎮 GPU detected! Advanced users can offer GPU compute.")
	}
}
