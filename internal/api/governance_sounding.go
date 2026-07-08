package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/sounding"
)

// This file adds the LOCAL-ONLY :8090 DEMAND-SOUNDING dashboard (GET /admin/sounding)
// to the GovernanceServer. It renders SERVER-SIDE SVG charts — no JS charting
// library, nothing to fetch — so it works on a 3G phone and a Smart TV browser
// exactly like the rest of the admin console. It reads the migration-025
// hypertables through the sounding read model (internal/sounding/readmodel.go) and
// renders through the SAME admin renderAdmin(admin_layout.html, page) path as the
// operator/fees/messaging pages, so it shares the governance nav and the loopback
// banner and is physically unreachable from public soholink.org.
//
// WEATHER METAPHOR (matches migration 025's ladder): a demand "sounding" reads the
// atmosphere for a storm forming before it exists. Jobs towering against the top
// available rung's ceiling are CAPE — convective energy building; a run of them is
// CONGESTUS, the cloud stage just before a thunderhead; when it is sustained the
// signal is "ship the STORM tier" (the coming_soon rung). The rejection log is the
// live, honest demand proxy.
//
// DATA-VIZ DISCIPLINE (STATE): server-rendered SVG only; a dark chart surface;
// ONE value axis per chart; tabular digits in every table; a legend whenever a
// chart carries >=2 series; a table view beside every chart; and — critically —
// rejection REASONS use a FIXED CATEGORICAL hue order that is NOT the status
// palette (ok/warn/danger are reserved for status, never for a data category).

// soundingReadModel is the read surface the dashboard consumes. An interface so a
// POST-only or test GovernanceServer can leave it nil (the handler then renders a
// 500 "read model unavailable", mirroring the console's template-not-found mode).
type soundingReadModel interface {
	Operators(ctx context.Context) ([]string, error)
	Totals(ctx context.Context, operatorID, window string) (sounding.Totals, error)
	JobShapeDistribution(ctx context.Context, operatorID, window string) ([]sounding.RungCount, error)
	CongestusSeries(ctx context.Context, operatorID, window, bucket string) ([]sounding.CongestusBucket, error)
	RejectionsByReason(ctx context.Context, operatorID, window, bucket string) ([]sounding.RejectionCell, error)
	CapacityVsDemand(ctx context.Context, operatorID, window, bucket string) ([]sounding.LevelBucket, error)
	LoadLadderReader(ctx context.Context) (sounding.Ladder, error)
}

// compile-time assertion that *sounding.Reader satisfies soundingReadModel.
var _ soundingReadModel = (*sounding.Reader)(nil)

// ConfigureSounding attaches the demand-sounding read model so GET /admin/sounding
// renders live charts. Separate from the constructor (like ConfigureConsole) so a
// POST-only server is unaffected: without this call the route is still registered
// but renders a 500 until the read model is wired. Kept on this LOCAL-ONLY mux.
func (g *GovernanceServer) ConfigureSounding(reader soundingReadModel) {
	g.sounding = reader
}

// -----------------------------------------------------------------------------
// Window / bucketing.
// -----------------------------------------------------------------------------

const (
	soundingWindow      = "7 days" // lookback for every sounding query
	soundingBucket      = "1 day"  // time_bucket width for the time series
	soundingWindowLabel = "Last 7 days"
	soundingBucketCount = 7 // daily buckets rendered on the time axis
)

// soundingDayBuckets returns n midnight-UTC bucket starts ending today (ascending),
// matching TimescaleDB time_bucket('1 day', ...) which aligns to UTC epoch days.
func soundingDayBuckets(n int) []time.Time {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]time.Time, n)
	for i := 0; i < n; i++ {
		out[i] = today.AddDate(0, 0, i-(n-1))
	}
	return out
}

// bucketKey normalizes a query-returned bucket timestamp to its midnight-UTC unix
// key so it can be matched to a generated daily bucket regardless of location.
func bucketKey(t time.Time) int64 { return t.UTC().Truncate(24 * time.Hour).Unix() }

// -----------------------------------------------------------------------------
// SVG geometry (all coordinates precomputed in Go; the template only emits them).
// -----------------------------------------------------------------------------

