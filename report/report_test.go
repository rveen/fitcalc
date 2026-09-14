package report

import (
	"bytes"
	"errors"
	"flag"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/fitcalc/thermal"
	"github.com/rveen/golib/csv"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

func TestSI(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		unit string
		want string
	}{
		{10e3, "Ω", "10 kΩ"},
		{1.0909e-3, "A", "1.09 mA"},
		{0.1273, "W", "127 mW"},
		{0, "V", "0 V"},
		{-12, "V", "-12 V"},
		{1.2e-11, "A", "12 pA"},
		{2e-15, "A", "2 fA"},
		{-7.77e-20, "A", "0 A"},
		{999.7, "V", "1 kV"},
		{1e6, "Ω", "1 MΩ"},
		{100e-9, "F", "100 nF"},
		{2.729, "°C", "2.73 °C"},
		{math.NaN(), "V", ""},
	} {
		if got := si(tc.v, tc.unit); got != tc.want {
			t.Errorf("si(%g, %q) = %q, want %q", tc.v, tc.unit, got, tc.want)
		}
	}
	for v, want := range map[float64]string{0.3666: "0.367", 39.87: "39.9", 65.98: "66", 1.5e7: "1.5e+07", 0.00012: "0.00012"} {
		if got := sig(v, 3); got != want {
			t.Errorf("sig(%g) = %q, want %q", v, got, want)
		}
	}
	for c, want := range map[float64]string{0.3639: "+36.4 %", -0.0667: "-6.7 %", 0.0049: "+0.5 %", 3.4e-6: "0 %", -1e-9: "0 %", 0: "0 %"} {
		if got := change(c); got != want {
			t.Errorf("change(%g) = %q, want %q", c, got, want)
		}
	}
}

func meta(file, name string, fields map[string]string) map[string]csv.Field {
	m := map[string]csv.Field{}
	for k, v := range fields {
		m[k] = csv.Field{Value: v, Sources: []csv.Source{{File: file, Name: name}}}
	}
	return m
}

func q(v float64, unit string, src stress.Source, note string) stress.Quantity {
	return stress.Quantity{Value: v, Unit: unit, Source: src, Note: note}
}

// sample is a report with one of each case: a component with and without
// FIT, an overstress, a subcircuit, a part only in the BOM, a
// simulation-only element and blocks.
func sample(t *testing.T) *Report {
	t.Helper()

	m, err := rel.LoadMission("../testdata/mission.csv")
	if err != nil {
		t.Fatal(err)
	}

	r1 := &parts.Component{
		Ref: "R1", Element: &spice.Element{Name: "R1", Kind: "R", Value: 10e3}, Class: "R", Tags: []string{"thick"},
		Meta: meta("parts.db.csv", "R0603", map[string]string{"package": "0603", "vmax": "75", "pmax": "0.1", "tmax": "155", "block": "B1"}),
		Stress: stress.Stress{
			"v": q(10.91, "V", stress.Derived, "v(vcc) − v(out)"), "i": q(1.091e-3, "A", stress.SPICE, "@r1[i]"),
			"p": q(0.0119, "W", stress.Derived, "v·i")},
	}
	r3 := &parts.Component{
		Ref: "R3", Element: &spice.Element{Name: "R3", Kind: "R", Value: 1e3}, Class: "R", Tags: []string{"thick"},
		Meta:   meta("parts.db.csv", "R0603", map[string]string{"package": "0603", "vmax": "75", "pmax": "0.1", "tmax": "155", "block": "B1"}),
		Stress: stress.Stress{"v": q(11.28, "V", stress.Derived, ""), "i": q(0.01128, "A", stress.SPICE, ""), "p": q(0.1273, "W", stress.Derived, "")},
	}
	d2 := &parts.Component{
		Ref: "D2", Element: &spice.Element{Name: "D2", Kind: "D", Value: math.NaN()}, Class: "D",
		Meta: meta("parts.db.csv", "D1N4148", map[string]string{"package": "SOD123", "vmax": "100", "imax": "0.2", "tmax": "150", "block": "B2"}),
		Stress: stress.Stress{"v": q(-12, "V", stress.Derived, ""), "vr": q(12, "V", stress.Derived, ""),
			"i": q(-1.2e-11, "A", stress.SPICE, ""), "p": q(1.44e-10, "W", stress.Derived, "")},
	}
	x1 := &parts.Component{
		Ref: "U1", Element: &spice.Element{Name: "XU1", Kind: "X", Value: math.NaN()}, Class: "U", Tags: []string{"analog"},
		Meta:   meta("probe.bom.csv", "U1", map[string]string{"class": "U", "tags": "analog", "package": "SOIC8", "vmax": "36", "tmax": "125"}),
		Stress: stress.Stress{"v": q(12, "V", stress.Derived, ""), "v:vcc": q(12, "V", stress.SPICE, "")},
		Notes:  []string{"netlist element XU1"},
	}
	j9 := &parts.Component{
		Ref: "J9", Class: "J", Tags: []string{"tht"},
		Meta:     meta("parts.db.csv", "TB2", map[string]string{"npins": "2", "tmax": "105", "vmax": "300"}),
		Warnings: []string{"not in the netlist: no SPICE stress"},
	}

	rows := []Row{
		{Component: r1, FIT: &rel.Result{FIT: 0.3666}},
		{Component: r3, FIT: &rel.Result{FIT: math.NaN(), Err: errors.New("Actual power (0.127288 W) exceeds its Pmax (0.100000 W) R=1000")}},
		{Component: d2, FIT: &rel.Result{FIT: 1.494, Inputs: stress.Stress{"T": q(4.857e-8, "°C", stress.Derived, "p·rth")}}},
		{Component: x1, FIT: &rel.Result{FIT: 39.87, Warnings: []string{"self-heating not included: no power"}}},
		{Component: j9, FIT: &rel.Result{FIT: 0.7879}},
	}
	for i := range rows {
		rows[i].FIT.Component = rows[i].Component
		rows[i].Derating = derating.Check(rows[i].Component)
	}

	return &Report{
		Title:   "Sample circuit",
		Netlist: "testdata/sample.net",
		Date:    time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC),
		Fitcalc: "v1.0.0",
		Fides:   "v0.1.0",
		Ngspice: "ngspice-47",
		Inputs: []Input{
			{"netlist", "testdata/sample.net", strings.Repeat("a", 64)},
			{"parts (BOM)", "testdata/probe.bom.csv", strings.Repeat("b", 64)},
			{"parts", "testdata/parts.db.csv", strings.Repeat("c", 64)},
			{"mission", "testdata/mission.csv", strings.Repeat("d", 64)},
		},
		Temp:     27,
		Rows:     rows,
		SimOnly:  []*spice.Element{{Name: "V1", Kind: "V"}},
		Mission:  m,
		Warnings: []string{"testdata/sample.net: analysis and output cards commented out: .op (line 9)"},
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("../testdata/golden", name)
	if *update {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file:\n%s", name, got)
	}
}

