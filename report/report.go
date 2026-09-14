package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/fitcalc/thermal"
)

// Input is an input file of the analysis.
type Input struct {
	Role   string // netlist, include, parts (BOM), parts, mission
	Path   string
	SHA256 string
}

// NewInput returns an input file with its SHA-256.
func NewInput(role, path string) (Input, error) {
	f, err := os.Open(path)
	if err != nil {
		return Input{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Input{}, err
	}
	return Input{Role: role, Path: path, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// PhaseFITChange is the relative difference between a component's FIT with
// the stresses at the phase temperatures and its FIT with the nominal
// stresses, above which the component gets a warning.
const PhaseFITChange = 0.1

// Corner is a simulation at an ambient temperature besides the nominal one:
// that of one or more operating mission phases, or the one set with --hot.
type Corner struct {
	Temp   float64  // ambient temperature, °C
	Phases []string // the operating mission phases at this temperature
	Hot    bool     // set with --hot
}

// Name is the temperature with what it stands for: "85 °C (full-op)".
func (k Corner) Name() string {
	what := slices.Clone(k.Phases)
	if k.Hot {
		what = append(what, "--hot")
	}
	return sig(k.Temp, 4) + " °C (" + strings.Join(what, ", ") + ")"
}

// Temps lists the temperatures of corners: "32, 60 and 85 °C".
func Temps(corners []Corner) string {
	var t []string
	for _, k := range corners {
		t = append(t, sig(k.Temp, 4))
	}
	return list(t) + " °C"
}

// list joins items with commas and a final "and": "a, b and c".
func list(items []string) string {
	if n := len(items); n > 1 {
		items = append(slices.Clone(items[:n-2]), items[n-2]+" and "+items[n-1])
	}
	return strings.Join(items, ", ")
}

// CornerResult is a component at a corner.
type CornerResult struct {
	Corner    Corner
	Component *parts.Component // with the stresses at the temperature of the corner
	Derating  *derating.Result // at the temperature of the corner
}

// Row is one component with its results.
type Row struct {
	Component *parts.Component
	FIT       *rel.Result      // nil without a mission profile; with the stresses of each phase if there are corners
	Derating  *derating.Result // nil if not checked

	NominalFIT *rel.Result    // with the nominal stresses in every phase; nil unless FIT is per phase
	Corners    []CornerResult // in the order of Report.Corners

	// With a sweep: the component at each of its points (Report.SweepPoints),
	// while Component has the means of its stresses over them
	Points []*parts.Component

	// With a transient: the statistics of the stresses of the component over
	// time, while Component has their effective values (stress.Effective)
	Stats map[string]stress.Stats

	// With a Monte Carlo analysis: the component over its runs
	MonteCarlo *MonteCarloRow
}

// stats returns the statistics over the transient of a column (quantity) of
// a row, as quantity names it.
func stats(row Row, col string) (stress.Stats, bool) {
	c := row.Component
	key := strings.ToLower(col)
	if col == "V" {
		key = parts.VoltageKey(c.Class)
	}
	s, ok := row.Stats[key]
	return s, ok
}

// sweepRange returns the least and largest value of a column (quantity) over
// the points of the sweep of a row, its unit, the index of the point of the
// largest, and false if a point does not have it.
func sweepRange(row Row, col string) (lo, hi float64, unit string, at int, ok bool) {
	lo, hi, at = math.Inf(1), math.Inf(-1), -1
	for k, pc := range row.Points {
		q, has := quantity(Row{Component: pc}, col)
		if !has {
			return 0, 0, "", -1, false
		}
		unit = q.Unit
		lo = math.Min(lo, q.Value)
		if q.Value > hi {
			hi, at = q.Value, k
		}
	}
	return lo, hi, unit, at, len(row.Points) > 0
}

// ThermalRun is the thermal network solved at one simulation temperature,
// with the power of the last simulation of the electro-thermal iteration.
type ThermalRun struct {
	Temp        float64 // the simulation temperature, °C; NaN if unknown
	Name        string  // "27 °C (nominal)", or the name of the corner
	Solution    *thermal.Solution
	Simulations int     // of the iteration
	Converged   bool    // the temperatures agree with the power
	Change      float64 // the largest change of an element's temperature in the last simulation, K
}

// Report is what a report shows.
type Report struct {
	Title       string // the netlist title
	Netlist     string
	Project     string // the project file; "" without
	Date        time.Time
	Fitcalc     string // versions
	Fides       string
	Ngspice     string
	Inputs      []Input
	Temp        float64 // simulation temperature, °C; NaN if unknown
	Rows        []Row
	SimOnly     []*spice.Element // simulation-only elements that are not components
	DNP         []string         // references not mounted: DNP in the BOM
	Mission     *rel.Mission     // nil: no FIT
	Corners     []Corner         // the simulations besides the nominal one, by temperature
	Sweep       *spice.Sweep     // the DC sweep; nil for the operating point
	SweepPoints []string         // the points of the sweep: "V1 = 13.2 V"
	Tran        *spice.Tran      // the transient analysis; nil without
	TranPoints  int              // the number of time points of the transient
	Thermal     *thermal.Network // the thermal network; nil without
	ThermalRuns []ThermalRun     // the network solved at the nominal temperature, then at the corners
	MonteCarlo  *MonteCarlo      // the Monte Carlo analysis; nil without
	Rules       derating.Rules   // the derating rules; nil for the default one
	Warnings    []string         // general warnings; those of the components are in the rows
}

// Total returns the total FIT and the number of components without a FIT.
// Without a mission profile the total is NaN.
func (r *Report) Total() (float64, int) {
	return r.total(fit)
}

// NominalTotal is Total with the nominal stresses in every phase.
func (r *Report) NominalTotal() (float64, int) {
	return r.total(nominalFit)
}

func (r *Report) total(f func(Row) float64) (float64, int) {
	if r.Mission == nil {
		return math.NaN(), len(r.Rows)
	}
	var total float64
	var missing int
	for _, row := range r.Rows {
		if x := f(row); math.IsNaN(x) {
			missing++
		} else {
			total += x
		}
	}
	return total, missing
}

// PerPhase reports whether the FIT is calculated with the stresses of each
// phase.
func (r *Report) PerPhase() bool {
	return slices.ContainsFunc(r.Rows, func(row Row) bool { return row.NominalFIT != nil })
}

// DeratingCounts returns the number of components with an overstress and
// with a derating warning.
func (r *Report) DeratingCounts() (overstress, warning int) {
	return r.deratingCounts(nominalLevel)
}

// CornerDeratingCounts is DeratingCounts at the corners: the worst result of
// each component over them.
func (r *Report) CornerDeratingCounts() (overstress, warning int) {
	return r.deratingCounts(cornerLevel)
}

func (r *Report) deratingCounts(level func(Row) (derating.Level, bool)) (overstress, warning int) {
	for _, row := range r.Rows {
		l, ok := level(row)
		switch {
		case !ok:
		case l == derating.Overstress:
			overstress++
		case l == derating.Warning:
			warning++
		}
	}
	return overstress, warning
}

// nominalLevel is the result of the derating check of a row, and false if
// it was not checked.
func nominalLevel(row Row) (derating.Level, bool) {
	if row.Derating == nil {
		return derating.OK, false
	}
	return row.Derating.Level(), true
}

// cornerLevel is the worst result of the derating checks of a row at the
// corners, and false without corners.
func cornerLevel(row Row) (derating.Level, bool) {
	l, ok := derating.OK, false
	for _, k := range row.Corners {
		if k.Derating != nil {
			l, ok = max(l, k.Derating.Level()), true
		}
	}
	return l, ok
}

// baseRatios are the stress ratios that tables always show.
var baseRatios = []string{"V/Vmax", "P/Pmax", "I/Imax"}

// levelText is the level of a ratio, with the limit of its rule for a
// warning: "warning (limit 50 %)".
func levelText(x derating.Ratio) string {
	if x.Level == derating.Warning {
		return fmt.Sprintf("warning (limit %s %%)", sig(100*x.Limit, 3))
	}
	return x.Level.String()
}

// cornerRatio is a stress ratio at a corner.
type cornerRatio struct {
	derating.Ratio
	Corner Corner
}

// worstRatios returns the largest stress ratio of each kind over the corners
// of a row, by name.
func worstRatios(row Row) map[string]cornerRatio {
	w := map[string]cornerRatio{}
	for _, k := range row.Corners {
		if k.Derating == nil {
			continue
		}
		for _, x := range k.Derating.Ratios {
			if cur, ok := w[x.Name]; !ok || x.Value > cur.Value {
				w[x.Name] = cornerRatio{x, k.Corner}
			}
		}
	}
	return w
}

// Warnings returns the warnings of a component: from the merge of netlist
// and parts files, from the FIDES calculation, from the derating checks and
// from the comparison of its FIT with the nominal stresses.
func Warnings(row Row) []string {
	w := slices.Clone(row.Component.Warnings)
	if row.FIT != nil {
		w = append(w, row.FIT.Warnings...)
		if row.FIT.Err != nil {
			w = append(w, "no FIT: "+row.FIT.Err.Error())
		}
	}
	if row.Derating != nil {
		for _, x := range row.Derating.Ratios {
			if x.Level != derating.OK {
				w = append(w, x.String()+": "+levelText(x))
			}
		}
		for _, x := range row.Derating.Issues {
			w = append(w, x.Text+": "+x.Level.String())
		}
	}

	worst := worstRatios(row)
	for _, name := range derating.Names() {
		if x, ok := worst[name]; ok && x.Level != derating.OK {
			w = append(w, "at "+x.Corner.Name()+": "+x.String()+": "+levelText(x.Ratio))
		}
	}
	for _, k := range row.Corners {
		if k.Derating != nil {
			for _, x := range k.Derating.Issues {
				w = append(w, "at "+k.Corner.Name()+": "+x.Text+": "+x.Level.String())
			}
		}
	}
	if c, ok := fitChange(row); ok && math.Abs(c) > PhaseFITChange {
		w = append(w, fmt.Sprintf("FIT %s with the stresses at the phase temperatures (%s instead of %s with the nominal stresses): the stresses depend on the temperature",
			change(c), sig(fit(row), 3), sig(nominalFit(row), 3)))
	}
	if row.MonteCarlo != nil {
		if s := monteCarloWarning(row.MonteCarlo); s != "" {
			w = append(w, s)
		}
	}
	return w
}

// fitChange returns the relative difference between the FIT of a row and
// its FIT with the nominal stresses, and false if either is missing.
func fitChange(row Row) (float64, bool) {
	f, nominal := fit(row), nominalFit(row)
	if math.IsNaN(f) || math.IsNaN(nominal) || nominal <= 0 {
		return math.NaN(), false
	}
	return (f - nominal) / nominal, true
}

// change formats a relative change as a signed percentage with one decimal:
// +12.3 %, -6.2 %, and 0 % below 0.05 %.
func change(c float64) string {
	if math.Abs(c) < 0.0005 {
		return "0 %"
	}
	return fmt.Sprintf("%+.1f %%", 100*c)
}

// fit returns the FIT of a row, or NaN.
func fit(row Row) float64 {
	if row.FIT == nil {
		return math.NaN()
	}
	return row.FIT.FIT
}

// nominalFit returns the FIT of a row with the nominal stresses, or NaN.
func nominalFit(row Row) float64 {
	if row.NominalFIT == nil {
		return math.NaN()
	}
	return row.NominalFIT.FIT
}

// quantity returns the quantity of a report column: V (the working voltage),
// I, P, or T (the temperature rise used by FIDES).
func quantity(row Row, col string) (stress.Quantity, bool) {
	c := row.Component
	switch col {
	case "V":
		q, ok := c.Stress[parts.VoltageKey(c.Class)]
		return q, ok
	case "T":
		if row.FIT != nil {
			if q, ok := row.FIT.Inputs["T"]; ok {
				return q, true
			}
		}
		q, ok := c.Stress["t"]
		return q, ok
	}
	q, ok := c.Stress[strings.ToLower(col)]
	return q, ok
}

// units are the units of the values of R, C and L.
var units = map[string]string{"R": "Ω", "C": "F", "L": "H"}

// valueText is the value of a component with its unit (10 kΩ, 100 nF), or
// its value as written in the parts files.
func valueText(c *parts.Component) string {
	v := math.NaN()
	if c.Element != nil {
		v = c.Element.Value
	}
	if math.IsNaN(v) {
		if x, ok := c.Number("value"); ok {
			v = x
		}
	}
	if unit, ok := units[c.Class]; ok && !math.IsNaN(v) {
		return si(v, unit)
	}
	return c.Text("value")
}

// sig formats a number with n significant digits, without an exponent
// between 0.001 and 10⁶ and without trailing zeros.
func sig(v float64, n int) string {
	if math.IsNaN(v) {
		return ""
	}
	if v == 0 {
		return "0"
	}
	a := math.Abs(v)
	if a < 1e-3 || a >= 1e6 {
		return strconv.FormatFloat(v, 'g', n, 64)
	}
	d := max(n-1-int(math.Floor(math.Log10(a))), 0)
	s := strconv.FormatFloat(v, 'f', d, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

var prefixes = []string{"f", "p", "n", "µ", "m", "", "k", "M", "G", "T"} // 10⁻¹⁵ … 10¹²

// si formats a value with an SI prefix and 3 significant digits: 10 kΩ,
// 1.09 mA. Temperatures get no prefix. Values below 1 f (10⁻¹⁵) in magnitude
// are numerical noise, and 0.
func si(v float64, unit string) string {
	switch {
	case math.IsNaN(v):
		return ""
	case unit == "°C":
		return sig(v, 3) + " °C"
	case math.Abs(v) < 1e-15:
		return "0 " + unit
	}
	e := min(max(int(math.Floor(math.Log10(math.Abs(v))/3)), -5), 4)
	m := v / math.Pow(1000, float64(e))
	if r, _ := strconv.ParseFloat(sig(m, 3), 64); math.Abs(r) >= 1000 && e < 4 {
		e++
		m = v / math.Pow(1000, float64(e))
	}
	return sig(m, 3) + " " + prefixes[e+5] + unit
}

// marks are the superscript marks of the sources of values.
var marks = map[stress.Source]string{
	stress.SPICE: "ˢ", stress.Derived: "ᴰ", stress.Metadata: "ᴹ", stress.Default: "ˀ",
}

// cellValue formats a quantity with the mark of its source.
func cellValue(q stress.Quantity) string {
	return si(q.Value, q.Unit) + marks[q.Source]
}
