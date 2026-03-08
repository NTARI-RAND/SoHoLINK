//go:build gui

// Package dashboard is the single unified GUI for SoHoLINK.
//
// Entry points:
//   - RunSetupWizard — first-time node configuration wizard
//   - RunDashboard   — full operator dashboard (shown after setup)
//
// Build with: go build -tags gui ./cmd/soholink/
package dashboard

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	fyneApp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/app"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/wizard"
)

// appID uniquely identifies this Fyne application.
const appID = "org.ntari.soholink"

// windowSize is the default window dimensions.
var windowSize = fyne.NewSize(1200, 760)

// ─────────────────────────────────────────────────────────────────────────────
// Entry points
// ─────────────────────────────────────────────────────────────────────────────

// RunSetupWizard launches the first-time setup wizard.
// Called by the CLI when no configuration exists.
func RunSetupWizard(cfg *config.Config, s *store.Store) {
	a := getOrCreateApp()
	w := a.NewWindow("SoHoLINK — Setup Wizard")
	w.Resize(fyne.NewSize(900, 640))
	w.CenterOnScreen()
	w.SetFixedSize(false)

	// onComplete is called when the user clicks "Open Dashboard →".
	// It loads the freshly written config and transitions the same window
	// to the full dashboard — no process restart required.
	onComplete := func() {
		go func() {
			newCfg, err := config.Load("")
			if err != nil {
				dialog.ShowError(fmt.Errorf("setup complete, but could not load configuration: %w\n\nPlease restart SoHoLINK.", err), w)
				return
			}
			application, err := app.New(newCfg)
			if err != nil {
				dialog.ShowError(fmt.Errorf("services could not start: %w\n\nPlease restart SoHoLINK.", err), w)
				return
			}
			// Transition the wizard window to the full operator dashboard.
			w.SetTitle("SoHoLINK")
			w.Resize(windowSize)
			w.SetMainMenu(buildMenuBar(w, application))
			w.SetContent(buildDashboard(w, application))
		}()
	}

	showWizard(w, cfg, s, onComplete)
	w.ShowAndRun()
}

// RunDashboard launches the full operator dashboard.
// Called by the CLI after a node is configured and running.
func RunDashboard(application *app.App) {
	a := getOrCreateApp()
	w := a.NewWindow("SoHoLINK")
	w.Resize(windowSize)
	w.CenterOnScreen()

	// If application is nil (first run without full init), show wizard
	if application == nil {
		showWizard(w, nil, nil, nil)
		w.ShowAndRun()
		return
	}

	w.SetMainMenu(buildMenuBar(w, application))
	w.SetContent(buildDashboard(w, application))
	w.ShowAndRun()
}

// getOrCreateApp returns the current Fyne application, creating one if needed.
func getOrCreateApp() fyne.App {
	if a := fyne.CurrentApp(); a != nil {
		return a
	}
	a := fyneApp.NewWithID(appID)
	a.Settings().SetTheme(theme.DarkTheme())
	return a
}

// ─────────────────────────────────────────────────────────────────────────────
// Menu bar
// ─────────────────────────────────────────────────────────────────────────────

func buildMenuBar(w fyne.Window, application *app.App) *fyne.MainMenu {
	// ── File ──────────────────────────────────────────────────────────────
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Refresh Dashboard", func() {
			w.SetContent(buildDashboard(w, application))
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			fyne.CurrentApp().Quit()
		}),
	)

	// ── Settings ──────────────────────────────────────────────────────────
	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Node Configuration…", func() {
			showNodeSettingsDialog(w, application)
		}),
		fyne.NewMenuItem("Pricing & Costs…", func() {
			showPricingSettingsDialog(w)
		}),
		fyne.NewMenuItem("Network…", func() {
			showNetworkSettingsDialog(w, application)
		}),
		fyne.NewMenuItem("Federation & Marketplace…", func() {
			showFederationSettingsDialog(w, application)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Payment Processors…", func() {
			showPaymentSettingsDialog(w, application)
		}),
		fyne.NewMenuItem("K8s Edge Clusters…", func() {
			showK8sEdgesDialog(w)
		}),
		fyne.NewMenuItem("IPFS Storage…", func() {
			showIPFSSettingsDialog(w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Provisioning Limits…", func() {
			showProvisioningLimitsDialog(w, application)
		}),
	)

	// ── View ──────────────────────────────────────────────────────────────
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Open Globe in Browser", func() {
			u, _ := url.Parse("http://localhost:9090/globe")
			_ = fyne.CurrentApp().OpenURL(u)
		}),
		fyne.NewMenuItem("Open HTTP API in Browser", func() {
			addr := "http://localhost:8080"
			if application != nil && application.Config.ResourceSharing.HTTPAPIAddress != "" {
				addr = "http://" + application.Config.ResourceSharing.HTTPAPIAddress
			}
			u, _ := url.Parse(addr)
			_ = fyne.CurrentApp().OpenURL(u)
		}),
	)

	// ── Help ──────────────────────────────────────────────────────────────
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About SoHoLINK", func() {
			showAboutDialog(w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for Updates…", func() {
			showCheckForUpdatesDialog(w, application)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Documentation", func() {
			u, _ := url.Parse("https://ntari.org/soholink/docs")
			_ = fyne.CurrentApp().OpenURL(u)
		}),
	)

	return fyne.NewMainMenu(fileMenu, settingsMenu, viewMenu, helpMenu)
}

// ─────────────────────────────────────────────────────────────────────────────
// Dashboard — tabbed main content
// ─────────────────────────────────────────────────────────────────────────────

func buildDashboard(w fyne.Window, application *app.App) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		buildOverviewTab(w, application),
		buildHardwareTab(w),
		buildOrchestrationTab(w, application),
		buildStorageTab(w, application),
		buildBillingTab(w, application),
		buildMarketplaceTab(w, application),
		buildUsersTab(w, application),
		buildPoliciesTab(w, application),
		buildLogsTab(w, application),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 1 — Overview
// ─────────────────────────────────────────────────────────────────────────────

