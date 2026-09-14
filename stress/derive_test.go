package stress

import (
	"context"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/spice"
)

func elementOP(name, kind string, nodes []string, volts []float64, device map[string]float64) spice.ElementOP {
	return spice.ElementOP{
		Element: &spice.Element{Name: name, Kind: kind, Nodes: nodes},
		Nodes:   volts,
		Device:  device,
		Branch:  math.NaN(),
	}
}

func near(a, b float64) bool {
	return a == b || math.Abs(a-b) <= 1e-9*math.Max(math.Abs(a), math.Abs(b))
}

func check(t *testing.T, s Stress, name string, value float64, source Source, note string) {
	t.Helper()
	q, ok := s[name]
	if !ok {
		t.Errorf("%s missing from %v", name, s)
		return
	}
	if !near(q.Value, value) || q.Source != source || q.Note != note {
		t.Errorf("%s = %v, want %g %s (%s)", name, q, value, source, note)
	}
}

func TestDeriveResistor(t *testing.T) {
	s, w := Derive(nil, elementOP("R1", "R", []string{"a", "b"}, []float64{5, 2}, map[string]float64{"i": 3e-3, "p": 9e-3}))
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	check(t, s, "v", 3, Derived, "v(a) − v(b)")
	check(t, s, "i", 3e-3, SPICE, "@r1[i]")
	check(t, s, "p", 9e-3, Derived, "v·i, as ngspice's @r1[p]")
	if got := s.Names(); !reflect.DeepEqual(got, []string{"v", "i", "p"}) {
		t.Errorf("names %v", got)
	}
}

func TestDerivePowerMismatch(t *testing.T) {
	_, w := Derive(nil, elementOP("R1", "R", []string{"a", "0"}, []float64{5, 0}, map[string]float64{"i": 1e-3, "p": 6e-3}))
	if len(w) != 1 || !strings.Contains(w[0], "ngspice reports @r1[p] = 0.006 W") {
		t.Errorf("warnings %q", w)
	}
}

func TestDeriveMissing(t *testing.T) {
	op := elementOP("R1", "R", []string{"a", "b"}, []float64{5, math.NaN()}, map[string]float64{})
	s, w := Derive(nil, op)
	if len(s) != 0 {
		t.Errorf("stress %v, want none", s)
	}
	want := []string{
		"R1: v not available (v(b) missing from the results)",
		"R1: i not available (@r1[i] missing from the results)",
		"R1: p not available (v or i missing)",
	}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(w, "\n"), strings.Join(want, "\n"))
	}
}

func TestDeriveDiode(t *testing.T) {
	// Reverse-biased: anode at ground, cathode at 12 V
	s, _ := Derive(nil, elementOP("D2", "D", []string{"0", "vcc"}, []float64{0, 12}, map[string]float64{"id": -1e-11}))
	check(t, s, "vd", -12, Derived, "−v(vcc)")
	check(t, s, "vr", 12, Derived, "max(−vd, 0)")
	check(t, s, "p", 1.2e-10, Derived, "vd·id")
	check(t, s, "v", -12, Derived, "−v(vcc)")
	check(t, s, "i", -1e-11, SPICE, "@d2[id]")

	// Forward-biased
	s, _ = Derive(nil, elementOP("D1", "D", []string{"a", "0"}, []float64{0.7, 0}, map[string]float64{"id": 0.01}))
	check(t, s, "vr", 0, Derived, "max(−vd, 0)")
	check(t, s, "p", 0.007, Derived, "vd·id")
}

func TestDeriveTransistors(t *testing.T) {
	q, w := Derive(nil, elementOP("Q1", "Q", []string{"c", "b", "e"}, []float64{5, 0.7, 0},
		map[string]float64{"ic": 1e-3, "ib": 1e-5, "ie": -1.01e-3, "p": 5e-3 + 0.7e-5}))
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	check(t, q, "vce", 5, Derived, "v(c) − v(e)")
	check(t, q, "vbe", 0.7, Derived, "v(b) − v(e)")
	check(t, q, "p", 5e-3+0.7e-5, Derived, "vce·ic + vbe·ib, as ngspice's @q1[p]")
	check(t, q, "v", 5, Derived, "v(c) − v(e)")
	check(t, q, "i", 1e-3, SPICE, "@q1[ic]")
	check(t, q, "vcb", 4.3, Derived, "vce − vbe")
	check(t, q, "veb", 0, Derived, "max(−vbe, 0)")

	// A PNP transistor with its emitter-base junction reverse-biased
	c := &spice.Circuit{Models: map[string]*spice.Model{"qp": {Name: "qp", Type: "pnp"}}}
	op := elementOP("Q2", "Q", []string{"c", "b", "e"}, []float64{0, 5, 3}, map[string]float64{"ic": 0, "ib": 0, "ie": 0})
	op.Element.Model = "qp"
	p, _ := Derive(c, op)
	check(t, p, "veb", 2, Derived, "max(vbe, 0), PNP")
	check(t, p, "vcb", -5, Derived, "vce − vbe")

	m, _ := Derive(nil, elementOP("M1", "M", []string{"d", "g", "s", "b"}, []float64{10, 3, 1, 0},
		map[string]float64{"vds": 9, "vgs": 2, "id": 2e-3}))
	check(t, m, "vds", 9, SPICE, "@m1[vds]")
	check(t, m, "p", 18e-3, Derived, "vds·id")

	j, _ := Derive(nil, elementOP("J1", "J", []string{"d", "g", "s"}, []float64{10, 0, 1},
		map[string]float64{"vgs": -1, "id": 1e-3}))
	check(t, j, "vds", 9, Derived, "v(d) − v(s)")
	check(t, j, "p", 9e-3, Derived, "vds·id")
}