const (
	soundChartW = 680
	soundChartH = 240
	soundPadL   = 48
	soundPadR   = 16
	soundPadT   = 16
	soundPadB   = 34
)

func soundPlotW() float64 { return soundChartW - soundPadL - soundPadR }
func soundPlotH() float64 { return soundChartH - soundPadT - soundPadB }
func soundBaseY() float64 { return soundPadT + soundPlotH() }

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// svgRect is a bar (or a stacked segment). Label/Value are used by bars only.
type svgRect struct {
	X, Y, W, H   float64
	Hue          string
	Label, Value string
}

// svgText is a positioned axis label.
type svgText struct {
	X, Y float64
	S    string
}

// svgGrid is a horizontal gridline plus its y-axis value label.
type svgGrid struct {
	Y     float64
	Label string
}

// svgDot marks a data point on a line series.
type svgDot struct{ X, Y float64 }

// svgLegend is one legend swatch.
type svgLegend struct{ Hue, Label string }

// svgSeries is one polyline series with its dots.
type svgSeries struct {
	Hue, Label, Points string
	Dots               []svgDot
}

type barChartVM struct {
	Empty               bool
	W, H, BaseY         float64
	AxisX               float64
	Bars                []svgRect
	XLabels             []svgText
	Grid                []svgGrid
	SingleHue, HueLabel string
}

type stackChartVM struct {
	Empty       bool
	W, H, BaseY float64
	AxisX       float64
	Segs        []svgRect
	XLabels     []svgText
	Grid        []svgGrid
	Legend      []svgLegend
}

type lineChartVM struct {
	Empty       bool
	W, H, BaseY float64
	AxisX       float64
	Series      []svgSeries
	XLabels     []svgText
	Grid        []svgGrid
	Legend      []svgLegend
}

// intGrid returns 0 / mid / max gridlines for an integer-valued axis.
func intGrid(maxVal int) []svgGrid {
	if maxVal <= 0 {
		return nil
	}
	base := soundBaseY()
	scale := soundPlotH() / float64(maxVal)
	vals := []int{0, (maxVal + 1) / 2, maxVal}
	seen := map[int]bool{}
	var out []svgGrid
	for _, v := range vals {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, svgGrid{Y: round2(base - float64(v)*scale), Label: fmt.Sprintf("%d", v)})
	}
	return out
}

// floatGrid returns 0 / mid / max gridlines for a float-valued axis (vCPU).
func floatGrid(maxVal float64) []svgGrid {
	if maxVal <= 0 {
		return nil
	}
	base := soundBaseY()
	scale := soundPlotH() / maxVal
	vals := []float64{0, maxVal / 2, maxVal}
	var out []svgGrid
	for _, v := range vals {
		out = append(out, svgGrid{Y: round2(base - v*scale), Label: fmt.Sprintf("%.1f", v)})
	}
	return out
}

// buildBarChart lays out one bar per (label, value) pair with a single hue. Used
// for the rung distribution and the congestus (jobs-against-ceiling) series.
func buildBarChart(labels []string, values []int, hue, hueLabel string) barChartVM {
	vm := barChartVM{W: soundChartW, H: soundChartH, BaseY: round2(soundBaseY()), AxisX: soundPadL, SingleHue: hue, HueLabel: hueLabel}
	maxVal := 0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	if len(values) == 0 || maxVal == 0 {
		vm.Empty = true
		return vm
	}
	vm.Grid = intGrid(maxVal)
	n := len(values)
	slot := soundPlotW() / float64(n)
	barW := slot * 0.62
	scale := soundPlotH() / float64(maxVal)
	base := soundBaseY()
	for i, v := range values {
		x := soundPadL + slot*float64(i) + (slot-barW)/2
		h := float64(v) * scale
		vm.Bars = append(vm.Bars, svgRect{
			X: round2(x), Y: round2(base - h), W: round2(barW), H: round2(h),
			Hue: hue, Label: labels[i], Value: fmt.Sprintf("%d", v),
		})
		vm.XLabels = append(vm.XLabels, svgText{X: round2(x + barW/2), Y: base + 14, S: labels[i]})
	}
	return vm
}

