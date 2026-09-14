package report

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/spice"
)

// topContributors is the number of components in the list of the largest
// contributors.
const topContributors = 10

// Markdown writes the report in Markdown.
func Markdown(w io.Writer, r *Report) error {
	m := &mdWriter{r: r}
	m.header()
	m.inputs()
	m.analysis()
	m.summary()
	m.components()
	m.sweep()
	m.transient()
	m.derating()
	m.rules()
	m.temperatures()
	m.thermal()
	m.monteCarlo()
	m.metadata()
	m.excluded()
	m.mission()
	m.warnings()
	_, err := io.WriteString(w, m.b.String())
	return err
}

type mdWriter struct {
	b strings.Builder
	r *Report
}

func (m *mdWriter) p(format string, a ...any) {
	fmt.Fprintf(&m.b, format, a...)
}

func (m *mdWriter) row(cells ...string) {
	for i, c := range cells {
		cells[i] = strings.ReplaceAll(strings.ReplaceAll(c, "|", `\|`), "\n", " ")
	}
	m.p("| %s |\n", strings.Join(cells, " | "))
}

func (m *mdWriter) table(head ...string) {
	m.row(head...)
	m.p("|%s\n", strings.Repeat("---|", len(head)))
}

func (m *mdWriter) header() {
	r := m.r
	title := r.Title
	if title == "" {
		title = filepath.Base(r.Netlist)
	}
	m.p("# Reliability analysis: %s\n\n", title)
	if r.Project != "" {
		m.p("- **Project:** `%s`\n", r.Project)
	}
	m.p("- **Netlist:** `%s`\n", r.Netlist)
	m.p("- **Date:** %s\n", r.Date.UTC().Format("2006-01-02 15:04 UTC"))
	m.p("- **fitcalc:** %s\n", r.Fitcalc)
	m.p("- **FIDES 2022 Edition A:** github.com/rveen/fides %s\n", r.Fides)
	m.p("- **Simulator:** %s\n\n", r.Ngspice)
}

func (m *mdWriter) inputs() {
	m.p("## Inputs\n\n")
	m.table("File", "Role", "SHA-256")
	for _, in := range m.r.Inputs {
		m.row("`"+in.Path+"`", in.Role, "`"+in.SHA256+"`")
	}
	m.p("\n")
}

// nominalTemp is the nominal simulation temperature, as text.
func (m *mdWriter) nominalTemp() string {
	if math.IsNaN(m.r.Temp) {
		return "the simulator's default temperature"
	}
	return sig(m.r.Temp, 4) + " °C"
}

