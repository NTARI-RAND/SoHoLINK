package wizard

import (
	"fmt"
	"math"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// CostCalculator calculates operating costs and suggests pricing.
type CostCalculator struct {
	capabilities *SystemCapabilities
	profile      *CostProfile
}

// NewCostCalculator creates a new cost calculator.
func NewCostCalculator(caps *SystemCapabilities) *CostCalculator {
	return &CostCalculator{
		capabilities: caps,
		profile:      &CostProfile{},
	}
}

// EstimatePowerDraw estimates system power draw in watts.
// Returns idle and load power estimates.
func (c *CostCalculator) EstimatePowerDraw() (idle, load float64) {
	// Try platform-specific measurement first
	measuredIdle, measuredLoad, err := MeasurePowerDraw()
	if err == nil && measuredLoad > 0 {
		return measuredIdle, measuredLoad
	}

	// Fall back to estimation based on components
	idle = c.estimateIdlePower()
	load = c.estimateLoadPower()

	return idle, load
}

// estimateIdlePower estimates idle power consumption.
func (c *CostCalculator) estimateIdlePower() float64 {
	power := 0.0

	// Motherboard + chipset: 30-50W
	power += 40.0

	// CPU idle (rough estimate: 15% of TDP)
	cpuTDP := c.estimateCPUTDP()
	power += cpuTDP * 0.15

	// RAM: ~3W per 8GB
	ramPower := c.capabilities.Memory.TotalGB / 8.0 * 3.0
	power += ramPower

	// Storage: 2-5W per drive
	power += 3.0

	// Fans: 5-10W
	power += 7.0

	// GPU idle (if present): 10-30W
	if c.capabilities.GPU != nil {
		power += 20.0
	}

	return power
}

// estimateLoadPower estimates power under full load.
func (c *CostCalculator) estimateLoadPower() float64 {
	power := 0.0

	// Motherboard + chipset: 40-60W
	power += 50.0

	// CPU under load (TDP)
	cpuTDP := c.estimateCPUTDP()
	power += cpuTDP

	// RAM: ~3W per 8GB
	ramPower := c.capabilities.Memory.TotalGB / 8.0 * 3.0
	power += ramPower

	// Storage under load: 5-8W
	power += 7.0

	// Fans under load: 10-15W
	power += 12.0

	// GPU under load (if present)
	if c.capabilities.GPU != nil {
		gpuPower := c.estimateGPUPower()
		power += gpuPower
	}

	return power
}

// estimateCPUTDP estimates CPU TDP (Thermal Design Power).
func (c *CostCalculator) estimateCPUTDP() float64 {
	cores := c.capabilities.CPU.Cores
	threads := c.capabilities.CPU.Threads

	// Desktop CPUs: ~15-20W per core
	// High-end CPUs: ~10-12W per core (better efficiency)
	// Server CPUs: ~8-10W per core

	if threads >= 16 && cores >= 8 {
		// High-end desktop or server (e.g., Ryzen 9, Threadripper, Xeon)
		return float64(cores) * 10.0
	} else if cores >= 6 {
		// Mid to high-end desktop
		return float64(cores) * 15.0
	} else {
		// Low to mid-range
		return float64(cores) * 12.0
	}
}

// estimateGPUPower estimates GPU power consumption.
func (c *CostCalculator) estimateGPUPower() float64 {
	if c.capabilities.GPU == nil {
		return 0.0
	}

	model := c.capabilities.GPU.Model

	// Rough estimates based on common GPUs
	// High-end (RTX 3090, RTX 4090, etc.): 300-450W
	// Mid-range (RTX 3060, RX 6700): 150-220W
	// Low-end: 75-120W

	modelLower := model
	if modelLower == "" {
		return 200.0 // Default assumption
	}

	// High-end NVIDIA
	if contains(modelLower, "3090") || contains(modelLower, "4090") || contains(modelLower, "Titan") {
		return 350.0
	}
	if contains(modelLower, "3080") || contains(modelLower, "4080") {
		return 320.0
	}

	// Mid-range NVIDIA
	if contains(modelLower, "3070") || contains(modelLower, "4070") {
		return 220.0
	}
	if contains(modelLower, "3060") || contains(modelLower, "4060") {
		return 170.0
	}

	// High-end AMD
	if contains(modelLower, "6900") || contains(modelLower, "7900") {
		return 300.0
	}

	// Default
	return 200.0
}

// contains checks if string contains substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr))
}