// buildStackChart stacks per-bucket segments bottom-up in the fixed categorical
// hue order. matrix[bucket][category] is the count; hues/legend are category-order.
func buildStackChart(xLabels []string, matrix [][]int, legend []svgLegend) stackChartVM {
	vm := stackChartVM{W: soundChartW, H: soundChartH, BaseY: round2(soundBaseY()), AxisX: soundPadL, Legend: legend}
	maxTotal := 0
	for _, row := range matrix {
		sum := 0
		for _, c := range row {
			sum += c
		}
		if sum > maxTotal {
			maxTotal = sum
		}
	}
	if maxTotal == 0 {
		vm.Empty = true
		return vm
	}
	vm.Grid = intGrid(maxTotal)
	n := len(matrix)
	slot := soundPlotW() / float64(n)
	barW := slot * 0.62
	scale := soundPlotH() / float64(maxTotal)
	base := soundBaseY()
	for i, row := range matrix {
		x := soundPadL + slot*float64(i) + (slot-barW)/2
		cursor := base
		for cat, c := range row {
			if c == 0 {
				continue
			}
			h := float64(c) * scale
			cursor -= h
			vm.Segs = append(vm.Segs, svgRect{
				X: round2(x), Y: round2(cursor), W: round2(barW), H: round2(h), Hue: legend[cat].Hue,
			})
		}
		vm.XLabels = append(vm.XLabels, svgText{X: round2(x + barW/2), Y: base + 14, S: xLabels[i]})
	}
	return vm
}

// buildLineChart draws one polyline per series on a shared vCPU axis. series[s] is
// aligned to xLabels; a nil entry is a gap (skipped, not zeroed).
func buildLineChart(xLabels []string, series [][]*float64, legend []svgLegend) lineChartVM {
	vm := lineChartVM{W: soundChartW, H: soundChartH, BaseY: round2(soundBaseY()), AxisX: soundPadL, Legend: legend}
	maxVal := 0.0
	for _, s := range series {
		for _, v := range s {
			if v != nil && *v > maxVal {
				maxVal = *v
			}
		}
	}
	if maxVal <= 0 {
		vm.Empty = true
		return vm
	}
	vm.Grid = floatGrid(maxVal)
	n := len(xLabels)
	slot := soundPlotW() / float64(n)
	scale := soundPlotH() / maxVal
	base := soundBaseY()
	xAt := func(i int) float64 { return soundPadL + slot*float64(i) + slot/2 }
	for s, vals := range series {
		var pts string
		var dots []svgDot
		for i, v := range vals {
			if v == nil {
				continue
			}
			x := round2(xAt(i))
			y := round2(base - *v*scale)
			if pts != "" {
				pts += " "
			}
			pts += fmt.Sprintf("%g,%g", x, y)
			dots = append(dots, svgDot{X: x, Y: y})
		}
		vm.Series = append(vm.Series, svgSeries{Hue: legend[s].Hue, Label: legend[s].Label, Points: pts, Dots: dots})
	}
	for i, lab := range xLabels {
		vm.XLabels = append(vm.XLabels, svgText{X: round2(xAt(i)), Y: base + 14, S: lab})
	}
	return vm
}

// -----------------------------------------------------------------------------
// Categorical palette for rejection reasons — FIXED order, NOT the status palette.
// -----------------------------------------------------------------------------

// soundingReasonOrder is the canonical rejection-reason order (matches the
// migration-025 CHECK set). The dashboard renders reasons in THIS order every
// render so a hue always means the same reason.
var soundingReasonOrder = []struct{ Key, Label, Hue string }{
	{sounding.ReasonTooBig, "Too big", "#6ea8ff"},
	{sounding.ReasonNoMatchingTier, "No matching tier", "#a98cff"},
	{sounding.ReasonHadToSplit, "Had to split", "#34c6b0"},
	{sounding.ReasonOptedOut, "Opted out", "#c9a0dc"},
	{sounding.ReasonNoCapacity, "No capacity", "#d97aa5"},
}

// -----------------------------------------------------------------------------
// Page data.
// -----------------------------------------------------------------------------

type soundingOpOption struct {
	Value    string
	Label    string
	Selected bool
}

type soundingRungRow struct {
	Name        string
	Order       int
	CPUCeiling  string
	MemCeiling  string
	DiskCeiling string
	State       string
	ComingSoon  bool
}