func (m *mdWriter) analysis() {
	r := m.r
	m.p("## Analysis\n\n")
	what := "DC operating point (`.op`)"
	switch {
	case r.Sweep != nil:
		what = "DC sweep (`.dc`)"
	case r.Tran != nil:
		what = "Transient analysis (`.tran`)"
	}
	if len(r.Corners) > 0 {
		var at []string
		for _, k := range r.Corners {
			at = append(at, k.Name())
		}
		m.p("- %s, simulated with %s at %s for the nominal stresses in the tables, and again at %s.\n",
			what, r.Ngspice, m.nominalTemp(), list(at))
	} else {
		m.p("- %s, simulated with %s at %s. The stresses are assumed not to depend on the temperatures of the mission phases.\n",
			what, r.Ngspice, m.nominalTemp())
	}
	if r.Sweep != nil {
		m.p("- The DC sweep of %s replaces the single operating point. The stresses in the tables and for the FIT are their means over the sweep; the derating checks take the largest ratio of each kind over it, and the warnings say where it is. The section DC sweep gives the range of the stresses.\n",
			r.Sweep)
	}
	if r.Tran != nil {
		m.p("- The transient analysis %s replaces the single operating point. The stresses in the tables and for the FIT are their RMS values over the recorded time, with the sign of their mean, and the mean power: for a circuit that does not change, those of its operating point. The derating checks compare the peak voltages, the RMS currents and the mean power with the ratings, as continuous current and power ratings are thermal. The section Transient gives the peaks.\n",
			m.tranText())
	}
	if r.Thermal != nil {
		m.p("- Thermal network `%s` (section Thermal network): the temperature of each of its components follows from the power of all of them. Every element of the network is simulated at its temperature (ngspice's `dtemp`), and each simulation is repeated until the temperatures change by no more than 0.01 K. FIDES and the derating checks take each component's local ambient, the ambient and the heating by the other components, and for diodes, transistors and ICs the rise by their own power in the network instead of P·Rth.\n",
			r.Thermal.File)
	}
	if r.MonteCarlo != nil {
		m.p("- Monte Carlo (section Monte Carlo): %d more runs of the whole analysis, with the values of elements drawn within their tolerances. The other sections are those of the nominal values.\n",
			r.MonteCarlo.Runs)
	}
	if r.Mission != nil {
		m.p("- FIDES 2022 failure rates in FIT (failures in 10⁹ hours) for the mission profile below")
		if r.PerPhase() {
			m.p(", with the stresses of each operating phase simulated at its ambient temperature. Phases that are not operating do not depend on the stresses")
		}
		m.p(".\n")
	} else {
		m.p("- No mission profile was given: no failure rates are calculated.\n")
	}
	m.p("- V is the working voltage: the reverse voltage for diodes, V_CE or V_DS for transistors, the largest voltage between two pins for subcircuits.\n")
	m.p("- T is the temperature rise FIDES uses for diodes, transistors and ICs: P·Rth, with the FIDES default of the package on an FR4 board where the parts files give no rth.\n")
	if r.Rules.Custom() {
		m.p("- Derating: a warning above the limit of the derating rule for the component and rating (see Derating rules; %.0f %% where none applies), an overstress above %.0f %% of the rating.\n",
			100*derating.WarnRatio, 100*derating.MaxRatio)
	} else {
		m.p("- Derating: a warning above %.0f %%, an overstress above %.0f %% of the rating.\n", 100*derating.WarnRatio, 100*derating.MaxRatio)
	}
	if len(r.Corners) > 0 {
		m.p("- Derating at %s: with the stresses simulated at that ambient temperature, and the power ratings derated linearly from the rated temperature (`trated`; default 70 °C for resistors, 25 °C for other classes) to 0 at `tmax`.\n",
			Temps(r.Corners))
	}
	if r.PerPhase() {
		m.p("- A component whose FIT differs by more than %.0f %% from its FIT with the nominal stresses in every phase gets a warning: its stresses depend on the temperature.\n",
			100*PhaseFITChange)
	}
	m.p("- Sources of values: ˢ SPICE results, ᴰ derived, ᴹ parts files, ˀ default.\n\n")
}

func (m *mdWriter) summary() {
	r := m.r
	m.p("## Summary\n\n")

	total, missing := r.Total()
	if r.Mission != nil {
		hours := 1e9 / total
		mtbf := sig(hours, 3) + " h"
		if hours >= 1e6 {
			mtbf = sig(hours/1e6, 3) + " × 10⁶ h"
		}
		m.p("- **Total:** %s FIT (MTBF %s, %s years, assuming constant failure rates)\n", sig(total, 3), mtbf, sig(hours/8760, 3))
		if r.PerPhase() {
			nominal, _ := r.NominalTotal()
			m.p("- **With the nominal stresses in every phase:** %s FIT", sig(nominal, 3))
			if nominal > 0 {
				m.p("; the stresses at the phase temperatures change the total by %s", change((total-nominal)/nominal))
			}
			m.p("\n")
		}
	} else {
		m.p("- **Total:** no FIT (no mission profile)\n")
	}

	inNet := 0
	for _, row := range r.Rows {
		if row.Component.InNet() {
			inNet++
		}
	}
	m.p("- **Components:** %d: %d in the netlist, %d only in the parts files", len(r.Rows), inNet, len(r.Rows)-inNet)
	if n := len(r.SimOnly); n > 0 {
		m.p("; %d simulation-only element%s left out", n, plural(n))
	}
	if n := len(r.DNP); n > 0 {
		m.p("; %d not mounted (DNP)", n)
	}
	m.p("\n")
	if r.Mission != nil {
		m.p("- **Without FIT:** %d%s\n", missing, m.refs(func(row Row) bool { return math.IsNaN(fit(row)) }))
	}

	over, warn := r.DeratingCounts()
	m.p("- **Derating:** %s\n", m.deratingCounts(over, warn, nominalLevel))
	if len(r.Corners) > 0 {
		over, warn := r.CornerDeratingCounts()
		m.p("- **Derating at %s:** %s\n", Temps(r.Corners), m.deratingCounts(over, warn, cornerLevel))
	}

	withWarnings := 0
	for _, row := range r.Rows {
		if len(Warnings(row)) > 0 {
			withWarnings++
		}
	}
	m.monteCarloSummary()
	m.p("- **Warnings:** %d general, %d component%s with warnings\n\n", len(r.Warnings), withWarnings, plural(withWarnings))

	if r.Mission == nil || total <= 0 {
		return
	}

	blocks := map[string]float64{}
	for _, row := range r.Rows {
		if f := fit(row); !math.IsNaN(f) {
			blocks[row.Component.Text("block")] += f
		}
	}
	if len(blocks) > 1 || blocks[""] == 0 {
		names := make([]string, 0, len(blocks))
		for b := range blocks {
			names = append(names, b)
		}
		sort.Slice(names, func(i, j int) bool { return blocks[names[i]] > blocks[names[j]] })
		m.p("### FIT per block\n\n")
		m.table("Block", "FIT", "%")
		for _, b := range names {
			label := b
			if b == "" {
				label = "(none)"
			}
			m.row(label, sig(blocks[b], 3), sig(100*blocks[b]/total, 3))
		}
		m.p("\n")
	}

	var rows []Row
	for _, row := range r.Rows {
		if !math.IsNaN(fit(row)) {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return fit(rows[i]) > fit(rows[j]) })
	m.p("### Largest contributors\n\n")
	m.table("Ref", "Class", "FIT", "%", "Cumulative %")
	var cum float64
	for _, row := range rows[:min(topContributors, len(rows))] {
		cum += fit(row)
		m.row(row.Component.Ref, row.Component.Class, sig(fit(row), 3), sig(100*fit(row)/total, 3), sig(100*cum/total, 3))
	}
	m.p("\n")
}