func buildOverviewTab(w fyne.Window, application *app.App) *container.TabItem {
	ctx := context.Background()

	cfg := application.Config

	// Node identity card
	nodeName := cfg.Node.Name
	if nodeName == "" {
		nodeName = "(unnamed)"
	}
	did := cfg.Node.DID
	if did == "" {
		did = "(not configured)"
	}

	identityCard := widget.NewCard("Node Identity", "",
		container.NewVBox(
			labelPair("Name", nodeName),
			labelPair("DID", truncate(did, 40)),
			labelPair("Platform", runtime.GOOS+"/"+runtime.GOARCH),
			labelPair("Auth address", cfg.Radius.AuthAddress),
			labelPair("Acct address", cfg.Radius.AcctAddress),
		),
	)

	// Quick stats card — read live from store
	userCount := 0
	peerCount := 0
	if peers, err := application.Store.GetP2PPeers(ctx); err == nil {
		peerCount = len(peers)
	}
	if n, err := application.Store.ActiveUserCount(ctx); err == nil {
		userCount = n
	}

	subsystemsOnline := countOnlineSubsystems(application)

	statsCard := widget.NewCard("Quick Stats", "",
		container.NewGridWithColumns(3,
			statBlock("Active Users", strconv.Itoa(userCount)),
			statBlock("Known Peers", strconv.Itoa(peerCount)),
			statBlock("Subsystems", strconv.Itoa(subsystemsOnline)),
		),
	)

	// Subsystem health checklist
	healthCard := widget.NewCard("Subsystem Health", "",
		container.NewVBox(
			healthRow("RADIUS server", true),
			healthRow("Resource sharing", cfg.ResourceSharing.Enabled),
			healthRow("Orchestration", cfg.Orchestration.Enabled && application.FedScheduler != nil),
			healthRow("Storage pool", cfg.ResourceSharing.StoragePool.Enabled && application.StoragePool != nil),
			healthRow("Payment ledger", application.PaymentLedger != nil),
			healthRow("P2P mesh", cfg.P2P.Enabled && application.P2PNetwork != nil),
			healthRow("Rental engine", application.RentalEngine != nil),
			healthRow("SLA monitor", application.SLAMonitor != nil),
			healthRow("Blockchain", application.LocalChain != nil),
		),
	)

	// Data dir
	dataCard := widget.NewCard("Storage", "",
		container.NewVBox(
			labelPair("Data directory", cfg.Storage.BasePath),
			labelPair("Accounting dir", cfg.AccountingDir()),
			labelPair("Policy dir", cfg.Policy.Directory),
		),
	)

	// Wallet balance tile (reads live from payment ledger when available)
	var walletCard *widget.Card
	if application.PaymentLedger != nil {
		balanceSats, err := application.PaymentLedger.GetWalletBalance(ctx, did)
		balStr := "unavailable"
		if err == nil {
			balStr = fmt.Sprintf("%d sats", balanceSats)
		}
		walletCard = widget.NewCard("Wallet Balance", "Prepaid sats — deducted at purchase",
			container.NewVBox(
				labelPair("Balance", balStr),
			),
		)
	} else {
		walletCard = widget.NewCard("Wallet Balance", "",
			widget.NewLabel("Payment ledger not initialized."),
		)
	}

	// Legal compliance notice — always visible to node operator.
	complianceCard := widget.NewCard("Legal Compliance", "Required before accepting workloads",
		container.NewVBox(
			widget.NewLabel("• Prohibited content (CSAM, malware, botnet tools) is blocked and reported automatically."),
			widget.NewLabel("• All workload purchases require a signed manifest — stored permanently for audit."),
			widget.NewLabel("• CSAM is reported to NCMEC within 24 hours as required by 18 U.S.C. § 2258A."),
			widget.NewLabel("• Acceptable Use Policy: https://ntari.org/aup"),
			widget.NewLabel("• Legal/DMCA: legal@soholink.network"),
		),
	)

	content := container.NewVBox(
		statsCard,
		container.NewGridWithColumns(2, identityCard, healthCard),
		container.NewGridWithColumns(2, walletCard, dataCard),
		complianceCard,
	)

	// Show update-available banner when a newer release has been detected.
	if application != nil && application.Updater != nil {
		if rel := application.Updater.LatestRelease(); rel != nil {
			notice := widget.NewLabel("⬆  Update available: " + rel.TagName +
				" — use Help ▸ Check for Updates to install.")
			notice.Wrapping = fyne.TextWrapWord
			content.Add(widget.NewCard("", "", notice))
		}
	}

	return container.NewTabItem("Overview", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 2 — Hardware
// ─────────────────────────────────────────────────────────────────────────────

func buildHardwareTab(w fyne.Window) *container.TabItem {
	status := widget.NewLabel("Detecting hardware…")
	progress := widget.NewProgressBarInfinite()
	loading := container.NewVBox(status, progress)

	// Run detection in background
	go func() {
		caps, err := wizard.DetectSystemCapabilities()
		if err != nil {
			status.SetText("Detection failed: " + err.Error())
			progress.Hide()
			return
		}

		alloc := caps.CalculateAvailableResources()

		progress.Hide()

		// Build results content
		gpuStr := "None detected"
		if caps.GPU != nil {
			gpuStr = fmt.Sprintf("%s %s", caps.GPU.Vendor, caps.GPU.Model)
		}
		hypStr := caps.Hypervisor.Type
		if !caps.Hypervisor.Installed {
			hypStr += " (not installed)"
		}

		cpuCard := widget.NewCard("CPU", "",
			container.NewVBox(
				labelPair("Model", caps.CPU.Model),
				labelPair("Vendor", caps.CPU.Vendor),
				labelPair("Physical cores", strconv.Itoa(caps.CPU.Cores)),
				labelPair("Logical threads", strconv.Itoa(caps.CPU.Threads)),
				labelPair("Frequency", fmt.Sprintf("%.0f MHz", caps.CPU.FrequencyMHz)),
				labelPair("Virtualization", caps.CPU.VirtualizationTech),
			),
		)

		memCard := widget.NewCard("Memory", "",
			container.NewVBox(
				labelPair("Total", fmt.Sprintf("%.1f GB", caps.Memory.TotalGB)),
				labelPair("Available", fmt.Sprintf("%.1f GB", caps.Memory.AvailableGB)),
				labelPair("Used", fmt.Sprintf("%.1f%%", caps.Memory.UsedPercent)),
				labelPair("Allocatable", fmt.Sprintf("%d GB", alloc.AllocatableMemoryGB)),
				labelPair("Reserved (host)", fmt.Sprintf("%d GB", alloc.ReservedMemoryGB)),
			),
		)

		storCard := widget.NewCard("Storage", "",
			container.NewVBox(
				labelPair("Total", fmt.Sprintf("%.1f GB", caps.Storage.TotalGB)),
				labelPair("Available", fmt.Sprintf("%.1f GB", caps.Storage.AvailableGB)),
				labelPair("Type", caps.Storage.DriveType),
				labelPair("Filesystem", caps.Storage.Filesystem),
				labelPair("Allocatable", fmt.Sprintf("%d GB", alloc.AllocatableStorageGB)),
			),
		)

		sysCard := widget.NewCard("System", "",
			container.NewVBox(
				labelPair("Platform", caps.OS.Platform),
				labelPair("Distribution", caps.OS.Distribution),
				labelPair("Architecture", caps.OS.Architecture),
				labelPair("Kernel", caps.OS.Kernel),
				labelPair("GPU", gpuStr),
				labelPair("Hypervisor", hypStr),
			),
		)

		allocCard := widget.NewCard("Marketplace Allocation", "Resources available to tenants",
			container.NewVBox(
				labelPair("Allocatable CPU cores", strconv.Itoa(alloc.AllocatableCores)),
				labelPair("Allocatable RAM", fmt.Sprintf("%d GB", alloc.AllocatableMemoryGB)),
				labelPair("Allocatable storage", fmt.Sprintf("%d GB", alloc.AllocatableStorageGB)),
				labelPair("Max concurrent VMs", strconv.Itoa(alloc.MaxVMs)),
				labelPair("GPU available", boolStr(alloc.HasGPU)),
			),
		)

		netCard := widget.NewCard("Network", "",
			container.NewVBox(
				labelPair("Interfaces", strconv.Itoa(len(caps.Network.Interfaces))),
				labelPair("Est. bandwidth", fmt.Sprintf("%d Mbps", caps.Network.BandwidthMbps)),
				labelPair("Firewall", boolStr(caps.Network.FirewallEnabled)),
			),
		)

		grid := container.NewGridWithColumns(2, cpuCard, memCard, storCard, sysCard, netCard, allocCard)

		loading.Objects = []fyne.CanvasObject{container.NewScroll(container.NewPadded(grid))}
		loading.Refresh()
	}()

	return container.NewTabItem("Hardware", container.NewPadded(loading))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 3 — Orchestration
// ─────────────────────────────────────────────────────────────────────────────

func buildOrchestrationTab(w fyne.Window, application *app.App) *container.TabItem {
	if application.FedScheduler == nil {
		return container.NewTabItem("Orchestration", disabledPanel(
			"Orchestration is disabled",
			"Enable it in config: orchestration.enabled = true",
		))
	}

	sched := application.FedScheduler

	// Workloads table — ListActiveWorkloads returns a lock-safe snapshot.
	type wlRow struct{ id, owner, status, replicas, region string }
	var rows []wlRow

	for _, ws := range sched.ListActiveWorkloads() {
		region := ""
		if len(ws.Workload.Constraints.Regions) > 0 {
			region = strings.Join(ws.Workload.Constraints.Regions, ", ")
		}
		rows = append(rows, wlRow{
			id:       truncate(ws.Workload.WorkloadID, 20),
			owner:    truncate(ws.Workload.OwnerDID, 20),
			status:   ws.Workload.Status,
			replicas: strconv.Itoa(len(ws.Placements)),
			region:   region,
		})
	}

	headers := []string{"Workload ID", "Owner DID", "Status", "Replicas", "Regions"}
	tbl := widget.NewTable(
		func() (int, int) { return len(rows) + 1, len(headers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			lbl := cell.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(headers[id.Col])
				return
			}
			r := rows[id.Row-1]
			switch id.Col {
			case 0:
				lbl.SetText(r.id)
			case 1:
				lbl.SetText(r.owner)
			case 2:
				lbl.SetText(r.status)
			case 3:
				lbl.SetText(r.replicas)
			case 4:
				lbl.SetText(r.region)
			}
		},
	)
	for i, w := range []float32{180, 180, 80, 70, 140} {
		tbl.SetColumnWidth(i, w)
	}

	summary := widget.NewCard("Scheduler Status", "",
		container.NewVBox(
			labelPair("Active workloads", strconv.Itoa(len(rows))),
		),
	)

	refreshBtn := widget.NewButton("Refresh", func() {
		w.SetContent(buildDashboard(w, application))
	})

	content := container.NewVBox(
		summary,
		container.NewHBox(refreshBtn),
		widget.NewLabel("Active Workloads:"),
		tbl,
	)

	return container.NewTabItem("Orchestration", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 4 — Storage
// ─────────────────────────────────────────────────────────────────────────────

func buildStorageTab(w fyne.Window, application *app.App) *container.TabItem {
	ctx := context.Background()
	_ = ctx

	// Local storage pool status
	localStatus := "Disabled"
	if application.StoragePool != nil {
		localStatus = "Running"
	}

	// IPFS status (check daemon reachability)
	ipfsAPIBase := os.Getenv("SOHOLINK_IPFS_API")
	if ipfsAPIBase == "" {
		ipfsAPIBase = "http://127.0.0.1:5001/api/v0"
	}
	ipfsStatus := widget.NewLabel("Checking IPFS daemon…")
	ipfsStatusCard := widget.NewCard("IPFS Storage", "Content-addressed distributed storage",
		container.NewVBox(
			labelPair("Kubo API", ipfsAPIBase),
			ipfsStatus,
		),
	)
	go func() {
		// Ping the IPFS daemon
		pingURL := ipfsAPIBase + "/id"
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Post(pingURL, "application/json", nil)
		if err != nil || resp.StatusCode != 200 {
			ipfsStatus.SetText("Status: Offline — run 'ipfs daemon' to enable")
		} else {
			resp.Body.Close()
			ipfsStatus.SetText("Status: Online ✓")
		}
	}()

	storDir := application.Config.StoragePoolDir()
	// Walk storage dir for basic stats
	var fileCount int
	var totalBytes int64
	if fi, err := os.Stat(storDir); err == nil && fi.IsDir() {
		_ = filepath.Walk(storDir, func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				fileCount++
				totalBytes += fi.Size()
			}
			return nil
		})
	}

	localCard := widget.NewCard("Local Storage Pool", "",
		container.NewVBox(
			labelPair("Status", localStatus),
			labelPair("Directory", storDir),
			labelPair("Files stored", strconv.Itoa(fileCount)),
			labelPair("Total size", fmt.Sprintf("%.2f MB", float64(totalBytes)/1024/1024)),
		),
	)

	content := container.NewVBox(
		ipfsStatusCard,
		localCard,
	)

	return container.NewTabItem("Storage", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 5 — Billing
// ─────────────────────────────────────────────────────────────────────────────

func buildBillingTab(w fyne.Window, application *app.App) *container.TabItem {
	if application.PaymentLedger == nil {
		return container.NewTabItem("Billing", disabledPanel(
			"Payment ledger not initialized",
			"Enable resource_sharing and configure payment processors.",
		))
	}

	ctx := context.Background()

	// Fee structure card
	feeCard := widget.NewCard("Revenue Split", "Per transaction",
		container.NewVBox(
			labelPair("Platform fee", "1% of net"),
			labelPair("Provider payout", "99% of net"),
			labelPair("Processor fee", "~2.9% + $0.30 (Stripe)"),
		),
	)

	// Payment processor status
	processorRows := widget.NewCard("Payment Processors", "",
		container.NewVBox(
			widget.NewLabel("Configured via resource_sharing.payment.processors in config."),
		),
	)

	// Pending payments
	pending, _ := application.Store.GetPendingPayments(ctx, 50)
	pendingCard := widget.NewCard("Pending Settlements", "",
		container.NewVBox(
			labelPair("Queued payments", strconv.Itoa(len(pending))),
		),
	)

	content := container.NewVBox(feeCard, processorRows, pendingCard)
	return container.NewTabItem("Billing", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 6 — Plan Work (Marketplace / Buyer)
// ─────────────────────────────────────────────────────────────────────────────

func buildMarketplaceTab(w fyne.Window, application *app.App) *container.TabItem {
	ctx := context.Background()

	// Build a manifest entry form. Fields mirror the WorkloadManifest schema.
	purposeSelect := widget.NewSelect([]string{
		"data_processing", "ml_training", "rendering", "web_serving",
		"simulation", "scientific_compute", "media_encoding", "other",
	}, nil)
	purposeSelect.SetSelected("data_processing")

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetPlaceHolder("Describe the workload in at least 20 characters…")
	descEntry.SetMinRowsVisible(3)

	networkSelect := widget.NewSelect([]string{"none", "declared_only", "unrestricted"}, nil)
	networkSelect.SetSelected("none")

	endpointsEntry := widget.NewEntry()
	endpointsEntry.SetPlaceHolder("comma-separated, e.g. api.example.com:443  (required if network ≠ none)")

	hwCheck := widget.NewCheck("Requires hardware access (GPIO / serial / USB)", nil)

	manifestForm := widget.NewCard("Workload Manifest", "Required for every purchase (stored permanently for audit)",
		container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Purpose category", purposeSelect),
				widget.NewFormItem("Description (≥ 20 chars)", descEntry),
				widget.NewFormItem("Network access", networkSelect),
				widget.NewFormItem("External endpoints", endpointsEntry),
				widget.NewFormItem("Hardware access", hwCheck),
			),
		),
	)

	// Marketplace node browser — pulls from the HTTP API if running locally.
	nodeList := widget.NewLabel("Node list: connect the HTTP API to browse live provider nodes.")
	nodeCard := widget.NewCard("Available Providers", "Fetched from local API",
		container.NewVBox(nodeList),
	)

	// Attempt to load marketplace nodes from the local API
	go func() {
		ownerDID, _ := application.Store.GetNodeInfo(ctx, "owner_did")
		if ownerDID == "" {
			return
		}
		resp, err := http.Get("http://127.0.0.1:8080/api/marketplace/nodes") // #nosec G107 — localhost only
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return
		}
		nodeList.SetText("API reachable — use the mobile app or curl to browse and purchase workloads.\n\nEndpoint: GET http://127.0.0.1:8080/api/marketplace/nodes")
	}()

	// Compliance reminder in the plan-work context.
	complianceReminder := widget.NewCard("AUP Reminder", "",
		widget.NewLabel(
			"By submitting a workload you certify the manifest is truthful.\n"+
				"False declarations result in DID suspension and wallet balance forfeiture.\n"+
				"Prohibited workloads (CSAM, malware, botnet tools) are blocked automatically.\n"+
				"See docs/TERMS_OF_SERVICE.md for the full Acceptable Use Policy.",
		),
	)

	content := container.NewVBox(manifestForm, nodeCard, complianceReminder)
	return container.NewTabItem("Plan Work", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 7 — Users
// ─────────────────────────────────────────────────────────────────────────────

func buildUsersTab(w fyne.Window, application *app.App) *container.TabItem {
	ctx := context.Background()

	users, _ := application.Store.ListUsers(ctx)

	// Add user form
	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("alice")
	didEntry := widget.NewEntry()
	didEntry.SetPlaceHolder("did:key:z6Mk…")
	roleSelect := widget.NewSelect([]string{"basic", "admin", "operator"}, nil)
	roleSelect.SetSelected("basic")

	addBtn := widget.NewButton("Add User", func() {
		if usernameEntry.Text == "" || didEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("username and DID are required"), w)
			return
		}
		err := application.Store.AddUser(ctx, usernameEntry.Text, didEntry.Text, nil, roleSelect.Selected)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("Success", "User added: "+usernameEntry.Text, w)
		usernameEntry.SetText("")
		didEntry.SetText("")
	})

	addForm := widget.NewCard("Add User", "",
		container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Username", usernameEntry),
				widget.NewFormItem("DID", didEntry),
				widget.NewFormItem("Role", roleSelect),
			),
			addBtn,
		),
	)

	// Revoke user form
	revokeEntry := widget.NewEntry()
	revokeEntry.SetPlaceHolder("username to revoke")
	reasonEntry := widget.NewEntry()
	reasonEntry.SetPlaceHolder("reason (optional)")

	revokeBtn := widget.NewButton("Revoke User", func() {
		if revokeEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("username is required"), w)
			return
		}
		dialog.ShowConfirm("Confirm Revoke",
			fmt.Sprintf("Revoke access for user %q?", revokeEntry.Text),
			func(ok bool) {
				if !ok {
					return
				}
				err := application.Store.RevokeUser(ctx, revokeEntry.Text, reasonEntry.Text)
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				dialog.ShowInformation("Revoked", "User revoked: "+revokeEntry.Text, w)
				revokeEntry.SetText("")
				reasonEntry.SetText("")
			}, w)
	})

	revokeForm := widget.NewCard("Revoke User", "",
		container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Username", revokeEntry),
				widget.NewFormItem("Reason", reasonEntry),
			),
			revokeBtn,
		),
	)

	// Users table
	headers := []string{"Username", "DID", "Role", "Created", "Status"}
	tbl := widget.NewTable(
		func() (int, int) { return len(users) + 1, len(headers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			lbl := cell.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(headers[id.Col])
				return
			}
			u := users[id.Row-1]
			switch id.Col {
			case 0:
				lbl.SetText(u.Username)
			case 1:
				lbl.SetText(truncate(u.DID, 24))
			case 2:
				lbl.SetText(u.Role)
			case 3:
				lbl.SetText(u.CreatedAt)
			case 4:
				if u.RevokedAt.Valid {
					lbl.SetText("revoked")
				} else {
					lbl.SetText("active")
				}
			}
		},
	)
	for i, cw := range []float32{120, 200, 80, 100, 70} {
		tbl.SetColumnWidth(i, cw)
	}

	content := container.NewVBox(
		container.NewGridWithColumns(2, addForm, revokeForm),
		widget.NewLabel(fmt.Sprintf("Users (%d):", len(users))),
		tbl,
	)

	return container.NewTabItem("Users", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 7 — Policies
// ─────────────────────────────────────────────────────────────────────────────

func buildPoliciesTab(w fyne.Window, application *app.App) *container.TabItem {
	ctx := context.Background()

	// Auto-accept rules table
	rules, _ := application.Store.GetAutoAcceptRules(ctx)

	ruleHeaders := []string{"Rule Name", "Resource", "Max Amount", "Min Score", "Action", "Enabled"}
	ruleTbl := widget.NewTable(
		func() (int, int) { return len(rules) + 1, len(ruleHeaders) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			lbl := cell.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(ruleHeaders[id.Col])
				return
			}
			r := rules[id.Row-1]
			switch id.Col {
			case 0:
				lbl.SetText(r.RuleName)
			case 1:
				rtype := r.ResourceType
				if rtype == "" {
					rtype = "any"
				}
				lbl.SetText(rtype)
			case 2:
				if r.MaxAmount == 0 {
					lbl.SetText("unlimited")
				} else {
					lbl.SetText(fmt.Sprintf("$%.2f", float64(r.MaxAmount)/100))
				}
			case 3:
				lbl.SetText(strconv.Itoa(r.MinUserScore))
			case 4:
				lbl.SetText(r.Action)
			case 5:
				lbl.SetText(boolStr(r.Enabled))
			}
		},
	)
	for i, cw := range []float32{150, 80, 100, 80, 80, 70} {
		ruleTbl.SetColumnWidth(i, cw)
	}

	rulesCard := widget.NewCard("Auto-Accept Rules", "Governs which rental requests are automatically accepted",
		container.NewVBox(
			ruleTbl,
			widget.NewButton("Manage Rules in Settings…", func() {
				showProvisioningLimitsDialog(w, application)
			}),
		),
	)

	// OPA policy summary
	opaCard := widget.NewCard("OPA Resource Limits", "Enforced per resource_sharing.rego",
		container.NewVBox(
			labelPair("Compute job max CPU", "4 cores"),
			labelPair("Compute job max RAM", "8192 MB"),
			labelPair("Compute job max disk", "10 GB"),
			labelPair("Compute job max time", "3600 seconds"),
			widget.NewLabel("Edit configs/policies/resource_sharing.rego to change these limits."),
		),
	)

	content := container.NewVBox(opaCard, rulesCard)
	return container.NewTabItem("Policies", container.NewScroll(container.NewPadded(content)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 8 — Logs
// ─────────────────────────────────────────────────────────────────────────────

func buildLogsTab(w fyne.Window, application *app.App) *container.TabItem {
	acctDir := application.Config.AccountingDir()
	logContent := widget.NewLabel("Loading logs…")
	logContent.Wrapping = fyne.TextWrapWord

	loadLogs := func() {
		entries, err := os.ReadDir(acctDir)
		if err != nil {
			logContent.SetText("Cannot read accounting directory: " + err.Error())
			return
		}

		// Sort by name descending (newest first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() > entries[j].Name()
		})

		var sb strings.Builder
		count := 0
		const maxFiles = 5
		const maxBytes = 8192

		for _, e := range entries {
			if e.IsDir() || (!strings.HasSuffix(e.Name(), ".log") && !strings.HasSuffix(e.Name(), ".gz")) {
				continue
			}
			if count >= maxFiles {
				break
			}

			sb.WriteString("═══ " + e.Name() + " ═══\n")
			fpath := filepath.Join(acctDir, e.Name())
			f, err := os.Open(fpath)
			if err != nil {
				sb.WriteString("(cannot open: " + err.Error() + ")\n\n")
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, maxBytes))
			f.Close()
			if err != nil {
				sb.WriteString("(read error)\n\n")
				continue
			}
			sb.Write(data)
			if len(data) >= maxBytes {
				sb.WriteString("\n… (truncated — file exceeds 8 KB) …\n")
			}
			sb.WriteString("\n")
			count++
		}

		if count == 0 {
			sb.WriteString("No log files found in " + acctDir)
		}
		logContent.SetText(sb.String())
	}

	loadLogs()

	refreshBtn := widget.NewButton("Refresh", loadLogs)
	header := container.NewHBox(
		widget.NewLabel("Accounting Logs — "+acctDir),
		layout.NewSpacer(),
		refreshBtn,
	)

	content := container.NewBorder(header, nil, nil, nil,
		container.NewScroll(logContent),
	)

	return container.NewTabItem("Logs", container.NewPadded(content))
}