func TestDeriveSubcircuit(t *testing.T) {
	c := &spice.Circuit{Subckts: map[string]*spice.Subckt{"opamp": {Name: "opamp", Ports: []string{"inp", "inn", "vcc", "vee", "out"}}}}
	op := elementOP("XU1", "X", []string{"/in", "/fb", "vcc", "0", "/out"}, []float64{1, 1, 12, 0, 1}, nil)
	op.Element.Model = "opamp"

	s, w := Derive(c, op)
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	check(t, s, "v:vcc", 12, SPICE, "v(vcc)")
	check(t, s, "v:inp", 1, SPICE, "v(/in)")
	check(t, s, "v", 12, Derived, "largest voltage between two pins: v:vcc − v:vee")

	// Unknown subcircuit: numbered pins
	op.Element.Model = "other"
	s, _ = Derive(c, op)
	check(t, s, "v:pin3", 12, SPICE, "v(vcc)")
}

// TestDeriveSubcircuitCurrents: the pin currents give the power the instance
// takes.
func TestDeriveSubcircuitCurrents(t *testing.T) {
	c := &spice.Circuit{Subckts: map[string]*spice.Subckt{"reg": {Name: "reg", Ports: []string{"in", "out", "gnd"}}}}
	op := elementOP("XU1", "X", []string{"vin", "vout", "0"}, []float64{12, 5, 0}, nil)
	op.Element.Model = "reg"
	op.Pins = []float64{0.101, -0.1, -0.001}

	s, w := Derive(c, op)
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	check(t, s, "i:in", 0.101, SPICE, "i(vfitcalc_xu1_1)")
	check(t, s, "i:out", -0.1, SPICE, "i(vfitcalc_xu1_2)")
	check(t, s, "i", 0.101, SPICE, "largest pin current: i:in")
	check(t, s, "p", 12*0.101-5*0.1, Derived, "Σ v·i over the pins")

	// Without measured currents there is no power
	op.Pins = []float64{math.NaN(), math.NaN(), math.NaN()}
	s, _ = Derive(c, op)
	if _, ok := s["p"]; ok {
		t.Errorf("stress %v, want no power", s)
	}
}

func TestDeriveSimulationOnly(t *testing.T) {
	for _, kind := range []string{"V", "I", "E", "F", "G", "H", "B", "K", "S", "W", "T"} {
		if s, w := Derive(nil, elementOP(kind+"1", kind, []string{"a", "0"}, []float64{1, 0}, nil)); s != nil || w != nil {
			t.Errorf("%s: stress %v, warnings %v; want none", kind, s, w)
		}
	}
}

// TestDeriveAllProbe runs ngspice on the probe circuit.
func TestDeriveAllProbe(t *testing.T) {
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
	v, err := spice.ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}

	all, w := DeriveAll(c, v)
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}

	// Every element but the voltage source has a stress, with a voltage
	for _, e := range c.Elements {
		s, ok := all[e.Name]
		if e.Kind == "V" {
			if ok {
				t.Errorf("%s: a source has a stress", e.Name)
			}
			continue
		}
		if !ok || math.IsNaN(s.Value("v")) {
			t.Errorf("%s: no voltage stress in %v", e.Name, s)
		}
	}

	if p := all["R1"].Value("p"); !near(p, (12-12/11.0)*(12-12/11.0)/10e3) {
		t.Errorf("R1: p = %g", p)
	}
	if d1 := all["D1"]; d1.Value("vr") != 0 || d1.Value("vd") < 0.6 || d1.Value("vd") > 0.8 {
		t.Errorf("D1: %v", d1)
	}
	if vr := all["D2"].Value("vr"); vr != 12 {
		t.Errorf("D2: vr = %g, want 12", vr)
	}
	if !strings.Contains(all["M1"]["p"].Note, "as ngspice's @m1[p]") {
		t.Errorf("M1: p = %v, want it confirmed by ngspice", all["M1"]["p"])
	}
}