// deratingCounts is the text of derating counts, with the references of the
// rows at each level.
func (m *mdWriter) deratingCounts(over, warn int, level func(Row) (derating.Level, bool)) string {
	at := func(l derating.Level) func(Row) bool {
		return func(row Row) bool {
			x, ok := level(row)
			return ok && x == l
		}
	}
	return fmt.Sprintf("%d overstressed%s, %d with warnings%s",
		over, m.refs(at(derating.Overstress)), warn, m.refs(at(derating.Warning)))
}

// refs lists the references of the rows that match, in parentheses.
func (m *mdWriter) refs(match func(Row) bool) string {
	var refs []string
	for _, row := range m.r.Rows {
		if match(row) {
			refs = append(refs, row.Component.Ref)
		}
	}
	if len(refs) == 0 {
		return ""
	}
	return " (" + strings.Join(refs, ", ") + ")"
}

func (m *mdWriter) components() {
	m.p("## Components\n\n")
	m.table("Ref", "Class", "Value", "V", "I", "P", "T", "FIT")
	for _, row := range m.r.Rows {
		c := row.Component
		cells := append([]string{c.Ref, c.Class, valueText(c)}, stressCells(row)...)
		m.row(append(cells, m.fitCell(row.FIT))...)
	}
	m.p("\n")
}

// sweep writes the range of the stresses over the DC sweep.
func (m *mdWriter) sweep() {
	r := m.r
	if r.Sweep == nil {
		return
	}
	m.p("## DC sweep\n\n")
	m.p("The stresses over the sweep of %s: the range of V and I, and the mean and the largest P, with the point where it is largest.\n\n", r.Sweep)
	m.table("Ref", "V", "I", "P mean", "P max", "At")
	for _, row := range r.Rows {
		if len(row.Points) == 0 || len(row.Component.Stress) == 0 {
			continue
		}
		cells := []string{row.Component.Ref}
		for _, col := range []string{"V", "I"} {
			cell := ""
			if lo, hi, unit, _, ok := sweepRange(row, col); ok {
				cell = si(lo, unit)
				if h := si(hi, unit); h != cell {
					cell += " … " + h
				}
			}
			cells = append(cells, cell)
		}
		mean, max, at := "", "", ""
		if q, ok := quantity(row, "P"); ok {
			mean = si(q.Value, q.Unit)
		}
		if _, hi, unit, k, ok := sweepRange(row, "P"); ok {
			max = si(hi, unit)
			if k < len(r.SweepPoints) {
				at = r.SweepPoints[k]
			}
		}
		m.row(append(cells, mean, max, at)...)
	}
	m.p("\n")
}

// tranText describes the transient analysis, with its number of time points.
func (m *mdWriter) tranText() string {
	s := m.r.Tran.String()
	if n := m.r.TranPoints; n > 0 {
		s += fmt.Sprintf(" (%d time points)", n)
	}
	return s
}