// ─────────────────────────────────────────────────────────────────────────────
// Settings dialogs
// ─────────────────────────────────────────────────────────────────────────────

func showNodeSettingsDialog(w fyne.Window, application *app.App) {
	cfg := application.Config

	nameEntry := widget.NewEntry()
	nameEntry.SetText(cfg.Node.Name)
	didEntry := widget.NewEntry()
	didEntry.SetText(cfg.Node.DID)
	authEntry := widget.NewEntry()
	authEntry.SetText(cfg.Radius.AuthAddress)
	acctEntry := widget.NewEntry()
	acctEntry.SetText(cfg.Radius.AcctAddress)
	secretEntry := widget.NewPasswordEntry()
	secretEntry.SetText(maskSecret(cfg.Radius.SharedSecret))
	dataDirEntry := widget.NewEntry()
	dataDirEntry.SetText(cfg.Storage.BasePath)

	items := []*widget.FormItem{
		widget.NewFormItem("Node name", nameEntry),
		widget.NewFormItem("Node DID", didEntry),
		widget.NewFormItem("Coop. auth address", authEntry),
		widget.NewFormItem("Coop. acct address", acctEntry),
		widget.NewFormItem("Cooperative secret", secretEntry),
		widget.NewFormItem("Data directory", dataDirEntry),
	}

	dialog.ShowForm("Node Configuration", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		cfg.Node.Name = nameEntry.Text
		cfg.Radius.AuthAddress = authEntry.Text
		cfg.Radius.AcctAddress = acctEntry.Text
		cfg.Storage.BasePath = dataDirEntry.Text
		dialog.ShowInformation("Saved", "Node settings updated.\nRestart the service to apply.", w)
	}, w)
}

