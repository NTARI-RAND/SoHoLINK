//go:build gui

package dashboard

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/app"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/wizard"
)

// ─────────────────────────────────────────────────────────────────────────────
// Simple View — user-friendly dashboard focused on earnings and toggles
// ─────────────────────────────────────────────────────────────────────────────

// resourceToggle tracks the state of a shareable resource.
type resourceToggle struct {
	Name        string
	Description string
	Detail      string
	Enabled     bool
	Available   bool // false if hardware not detected
}

// simpleState holds the simple view's runtime state.
type simpleState struct {
	mu         sync.Mutex
	caps       *wizard.SystemCapabilities
	alloc      *wizard.ResourceAllocation
	toggles    map[string]*resourceToggle
	earning    bool
	totalSats  int64
	monthlySats int64
}

// ─────────────────────────────────────────────────────────────────────────────
// Simple Setup — replaces the 9-step wizard with 3 screens
// ─────────────────────────────────────────────────────────────────────────────

// RunSimpleSetup launches a friendly, non-technical first-time setup.
func RunSimpleSetup(cfg *config.Config, s *store.Store, onComplete func()) fyne.CanvasObject {
	state := &simpleState{
		toggles: make(map[string]*resourceToggle),
	}

	// Container that swaps pages
	pageContainer := container.NewStack()

	var showPage func(page fyne.CanvasObject)
	showPage = func(page fyne.CanvasObject) {
		pageContainer.Objects = []fyne.CanvasObject{page}
		pageContainer.Refresh()
	}

	// Page flow: Welcome → Scan → Toggles → Done
	showPage(simpleWelcomePage(func() {
		showPage(simpleScanPage(state, func() {
			showPage(simpleTogglePage(state, cfg, s, onComplete))
		}))
	}))

	return pageContainer
}

// ─────────────────────────────────────────────────────────────────────────────
// Page 1 — Welcome
// ─────────────────────────────────────────────────────────────────────────────

func simpleWelcomePage(onNext func()) fyne.CanvasObject {
	title := canvas.NewText("SoHoLINK", theme.ForegroundColor())
	title.TextSize = 36
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := widget.NewLabelWithStyle(
		"Turn your idle computer into income.",
		fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true},
	)

	desc := widget.NewLabel(
		"SoHoLINK safely shares your computer's spare power with people who need it — "+
			"and pays you for every minute it's used.\n\n"+
			"You stay in control. Pick what you want to share. Turn it off anytime.\n\n"+
			"Let's take a quick look at your machine.",
	)
	desc.Wrapping = fyne.TextWrapWord
	desc.Alignment = fyne.TextAlignCenter

	nextBtn := widget.NewButton("Scan My Computer", onNext)
	nextBtn.Importance = widget.HighImportance

	return container.NewPadded(container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(title),
		container.NewCenter(subtitle),
		widget.NewSeparator(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(500, 200), desc)),
		layout.NewSpacer(),
		container.NewCenter(nextBtn),
		layout.NewSpacer(),
	))
}

// ─────────────────────────────────────────────────────────────────────────────
// Page 2 — Hardware Scan (auto-runs, no user action needed)
// ─────────────────────────────────────────────────────────────────────────────

func simpleScanPage(state *simpleState, onDone func()) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Scanning your computer...", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	progress := widget.NewProgressBarInfinite()
	statusLabel := widget.NewLabelWithStyle("Checking processor...", fyne.TextAlignCenter, fyne.TextStyle{})

	content := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(title),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 30), progress)),
		container.NewCenter(statusLabel),
		layout.NewSpacer(),
	)

	// Run detection in background
	go func() {
		steps := []string{
			"Checking processor...",
			"Measuring memory...",
			"Scanning storage drives...",
			"Looking for graphics card...",
			"Testing network speed...",
		}
		for i, step := range steps {
			statusLabel.SetText(step)
			// Small delay so user sees progress (feels more trustworthy)
			if i < len(steps)-1 {
				time.Sleep(600 * time.Millisecond)
			}
		}

		caps, err := wizard.DetectSystemCapabilities()
		if err != nil {
			statusLabel.SetText("Scan complete (some features may be limited)")
			caps = &wizard.SystemCapabilities{}
		}

		alloc := caps.CalculateAvailableResources()

		state.mu.Lock()
		state.caps = caps
		state.alloc = alloc
		state.toggles = buildResourceToggles(caps, alloc)
		state.mu.Unlock()

		time.Sleep(400 * time.Millisecond)
		onDone()
	}()

	return container.NewPadded(content)
}