// transient writes the peaks, RMS values and means of the stresses over the
// transient.
func (m *mdWriter) transient() {
	r := m.r
	if r.Tran == nil {
		return
	}
	m.p("## Transient\n\n")
	m.p("The stresses over the transient %s: the peak of V, I and P, the value of the largest magnitude with the time where it is, the RMS values of V and I and the mean of P.\n\n", m.tranText())
	m.table("Ref", "V peak", "V RMS", "I peak", "I RMS", "P peak", "P mean")
	for _, row := range r.Rows {
		if len(row.Stats) == 0 || len(row.Component.Stress) == 0 {
			continue
		}
		cells := []string{row.Component.Ref}
		for _, col := range []string{"V", "I", "P"} {
			s, ok := stats(row, col)
			if !ok {
				cells = append(cells, "", "")
				continue
			}
			peak, at := s.Peak()
			second := s.RMS
			if col == "P" {
				second = s.Mean
			}
			cells = append(cells, si(peak, s.Unit)+" at "+spice.Time(at), si(second, s.Unit))
		}
		m.row(cells...)
	}
	m.p("\n")
}

// stressCells are the V, I, P and T cells of a row.
func stressCells(row Row) []string {
	var cells []string
	for _, col := range []string{"V", "I", "P", "T"} {
		if q, ok := quantity(row, col); ok {
			cells = append(cells, cellValue(q))
		} else {
			cells = append(cells, "")
		}
	}
	return cells
}

// fitCell is the cell of a FIT result: — without a mission profile, and
// **none** if it could not be calculated.
func (m *mdWriter) fitCell(f *rel.Result) string {
	switch {
	case m.r.Mission == nil:
		return "—"
	case f == nil:
		return ""
	case f.Err != nil:
		return "**none**"
	}
	return sig(f.FIT, 3)
}

// ratioColumns are the ratio columns of a table: V/Vmax, P/Pmax and I/Imax,
// and the other ratios that the derating checks of the rows have.
func (m *mdWriter) ratioColumns(checks func(Row) []*derating.Result) []string {
	cols := slices.Clone(baseRatios)
	for _, name := range derating.Names() {
		if slices.Contains(cols, name) {
			continue
		}
		if slices.ContainsFunc(m.r.Rows, func(row Row) bool {
			return slices.ContainsFunc(checks(row), func(d *derating.Result) bool {
				return d != nil && slices.ContainsFunc(d.Ratios, func(x derating.Ratio) bool { return x.Name == name })
			})
		}) {
			cols = append(cols, name)
		}
	}
	return cols
}

// nominalChecks and cornerChecks are the derating checks of a row.
func nominalChecks(row Row) []*derating.Result { return []*derating.Result{row.Derating} }

func cornerChecks(row Row) []*derating.Result {
	var d []*derating.Result
	for _, k := range row.Corners {
		d = append(d, k.Derating)
	}
	return d
}

func (m *mdWriter) derating() {
	m.p("## Derating\n\n")
	cols := m.ratioColumns(nominalChecks)
	m.table(append(append([]string{"Ref"}, cols...), "Result")...)
	var noRatings []string
	for _, row := range m.r.Rows {
		d := row.Derating
		if d == nil || len(d.Ratios) == 0 && len(d.Issues) == 0 {
			if len(row.Component.Stress) > 0 {
				noRatings = append(noRatings, row.Component.Ref)
			}
			continue
		}
		cells := []string{row.Component.Ref}
		for _, name := range cols {
			cell := ""
			for _, x := range d.Ratios {
				if x.Name == name {
					cell = ratioCell(x)
				}
			}
			cells = append(cells, cell)
		}
		m.row(append(cells, levelCell(d.Level())+issues(d, ""))...)
	}
	m.p("\n")
	if len(noRatings) > 0 {
		m.p("No ratings in the parts files for: %s.\n\n", strings.Join(noRatings, ", "))
	}
}

// rules writes the derating rules, if there are others than the default one.
func (m *mdWriter) rules() {
	if !m.r.Rules.Custom() {
		return
	}
	m.p("## Derating rules\n\n")
	m.p("Each ratio takes the most specific rule that matches the component's class, its tags and the rating: the one with the most tags, then one for its class, then one for the rating.\n\n")
	m.table("Class", "Tags", "Rating", "Limit", "From")
	for _, x := range m.r.Rules {
		m.row(x.Class, strings.Join(x.Tags, " "), x.Rating, sig(100*x.Limit, 3)+" %", x.Source)
	}
	m.p("\n")
}