func showPricingSettingsDialog(w fyne.Window) {
	electricityEntry := widget.NewEntry()
	electricityEntry.SetPlaceHolder("0.12")
	marginEntry := widget.NewEntry()
	marginEntry.SetPlaceHolder("30")
	modeSelect := widget.NewSelect(
		[]string{"competitive", "premium", "cost-recovery", "custom"},
		nil,
	)
	modeSelect.SetSelected("competitive")

	items := []*widget.FormItem{
		widget.NewFormItem("Electricity rate ($/kWh)", electricityEntry),
		widget.NewFormItem("Profit margin (%)", marginEntry),
		widget.NewFormItem("Pricing mode", modeSelect),
	}

	dialog.ShowForm("Pricing & Costs", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		dialog.ShowInformation("Saved", "Pricing settings updated.", w)
	}, w)
}

func showNetworkSettingsDialog(w fyne.Window, application *app.App) {
	cfg := application.Config

	p2pCheck := widget.NewCheck("Enable P2P mesh networking", nil)
	p2pCheck.SetChecked(cfg.P2P.Enabled)
	p2pAddr := widget.NewEntry()
	p2pAddr.SetText(cfg.P2P.ListenAddr)
	if p2pAddr.Text == "" {
		p2pAddr.SetPlaceHolder("0.0.0.0:9090")
	}
	httpAPI := widget.NewEntry()
	httpAPI.SetText(cfg.ResourceSharing.HTTPAPIAddress)
	if httpAPI.Text == "" {
		httpAPI.SetPlaceHolder("0.0.0.0:8080")
	}

	items := []*widget.FormItem{
		widget.NewFormItem("P2P mesh", p2pCheck),
		widget.NewFormItem("P2P listen address", p2pAddr),
		widget.NewFormItem("HTTP API address", httpAPI),
	}

	dialog.ShowForm("Network", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		cfg.P2P.Enabled = p2pCheck.Checked
		cfg.P2P.ListenAddr = p2pAddr.Text
		cfg.ResourceSharing.HTTPAPIAddress = httpAPI.Text
		dialog.ShowInformation("Saved", "Network settings updated.\nRestart the service to apply.", w)
	}, w)
}

// showFederationSettingsDialog opens a dialog for configuring the federation
// and marketplace layer. It covers two complementary roles:
//
//   - Coordinator: one machine per group that runs the node registry and
//     routes external jobs to member nodes. Set is_coordinator = true and
//     optionally charge a fee_percent.
//
//   - Provider: every other machine that registers with a coordinator and
//     advertises its capacity at a given price. Set coordinator_url to the
//     coordinator's HTTP API address.
//
// A single machine can act as both coordinator and provider simultaneously,
// which is the recommended setup for small groups (e.g. a school office).
func showFederationSettingsDialog(w fyne.Window, application *app.App) {
	cfg := application.Config

	// ── Coordinator card ──────────────────────────────────────────────────
	coordCheck := widget.NewCheck("This node acts as a coordinator (runs the group registry)", nil)
	coordCheck.SetChecked(cfg.Federation.IsCoordinator)

	feeEntry := widget.NewEntry()
	feeEntry.SetText(fmt.Sprintf("%.2f", cfg.Federation.FeePercent))
	feeEntry.SetPlaceHolder("1.00")

	coordHint := widget.NewLabel(
		"Enable on one machine per group — it runs the directory that all other " +
			"nodes and external clients use to discover available resources. " +
			"A small group (e.g. a school) can run coordinator and provider on the same machine.")
	coordHint.Wrapping = fyne.TextWrapWord

	// ── Provider card ─────────────────────────────────────────────────────
	coordURL := widget.NewEntry()
	coordURL.SetText(cfg.Federation.CoordinatorURL)
	coordURL.SetPlaceHolder("http://school-server:8080")

	regionEntry := widget.NewEntry()
	regionEntry.SetText(cfg.Federation.Region)
	regionEntry.SetPlaceHolder("ca-ontario")

	priceEntry := widget.NewEntry()
	if cfg.Federation.PricePerCPUHourSats > 0 {
		priceEntry.SetText(fmt.Sprintf("%d", cfg.Federation.PricePerCPUHourSats))
	}
	priceEntry.SetPlaceHolder("100")

	heartbeatEntry := widget.NewEntry()
	heartbeatEntry.SetText(cfg.Federation.HeartbeatInterval)
	heartbeatEntry.SetPlaceHolder("30s")

	providerHint := widget.NewLabel(
		"Enter the coordinator's HTTP API address so this node can register itself " +
			"and receive job assignments. Leave blank only if this is a standalone " +
			"coordinator that does not also contribute resources.")
	providerHint.Wrapping = fyne.TextWrapWord

	// Disable the coordinator URL when this node IS the coordinator, but
	// re-enable it if the operator also wants to register with a parent
	// coordinator (nested federation). For now we grey it out as a hint.
	updateCoordState := func(isCoord bool) {
		if isCoord {
			coordURL.Disable()
		} else {
			coordURL.Enable()
		}
	}
	updateCoordState(cfg.Federation.IsCoordinator)
	coordCheck.OnChanged = updateCoordState

	content := container.NewVBox(
		widget.NewCard("Coordinator Role", "",
			container.NewVBox(
				coordCheck,
				coordHint,
				widget.NewSeparator(),
				widget.NewForm(
					widget.NewFormItem("Coordinator fee (%)", feeEntry),
				),
			),
		),
		widget.NewCard("Provider Settings", "",
			container.NewVBox(
				providerHint,
				widget.NewForm(
					widget.NewFormItem("Coordinator URL", coordURL),
					widget.NewFormItem("Region", regionEntry),
					widget.NewFormItem("Price / CPU-hour (sats)", priceEntry),
					widget.NewFormItem("Heartbeat interval", heartbeatEntry),
				),
			),
		),
		widget.NewButton("Save", func() {
			cfg.Federation.IsCoordinator = coordCheck.Checked

			if feeEntry.Text != "" {
				if v, err := strconv.ParseFloat(strings.TrimSpace(feeEntry.Text), 64); err == nil {
					cfg.Federation.FeePercent = v
				}
			}

			cfg.Federation.CoordinatorURL = strings.TrimSpace(coordURL.Text)
			cfg.Federation.Region = strings.TrimSpace(regionEntry.Text)

			if priceEntry.Text != "" {
				if v, err := strconv.ParseInt(strings.TrimSpace(priceEntry.Text), 10, 64); err == nil {
					cfg.Federation.PricePerCPUHourSats = v
				}
			}

			if hb := strings.TrimSpace(heartbeatEntry.Text); hb != "" {
				if _, err := time.ParseDuration(hb); err == nil {
					cfg.Federation.HeartbeatInterval = hb
				} else {
					dialog.ShowError(fmt.Errorf("invalid heartbeat interval %q — use e.g. 30s, 1m", hb), w)
					return
				}
			}

			dialog.ShowInformation("Saved",
				"Federation settings updated.\nRestart the service to apply.", w)
		}),
	)

	d := dialog.NewCustom("Federation & Marketplace", "Close",
		container.NewScroll(content), w)
	d.Resize(fyne.NewSize(520, 540))
	d.Show()
}