// SetElectricityRate sets the electricity rate per kWh.
func (c *CostCalculator) SetElectricityRate(ratePerKWh float64) {
	c.profile.ElectricityRatePerKWh = ratePerKWh

	// Recalculate power costs
	idle, load := c.EstimatePowerDraw()
	c.profile.BasePowerWatts = idle
	c.profile.LoadPowerWatts = load

	// Calculate cost per hour (use load power for VMs)
	wattsToKW := load / 1000.0
	c.profile.PowerCostPerHour = wattsToKW * ratePerKWh
}

// SetCoolingCost sets additional cooling cost per hour.
func (c *CostCalculator) SetCoolingCost(hasExtra bool, costPerHour float64) {
	c.profile.HasExtraCooling = hasExtra
	if hasExtra {
		c.profile.CoolingCostPerHour = costPerHour
	} else {
		c.profile.CoolingCostPerHour = 0.0
	}
}

// EstimateCoolingCost estimates cooling cost based on system config.
func (c *CostCalculator) EstimateCoolingCost(electricityRate float64) float64 {
	if c.capabilities.GPU == nil {
		return 0.0 // No extra cooling needed
	}

	// Estimate heat output from GPU
	gpuPower := c.estimateGPUPower()

	// Convert watts to BTU/hr (1 watt = 3.412 BTU/hr)
	heatBTU := gpuPower * 3.412

	// Assume AC efficiency: 10 BTU/watt (typical window AC)
	acWatts := heatBTU / 10.0

	// Cost per hour
	costPerHour := (acWatts / 1000.0) * electricityRate

	return costPerHour
}

// SetDepreciation sets hardware depreciation parameters.
func (c *CostCalculator) SetDepreciation(hardwareCost, lifespanYears float64) {
	c.profile.HardwareCost = hardwareCost
	c.profile.HardwareLifespanYears = lifespanYears

	if lifespanYears > 0 {
		// Calculate hourly depreciation
		hoursInYear := 24.0 * 365.0
		totalHours := hoursInYear * lifespanYears
		c.profile.DepreciationPerHour = hardwareCost / totalHours
	} else {
		c.profile.DepreciationPerHour = 0.0
	}
}

// CalculateTotalCost calculates total operating cost per hour.
func (c *CostCalculator) CalculateTotalCost() float64 {
	total := c.profile.PowerCostPerHour +
		c.profile.CoolingCostPerHour +
		c.profile.DepreciationPerHour

	c.profile.TotalCostPerHour = total
	return total
}

// GetCostProfile returns the current cost profile.
func (c *CostCalculator) GetCostProfile() *CostProfile {
	return c.profile
}

// SuggestPricing suggests pricing based on cost and profit margin.
func (c *CostCalculator) SuggestPricing(profitMarginPercent float64) *PricingConfig {
	// Calculate cost per VM
	alloc := c.capabilities.CalculateAvailableResources()
	if alloc.MaxVMs == 0 {
		alloc.MaxVMs = 1 // Avoid division by zero
	}

	costPerVM := c.profile.TotalCostPerHour / float64(alloc.MaxVMs)

	// Add profit margin
	pricePerVM := costPerVM * (1.0 + profitMarginPercent/100.0)

	// Determine price mode
	mode := "custom"
	switch profitMarginPercent {
	case 10.0:
		mode = "cost-recovery"
	case 30.0:
		mode = "competitive"
	case 50.0:
		mode = "premium"
	}

	return &PricingConfig{
		PerVMPerHour:        roundToTwoDecimals(pricePerVM),
		Currency:            "USD",
		ProfitMarginPercent: profitMarginPercent,
		PriceMode:           mode,
	}
}

// roundToTwoDecimals rounds a float to 2 decimal places.
func roundToTwoDecimals(val float64) float64 {
	return math.Round(val*100) / 100
}

// CompareToMarket compares pricing to market rates (placeholder).
// In real implementation, this would query the federated network.
func (c *CostCalculator) CompareToMarket(yourPrice float64) (*MarketRates, error) {
	// Placeholder: return synthetic market data
	// Real implementation would query federated nodes

	return &MarketRates{
		Min:       yourPrice * 0.8,
		P25:       yourPrice * 0.9,
		Median:    yourPrice,
		P75:       yourPrice * 1.1,
		Max:       yourPrice * 1.3,
		Count:     25,
		Timestamp: time.Now(),
	}, nil
}