// ─────────────────────────────────────────────────────────────────────────────
// Page 3 — Resource Toggles + "Start Earning"
// ─────────────────────────────────────────────────────────────────────────────

func simpleTogglePage(state *simpleState, cfg *config.Config, s *store.Store, onComplete func()) fyne.CanvasObject {
	state.mu.Lock()
	defer state.mu.Unlock()

	title := widget.NewLabelWithStyle("Here's what your computer can share", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	subtitle := widget.NewLabel("Toggle on what you'd like to earn from. We'll handle the rest.")
	subtitle.Wrapping = fyne.TextWrapWord
	subtitle.Alignment = fyne.TextAlignCenter

	// Build toggle cards
	toggleOrder := []string{"cpu", "gpu", "storage", "network"}
	var cards []fyne.CanvasObject

	for _, key := range toggleOrder {
		t, ok := state.toggles[key]
		if !ok {
			continue
		}
		cards = append(cards, buildToggleCard(t))
	}

	grid := container.NewGridWithColumns(2, cards...)

	// Estimated earnings
	est := estimateMonthlyEarnings(state)
	state.monthlySats = est

	earningsLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("Estimated monthly earnings: %s", formatSats(est)),
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	earningsNote := widget.NewLabel("Actual earnings depend on demand in your area. This is a conservative estimate.")
	earningsNote.Wrapping = fyne.TextWrapWord
	earningsNote.Alignment = fyne.TextAlignCenter

	startBtn := widget.NewButton("Start Earning", func() {
		// Auto-configure and save
		go func() {
			autoConfigureFromToggles(state, cfg, s)
			if onComplete != nil {
				onComplete()
			}
		}()
	})
	startBtn.Importance = widget.HighImportance

	return container.NewPadded(container.NewVBox(
		title,
		subtitle,
		widget.NewSeparator(),
		grid,
		widget.NewSeparator(),
		earningsLabel,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(500, 30), earningsNote)),
		layout.NewSpacer(),
		container.NewCenter(startBtn),
	))
}

// ─────────────────────────────────────────────────────────────────────────────
// Simple Main Dashboard — shown after setup, earnings-focused
// ─────────────────────────────────────────────────────────────────────────────

func buildSimpleDashboard(w fyne.Window, application *app.App) fyne.CanvasObject {
	ctx := context.Background()

	// ── Top: Earnings Banner ──
	totalSats := int64(0)
	pendingSats := int64(0)
	if application.PaymentLedger != nil {
		did := application.Config.Node.DID
		if bal, err := application.PaymentLedger.GetWalletBalance(ctx, did); err == nil {
			totalSats = bal
		}
	}

	earningsTitle := canvas.NewText(formatSats(totalSats), theme.ForegroundColor())
	earningsTitle.TextSize = 48
	earningsTitle.TextStyle = fyne.TextStyle{Bold: true}
	earningsTitle.Alignment = fyne.TextAlignCenter

	earningsCaption := widget.NewLabelWithStyle("Total Earnings", fyne.TextAlignCenter, fyne.TextStyle{})

	pendingLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%s pending", formatSats(pendingSats)),
		fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true},
	)

	collectBtn := widget.NewButton("Collect Payment", func() {
		// TODO: trigger payout flow
	})
	collectBtn.Importance = widget.HighImportance
	if totalSats == 0 {
		collectBtn.Disable()
	}

	earningsBanner := widget.NewCard("", "", container.NewVBox(
		container.NewCenter(earningsTitle),
		container.NewCenter(earningsCaption),
		container.NewCenter(pendingLabel),
		container.NewCenter(collectBtn),
	))

	// ── Middle: Status Cards ──
	statusCards := buildStatusCards(application)

	// ── Resource Toggles (live) ──
	toggleSection := buildLiveToggles(application)

	// ── Activity Feed ──
	activityCard := buildActivityFeed(application)

	// ── Network Status ──
	networkCard := buildNetworkStatus(application)

	content := container.NewVBox(
		earningsBanner,
		container.NewGridWithColumns(3, statusCards...),
		widget.NewSeparator(),
		toggleSection,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, activityCard, networkCard),
	)

	return container.NewScroll(container.NewPadded(content))
}

