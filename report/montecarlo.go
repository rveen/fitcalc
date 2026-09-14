package report

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rveen/fitcalc/derating"
)

// MonteCarlo is a Monte Carlo analysis: runs of the whole analysis with the
// values of elements drawn within their tolerances.
type MonteCarlo struct {
	Runs   int // requested
	Seed   uint64
	Varied []Variation
	Failed []string  // why runs failed: "run 7: at 85 °C: …"
	Total  []float64 // the total FIT of each run that succeeded; nil without a mission
}

// Variation is an element whose value varies over the runs.
type Variation struct {
	Element   string
	Kind      string  // the element letter
	Ref       string  // its component; "" for an element that is none, such as a source
	Nominal   float64 // the value in the netlist
	Tolerance float64 // %: the largest deviation, 3 σ
	Source    string  // where the tolerance comes from
}

// MonteCarloRow is a component over the runs that succeeded, in their order.
type MonteCarloRow struct {
	FIT    []float64            // NaN where there is none
	Ratios map[string][]float64 // by ratio name: the largest over the temperatures
	Levels []derating.Level     // the worst derating result over the temperatures
}

// Share returns the share of the runs whose derating result is l or worse.
func (r *MonteCarloRow) Share(l derating.Level) float64 {
	if len(r.Levels) == 0 {
		return 0
	}
	n := 0
	for _, x := range r.Levels {
		if x >= l {
			n++
		}
	}
	return float64(n) / float64(len(r.Levels))
}

// largest returns the ratio with the largest value over the runs, and that
// value.
func (r *MonteCarloRow) largest() (string, float64) {
	name, v := "", math.Inf(-1)
	for _, n := range derating.Names() {
		for _, x := range r.Ratios[n] {
			if x > v {
				name, v = n, x
			}
		}
	}
	return name, v
}

// TotalRange returns the 5 % and 95 % percentiles of the total FIT, and
// false without them.
func (mc *MonteCarlo) TotalRange() (float64, float64, bool) {
	if len(mc.Total) == 0 {
		return math.NaN(), math.NaN(), false
	}
	return percentile(mc.Total, 0.05), percentile(mc.Total, 0.95), true
}

// finite returns the values of x that are not NaN.
func finite(x []float64) []float64 {
	return slices.DeleteFunc(slices.Clone(x), math.IsNaN)
}

