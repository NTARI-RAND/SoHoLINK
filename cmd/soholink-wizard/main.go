//go:build gui

package main

import (
	"fmt"
	"image/color"
	"log"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/wizard"
)

func main() {
	// Create Fyne application
	myApp := app.New()
	myApp.Settings().SetTheme(&wizardTheme{})

	// Create main window
	myWindow := myApp.NewWindow("SoHoLINK Setup Wizard")
	myWindow.Resize(fyne.NewSize(800, 600))
	myWindow.CenterOnScreen()

	// Show welcome screen
	showWelcomeScreen(myWindow)

	myWindow.ShowAndRun()
}

func showWelcomeScreen(w fyne.Window) {
	// Create welcome content
	title := widget.NewLabelWithStyle(
		"Welcome to SoHoLINK!",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	description := widget.NewLabel(
		"Transform your spare compute into income by joining the federated cloud marketplace.\n\n" +
		"This wizard will guide you through:\n" +
		"• System detection and capability assessment\n" +
		"• Cost calculation (electricity, cooling, depreciation)\n" +
		"• Competitive pricing suggestions\n" +
		"• Identity creation (decentralized)\n" +
		"• Network configuration\n" +
		"• Complete setup in under 15 minutes\n",
	)
	description.Wrapping = fyne.TextWrapWord

	// Start button
	startButton := widget.NewButton("Start Setup Wizard", func() {
		runWizardFlow(w)
	})

	// Exit button
	exitButton := widget.NewButton("Exit", func() {
		os.Exit(0)
	})

	// Layout
	content := container.NewVBox(
		title,
		widget.NewSeparator(),
		description,
		widget.NewSeparator(),
		container.NewHBox(
			startButton,
			exitButton,
		),
	)

	w.SetContent(container.NewPadded(content))
}

func runWizardFlow(w fyne.Window) {
	// Create progress dialog
	progress := widget.NewProgressBarInfinite()
	statusLabel := widget.NewLabel("Detecting system capabilities...")

	progressContent := container.NewVBox(
		statusLabel,
		progress,
	)

	w.SetContent(container.NewPadded(progressContent))

	// Run wizard in background
	go func() {
		// Step 1: Detect system
		statusLabel.SetText("Detecting system capabilities...")

		caps, err := wizard.DetectSystemCapabilities()
		if err != nil {
			showError(w, "System Detection Failed", err.Error())
			return
		}

		// Validate provider capability
		if err := caps.ValidateProviderCapability(); err != nil {
			showWarning(w, "System Requirements",
				fmt.Sprintf("⚠️  Warning: %v\n\nYou can continue, but your system may not be suitable as a provider.", err))
		}

		// Step 2: Calculate resources
		statusLabel.SetText("Calculating available resources...")
		alloc := caps.CalculateAvailableResources()

		// Step 3: Show configuration screen
		showConfigurationScreen(w, caps, alloc)
	}()
}

func showConfigurationScreen(w fyne.Window, caps *wizard.SystemCapabilities, alloc *wizard.ResourceAllocation) {
	// Create configuration form
	title := widget.NewLabelWithStyle(
		"Configuration",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// System summary
	summary := widget.NewLabel(fmt.Sprintf(
		"System Detected:\n"+
		"• CPU: %s (%d cores)\n"+
		"• RAM: %.1f GB\n"+
		"• Storage: %.1f GB available\n"+
		"• Hypervisor: %s\n"+
		"• Max VMs: %d\n",
		caps.CPU.Model,
		caps.CPU.Cores,
		caps.Memory.TotalGB,
		caps.Storage.AvailableGB,
		caps.Hypervisor.Type,
		alloc.MaxVMs,
	))

	// Electricity rate input
	electricityEntry := widget.NewEntry()
	electricityEntry.SetPlaceHolder("0.12")
	electricityEntry.SetText("0.12")
	electricityForm := container.NewVBox(
		widget.NewLabel("Electricity Rate ($/kWh):"),
		electricityEntry,
		widget.NewLabel("Find this on your electricity bill"),
	)

	// Cooling checkbox
	coolingCheck := widget.NewCheck("I have extra cooling (GPU rack, server closet, etc.)", nil)

	// Hardware cost (optional)
	hardwareCostEntry := widget.NewEntry()
	hardwareCostEntry.SetPlaceHolder("3500")
	lifespanEntry := widget.NewEntry()
	lifespanEntry.SetPlaceHolder("5")
	lifespanEntry.SetText("5")

	depreciationForm := container.NewVBox(
		widget.NewLabel("Hardware Cost (optional):"),
		hardwareCostEntry,
		widget.NewLabel("Expected Lifespan (years):"),
		lifespanEntry,
	)

	// Continue button
	continueButton := widget.NewButton("Calculate Costs & Continue", func() {
		// Parse inputs
		electricityRate := 0.12
		fmt.Sscanf(electricityEntry.Text, "%f", &electricityRate)

		hardwareCost := 0.0
		fmt.Sscanf(hardwareCostEntry.Text, "%f", &hardwareCost)

		lifespan := 5.0
		fmt.Sscanf(lifespanEntry.Text, "%f", &lifespan)

		// Calculate costs
		showCostCalculation(w, caps, alloc, electricityRate, coolingCheck.Checked, hardwareCost, lifespan)
	})

	// Layout
	content := container.NewVBox(
		title,
		widget.NewSeparator(),
		summary,
		widget.NewSeparator(),
		electricityForm,
		coolingCheck,
		depreciationForm,
		widget.NewSeparator(),
		continueButton,
	)

	w.SetContent(container.NewPadded(content))
}

func showCostCalculation(w fyne.Window, caps *wizard.SystemCapabilities, alloc *wizard.ResourceAllocation,
	electricityRate float64, hasExtraCooling bool, hardwareCost, lifespan float64) {

	// Calculate costs
	costCalc := wizard.NewCostCalculator(caps)
	costCalc.SetElectricityRate(electricityRate)

	coolingCost := 0.0
	if hasExtraCooling {
		coolingCost = costCalc.EstimateCoolingCost(electricityRate)
	}
	costCalc.SetCoolingCost(hasExtraCooling, coolingCost)

	if hardwareCost > 0 && lifespan > 0 {
		costCalc.SetDepreciation(hardwareCost, lifespan)
	}

	totalCost := costCalc.CalculateTotalCost()

	// Suggest pricing (30% margin)
	pricing := costCalc.SuggestPricing(30.0)

	// Show results
	title := widget.NewLabelWithStyle(
		"Cost Analysis Complete!",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	costBreakdown := widget.NewLabel(costCalc.FormatCostBreakdown())
	profitEstimate := widget.NewLabel(costCalc.FormatProfitEstimate(pricing))

	// AWS comparison
	awsComp := costCalc.CompareToAWS(pricing.PerVMPerHour)
	comparison := widget.NewLabel(fmt.Sprintf(
		"AWS Comparison:\n"+
		"AWS %s: $%.2f/hour\n"+
		"Your Price: $%.3f/hour\n"+
		"You're %.1f%% cheaper than AWS! 🎉",
		awsComp.InstanceType,
		awsComp.AWSPrice,
		awsComp.YourPrice,
		awsComp.SavingsPercent,
	))

	// Generate configuration
	generateButton := widget.NewButton("Generate Configuration & Finish", func() {
		generateConfiguration(w, caps, alloc, costCalc.GetCostProfile(), pricing)
	})

	// Layout
	content := container.NewVBox(
		title,
		widget.NewSeparator(),
		costBreakdown,
		widget.NewSeparator(),
		profitEstimate,
		widget.NewSeparator(),
		comparison,
		widget.NewSeparator(),
		generateButton,
	)

	w.SetContent(container.NewPadded(container.NewVScroll(content)))
}

func generateConfiguration(w fyne.Window, caps *wizard.SystemCapabilities, alloc *wizard.ResourceAllocation,
	costProfile *wizard.CostProfile, pricing *wizard.PricingConfig) {

	// Show progress
	progress := widget.NewProgressBarInfinite()
	statusLabel := widget.NewLabel("Generating configuration...")

	w.SetContent(container.NewPadded(container.NewVBox(
		statusLabel,
		progress,
	)))

	// Generate in background
	go func() {
		// Create wizard config
		wizardCfg := &wizard.WizardConfig{
			Mode:        "provider",
			Resources:   *alloc,
			Pricing:     *pricing,
			CostProfile: *costProfile,
			NetworkMode: "public",
			AutoAccept:  true,
			Policies: wizard.PolicyConfig{
				MaxVMsPerCustomer:      4,
				MaxCPUCoresPerVM:       4,
				MaxMemoryPerVMGB:       8,
				MaxStoragePerVMGB:      100,
				MinContractLeadTime:    "24h",
				MaxContractDuration:    "720h",
				RequireSignatures:      true,
				RateLimitingEnabled:    true,
			},
		}

		// Generate configuration
		configGen := wizard.NewConfigGenerator(wizardCfg, caps)

		if err := configGen.Generate(); err != nil {
			showError(w, "Configuration Failed", err.Error())
			return
		}

		// Validate
		if err := configGen.ValidateConfig(); err != nil {
			showError(w, "Validation Failed", err.Error())
			return
		}

		// Show success
		showSuccess(w, configGen)
	}()
}

func showSuccess(w fyne.Window, configGen *wizard.ConfigGenerator) {
	title := widget.NewLabelWithStyle(
		"🎉 Setup Complete!",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	summary := widget.NewLabel(configGen.GenerateSummary())

	instructions := widget.NewLabel(
		"\nNext Steps:\n\n" +
		"1. Your configuration has been saved\n" +
		"2. Firewall ports 1812-1813 need to be opened\n" +
		"3. Review the dependency report\n" +
		"4. Start SoHoLINK service to begin earning!\n",
	)

	finishButton := widget.NewButton("Finish", func() {
		os.Exit(0)
	})

	content := container.NewVBox(
		title,
		widget.NewSeparator(),
		summary,
		widget.NewSeparator(),
		instructions,
		widget.NewSeparator(),
		finishButton,
	)

	w.SetContent(container.NewPadded(container.NewVScroll(content)))
}

func showError(w fyne.Window, title, message string) {
	errorLabel := widget.NewLabel(fmt.Sprintf("❌ %s\n\n%s", title, message))
	exitButton := widget.NewButton("Exit", func() {
		os.Exit(1)
	})

	w.SetContent(container.NewPadded(container.NewVBox(
		errorLabel,
		exitButton,
	)))
}

func showWarning(w fyne.Window, title, message string) {
	// For now, just log the warning and continue
	log.Printf("Warning: %s - %s", title, message)
}

// wizardTheme customizes the Fyne theme
type wizardTheme struct{}

func (t *wizardTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(name, variant)
}

func (t *wizardTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *wizardTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *wizardTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}