// CompareToAWS compares pricing to AWS equivalent.
func (c *CostCalculator) CompareToAWS(yourPrice float64) *AWSComparison {
	// Estimate AWS equivalent based on specs
	alloc := c.capabilities.CalculateAvailableResources()

	// Typical AWS pricing for similar specs
	// t3.xlarge: 4 vCPU, 16 GB RAM = ~$0.17/hour
	// t3.2xlarge: 8 vCPU, 32 GB RAM = ~$0.33/hour
	// m5.2xlarge: 8 vCPU, 32 GB RAM = ~$0.38/hour

	var awsPrice float64
	var instanceType string

	if alloc.AllocatableCores >= 8 && alloc.AllocatableMemoryGB >= 32 {
		instanceType = "m5.2xlarge"
		awsPrice = 0.38
	} else if alloc.AllocatableCores >= 4 && alloc.AllocatableMemoryGB >= 16 {
		instanceType = "t3.xlarge"
		awsPrice = 0.17
	} else {
		instanceType = "t3.large"
		awsPrice = 0.08
	}

	savingsPercent := 0.0
	if awsPrice > 0 {
		savingsPercent = ((awsPrice - yourPrice) / awsPrice) * 100.0
	}

	return &AWSComparison{
		InstanceType:   instanceType,
		AWSPrice:       awsPrice,
		YourPrice:      yourPrice,
		SavingsPercent: roundToTwoDecimals(savingsPercent),
	}
}

// MeasureCurrentUsage measures current CPU and memory usage.
func MeasureCurrentUsage() (cpuPercent, memPercent float64, err error) {
	// CPU usage
	cpuPercentages, err := cpu.Percent(time.Second, false)
	if err != nil {
		return 0, 0, err
	}
	if len(cpuPercentages) > 0 {
		cpuPercent = cpuPercentages[0]
	}

	// Memory usage
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, err
	}
	memPercent = vmem.UsedPercent

	return cpuPercent, memPercent, nil
}

// EstimatePowerFromUsage estimates power draw from current usage.
func EstimatePowerFromUsage(cpuPercent, memPercent float64, hasGPU bool) float64 {
	// Base power (motherboard, fans, drives)
	basePower := 50.0

	// CPU power (rough estimate from TDP)
	// Assume average desktop CPU TDP: 125W
	cpuTDP := 125.0
	cpuPower := (cpuPercent / 100.0) * cpuTDP

	// Memory power (~3W per 8GB, affected by usage)
	// Assume 16GB RAM
	memPower := (memPercent / 100.0) * 6.0

	// GPU power (if present and in use)
	gpuPower := 0.0
	if hasGPU {
		// Assume some GPU usage
		gpuPower = 100.0 // Conservative estimate
	}

	return basePower + cpuPower + memPower + gpuPower
}

// FormatCostBreakdown returns a formatted cost breakdown string.
func (c *CostCalculator) FormatCostBreakdown() string {
	profile := c.profile

	return fmt.Sprintf(`Cost Breakdown (per hour):
  Power:        $%.3f (%.0fW × $%.3f/kWh)
  Cooling:      $%.3f
  Depreciation: $%.3f
  ───────────────────
  Total:        $%.3f/hour = $%.2f/month
`,
		profile.PowerCostPerHour,
		profile.LoadPowerWatts,
		profile.ElectricityRatePerKWh,
		profile.CoolingCostPerHour,
		profile.DepreciationPerHour,
		profile.TotalCostPerHour,
		profile.TotalCostPerHour*24*30,
	)
}

// FormatProfitEstimate returns a formatted profit estimate.
func (c *CostCalculator) FormatProfitEstimate(pricing *PricingConfig) string {
	alloc := c.capabilities.CalculateAvailableResources()

	monthlyRevenue := pricing.PerVMPerHour * float64(alloc.MaxVMs) * 24 * 30
	monthlyCost := c.profile.TotalCostPerHour * 24 * 30
	monthlyProfit := monthlyRevenue - monthlyCost

	return fmt.Sprintf(`Estimated Monthly Financials:
  Revenue:  $%.2f (%d VMs × $%.3f/hour × 720 hours)
  Costs:    $%.2f
  ─────────────────
  Profit:   $%.2f (%.0f%% margin)
`,
		monthlyRevenue,
		alloc.MaxVMs,
		pricing.PerVMPerHour,
		monthlyCost,
		monthlyProfit,
		pricing.ProfitMarginPercent,
	)
}