func showPaymentSettingsDialog(w fyne.Window, application *app.App) {
	stripeKey := widget.NewPasswordEntry()
	stripeKey.SetPlaceHolder("sk_live_…  or  sk_test_…")

	lndHost := widget.NewEntry()
	lndHost.SetPlaceHolder("127.0.0.1:10009")

	barterCheck := widget.NewCheck("Enable barter (fee-free federation trades)", nil)

	items := []*widget.FormItem{
		widget.NewFormItem("Stripe secret key", stripeKey),
		widget.NewFormItem("Lightning (LND) host", lndHost),
		widget.NewFormItem("Barter processor", barterCheck),
	}

	dialog.ShowForm("Payment Processors", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		dialog.ShowInformation("Saved",
			"Payment settings saved.\nAdd the keys to your environment and restart.", w)
	}, w)
}

func showK8sEdgesDialog(w fyne.Window) {
	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("us-east-1")
	apiEntry := widget.NewEntry()
	apiEntry.SetPlaceHolder("https://k8s.us-east-1.example.com")
	tokenEntry := widget.NewPasswordEntry()
	tokenEntry.SetPlaceHolder("service account bearer token")
	nsEntry := widget.NewEntry()
	nsEntry.SetText("soholink")
	latEntry := widget.NewEntry()
	latEntry.SetPlaceHolder("40.71")
	lonEntry := widget.NewEntry()
	lonEntry.SetPlaceHolder("-74.01")

	items := []*widget.FormItem{
		widget.NewFormItem("Region name", regionEntry),
		widget.NewFormItem("K8s API server", apiEntry),
		widget.NewFormItem("Bearer token", tokenEntry),
		widget.NewFormItem("Namespace", nsEntry),
		widget.NewFormItem("Latitude", latEntry),
		widget.NewFormItem("Longitude", lonEntry),
	}

	dialog.ShowForm("Register K8s Edge Cluster", "Register", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		if regionEntry.Text == "" || apiEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("region and API server are required"), w)
			return
		}
		dialog.ShowInformation("Registered",
			fmt.Sprintf("Edge cluster %q registered.\nCall EdgeRegistry.Register() in your start script.", regionEntry.Text), w)
	}, w)
}

func showIPFSSettingsDialog(w fyne.Window) {
	apiEntry := widget.NewEntry()
	apiEntry.SetText(os.Getenv("SOHOLINK_IPFS_API"))
	if apiEntry.Text == "" {
		apiEntry.SetText("http://127.0.0.1:5001/api/v0")
	}

	pinCheck := widget.NewCheck("Auto-pin uploads", nil)
	pinCheck.SetChecked(true)

	items := []*widget.FormItem{
		widget.NewFormItem("Kubo API base URL", apiEntry),
		widget.NewFormItem("Auto-pin", pinCheck),
	}

	dialog.ShowForm("IPFS Storage Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		_ = os.Setenv("SOHOLINK_IPFS_API", apiEntry.Text)
		dialog.ShowInformation("Saved", "IPFS API URL set to:\n"+apiEntry.Text, w)
	}, w)
}

func showProvisioningLimitsDialog(w fyne.Window, application *app.App) {
	maxVMsSlider := widget.NewSlider(1, 20)
	maxVMsSlider.SetValue(4)
	maxVMsLabel := widget.NewLabel("4")
	maxVMsSlider.OnChanged = func(v float64) { maxVMsLabel.SetText(strconv.Itoa(int(v))) }

	maxCPUSlider := widget.NewSlider(1, 32)
	maxCPUSlider.SetValue(4)
	maxCPULabel := widget.NewLabel("4")
	maxCPUSlider.OnChanged = func(v float64) { maxCPULabel.SetText(strconv.Itoa(int(v))) }

	maxRAMSlider := widget.NewSlider(1, 128)
	maxRAMSlider.SetValue(8)
	maxRAMLabel := widget.NewLabel("8 GB")
	maxRAMSlider.OnChanged = func(v float64) { maxRAMLabel.SetText(strconv.Itoa(int(v)) + " GB") }

	maxStorSlider := widget.NewSlider(10, 2000)
	maxStorSlider.SetValue(100)
	maxStorLabel := widget.NewLabel("100 GB")
	maxStorSlider.OnChanged = func(v float64) { maxStorLabel.SetText(strconv.Itoa(int(v)) + " GB") }

	requireSigCheck := widget.NewCheck("Require digital signatures on contracts", nil)
	requireSigCheck.SetChecked(true)
	rateLimitCheck := widget.NewCheck("Enable rate limiting", nil)
	rateLimitCheck.SetChecked(true)

	content := container.NewVBox(
		widget.NewCard("Per-Customer Limits", "",
			container.NewVBox(
				container.NewHBox(widget.NewLabel("Max VMs per customer:"), maxVMsLabel),
				maxVMsSlider,
				container.NewHBox(widget.NewLabel("Max CPU cores per VM:"), maxCPULabel),
				maxCPUSlider,
				container.NewHBox(widget.NewLabel("Max RAM per VM:"), maxRAMLabel),
				maxRAMSlider,
				container.NewHBox(widget.NewLabel("Max storage per VM:"), maxStorLabel),
				maxStorSlider,
			),
		),
		widget.NewCard("Security", "",
			container.NewVBox(
				requireSigCheck,
				rateLimitCheck,
			),
		),
		widget.NewButton("Save Limits", func() {
			dialog.ShowInformation("Saved",
				fmt.Sprintf("Limits updated:\n• Max VMs/customer: %s\n• Max CPU/VM: %s cores\n• Max RAM/VM: %s\n• Max storage/VM: %s",
					maxVMsLabel.Text, maxCPULabel.Text, maxRAMLabel.Text, maxStorLabel.Text), w)
		}),
	)

	limitsDialog := dialog.NewCustom("Provisioning Limits", "Close", container.NewScroll(content), w)
	limitsDialog.Resize(fyne.NewSize(500, 520))
	limitsDialog.Show()
}