type distRow struct {
	Rung  string
	Count int
}

type congestusRow struct {
	Bucket         string
	Total          int
	AgainstCeiling int
}

type rejectionRow struct {
	Bucket string
	Counts []int
	Total  int
}

type capDemandRow struct {
	Bucket   string
	Demand   string
	Capacity string
}

type soundingPageData struct {
	Operators       []soundingOpOption
	SelectedOpLabel string
	WindowLabel     string

	Totals    sounding.Totals
	PlacedPct string

	Rungs []soundingRungRow

	Dist     barChartVM
	DistRows []distRow

	Congestus     barChartVM
	CongestusRows []congestusRow

	Rejections      stackChartVM
	RejectionLegend []svgLegend // table header order (same as chart legend)
	RejectionRows   []rejectionRow
	RejectionTotals []int // column totals across the window

	CapDemand     lineChartVM
	CapDemandRows []capDemandRow
}

// -----------------------------------------------------------------------------
// GET /admin/sounding
// -----------------------------------------------------------------------------

// handleAdminSoundingPage renders the demand-sounding dashboard for the selected
// operator (query param `operator`; empty = all-operators aggregate). Pure read.
// Every chart is server-rendered SVG with a table view beside it. It is served on
// the SAME loopback-bound, loopback-source-guarded mux as the other admin pages.
func (g *GovernanceServer) handleAdminSoundingPage(w http.ResponseWriter, r *http.Request) {
	if g.sounding == nil {
		http.Error(w, "sounding read model unavailable", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	selectedOp := r.URL.Query().Get("operator") // "" = all-operators aggregate

	ops, err := g.sounding.Operators(ctx)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := soundingPageData{WindowLabel: soundingWindowLabel}

	// Operator dropdown: "all operators" first, then each operator.
	data.Operators = append(data.Operators, soundingOpOption{Value: "", Label: "All operators (aggregate)", Selected: selectedOp == ""})
	data.SelectedOpLabel = "All operators (aggregate)"
	for _, op := range ops {
		sel := op == selectedOp
		if sel {
			data.SelectedOpLabel = op
		}
		data.Operators = append(data.Operators, soundingOpOption{Value: op, Label: op, Selected: sel})
	}

	// Headline totals.
	totals, err := g.sounding.Totals(ctx, selectedOp, soundingWindow)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data.Totals = totals
	if totals.Jobs > 0 {
		data.PlacedPct = fmt.Sprintf("%.0f%%", float64(totals.Placed)/float64(totals.Jobs)*100)
	} else {
		data.PlacedPct = "—"
	}

	// Rung ladder (incl. coming_soon storm tier). Fail-soft: an empty ladder just
	// renders no rungs rather than failing the page.
	ladder, _ := g.sounding.LoadLadderReader(ctx)
	tiers := ladder.Tiers()
	for _, t := range tiers {
		data.Rungs = append(data.Rungs, soundingRungRow{
			Name:        t.Name,
			Order:       t.Order,
			CPUCeiling:  fmt.Sprintf("%g", t.CPUCeiling),
			MemCeiling:  fmt.Sprintf("%d", t.MemCeiling),
			DiskCeiling: fmt.Sprintf("%d", t.DiskCeiling),
			State:       t.State,
			ComingSoon:  t.State == "coming_soon",
		})
	}

	// ---- Chart 1: job-shape distribution across the rung ceilings. ----
	// Axis categories are the ladder rungs in order, plus an "unplaced" bucket for
	// jobs that recorded no rung. This shows where demand sits against the ceilings.
	dist, err := g.sounding.JobShapeDistribution(ctx, selectedOp, soundingWindow)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	distByRung := map[string]int{}
	for _, d := range dist {
		distByRung[d.Rung] += d.Count
	}
	var distLabels []string
	var distValues []int
	for _, t := range tiers {
		distLabels = append(distLabels, t.Name)
		distValues = append(distValues, distByRung[t.Name])
		data.DistRows = append(data.DistRows, distRow{Rung: t.Name, Count: distByRung[t.Name]})
	}
	// Unplaced bucket (rung == "").
	distLabels = append(distLabels, "unplaced")
	distValues = append(distValues, distByRung[""])
	data.DistRows = append(data.DistRows, distRow{Rung: "unplaced", Count: distByRung[""]})
	data.Dist = buildBarChart(distLabels, distValues, "#00b4d8", "Jobs")

	// Time axis shared by charts 2-4.
	buckets := soundingDayBuckets(soundingBucketCount)
	idxOf := map[int64]int{}
	xLabels := make([]string, len(buckets))
	for i, b := range buckets {
		idxOf[b.Unix()] = i
		xLabels[i] = b.Format("01/02")
	}

	// ---- Chart 2: jobs towering against the top ceiling, over time (CAPE). ----
	cong, err := g.sounding.CongestusSeries(ctx, selectedOp, soundingWindow, soundingBucket)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	congAgainst := make([]int, len(buckets))
	congTotal := make([]int, len(buckets))
	for _, c := range cong {
		if i, ok := idxOf[bucketKey(c.Bucket)]; ok {
			congAgainst[i] = c.AgainstCeiling
			congTotal[i] = c.Total
		}
	}
	data.Congestus = buildBarChart(xLabels, congAgainst, "#00b4d8", "Against ceiling")
	for i, b := range buckets {
		data.CongestusRows = append(data.CongestusRows, congestusRow{
			Bucket: b.Format("01/02"), Total: congTotal[i], AgainstCeiling: congAgainst[i],
		})
	}

	// ---- Chart 3: rejections by reason, over time (categorical). ----
	rej, err := g.sounding.RejectionsByReason(ctx, selectedOp, soundingWindow, soundingBucket)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reasonIdx := map[string]int{}
	var legend []svgLegend
	for i, ro := range soundingReasonOrder {
		reasonIdx[ro.Key] = i
		legend = append(legend, svgLegend{Hue: ro.Hue, Label: ro.Label})
	}
	matrix := make([][]int, len(buckets))
	for i := range matrix {
		matrix[i] = make([]int, len(soundingReasonOrder))
	}
	for _, c := range rej {
		bi, ok := idxOf[bucketKey(c.Bucket)]
		if !ok {
			continue
		}
		ri, ok := reasonIdx[c.Reason]
		if !ok {
			continue // unknown reason (schema drift) — skip silently
		}
		matrix[bi][ri] += c.Count
	}
	data.Rejections = buildStackChart(xLabels, matrix, legend)
	data.RejectionLegend = legend
	data.RejectionTotals = make([]int, len(soundingReasonOrder))
	for i, b := range buckets {
		row := rejectionRow{Bucket: b.Format("01/02"), Counts: matrix[i]}
		for ri, c := range matrix[i] {
			row.Total += c
			data.RejectionTotals[ri] += c
		}
		data.RejectionRows = append(data.RejectionRows, row)
	}

	// ---- Chart 4: capacity vs demand, over time (one axis: vCPU). ----
	cd, err := g.sounding.CapacityVsDemand(ctx, selectedOp, soundingWindow, soundingBucket)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	demand := make([]*float64, len(buckets))
	capacity := make([]*float64, len(buckets))
	for _, lb := range cd {
		i, ok := idxOf[bucketKey(lb.Bucket)]
		if !ok {
			continue
		}
		if lb.HasDemand {
			v := round2(lb.DemandVCPU)
			demand[i] = &v
		}
		if lb.HasCapacity {
			v := round2(lb.CapacityVCPU)
			capacity[i] = &v
		}
	}
	cdLegend := []svgLegend{
		{Hue: "#00b4d8", Label: "Demand (avg vCPU/job)"},
		{Hue: "#a98cff", Label: "Capacity (avg vCPU/group)"},
	}
	data.CapDemand = buildLineChart(xLabels, [][]*float64{demand, capacity}, cdLegend)
	for i, b := range buckets {
		row := capDemandRow{Bucket: b.Format("01/02"), Demand: "—", Capacity: "—"}
		if demand[i] != nil {
			row.Demand = fmt.Sprintf("%.2f", *demand[i])
		}
		if capacity[i] != nil {
			row.Capacity = fmt.Sprintf("%.2f", *capacity[i])
		}
		data.CapDemandRows = append(data.CapDemandRows, row)
	}

	g.renderAdmin(w, "gov_sounding.html", data)
}