// ratioCell is a stress ratio in percent, emphasized for a warning and in
// bold for an overstress.
func ratioCell(x derating.Ratio) string {
	cell := sig(100*x.Value, 3) + " %"
	if x.Value > 0 && x.Value < 0.001 {
		cell = "< 0.1 %"
	}
	switch x.Level {
	case derating.Overstress:
		cell = "**" + cell + "**"
	case derating.Warning:
		cell = "*" + cell + "*"
	}
	return cell
}

// levelCell is the result of a derating check, in bold unless it is ok.
func levelCell(l derating.Level) string {
	if l != derating.OK {
		return "**" + l.String() + "**"
	}
	return l.String()
}

// issues lists the issues of a derating check, each after "; " and prefix.
func issues(d *derating.Result, prefix string) string {
	var s string
	for _, x := range d.Issues {
		s += "; " + prefix + x.Text
	}
	return s
}

// temperatures writes the stresses and FIT of each phase, and the derating
// at the corners.
func (m *mdWriter) temperatures() {
	r := m.r
	if len(r.Corners) == 0 {
		return
	}
	m.p("## Temperatures\n\n")

	if r.PerPhase() {
		total, _ := r.Total()
		nominal := m.nominalTemp() + " (nominal)"
		m.p("The stresses of each mission phase, and its share of the total FIT.\n\n")
		m.table("Phase", "On", "Tamb", "Duration", "Stresses at", "FIT", "%")
		for i, ph := range r.Mission.Phases {
			at := nominal
			for _, k := range r.Corners {
				if ph.On && k.Temp == ph.Tamb && slices.Contains(k.Phases, ph.Name) {
					at = sig(k.Temp, 4) + " °C"
				}
			}
			var f float64
			for _, row := range r.Rows {
				if row.FIT != nil && i < len(row.FIT.Phases) {
					f += row.FIT.Phases[i]
				}
			}
			on := "no"
			if ph.On {
				on = "yes"
			}
			m.row(ph.Name, on, sig(ph.Tamb, 4)+" °C", sig(ph.Duration, 6)+" h", at, sig(f, 3), sig(100*f/total, 3))
		}
		m.p("\n")
	}

	m.p("For each component, the largest stress ratios at %s, relative to the power ratings derated for the temperature", Temps(r.Corners))
	if r.PerPhase() {
		m.p(", and its FIT compared with the FIT with the nominal stresses in every phase")
	}
	m.p(".\n\n")

	head := []string{"Ref"}
	if r.PerPhase() {
		head = append(head, "FIT", "Nominal FIT", "Change")
	}
	cols := m.ratioColumns(cornerChecks)
	m.table(append(append(head, cols...), "Result")...)

	for _, row := range r.Rows {
		level, ok := cornerLevel(row)
		worst := worstRatios(row)
		if !ok || len(worst) == 0 && level == derating.OK && row.NominalFIT == nil {
			continue
		}
		cells := []string{row.Component.Ref}
		if r.PerPhase() {
			diff := ""
			if c, ok := fitChange(row); ok {
				diff = change(c)
				if math.Abs(c) > PhaseFITChange {
					diff = "**" + diff + "**"
				}
			}
			cells = append(cells, m.fitCell(row.FIT), m.fitCell(row.NominalFIT), diff)
		}
		for _, name := range cols {
			cell := ""
			if x, ok := worst[name]; ok {
				cell = ratioCell(x.Ratio) + " at " + sig(x.Corner.Temp, 4) + " °C"
			}
			cells = append(cells, cell)
		}
		result := levelCell(level)
		for _, k := range row.Corners {
			if k.Derating != nil {
				result += issues(k.Derating, "at "+sig(k.Corner.Temp, 4)+" °C: ")
			}
		}
		m.row(append(cells, result)...)
	}
	m.p("\n")
}