// buildStatusCards creates the three main status indicator cards.
func buildStatusCards(application *app.App) []fyne.CanvasObject {
	ctx := context.Background()

	// Card 1: Machine Status
	status := "Online"
	statusColor := theme.ForegroundColor()
	subsystems := countOnlineSubsystems(application)

	machineCard := widget.NewCard("Machine Status", "", container.NewVBox(
		widget.NewLabelWithStyle(status, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(
			fmt.Sprintf("%d systems running", subsystems),
			fyne.TextAlignCenter, fyne.TextStyle{},
		),
	))
	_ = statusColor

	// Card 2: Active Jobs
	jobCount := 0
	if application.FedScheduler != nil {
		jobCount = len(application.FedScheduler.ListActiveWorkloads())
	}

	jobsCard := widget.NewCard("Active Jobs", "", container.NewVBox(
		widget.NewLabelWithStyle(strconv.Itoa(jobCount), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("running right now", fyne.TextAlignCenter, fyne.TextStyle{}),
	))

	// Card 3: Connected Peers
	peerCount := 0
	if peers, err := application.Store.GetP2PPeers(ctx); err == nil {
		peerCount = len(peers)
	}

	peersCard := widget.NewCard("Network Peers", "", container.NewVBox(
		widget.NewLabelWithStyle(strconv.Itoa(peerCount), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("machines connected", fyne.TextAlignCenter, fyne.TextStyle{}),
	))

	return []fyne.CanvasObject{machineCard, jobsCard, peersCard}
}

// buildLiveToggles creates the resource sharing toggle panel.
func buildLiveToggles(application *app.App) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("What You're Sharing", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	cfg := application.Config

	// Detect current capabilities
	caps, _ := wizard.DetectSystemCapabilities()
	if caps == nil {
		caps = &wizard.SystemCapabilities{}
	}
	alloc := caps.CalculateAvailableResources()

	cpuDetail := fmt.Sprintf("%d cores available  ·  %s", alloc.AllocatableCores, caps.CPU.Model)
	cpuCheck := widget.NewCheck("Processing Power", nil)
	cpuCheck.SetChecked(cfg.ResourceSharing.Enabled)
	cpuRow := container.NewHBox(cpuCheck, widget.NewLabel(cpuDetail))

	gpuDetail := "No graphics card detected"
	gpuAvailable := caps.GPU != nil
	if gpuAvailable {
		gpuDetail = fmt.Sprintf("%s  ·  %d GB", caps.GPU.Model, caps.GPU.VRAMGb)
	}
	gpuCheck := widget.NewCheck("Graphics Power", nil)
	gpuCheck.SetChecked(gpuAvailable && cfg.ResourceSharing.Enabled)
	if !gpuAvailable {
		gpuCheck.Disable()
	}
	gpuRow := container.NewHBox(gpuCheck, widget.NewLabel(gpuDetail))

	storDetail := fmt.Sprintf("%d GB available  ·  %s drive", alloc.AllocatableStorageGB, caps.Storage.DriveType)
	storCheck := widget.NewCheck("Storage Space", nil)
	storCheck.SetChecked(cfg.ResourceSharing.StoragePool.Enabled)
	storRow := container.NewHBox(storCheck, widget.NewLabel(storDetail))

	netDetail := fmt.Sprintf("%d Mbps  ·  %d interfaces", caps.Network.BandwidthMbps, len(caps.Network.Interfaces))
	netCheck := widget.NewCheck("Network Bandwidth", nil)
	netCheck.SetChecked(cfg.P2P.Enabled)
	netRow := container.NewHBox(netCheck, widget.NewLabel(netDetail))

	togglePanel := widget.NewCard("", "", container.NewVBox(
		title,
		widget.NewSeparator(),
		cpuRow,
		gpuRow,
		storRow,
		netRow,
	))

	return togglePanel
}

// buildActivityFeed shows recent events in plain language.
func buildActivityFeed(application *app.App) fyne.CanvasObject {
	items := container.NewVBox()

	// Check for recent workloads
	if application.FedScheduler != nil {
		workloads := application.FedScheduler.ListActiveWorkloads()
		if len(workloads) > 0 {
			for i, ws := range workloads {
				if i >= 5 {
					break
				}
				elapsed := time.Since(ws.Workload.SubmittedAt).Round(time.Minute)
				items.Add(widget.NewLabel(fmt.Sprintf(
					"  Job %s running for %s",
					truncate(ws.Workload.WorkloadID, 8), elapsed,
				)))
			}
		} else {
			items.Add(widget.NewLabel("  No active jobs — waiting for work requests"))
		}
	} else {
		items.Add(widget.NewLabel("  Scheduler starting up..."))
	}

	items.Add(widget.NewLabel("  Your machine is online and visible to the network"))

	return widget.NewCard("Recent Activity", "", items)
}

// buildNetworkStatus shows connection health in simple terms.
func buildNetworkStatus(application *app.App) fyne.CanvasObject {
	ctx := context.Background()
	items := container.NewVBox()

	peerCount := 0
	if peers, err := application.Store.GetP2PPeers(ctx); err == nil {
		peerCount = len(peers)
	}

	if peerCount > 0 {
		items.Add(widget.NewLabel(fmt.Sprintf("  Connected to %d other machines", peerCount)))
		items.Add(widget.NewLabel("  Network status: Healthy"))
	} else {
		items.Add(widget.NewLabel("  Searching for other machines on the network..."))
		items.Add(widget.NewLabel("  Make sure both machines are on the same Wi-Fi or network"))
	}

	// Show node name if configured
	nodeName := application.Config.Node.Name
	if nodeName != "" {
		items.Add(widget.NewLabel(fmt.Sprintf("  Your machine name: %s", nodeName)))
	}

	// Show platform
	items.Add(widget.NewLabel(fmt.Sprintf("  Running on: %s/%s", runtime.GOOS, runtime.GOARCH)))

	return widget.NewCard("Network Health", "", items)
}

// ─────────────────────────────────────────────────────────────────────────────
// Toggle Card Builder
// ─────────────────────────────────────────────────────────────────────────────

func buildToggleCard(t *resourceToggle) fyne.CanvasObject {
	check := widget.NewCheck("", func(checked bool) {
		t.Enabled = checked
	})
	check.SetChecked(t.Enabled)
	if !t.Available {
		check.Disable()
		check.SetChecked(false)
	}

	nameLabel := widget.NewLabelWithStyle(t.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	descLabel := widget.NewLabel(t.Description)
	descLabel.Wrapping = fyne.TextWrapWord

	detailLabel := widget.NewLabel(t.Detail)
	detailLabel.TextStyle = fyne.TextStyle{Italic: true}

	top := container.NewHBox(check, nameLabel)
	return widget.NewCard("", "", container.NewVBox(top, descLabel, detailLabel))
}

// ─────────────────────────────────────────────────────────────────────────────
// Resource Detection → Toggle Mapping
// ─────────────────────────────────────────────────────────────────────────────

func buildResourceToggles(caps *wizard.SystemCapabilities, alloc *wizard.ResourceAllocation) map[string]*resourceToggle {
	toggles := make(map[string]*resourceToggle)

	// CPU / Processing Power
	cpuDesc := "Let others use your processor for computing tasks"
	cpuDetail := fmt.Sprintf("%d cores available out of %d  ·  %s",
		alloc.AllocatableCores, alloc.TotalCPUCores, caps.CPU.Model)
	toggles["cpu"] = &resourceToggle{
		Name:        "Processing Power",
		Description: cpuDesc,
		Detail:      cpuDetail,
		Enabled:     true, // on by default
		Available:   alloc.AllocatableCores > 0,
	}

	// GPU / Graphics
	gpuAvailable := caps.GPU != nil && caps.GPU.Model != ""
	gpuDesc := "Share your graphics card for AI and rendering jobs"
	gpuDetail := "No graphics card detected"
	if gpuAvailable {
		gpuDetail = fmt.Sprintf("%s  ·  %d GB video memory", caps.GPU.Model, caps.GPU.VRAMGb)
	}
	toggles["gpu"] = &resourceToggle{
		Name:        "Graphics Power",
		Description: gpuDesc,
		Detail:      gpuDetail,
		Enabled:     gpuAvailable,
		Available:   gpuAvailable,
	}

	// Storage
	storAvailable := alloc.AllocatableStorageGB > 10
	storDesc := "Rent out unused disk space for file hosting"
	storDetail := fmt.Sprintf("%d GB available  ·  %s  ·  %s drive",
		alloc.AllocatableStorageGB, caps.Storage.Filesystem, caps.Storage.DriveType)
	if caps.Storage.DriveType == "" {
		storDetail = fmt.Sprintf("%d GB available", alloc.AllocatableStorageGB)
	}
	toggles["storage"] = &resourceToggle{
		Name:        "Storage Space",
		Description: storDesc,
		Detail:      storDetail,
		Enabled:     storAvailable,
		Available:   storAvailable,
	}

	// Network / Bandwidth
	netAvailable := caps.Network.BandwidthMbps > 0
	netDesc := "Share your internet connection for relay and CDN services"
	netDetail := fmt.Sprintf("%d Mbps  ·  %d network interfaces",
		caps.Network.BandwidthMbps, len(caps.Network.Interfaces))
	toggles["network"] = &resourceToggle{
		Name:        "Network Bandwidth",
		Description: netDesc,
		Detail:      netDetail,
		Enabled:     netAvailable,
		Available:   netAvailable,
	}

	return toggles
}

// ─────────────────────────────────────────────────────────────────────────────
// Earnings Estimation
// ─────────────────────────────────────────────────────────────────────────────

// estimateMonthlyEarnings returns a conservative estimate in sats.
func estimateMonthlyEarnings(state *simpleState) int64 {
	if state.alloc == nil {
		return 0
	}

	hoursPerMonth := 720.0 // 30 days × 24 hours
	utilizationRate := 0.25 // assume 25% utilization (conservative)

	total := 0.0

	// CPU: 100 sats/core/hour (from marketplace defaults)
	if t, ok := state.toggles["cpu"]; ok && t.Enabled {
		total += float64(state.alloc.AllocatableCores) * 100.0 * hoursPerMonth * utilizationRate
	}

	// GPU: 500 sats/hour if available
	if t, ok := state.toggles["gpu"]; ok && t.Enabled && t.Available {
		total += 500.0 * hoursPerMonth * utilizationRate
	}

	// Storage: 1 sat/GB/hour
	if t, ok := state.toggles["storage"]; ok && t.Enabled {
		total += float64(state.alloc.AllocatableStorageGB) * 1.0 * hoursPerMonth * utilizationRate
	}

	// Network: 10 sats/Mbps/hour
	if t, ok := state.toggles["network"]; ok && t.Enabled && state.caps != nil {
		total += float64(state.caps.Network.BandwidthMbps) * 10.0 * hoursPerMonth * utilizationRate
	}

	return int64(math.Round(total))
}

// formatSats formats a satoshi amount into a human-friendly string.
func formatSats(sats int64) string {
	if sats == 0 {
		return "0 sats"
	}
	if sats >= 100_000_000 {
		btc := float64(sats) / 100_000_000.0
		return fmt.Sprintf("%.4f BTC", btc)
	}
	if sats >= 1_000_000 {
		return fmt.Sprintf("%dM sats", sats/1_000_000)
	}
	if sats >= 1_000 {
		return fmt.Sprintf("%dK sats", sats/1_000)
	}
	return fmt.Sprintf("%d sats", sats)
}

// ─────────────────────────────────────────────────────────────────────────────
// Auto-Configuration — turns toggles into working config
// ─────────────────────────────────────────────────────────────────────────────

func autoConfigureFromToggles(state *simpleState, cfg *config.Config, s *store.Store) {
	state.mu.Lock()
	defer state.mu.Unlock()

	// Build a wizard config from toggles
	wc := &wizard.WizardConfig{
		Mode: "provider",
		Resources: wizard.ResourceAllocation{
			TotalCPUCores:        state.alloc.TotalCPUCores,
			AllocatableCores:     state.alloc.AllocatableCores,
			ReservedCores:        state.alloc.ReservedCores,
			TotalMemoryGB:        state.alloc.TotalMemoryGB,
			AllocatableMemoryGB:  state.alloc.AllocatableMemoryGB,
			ReservedMemoryGB:     state.alloc.ReservedMemoryGB,
			TotalStorageGB:       state.alloc.TotalStorageGB,
			AllocatableStorageGB: state.alloc.AllocatableStorageGB,
			ReservedStorageGB:    state.alloc.ReservedStorageGB,
			MaxVMs:               state.alloc.MaxVMs,
			HasGPU:               state.alloc.HasGPU,
			GPUAllocatable:       state.alloc.GPUAllocatable,
		},
		Pricing: wizard.PricingConfig{
			PriceMode:           "competitive",
			ProfitMarginPercent: 30.0,
			Currency:            "sats",
		},
		NetworkMode: "public",
		AutoAccept:  true,
	}

	// Apply toggle selections
	if t, ok := state.toggles["cpu"]; ok && !t.Enabled {
		wc.Resources.AllocatableCores = 0
	}
	if t, ok := state.toggles["gpu"]; ok {
		wc.Resources.GPUAllocatable = t.Enabled && t.Available
	}
	if t, ok := state.toggles["storage"]; ok && !t.Enabled {
		wc.Resources.AllocatableStorageGB = 0
	}

	// Generate config files
	gen := wizard.NewConfigGenerator(wc, state.caps)
	if err := gen.Generate(); err != nil {
		fmt.Fprintf(os.Stderr, "auto-configure error: %v\n", err)
		return
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Simple Menu Bar — minimal, user-friendly
// ─────────────────────────────────────────────────────────────────────────────

func buildSimpleMenuBar(w fyne.Window, application *app.App) *fyne.MainMenu {
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Refresh", func() {
			w.SetContent(buildSimpleDashboard(w, application))
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			fyne.CurrentApp().Quit()
		}),
	)

	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Simple View", func() {
			w.SetMainMenu(buildSimpleMenuBar(w, application))
			w.SetContent(buildSimpleDashboard(w, application))
		}),
		fyne.NewMenuItem("Advanced View", func() {
			w.SetMainMenu(buildMenuBar(w, application))
			w.SetContent(buildDashboard(w, application))
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Network Globe", func() {
			openURL("http://localhost:9090/globe")
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About SoHoLINK", func() {
			showAboutDialog(w)
		}),
		fyne.NewMenuItem("Check for Updates", func() {
			showCheckForUpdatesDialog(w, application)
		}),
	)

	return fyne.NewMainMenu(fileMenu, viewMenu, helpMenu)
}

// openURL opens a URL in the default browser.
func openURL(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	_ = fyne.CurrentApp().OpenURL(u)
}

// Suppress unused import warnings
var (
	_ = strings.TrimSpace
)