// mean is the mean of the values that are not NaN; NaN without any.
func mean(x []float64) float64 {
	x = finite(x)
	if len(x) == 0 {
		return math.NaN()
	}
	var s float64
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// stddev is the sample standard deviation of the values that are not NaN;
// 0 for a single one.
func stddev(x []float64) float64 {
	x = finite(x)
	if len(x) < 2 {
		return math.NaN()
	}
	m := mean(x)
	var s float64
	for _, v := range x {
		s += (v - m) * (v - m)
	}
	return math.Sqrt(s / float64(len(x)-1))
}

// percentile returns the p-quantile (0 to 1) of the values that are not NaN,
// interpolated linearly between them; NaN without any.
func percentile(x []float64, p float64) float64 {
	x = finite(x)
	if len(x) == 0 {
		return math.NaN()
	}
	slices.Sort(x)
	pos := p * float64(len(x)-1)
	i := int(math.Floor(pos))
	if i >= len(x)-1 {
		return x[len(x)-1]
	}
	return x[i] + (pos-float64(i))*(x[i+1]-x[i])
}

// share formats a share as a percentage: "14 %".
func share(x float64) string {
	return sig(100*x, 3) + " %"
}

// monteCarloWarning is the warning of a row whose derating goes above a
// limit in some runs; "" if it never does.
func monteCarloWarning(r *MonteCarloRow) string {
	warn, over := r.Share(derating.Warning), r.Share(derating.Overstress)
	if warn == 0 {
		return ""
	}
	s := "Monte Carlo: above a derating limit in " + share(warn) + " of the runs"
	if over > 0 {
		s += ", overstressed in " + share(over)
	}
	if name, v := r.largest(); name != "" {
		s += fmt.Sprintf(" (largest %s = %s %%)", name, sig(100*v, 3))
	}
	return s
}

// monteCarlo writes the Monte Carlo section.
func (m *mdWriter) monteCarlo() {
	r := m.r
	mc := r.MonteCarlo
	if mc == nil {
		return
	}
	m.p("## Monte Carlo\n\n")
	ok := mc.Runs - len(mc.Failed)
	m.p("%d runs with seed %d", mc.Runs, mc.Seed)
	if len(mc.Failed) > 0 {
		m.p(" (%d failed, left out)", len(mc.Failed))
	}
	m.p(". In each run, the value of every varied element is drawn from a normal distribution around its nominal value, with σ = tolerance / 3, truncated at the tolerance; the run is the whole analysis")
	if len(r.Corners) > 0 {
		m.p(", at %s and %s", m.nominalTemp(), Temps(r.Corners))
	}
	if r.Thermal != nil {
		m.p(", with the thermal network")
	}
	m.p(".\n\n")
	if len(mc.Varied) == 0 {
		m.p("No element varies.\n\n")
		return
	}

	m.table("Element", "Ref", "Nominal", "Tolerance", "From")
	for _, v := range mc.Varied {
		m.row(v.Element, v.Ref, si(v.Nominal, kindUnits[v.Kind]), sig(v.Tolerance, 3)+" %", v.Source)
	}
	m.p("\n")
	if ok == 0 {
		return
	}

	if lo, hi, has := mc.TotalRange(); has {
		nominal, _ := r.Total()
		m.p("**Total FIT:** %s with the nominal values; over the runs %s on average (σ %s), from %s to %s (5 %% to 95 %%).\n\n",
			sig(nominal, 3), sig(mean(mc.Total), 3), sig(stddev(mc.Total), 2), sig(lo, 3), sig(hi, 3))
	}

	var cols []string
	for _, name := range derating.Names() {
		if slices.ContainsFunc(r.Rows, func(row Row) bool { return row.MonteCarlo != nil && len(row.MonteCarlo.Ratios[name]) > 0 }) {
			cols = append(cols, name)
		}
	}
	m.p("For each component: ")
	head := []string{"Ref"}
	if r.Mission != nil {
		head = append(head, "FIT", "FIT mean", "FIT 5 %", "FIT 95 %")
		m.p("its FIT with the nominal values and over the runs; ")
	}
	m.p("the largest stress ratios over the runs and the temperatures, and the share of the runs with a derating warning or worse, and with an overstress.\n\n")
	for _, c := range cols {
		head = append(head, c+" max")
	}
	m.table(append(head, "Above a limit", "Overstress")...)
	for _, row := range r.Rows {
		x := row.MonteCarlo
		if x == nil {
			continue
		}
		cells := []string{row.Component.Ref}
		if r.Mission != nil {
			cells = append(cells, m.fitCell(row.FIT), sig(mean(x.FIT), 3), sig(percentile(x.FIT, 0.05), 3), sig(percentile(x.FIT, 0.95), 3))
		}
		for _, c := range cols {
			cell := ""
			if v := finite(x.Ratios[c]); len(v) > 0 {
				cell = ratioCell(derating.Ratio{Value: slices.Max(v)})
			}
			cells = append(cells, cell)
		}
		warn, over := x.Share(derating.Warning), x.Share(derating.Overstress)
		cells = append(cells, emphasize(share(warn), warn > 0), emphasize(share(over), over > 0))
		m.row(cells...)
	}
	m.p("\n")
}

// emphasize puts s in bold if b.
func emphasize(s string, b bool) string {
	if b {
		return "**" + s + "**"
	}
	return s
}

// kindUnits are the units of the values of the element kinds.
var kindUnits = map[string]string{"R": "Ω", "C": "F", "L": "H", "V": "V", "I": "A"}

// monteCarloSummary is the summary line of the Monte Carlo analysis.
func (m *mdWriter) monteCarloSummary() {
	mc := m.r.MonteCarlo
	if mc == nil {
		return
	}
	m.p("- **Monte Carlo:** %d runs", mc.Runs)
	if lo, hi, ok := mc.TotalRange(); ok {
		m.p(", total FIT from %s to %s (5 %% to 95 %%)", sig(lo, 3), sig(hi, 3))
	}
	var above []string
	for _, row := range m.r.Rows {
		if x := row.MonteCarlo; x != nil && x.Share(derating.Warning) > 0 {
			above = append(above, fmt.Sprintf("%s (%s)", row.Component.Ref, share(x.Share(derating.Warning))))
		}
	}
	if len(above) > 0 {
		m.p("; above a derating limit in some runs: %s", strings.Join(above, ", "))
	}
	m.p("\n")
}