// thermal writes the thermal network and the temperatures of its nodes.
func (m *mdWriter) thermal() {
	r := m.r
	net := r.Thermal
	if net == nil || len(r.ThermalRuns) == 0 {
		return
	}
	m.p("## Thermal network\n\n")
	var sims []string
	for _, run := range r.ThermalRuns {
		sims = append(sims, fmt.Sprintf("%d at %s", run.Simulations, sig(run.Temp, 4)+" °C"))
	}
	m.p("`%s`: %d thermal resistances between %d nodes and the ambient (amb). Simulations until the temperatures agree with the power: %s.\n\n",
		net.File, len(net.Edges), len(net.Nodes), list(sims))
	m.table("From", "To", "Rth", "Line")
	for _, e := range net.Edges {
		m.row(e.From, e.To, sig(e.Rth, 4)+" K/W", fmt.Sprint(e.Line))
	}
	m.p("\n")

	byRef := map[string]*Row{}
	for i := range r.Rows {
		byRef[strings.ToUpper(r.Rows[i].Component.Ref)] = &r.Rows[i]
	}
	nom := r.ThermalRuns[0]
	m.p("The power into each node and its temperature rise at %s: by its own power and by the power of the others; then its temperature at each simulated temperature.\n\n", nom.Name)
	head := []string{"Node", "Class", "P", "Own rise", "From the others"}
	for _, run := range r.ThermalRuns {
		head = append(head, "At "+sig(run.Temp, 4)+" °C")
	}
	m.table(head...)
	for _, n := range net.Nodes {
		class, p := "", ""
		if row := byRef[n]; row != nil {
			class = row.Component.Class
			p = si(nom.Solution.Power[n], "W")
		}
		cells := []string{n, class, p, sig(nom.Solution.Own[n], 3) + " K", sig(nom.Solution.Coupled(n), 3) + " K"}
		for _, run := range r.ThermalRuns {
			cells = append(cells, sig(run.Temp+run.Solution.Rise[n], 4)+" °C")
		}
		m.row(cells...)
	}
	m.p("\n")
}

func (m *mdWriter) metadata() {
	m.p("## FIDES metadata\n\n")
	m.table("Ref", "Class", "Tags", "Package", "Part", "Vmax", "Pmax", "Imax", "Tmax", "From", "Notes")
	for _, row := range m.r.Rows {
		c := row.Component
		rating := func(field, unit string) string {
			if s := c.Text(field); s != "" {
				return s + " " + unit
			}
			return ""
		}
		m.row(c.Ref, c.Class, strings.Join(c.Tags, " "), c.Text("package"), strings.TrimSpace(c.Text("manufacturer")+" "+c.Text("mpn")),
			rating("vmax", "V"), rating("pmax", "W"), rating("imax", "A"), rating("tmax", "°C"),
			m.sources(c), strings.Join(c.Notes, "; "))
	}
	m.p("\n")
}

// sources lists the rows of the parts files a component takes its metadata
// from, in the order of the files.
func (m *mdWriter) sources(c *parts.Component) string {
	order := map[string]int{}
	for i, in := range m.r.Inputs {
		order[in.Path] = i
	}
	type src struct{ file, name string }
	var all []src
	for _, f := range c.Meta {
		for _, s := range f.Sources {
			if !slices.Contains(all, src{s.File, s.Name}) {
				all = append(all, src{s.File, s.Name})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if order[all[i].file] != order[all[j].file] {
			return order[all[i].file] < order[all[j].file]
		}
		return all[i].name < all[j].name
	})
	var s []string
	for _, x := range all {
		s = append(s, filepath.Base(x.file)+" ("+x.name+")")
	}
	return strings.Join(s, ", ")
}

func (m *mdWriter) excluded() {
	var bomOnly []string
	for _, row := range m.r.Rows {
		if !row.Component.InNet() {
			bomOnly = append(bomOnly, row.Component.Ref)
		}
	}
	if len(m.r.SimOnly) == 0 && len(bomOnly) == 0 && len(m.r.DNP) == 0 {
		return
	}
	m.p("## Not simulated or not rated\n\n")
	for _, e := range m.r.SimOnly {
		m.p("- `%s` (%s): simulation only, not a component.\n", e.Name, parts.KindName(e.Kind))
	}
	if len(bomOnly) > 0 {
		m.p("- Only in the parts files, without SPICE stress: %s.\n", strings.Join(bomOnly, ", "))
	}
	if len(m.r.DNP) > 0 {
		m.p("- Not mounted (DNP in the BOM): %s.\n", strings.Join(m.r.DNP, ", "))
	}
	m.p("\n")
}

func (m *mdWriter) mission() {
	if m.r.Mission == nil {
		return
	}
	m.p("## Mission profile\n\n")
	m.p("%s h in total.\n\n", sig(m.r.Mission.Ttotal, 6))
	m.p("%s\n", m.r.Mission.ToMD())
}

func (m *mdWriter) warnings() {
	m.p("## Warnings\n\n")
	n := 0
	for _, w := range m.r.Warnings {
		m.p("- %s\n", w)
		n++
	}
	for _, row := range m.r.Rows {
		for _, w := range Warnings(row) {
			m.p("- **%s**: %s\n", row.Component.Ref, w)
			n++
		}
	}
	if n == 0 {
		m.p("None.\n")
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