func TestMarkdown(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sample(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report.md", b.Bytes())
}

func TestCSV(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sample(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report.csv", b.Bytes())
}

// sampleCorners is sample with the operating phases of the mission simulated
// at 32, 60 and 85 °C: the power 10 % higher at 60 °C and 20 % at 85 °C, and
// R1's FIT up by more than 10 %.
func sampleCorners(t *testing.T) *Report {
	t.Helper()
	r := sample(t)
	r.Corners = []Corner{
		{Temp: 32, Phases: []string{"start-night", "start-day"}},
		{Temp: 60, Phases: []string{"motorway"}},
		{Temp: 85, Phases: []string{"full-op"}},
	}
	power := []float64{1, 1.1, 1.2}                     // by corner
	shares := []float64{0.1, 0.4, 0.1, 0.05, 0.3, 0.05} // of the FIT, by phase
	fits := []float64{0.5, math.NaN(), 1.52, 39.87, 0.7879}
	for i := range r.Rows {
		row := &r.Rows[i]
		row.NominalFIT = row.FIT
		row.FIT = &rel.Result{Component: row.Component, FIT: fits[i], Inputs: row.NominalFIT.Inputs, Warnings: row.NominalFIT.Warnings}
		if math.IsNaN(fits[i]) {
			row.FIT.Err = errors.New("phase full-op: Actual power (0.152746 W) exceeds its Pmax (0.100000 W) R=1000")
		} else {
			for _, s := range shares {
				row.FIT.Phases = append(row.FIT.Phases, s*fits[i])
			}
		}
		for k, corner := range r.Corners {
			c := *row.Component
			c.Stress = maps.Clone(c.Stress)
			if p, ok := c.Stress["p"]; ok {
				p.Value *= power[k]
				c.Stress["p"] = p
			}
			row.Corners = append(row.Corners, CornerResult{Corner: corner, Component: &c, Derating: derating.CheckAt(&c, corner.Temp)})
		}
	}
	return r
}

func TestMarkdownCorners(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sampleCorners(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_corners.md", b.Bytes())

	for _, want := range []string{
		"- **Total:** 42.7 FIT",
		"- **With the nominal stresses in every phase:** 42.5 FIT; the stresses at the phase temperatures change the total by +0.4 %\n",
		"- **Derating at 32, 60 and 85 °C:** 1 overstressed (R3), 0 with warnings\n",
		"| off-day | no | 14 °C | 720 h | 27 °C (nominal) | 4.27 | 10 |\n",
		"| full-op | yes | 85 °C | 201 h | 85 °C | 12.8 | 30 |\n",
		"| R1 | 0.5 | 0.367 | **+36.4 %** | 14.5 % at 32 °C | 17.3 % at 85 °C |  | ok |\n",
		"- **R1**: FIT +36.4 % with the stresses at the phase temperatures (0.5 instead of 0.367 with the nominal stresses)",
		"- **R3**: at 85 °C (full-op): P/Pmax = 185% (0.1528 W of 0.08235 W): overstress\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report without %q", want)
		}
	}
}

func TestCSVCorners(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sampleCorners(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_corners.csv", b.Bytes())
}

// sampleSweep is sample with a DC sweep of V1 at three points, the stresses
// at 90 %, 100 % and 110 % of the operating point.
func sampleSweep(t *testing.T) *Report {
	t.Helper()
	r := sample(t)
	w, err := spice.ParseSweep("V1 10.8 13.2 1.2")
	if err != nil {
		t.Fatal(err)
	}
	r.Sweep, r.SweepPoints = w, []string{"V1 = 10.8 V", "V1 = 12 V", "V1 = 13.2 V"}
	for i := range r.Rows {
		row := &r.Rows[i]
		var checks []*derating.Result
		for _, f := range []float64{0.9, 1, 1.1} {
			c := *row.Component
			c.Stress = stress.Stress{}
			for k, q := range row.Component.Stress {
				q.Value *= f
				c.Stress[k] = q
			}
			row.Points = append(row.Points, &c)
			checks = append(checks, derating.Check(&c))
		}
		row.Derating = derating.Worst(row.Component, checks, r.SweepPoints)
	}
	return r
}

func TestMarkdownSweep(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sampleSweep(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_sweep.md", b.Bytes())

	for _, want := range []string{
		"- DC sweep (`.dc`), simulated with ngspice-47 at 27 °C.",
		"- The DC sweep of V1 from 10.8 V to 13.2 V in steps of 1.2 V (3 points) replaces the single operating point.",
		"| R1 | 9.82 V … 12 V | 982 µA … 1.2 mA | 11.9 mW | 13.1 mW | V1 = 13.2 V |\n",
		"| U1 | 10.8 V … 13.2 V |  |  |  |  |\n",
		"- **R3**: P/Pmax = 140% (0.14 W of 0.1 W) with V1 = 13.2 V: overstress\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report without %q", want)
		}
	}
	_, section, _ := strings.Cut(b.String(), "## DC sweep")
	section, _, _ = strings.Cut(section, "## Derating")
	if strings.Contains(section, "| J9 |") {
		t.Error("J9, without stresses, in the DC sweep table")
	}
}

func TestCSVSweep(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sampleSweep(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_sweep.csv", b.Bytes())
}

// sampleTran is sample with a transient analysis, over which each stress goes
// from 90 % (at 1.5 ms) to 110 % (at 2.25 ms) of its effective value, with an
// RMS value 1 % above its mean.
func sampleTran(t *testing.T) *Report {
	t.Helper()
	r := sample(t)
	tr, err := spice.ParseTran("10u 3m 1m")
	if err != nil {
		t.Fatal(err)
	}
	r.Tran, r.TranPoints = tr, 201
	for i := range r.Rows {
		row := &r.Rows[i]
		if len(row.Component.Stress) == 0 {
			continue
		}
		row.Stats = map[string]stress.Stats{}
		for k, q := range row.Component.Stress {
			lo, hi := 0.9*q.Value, 1.1*q.Value
			row.Stats[k] = stress.Stats{Unit: q.Unit, Source: q.Source, Note: q.Note,
				Min: math.Min(lo, hi), Max: math.Max(lo, hi), MinAt: 1.5e-3, MaxAt: 2.25e-3, Mean: q.Value, RMS: 1.01 * math.Abs(q.Value)}
		}
	}
	return r
}

func TestMarkdownTran(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sampleTran(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_tran.md", b.Bytes())

	for _, want := range []string{
		"- Transient analysis (`.tran`), simulated with ngspice-47 at 27 °C.",
		"- The transient analysis from 0 to 3 ms in steps of 10 µs, recorded from 1 ms (201 time points) replaces the single operating point.",
		"| R1 | 12 V at 2.25 ms | 11 V | 1.2 mA at 2.25 ms | 1.1 mA | 13.1 mW at 2.25 ms | 11.9 mW |\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report without %q", want)
		}
	}
	_, section, _ := strings.Cut(b.String(), "## Transient")
	section, _, _ = strings.Cut(section, "## Derating")
	if strings.Contains(section, "| J9 |") {
		t.Error("J9, without stresses, in the transient table")
	}
}

func TestCSVTran(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sampleTran(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_tran.csv", b.Bytes())
}

// sampleThermal is sample with a thermal network of R1, R3 and U1 on a
// board, solved at 27 °C and at 85 °C.
func sampleThermal(t *testing.T) *Report {
	t.Helper()
	r := sample(t)
	net, err := thermal.Read(strings.NewReader("from,to,rth\nR1,board,100\nR3,board,200\nU1,board,50\nboard,amb,20\n"))
	if err != nil {
		t.Fatal(err)
	}
	net.File = "thermal.csv"
	sol := net.Solve(map[string]float64{"R1": 0.0119, "R3": 0.1273})
	r.Thermal = net
	r.ThermalRuns = []ThermalRun{
		{Temp: 27, Name: "27 °C (nominal)", Solution: sol, Simulations: 2, Converged: true},
		{Temp: 85, Name: "85 °C (full-op)", Solution: sol, Simulations: 3, Converged: true},
	}
	return r
}

func TestMarkdownThermal(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sampleThermal(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_thermal.md", b.Bytes())

	for _, want := range []string{
		"- Thermal network `thermal.csv` (section Thermal network): ",
		"`thermal.csv`: 4 thermal resistances between 4 nodes and the ambient (amb). Simulations until the temperatures agree with the power: 2 at 27 °C and 3 at 85 °C.",
		"| R3 | BOARD | 200 K/W | 3 |\n",
		"| R3 | R | 127 mW | 28 K | 0.238 K | 55.24 °C | 113.2 °C |\n",
		"| BOARD |  |  | 0 K | 2.78 K | 29.78 °C | 87.78 °C |\n",
		"| U1 | U | 0 W | 0 K | 2.78 K | 29.78 °C | 87.78 °C |\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report without %q", want)
		}
	}
}

func TestCSVThermal(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sampleThermal(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_thermal.csv", b.Bytes())
	if !strings.Contains(b.String(), ",28.244,0.238,") {
		t.Errorf("no thermal columns for R3:\n%s", b.String())
	}
}

func TestTemps(t *testing.T) {
	for _, tc := range []struct {
		temps []float64
		want  string
	}{
		{[]float64{85}, "85 °C"},
		{[]float64{60, 85}, "60 and 85 °C"},
		{[]float64{-40, 32, 60, 85}, "-40, 32, 60 and 85 °C"},
	} {
		var ks []Corner
		for _, x := range tc.temps {
			ks = append(ks, Corner{Temp: x})
		}
		if got := Temps(ks); got != tc.want {
			t.Errorf("Temps(%v) = %q, want %q", tc.temps, got, tc.want)
		}
	}
	if got := (Corner{Temp: 32, Phases: []string{"start-night", "start-day"}, Hot: true}).Name(); got != "32 °C (start-night, start-day, --hot)" {
		t.Errorf("Name() = %q", got)
	}
}

func TestTotals(t *testing.T) {
	r := sample(t)
	total, missing := r.Total()
	if math.Abs(total-(0.3666+1.494+39.87+0.7879)) > 1e-12 || missing != 1 {
		t.Errorf("total %g, missing %d", total, missing)
	}
	if over, warn := r.DeratingCounts(); over != 1 || warn != 0 {
		t.Errorf("overstress %d, warnings %d", over, warn)
	}
	if w := Warnings(r.Rows[1]); len(w) != 2 || !strings.HasPrefix(w[0], "no FIT: Actual power") ||
		w[1] != "P/Pmax = 127% (0.1273 W of 0.1 W): overstress" {
		t.Errorf("R3 warnings %q", w)
	}
}

// TestNoMission: without a mission profile there is no FIT anywhere.
func TestNoMission(t *testing.T) {
	r := sample(t)
	r.Mission = nil
	for i := range r.Rows {
		r.Rows[i].FIT = nil
	}
	var b bytes.Buffer
	if err := Markdown(&b, r); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"No mission profile was given", "**Total:** no FIT", "| R1 | R | 10 kΩ | 10.9 Vᴰ | 1.09 mAˢ | 11.9 mWᴰ |  | — |"} {
		if !strings.Contains(s, want) {
			t.Errorf("report without %q", want)
		}
	}
	if strings.Contains(s, "## Mission profile") || strings.Contains(s, "Largest contributors") {
		t.Error("report without a mission has a mission profile or contributors")
	}
}

func TestNewInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.net")
	if err := os.WriteFile(path, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	in, err := NewInput("netlist", path)
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256 of "abc"
	if in.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("hash %s", in.SHA256)
	}
	if _, err := NewInput("netlist", path+".missing"); err == nil {
		t.Error("missing file: no error")
	}
}
