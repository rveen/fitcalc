package rel

import (
	"context"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/rveen/fides"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/golib/csv"
)

func mission(t *testing.T) *fides.Mission {
	t.Helper()
	m, err := LoadMission("../testdata/mission.csv")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func meta(fields map[string]string) map[string]csv.Field {
	m := map[string]csv.Field{}
	for k, v := range fields {
		m[k] = csv.Field{Value: v, Sources: []csv.Source{{File: "db.csv", Name: "T"}}}
	}
	return m
}

func q(v float64, unit string, src stress.Source) stress.Quantity {
	return stress.Quantity{Value: v, Unit: unit, Source: src, Note: "test"}
}

func TestLoadMission(t *testing.T) {
	if m := mission(t); len(m.Phases) != 6 || m.Ttotal != 8759 {
		t.Errorf("%d phases, %g h; want 6, 8759", len(m.Phases), m.Ttotal)
	}
	if _, err := LoadMission("../testdata/missing.csv"); err == nil {
		t.Error("missing file: no error")
	}
}

// TestResistor: the adapter gives FIDES what a direct call with the same
// inputs gets.
func TestResistor(t *testing.T) {
	m := mission(t)
	c := &parts.Component{
		Ref:     "R1",
		Element: &spice.Element{Name: "R1", Kind: "R", Value: 10e3},
		Class:   "R",
		Tags:    []string{"thick"},
		Meta:    meta(map[string]string{"package": "0603", "tmax": "155", "vmax": "75", "pmax": "0.1"}),
		Stress:  stress.Stress{"v": q(-10.9, "V", stress.Derived), "i": q(-1.09e-3, "A", stress.SPICE), "p": q(0.0119, "W", stress.Derived)},
	}
	r := FIT([]*parts.Component{c}, m)[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}

	fc := fides.NewComponent("R1")
	fc.Class, fc.Tags, fc.Package = "R", []string{"thick"}, "0603"
	fc.Value, fc.Tmax, fc.Vmax, fc.Pmax = 10e3, 155, 75, 0.1
	fc.V, fc.I, fc.P = 10.9, 1.09e-3, 0.0119
	want, err := fides.FIT(fc, m)
	if err != nil {
		t.Fatal(err)
	}
	if r.FIT != want {
		t.Errorf("FIT %g, want %g", r.FIT, want)
	}
	if r.Inputs["V"].Value != 10.9 || r.Inputs["P"].Value != 0.0119 || r.Inputs["I"].Source != stress.SPICE {
		t.Errorf("inputs %v", r.Inputs)
	}
	if _, ok := r.Inputs["T"]; ok {
		t.Error("a resistor got T: its FIDES model computes its own temperature")
	}
}

// TestDiode: V is the reverse voltage.
func TestDiode(t *testing.T) {
	c := &parts.Component{
		Ref:    "D2",
		Class:  "D",
		Meta:   meta(map[string]string{"package": "SOD123", "tmax": "150", "vmax": "100", "imax": "0.2", "rth": "300"}),
		Stress: stress.Stress{"v": q(-12, "V", stress.Derived), "vr": q(12, "V", stress.Derived), "p": q(1e-10, "W", stress.Derived)},
	}
	r := FIT([]*parts.Component{c}, mission(t))[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Fides.V != 12 || !strings.HasPrefix(r.Inputs["V"].Note, "vr: ") {
		t.Errorf("V = %g (%s), want the reverse voltage 12", r.Fides.V, r.Inputs["V"].Note)
	}
	if !strings.Contains(r.Inputs["T"].Note, "rth = 300 K/W from db.csv (T)") {
		t.Errorf("T %v", r.Inputs["T"])
	}
}

// TestSelfHeating: T = P·Rth, with the package default when the parts files
// give none, and a higher temperature gives a higher FIT.
func TestSelfHeating(t *testing.T) {
	m := mission(t)
	mos := func(p float64, fields map[string]string) *parts.Component {
		base := map[string]string{"package": "SOT23", "tmax": "150", "pmax": "0.36"}
		for k, v := range fields {
			base[k] = v
		}
		return &parts.Component{Ref: "M1", Class: "Q", Tags: []string{"mos"}, Meta: meta(base),
			Stress: stress.Stress{"p": q(p, "W", stress.Derived)}}
	}

	def := FIT([]*parts.Component{mos(0.1, nil)}, m)[0]
	if def.Err != nil {
		t.Fatal(def.Err)
	}
	rth := fides.NewPackage("SOT23").Rtha(0)
	if def.Fides.T != 0.1*rth || !strings.Contains(def.Inputs["T"].Note, "FIDES default for package SOT23") {
		t.Errorf("T = %g (%s), want 0.1 W × %g K/W", def.Fides.T, def.Inputs["T"].Note, rth)
	}

	cool := FIT([]*parts.Component{mos(0.001, nil)}, m)[0]
	given := FIT([]*parts.Component{mos(0.1, map[string]string{"rth": "200"})}, m)[0]
	if given.Fides.T != 20 {
		t.Errorf("T = %g, want 0.1 W × 200 K/W", given.Fides.T)
	}
	if !(cool.FIT < given.FIT && given.FIT < def.FIT) {
		t.Errorf("FIT at T %g, %g, %g: %g, %g, %g; want increasing", cool.Fides.T, given.Fides.T, def.Fides.T,
			cool.FIT, given.FIT, def.FIT)
	}

	// A parts field t wins
	c := mos(0.1, nil)
	c.Stress["t"] = q(5, "°C", stress.Metadata)
	if r := FIT([]*parts.Component{c}, m)[0]; r.Fides.T != 5 {
		t.Errorf("T = %g, want the parts value 5", r.Fides.T)
	}

	// No power: no self-heating, with a warning
	c = mos(0, nil)
	delete(c.Stress, "p")
	if r := FIT([]*parts.Component{c}, m)[0]; r.Fides.T != 0 || !slices.Contains(r.Warnings, "self-heating not included: no power") {
		t.Errorf("T = %g, warnings %v", r.Fides.T, r.Warnings)
	}
}

func TestErrors(t *testing.T) {
	m := mission(t)
	noTmax := &parts.Component{Ref: "C1", Class: "C", Meta: meta(map[string]string{"vmax": "50"})}
	badPkg := &parts.Component{Ref: "Q9", Class: "Q", Meta: meta(map[string]string{"package": "NOSUCH", "tmax": "150"})}

	results := FIT([]*parts.Component{noTmax, badPkg}, m)

	if r := results[0]; r.Err == nil || !strings.Contains(r.Err.Error(), "Tmax") || !math.IsNaN(r.FIT) {
		t.Errorf("C1: FIT %g, error %v", r.FIT, r.Err)
	}
	if r := results[1]; r.Err == nil || !slices.ContainsFunc(r.Warnings, func(w string) bool {
		return strings.Contains(w, "package not found")
	}) {
		t.Errorf("Q9: error %v, warnings %v", r.Err, r.Warnings)
	}
	if total, missing := Total(results); total != 0 || missing != 2 {
		t.Errorf("total %g, missing %d", total, missing)
	}
}

// TestProbe runs the whole chain on the probe circuit.
func TestProbe(t *testing.T) {
	name := os.Getenv("NGSPICE")
	if name == "" {
		name = "ngspice"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("ngspice not found")
	}

	c, err := spice.Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&spice.Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	values, err := spice.ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	stresses, _ := stress.DeriveAll(c, values)
	p, err := parts.Load([]string{"../testdata/probe.bom.csv", "../testdata/parts.db.csv"})
	if err != nil {
		t.Fatal(err)
	}
	comps, _, _ := parts.Merge(c, stresses, p)

	results := FIT(comps, mission(t))
	for _, r := range results {
		if r.Err != nil || !(r.FIT > 0) {
			t.Errorf("%s: FIT %g, error %v", r.Component.Ref, r.FIT, r.Err)
		}
		if len(r.Warnings) != 0 && r.Component.Ref != "X1" {
			t.Errorf("%s: warnings %v", r.Component.Ref, r.Warnings)
		}
		if slices.Contains([]string{"D1", "D2", "Q1", "M1", "J1"}, r.Component.Ref) {
			if _, ok := r.Inputs["T"]; !ok {
				t.Errorf("%s: no self-heating", r.Component.Ref)
			}
		}
	}
	if total, missing := Total(results); total <= 0 || missing != 0 {
		t.Errorf("total %g FIT, %d without FIT", total, missing)
	}
}