func showAboutDialog(w fyne.Window) {
	content := container.NewVBox(
		widget.NewLabelWithStyle("SoHoLINK", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Federated SOHO Compute Marketplace"),
		widget.NewSeparator(),
		labelPair("Module", "github.com/NetworkTheoryAppliedResearchInstitute/soholink"),
		labelPair("Network", "NTARI Federation"),
		labelPair("License", "See LICENSE file"),
		widget.NewSeparator(),
		widget.NewLabel("Built with Fyne · Go · SQLite · OPA"),
	)

	d := dialog.NewCustom("About SoHoLINK", "Close", container.NewPadded(content), w)
	d.Resize(fyne.NewSize(420, 280))
	d.Show()
}

// showCheckForUpdatesDialog checks for a newer release and, if found, offers
// to download and install it.  The download is SHA-256 verified against the
// official checksums.txt before the binary is replaced.
func showCheckForUpdatesDialog(w fyne.Window, application *app.App) {
	if application == nil || application.Updater == nil {
		dialog.ShowInformation("Auto-Updates Disabled",
			"Auto-updates are not enabled.\n\nTo enable them, set updates.enabled: true\nin your config file and restart.", w)
		return
	}

	prog := dialog.NewProgressInfinite("Checking for Updates", "Contacting update server…", w)
	prog.Show()

	go func() {
		rel, err := application.Updater.CheckNow()
		prog.Hide()

		if err != nil {
			dialog.ShowError(fmt.Errorf("update check failed: %w", err), w)
			return
		}
		if rel == nil {
			dialog.ShowInformation("Up to Date",
				"You are running the latest version of SoHoLINK.", w)
			return
		}

		msg := fmt.Sprintf(
			"Update available: %s\nPublished: %s\n\nThe new binary will be downloaded and verified\n(SHA-256 checksum) before installation.\n\nDownload and install now?",
			rel.TagName, rel.PublishedAt.Format("2006-01-02"))

		dialog.ShowConfirm("Update Available", msg, func(confirmed bool) {
			if !confirmed {
				return
			}
			dl := dialog.NewProgressInfinite("Downloading Update", "Downloading "+rel.TagName+"…", w)
			dl.Show()

			go func() {
				ctx := context.Background()
				err := application.Updater.Download(ctx, rel)
				dl.Hide()
				if err != nil {
					dialog.ShowError(fmt.Errorf("download failed: %w", err), w)
					return
				}
				dialog.ShowInformation("Update Ready",
					"Update installed successfully.\n\nRestart SoHoLINK to apply version "+rel.TagName+".", w)
			}()
		}, w)
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// Setup wizard
// ─────────────────────────────────────────────────────────────────────────────

// wizardStep enumerates the pages of the setup wizard.
type wizardStep int

const (
	stepWelcome wizardStep = iota
	stepLicense
	stepDeploymentMode
	stepConfiguration
	stepHardware       // NEW: system resource assessment
	stepAdvancedConfig
	stepReview
	stepInstallProgress
	stepComplete
)

// wizardState carries mutable form values across wizard steps.
type wizardState struct {
	LicenseAccepted  bool
	DeploymentMode   string
	NodeName         string
	AuthPort         string
	AcctPort         string
	DataDir          string
	Secret           string
	P2PEnabled       bool
	P2PPort          string
	UpdatesEnabled   bool
	MetricsEnabled   bool
	MetricsPort      string
	PaymentsEnabled  bool
	StorageLimitGB   int
	Capabilities     *wizard.SystemCapabilities // set by wizardHardwarePage; nil until detection runs
	LogOutput        []string
}

func showWizard(w fyne.Window, cfg *config.Config, s *store.Store, onComplete func()) {
	state := &wizardState{
		DeploymentMode: "standalone",
		AuthPort:       "1812",
		AcctPort:       "1813",
		DataDir:        defaultDataDir(),
		P2PPort:        "9090",
		MetricsPort:    "9100",
		StorageLimitGB: 50,
		UpdatesEnabled: true,
	}

	var step wizardStep
	var content *fyne.Container
	content = container.NewPadded(buildWizardPage(w, &step, state, cfg, s, content, onComplete))

	w.SetContent(content)
}

func buildWizardPage(w fyne.Window, step *wizardStep, state *wizardState,
	cfg *config.Config, s *store.Store, content *fyne.Container, onComplete func()) fyne.CanvasObject {

	nextStep := func() {
		*step++
		w.SetContent(container.NewPadded(
			buildWizardPage(w, step, state, cfg, s, content, onComplete)))
	}
	prevStep := func() {
		if *step > 0 {
			*step--
			w.SetContent(container.NewPadded(
				buildWizardPage(w, step, state, cfg, s, content, onComplete)))
		}
	}

	total := int(stepComplete) + 1
	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = float64(total - 1)
	progress.SetValue(float64(*step))

	stepLabel := widget.NewLabel(fmt.Sprintf("Step %d of %d", int(*step)+1, total))

	var body fyne.CanvasObject
	switch *step {
	case stepWelcome:
		body = wizardWelcomePage(nextStep)
	case stepLicense:
		body = wizardLicensePage(state, nextStep, prevStep)
	case stepDeploymentMode:
		body = wizardDeploymentPage(state, nextStep, prevStep)
	case stepConfiguration:
		body = wizardConfigPage(state, nextStep, prevStep)
	case stepHardware:
		body = wizardHardwarePage(state, nextStep, prevStep)
	case stepAdvancedConfig:
		body = wizardAdvancedPage(state, nextStep, prevStep)
	case stepReview:
		body = wizardReviewPage(state, nextStep, prevStep)
	case stepInstallProgress:
		body = wizardInstallPage(w, state, cfg, s, nextStep)
	case stepComplete:
		body = wizardCompletePage(w, onComplete)
	default:
		body = widget.NewLabel("Unknown step")
	}

	return container.NewBorder(
		container.NewVBox(progress, stepLabel, widget.NewSeparator()),
		nil, nil, nil,
		body,
	)
}

func wizardWelcomePage(next func()) fyne.CanvasObject {
	intro := widget.NewLabel(
		"SoHoLINK lets your spare computer power earn money.\n\n" +
			"When you're not using your PC, other people and businesses can rent your CPU,\n" +
			"storage, or printer through a secure marketplace — and you get paid automatically.\n\n" +
			"This 6-step wizard takes about 5 minutes. It will:\n" +
			"  • Measure what your computer can offer\n" +
			"  • Suggest a fair price for your resources\n" +
			"  • Create a secure identity for your node\n" +
			"  • Ask a few optional questions about extra features\n" +
			"  • Save your settings and open the earnings dashboard\n\n" +
			"You can change any setting later in Settings.",
	)
	intro.Wrapping = fyne.TextWrapWord
	return container.NewBorder(
		widget.NewLabelWithStyle("Welcome to SoHoLINK", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(layout.NewSpacer(), widget.NewButton("Get Started →", next)),
		nil, nil,
		container.NewPadded(intro),
	)
}

func wizardLicensePage(state *wizardState, next, prev func()) fyne.CanvasObject {
	licenseText := widget.NewLabel(licenseText())
	licenseText.Wrapping = fyne.TextWrapWord
	acceptCheck := widget.NewCheck("I accept the license terms", func(checked bool) {
		state.LicenseAccepted = checked
	})

	return container.NewBorder(
		widget.NewLabelWithStyle("License Agreement", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewVBox(
			acceptCheck,
			container.NewHBox(
				widget.NewButton("← Back", prev),
				layout.NewSpacer(),
				widget.NewButton("Accept & Continue →", func() {
					if !state.LicenseAccepted {
						return
					}
					next()
				}),
			),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(licenseText)),
	)
}

func wizardDeploymentPage(state *wizardState, next, prev func()) fyne.CanvasObject {
	group := widget.NewRadioGroup(
		[]string{
			"Home / Small Office  —  easiest, most common",
			"Managed Cloud Provider  —  run a service for paying clients",
		},
		func(chosen string) {
			if strings.HasPrefix(chosen, "Home") {
				state.DeploymentMode = "standalone"
			} else {
				state.DeploymentMode = "saas"
			}
		},
	)
	group.SetSelected("Home / Small Office  —  easiest, most common")

	standaloneDesc := widget.NewLabel(
		"🏠  Home / Small Office\n" +
			"Your computer joins the open marketplace and earns money by completing jobs that\n" +
			"other users post — compute tasks, file storage, or printing. No business setup\n" +
			"required. Best for individuals, remote workers, and small teams who want passive\n" +
			"income from hardware that would otherwise sit idle.",
	)
	standaloneDesc.Wrapping = fyne.TextWrapWord

	saasDesc := widget.NewLabel(
		"☁️  Managed Cloud Provider\n" +
			"Run a professional cloud service where clients pay you for dedicated compute,\n" +
			"storage, or print capacity on contract. Includes customer billing dashboards,\n" +
			"capacity alerts, and service-level management. Best for IT professionals and\n" +
			"small hosting businesses who want to offer a branded service to clients.",
	)
	saasDesc.Wrapping = fyne.TextWrapWord

	hint := widget.NewLabel("Not sure? Choose Home / Small Office — it's the right choice for most people and you can switch later.")
	hint.Wrapping = fyne.TextWrapWord

	return container.NewBorder(
		widget.NewLabelWithStyle("How do you want to participate?", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton("← Back", prev),
			layout.NewSpacer(),
			widget.NewButton("Continue →", next),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(container.NewVBox(
			widget.NewSeparator(),
			group,
			widget.NewSeparator(),
			standaloneDesc,
			widget.NewSeparator(),
			saasDesc,
			widget.NewSeparator(),
			hint,
		))),
	)
}

func wizardConfigPage(state *wizardState, next, prev func()) fyne.CanvasObject {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(state.NodeName)
	nameEntry.SetPlaceHolder("home-office-1")
	nameEntry.OnChanged = func(s string) { state.NodeName = s }

	nameHint := widget.NewLabel("A nickname for this computer on the network — like a label on a server rack. Use letters, numbers, and hyphens. Example: home-office-1 or janes-desktop")
	nameHint.Wrapping = fyne.TextWrapWord

	authEntry := widget.NewEntry()
	authEntry.SetText(state.AuthPort)
	authEntry.OnChanged = func(s string) { state.AuthPort = s }

	acctEntry := widget.NewEntry()
	acctEntry.SetText(state.AcctPort)
	acctEntry.OnChanged = func(s string) { state.AcctPort = s }

	dirEntry := widget.NewEntry()
	dirEntry.SetText(state.DataDir)
	dirEntry.OnChanged = func(s string) { state.DataDir = s }

	dirHint := widget.NewLabel("Where SoHoLINK stores its database, identity files, and logs. The default location is fine for most users — only change this if you want data on a different drive.")
	dirHint.Wrapping = fyne.TextWrapWord

	secretEntry := widget.NewPasswordEntry()
	secretEntry.SetPlaceHolder("Leave blank to use the open marketplace")
	secretEntry.OnChanged = func(s string) { state.Secret = s }

	coopHint := widget.NewLabel(
		"A Cooperative Earning Group is a private circle of trusted nodes — like a co-op. " +
			"Members pay zero platform fee to each other and get priority job routing between " +
			"themselves. To join an existing group, enter the shared passphrase your group uses. " +
			"To start a group, invent a strong passphrase and share it with the other node operators. " +
			"Leave blank if you're setting up your first node or just want the open marketplace.")
	coopHint.Wrapping = fyne.TextWrapWord

	portsHint := widget.NewLabel("Advanced: these network ports are used for cooperative authentication. Change only if port 1812 or 1813 is already in use on this computer.")
	portsHint.Wrapping = fyne.TextWrapWord

	return container.NewBorder(
		widget.NewLabelWithStyle("Name Your Node", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton("← Back", prev),
			layout.NewSpacer(),
			widget.NewButton("Continue →", func() {
				if state.NodeName == "" {
					state.NodeName = "my-soho-node"
				}
				next()
			}),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(
			container.NewVBox(
				widget.NewForm(widget.NewFormItem("Node nickname", nameEntry)),
				nameHint,
				widget.NewSeparator(),
				widget.NewForm(widget.NewFormItem("Data folder", dirEntry)),
				dirHint,
				widget.NewSeparator(),
				widget.NewLabelWithStyle("Cooperative Earning Group  (optional)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				coopHint,
				widget.NewForm(
					widget.NewFormItem("Group passphrase", secretEntry),
				),
				portsHint,
				widget.NewForm(
					widget.NewFormItem("Auth port", authEntry),
					widget.NewFormItem("Accounting port", acctEntry),
				),
			),
		)),
	)
}

// wizardHardwarePage detects the system's hardware in a background goroutine and
// displays each resource as a plain-English card. It also pre-fills
// state.StorageLimitGB to 50% of actual free disk space so the next step's
// slider opens at a sensible default rather than the hardcoded 50 GB.
func wizardHardwarePage(state *wizardState, next, prev func()) fyne.CanvasObject {
	spin := widget.NewProgressBarInfinite()
	status := widget.NewLabel("Scanning your hardware… this takes a few seconds.")
	status.Wrapping = fyne.TextWrapWord
	results := container.NewVBox()

	go func() {
		caps, err := wizard.DetectSystemCapabilities()
		spin.Hide()
		if err != nil {
			status.SetText("⚠️  Could not scan hardware: " + err.Error() +
				"\n\nYou can still continue — the default values will be used.")
			return
		}
		state.Capabilities = caps

		// ── Smart storage default: 50% of free space, rounded to nearest 10 GB ──
		free := caps.Storage.AvailableGB // float64
		suggested := math.Floor(free/2/10) * 10
		if suggested < 10 {
			suggested = 10
		}
		if suggested > 1000 {
			suggested = 1000
		}
		state.StorageLimitGB = int(suggested)

		status.SetText("Here's what we found on this computer:")

		// CPU
		cpuNote := fmt.Sprintf(
			"Can handle %d simultaneous compute jobs (one per core). More cores = more earning potential.",
			caps.CPU.Cores)
		results.Add(wizardHardwareCard(
			"🖥️  Processor",
			fmt.Sprintf("%s  ·  %d cores / %d threads", caps.CPU.Model, caps.CPU.Cores, caps.CPU.Threads),
			cpuNote,
		))

		// Memory
		vmCount := int(caps.Memory.AvailableGB / 2)
		if vmCount < 1 {
			vmCount = 1
		}
		memNote := fmt.Sprintf(
			"Enough memory to run up to %d small virtual machines simultaneously.", vmCount)
		results.Add(wizardHardwareCard(
			"🧠  Memory (RAM)",
			fmt.Sprintf("%.1f GB total  /  %.1f GB available",
				caps.Memory.TotalGB, caps.Memory.AvailableGB),
			memNote,
		))

		// Storage
		driveType := caps.Storage.DriveType
		if driveType == "" {
			driveType = "Disk"
		}
		storNote := fmt.Sprintf(
			"We suggest sharing %d GB (50%% of your free space). You'll set the exact amount on the next screen.",
			int(suggested))
		results.Add(wizardHardwareCard(
			"💾  Storage",
			fmt.Sprintf("%s  ·  %.1f GB free of %.1f GB total", driveType, caps.Storage.AvailableGB, caps.Storage.TotalGB),
			storNote,
		))

		// GPU
		gpuValue := "None detected"
		gpuNote := "Compute jobs will use your CPU only. That's perfectly fine for most tasks."
		if caps.GPU != nil && caps.GPU.Model != "" {
			gpuValue = caps.GPU.Model
			if caps.GPU.VRAMGb > 0 {
				gpuValue = fmt.Sprintf("%s  ·  %d GB VRAM", caps.GPU.Model, caps.GPU.VRAMGb)
			}
			gpuNote = "GPU acceleration is available — unlocks AI inference, 3D rendering, and video transcoding jobs which pay premium rates."
		}
		results.Add(wizardHardwareCard("🎮  Graphics (GPU)", gpuValue, gpuNote))

		results.Refresh()
	}()

	return container.NewBorder(
		widget.NewLabelWithStyle("Your Computer's Resources", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton("← Back", prev),
			layout.NewSpacer(),
			widget.NewButton("Continue →", next),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(
			container.NewVBox(status, spin, results),
		)),
	)
}

// wizardHardwareCard renders one resource row: a bold title, a measured value line,
// and an optional plain-English explanation prefixed with "→".
func wizardHardwareCard(title, value, explanation string) fyne.CanvasObject {
	titleLbl := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	valueLbl := widget.NewLabel(value)
	valueLbl.Wrapping = fyne.TextWrapWord
	items := []fyne.CanvasObject{titleLbl, valueLbl}
	if explanation != "" {
		expLbl := widget.NewLabel("→ " + explanation)
		expLbl.Wrapping = fyne.TextWrapWord
		items = append(items, expLbl)
	}
	return widget.NewCard("", "", container.NewVBox(items...))
}

func wizardAdvancedPage(state *wizardState, next, prev func()) fyne.CanvasObject {
	// ── Local network discovery (P2P) ─────────────────────────────────────────
	p2pCheck := widget.NewCheck("Find other nodes automatically on my local network", func(b bool) { state.P2PEnabled = b })
	p2pCheck.SetChecked(state.P2PEnabled)
	p2pHint := widget.NewLabel(
		"When enabled, SoHoLINK computers on the same home or office Wi-Fi/LAN find\n" +
			"each other automatically — no internet required for local jobs. This makes\n" +
			"local jobs faster, cheaper, and resilient to internet outages.\n" +
			"→ Recommended if you have more than one SoHoLINK node at home or in the office.\n" +
			"→ Safe to leave off if this is your only node.",
	)
	p2pHint.Wrapping = fyne.TextWrapWord
	p2pPortEntry := widget.NewEntry()
	p2pPortEntry.SetText(state.P2PPort)
	p2pPortEntry.OnChanged = func(s string) { state.P2PPort = s }

	// ── Auto-updates ──────────────────────────────────────────────────────────
	updateCheck := widget.NewCheck("Keep SoHoLINK updated automatically", func(b bool) { state.UpdatesEnabled = b })
	updateCheck.SetChecked(state.UpdatesEnabled)
	updateHint := widget.NewLabel(
		"Once a day, SoHoLINK quietly checks for a new version. If one is available it\n" +
			"downloads it, verifies the file hasn't been tampered with (SHA-256 checksum),\n" +
			"and applies it the next time the app starts. No action needed from you.\n" +
			"→ Recommended for most users — keeps your node secure and earning with new features.\n" +
			"→ Disable only if you prefer to update manually.",
	)
	updateHint.Wrapping = fyne.TextWrapWord

	// ── Performance monitoring feed (Prometheus metrics) ──────────────────────
	metricsCheck := widget.NewCheck("Expose a performance monitoring feed", func(b bool) { state.MetricsEnabled = b })
	metricsCheck.SetChecked(state.MetricsEnabled)
	metricsHint := widget.NewLabel(
		"Publishes a live data stream about your node's CPU usage, memory, job activity,\n" +
			"and earnings on a local port. Server monitoring tools like Prometheus, Grafana,\n" +
			"and Datadog can read this feed to draw graphs and send alerts.\n" +
			"→ Only useful if you already run server monitoring software.\n" +
			"→ Leave this off if you don't know what Prometheus is — the dashboard covers the basics.",
	)
	metricsHint.Wrapping = fyne.TextWrapWord
	metricsPortEntry := widget.NewEntry()
	metricsPortEntry.SetText(state.MetricsPort)
	metricsPortEntry.OnChanged = func(s string) { state.MetricsPort = s }

	// ── Payment processors ────────────────────────────────────────────────────
	paymentsCheck := widget.NewCheck("Accept credit card and Bitcoin Lightning payments", func(b bool) { state.PaymentsEnabled = b })
	paymentsCheck.SetChecked(state.PaymentsEnabled)
	paymentsHint := widget.NewLabel(
		"Lets clients pay you with a credit card (via Stripe) or Bitcoin Lightning — real\n" +
			"money deposited to your account. You'll enter your payment credentials in\n" +
			"Settings → Payment after the wizard finishes.\n" +
			"Without this, your node still earns barter credits redeemable within the federation.\n" +
			"→ Enable if you want cash payouts. You configure your Stripe or Lightning details after setup.",
	)
	paymentsHint.Wrapping = fyne.TextWrapWord

	// ── Shared storage pool ───────────────────────────────────────────────────
	sliderMax := 2000.0
	if state.Capabilities != nil && state.Capabilities.Storage.AvailableGB > 0 {
		sliderMax = state.Capabilities.Storage.AvailableGB
	}
	storageLimitSlider := widget.NewSlider(10, sliderMax)
	storageLimitSlider.SetValue(float64(state.StorageLimitGB))
	storageLimitLabel := widget.NewLabel(fmt.Sprintf("%d GB", state.StorageLimitGB))
	storageLimitSlider.OnChanged = func(v float64) {
		state.StorageLimitGB = int(v)
		storageLimitLabel.SetText(fmt.Sprintf("%d GB", int(v)))
	}
	sliderNote := widget.NewLabel("slider range matches your actual free disk space")
	if state.Capabilities != nil {
		sliderNote.SetText(fmt.Sprintf("%.1f GB available on this computer", state.Capabilities.Storage.AvailableGB))
	}
	sliderNote.Wrapping = fyne.TextWrapWord
	storageHint := widget.NewLabel(
		"The maximum amount of your hard drive that clients can use to store files.\n" +
			"Your own files are untouched — SoHoLINK only uses the space you allocate here,\n" +
			"and only when clients are actively storing data.\n" +
			"→ Set to the amount of free disk space you're comfortable sharing.\n" +
			"→ More shared space can mean more storage earnings.",
	)
	storageHint.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		// P2P
		p2pCheck,
		p2pHint,
		widget.NewForm(widget.NewFormItem("  Local network port", p2pPortEntry)),
		widget.NewSeparator(),
		// Auto-updates
		updateCheck,
		updateHint,
		widget.NewSeparator(),
		// Prometheus metrics
		metricsCheck,
		metricsHint,
		widget.NewForm(widget.NewFormItem("  Monitoring port", metricsPortEntry)),
		widget.NewSeparator(),
		// Payments
		paymentsCheck,
		paymentsHint,
		widget.NewSeparator(),
		// Storage
		widget.NewLabelWithStyle("Shared storage space", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(storageLimitSlider, storageLimitLabel),
		sliderNote,
		storageHint,
	)

	return container.NewBorder(
		widget.NewLabelWithStyle("Features & Earning Options", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton("← Back", prev),
			layout.NewSpacer(),
			widget.NewButton("Continue →", next),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(content)),
	)
}

func wizardReviewPage(state *wizardState, next, prev func()) fyne.CanvasObject {
	modeLabel := "Home / Small Office Node"
	if state.DeploymentMode == "saas" {
		modeLabel = "Managed Cloud Provider"
	}
	coopLabel := "No — open marketplace only"
	if state.Secret != "" {
		coopLabel = "Yes — group passphrase set"
	}

	summary := fmt.Sprintf(
		"Node nickname:       %s\n"+
			"Participation:       %s\n"+
			"Data folder:         %s\n"+
			"Cooperative group:   %s\n"+
			"Local networking:    %s\n"+
			"Auto-updates:        %s\n"+
			"Monitoring feed:     %s\n"+
			"Payment processors:  %s\n"+
			"Shared storage:      %d GB\n",
		state.NodeName, modeLabel, state.DataDir, coopLabel,
		boolStr(state.P2PEnabled), boolStr(state.UpdatesEnabled),
		boolStr(state.MetricsEnabled), boolStr(state.PaymentsEnabled),
		state.StorageLimitGB,
	)

	summaryLabel := widget.NewLabel(summary)
	summaryLabel.Wrapping = fyne.TextWrapWord

	note := widget.NewLabel("Everything above can be changed later in Settings. Click 'Install Now' to save your configuration and open the dashboard.")
	note.Wrapping = fyne.TextWrapWord

	return container.NewBorder(
		widget.NewLabelWithStyle("Review Your Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton("← Back", prev),
			layout.NewSpacer(),
			widget.NewButton("Install Now →", next),
		),
		nil, nil,
		container.NewScroll(container.NewPadded(container.NewVBox(
			widget.NewCard("Your configuration", "", summaryLabel),
			note,
		))),
	)
}

func wizardInstallPage(w fyne.Window, state *wizardState, cfg *config.Config, s *store.Store, next func()) fyne.CanvasObject {
	progress := widget.NewProgressBar()
	logLabel := widget.NewLabel("Starting installation…")
	logLabel.Wrapping = fyne.TextWrapWord

	go func() {
		// Ensure we always have a valid config to populate and save.
		workCfg := cfg
		if workCfg == nil {
			var err error
			workCfg, err = config.Load("")
			if err != nil {
				// Even if Load fails (missing defaults), create a minimal config.
				workCfg = &config.Config{}
			}
		}
		configPath := filepath.Join(config.DefaultConfigDir(), "config.yaml")

		steps := []struct {
			label string
			fn    func() error
		}{
			{"Applying configuration…", func() error {
				workCfg.Node.Name = state.NodeName
				workCfg.Radius.AuthAddress = "0.0.0.0:" + state.AuthPort
				workCfg.Radius.AcctAddress = "0.0.0.0:" + state.AcctPort
				workCfg.Storage.BasePath = state.DataDir
				workCfg.Radius.SharedSecret = state.Secret
				workCfg.Updates.Enabled = state.UpdatesEnabled
				return nil
			}},
			{"Creating directories…", func() error {
				// Ensure the config directory exists before we save into it.
				if err := os.MkdirAll(config.DefaultConfigDir(), 0750); err != nil {
					return fmt.Errorf("failed to create config directory: %w", err)
				}
				return config.EnsureDirectories(workCfg)
			}},
			{"Saving configuration…", func() error {
				return config.Save(workCfg, configPath)
			}},
			{"Initializing database…", func() error {
				if s != nil {
					return nil
				}
				dbPath := filepath.Join(state.DataDir, "soholink.db")
				_, err := store.NewStore(dbPath)
				return err
			}},
			{"Writing node identity…", func() error {
				if s != nil {
					if err := s.SetNodeInfo(context.Background(), "node_name", state.NodeName); err != nil {
						return err
					}
					return s.SetNodeInfo(context.Background(), "deployment_mode", state.DeploymentMode)
				}
				return nil
			}},
		}

		for i, step := range steps {
			logLabel.SetText(step.label)
			progress.SetValue(float64(i) / float64(len(steps)))
			time.Sleep(600 * time.Millisecond) // allow UI to update

			if err := step.fn(); err != nil {
				logLabel.SetText("❌ Error: " + err.Error())
				return
			}
			logLabel.SetText("✓ " + step.label)
		}

		progress.SetValue(1.0)
		time.Sleep(400 * time.Millisecond)
		next()
	}()

	return container.NewVBox(
		widget.NewLabelWithStyle("Installing…", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		progress,
		widget.NewSeparator(),
		logLabel,
	)
}

func wizardCompletePage(w fyne.Window, onComplete func()) fyne.CanvasObject {
	msg := widget.NewLabel(
		"Your node is configured and ready to earn.\n\n" +
			"The dashboard will show:\n" +
			"  • Live node status and earnings summary\n" +
			"  • Incoming job requests and completions\n" +
			"  • Hardware usage and storage pool activity\n" +
			"  • Billing history and payout records\n\n" +
			"Tip: To earn whenever your computer is on, look for 'Run at Login' in\n" +
			"Settings → Node once the dashboard opens.",
	)
	msg.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		widget.NewLabelWithStyle("You're all set!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		msg,
		layout.NewSpacer(),
		container.NewHBox(
			layout.NewSpacer(),
			widget.NewButton("Open Dashboard →", func() {
				if onComplete != nil {
					onComplete()
				} else {
					w.Close()
				}
			}),
		),
	)
	return container.NewPadded(content)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper functions
// ─────────────────────────────────────────────────────────────────────────────

// labelPair renders a label:value row using a small form-like layout.
func labelPair(key, value string) fyne.CanvasObject {
	k := widget.NewLabel(key + ":")
	k.TextStyle = fyne.TextStyle{Bold: true}
	v := widget.NewLabel(value)
	v.Wrapping = fyne.TextTruncate
	return container.NewHBox(k, v)
}

// statBlock renders a large stat value with a caption for the overview grid.
func statBlock(caption, value string) fyne.CanvasObject {
	val := widget.NewLabelWithStyle(value, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	cap := widget.NewLabelWithStyle(caption, fyne.TextAlignCenter, fyne.TextStyle{})
	return container.NewVBox(val, cap)
}

// healthRow renders a checkmark/cross + label row for the health card.
func healthRow(label string, ok bool) fyne.CanvasObject {
	icon := "✓"
	if !ok {
		icon = "–"
	}
	return container.NewHBox(widget.NewLabel(icon), widget.NewLabel(label))
}

// disabledPanel is shown when a subsystem is disabled or unavailable.
func disabledPanel(title, subtitle string) fyne.CanvasObject {
	return container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(subtitle),
	))
}

// truncate shortens a string to maxLen, appending "…" if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// boolStr returns "Yes" or "No".
func boolStr(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// maskSecret replaces a secret with asterisks.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return strings.Repeat("*", len(s))
}

// countOnlineSubsystems returns the number of non-nil subsystems.
func countOnlineSubsystems(a *app.App) int {
	count := 1 // cooperative auth server always running
	if a.LBTASManager != nil {
		count++
	}
	if a.PaymentLedger != nil {
		count++
	}
	if a.ComputeSched != nil {
		count++
	}
	if a.StoragePool != nil {
		count++
	}
	if a.FedScheduler != nil {
		count++
	}
	if a.P2PNetwork != nil {
		count++
	}
	if a.RentalEngine != nil {
		count++
	}
	if a.SLAMonitor != nil {
		count++
	}
	if a.LocalChain != nil {
		count++
	}
	return count
}

// defaultDataDir returns the platform-appropriate default data directory.
// Delegates to config.DefaultDataDir so the wizard and the backend agree
// on the same path.
func defaultDataDir() string {
	return config.DefaultDataDir()
}

// licenseText returns the license summary shown in the wizard.
func licenseText() string {
	return `SoHoLINK — NTARI Federation Node Software

Copyright (c) Network Theory Applied Research Institute

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software to use, copy, modify, and distribute it for any purpose,
subject to the following conditions:

1. Attribution: Include this copyright notice in all copies or substantial
   portions of the software.

2. Federation Terms: Nodes participating in the NTARI federation must comply
   with the federation's acceptable use policy at https://ntari.org/aup.

3. No Warranty: This software is provided "as is" without warranty of any kind,
   express or implied.

4. Revenue Split: Nodes using the central SOHO matching service agree to a 1%
   platform fee on all settled transactions.

By accepting, you confirm that you have read, understood, and agree to these
terms and the full license available in the repository.`
}
