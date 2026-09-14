package derating

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/golib/csv"
)

func comp(class string, tags []string, s stress.Stress, ratings map[string]string) *parts.Component {
	m := map[string]csv.Field{}
	for k, v := range ratings {
		m[k] = csv.Field{Value: v, Sources: []csv.Source{{File: "db.csv", Name: "T"}}}
	}
	return &parts.Component{Ref: "X", Class: class, Tags: tags, Stress: s, Meta: m}
}

func q(v float64, unit string) stress.Quantity {
	return stress.Quantity{Value: v, Unit: unit, Source: stress.Derived}
}

func ratio(t *testing.T, r *Result, name string) Ratio {
	t.Helper()
	for _, x := range r.Ratios {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no %s in %v", name, r.Ratios)
	return Ratio{}
}

func TestLevels(t *testing.T) {
	for _, tc := range []struct {
		p    float64
		want Level
	}{
		{0.05, OK}, {0.08, OK}, {0.085, Warning}, {0.1, Warning}, {0.127, Overstress},
	} {
		r := Check(comp("R", nil, stress.Stress{"p": q(tc.p, "W")}, map[string]string{"pmax": "0.1"}))
		if x := ratio(t, r, "P/Pmax"); x.Level != tc.want || r.Level() != tc.want {
			t.Errorf("P = %g W of 0.1 W: %v, result %v; want %v", tc.p, x.Level, r.Level(), tc.want)
		}
	}
}

func TestRatios(t *testing.T) {
	// Signed stresses are compared as magnitudes
	r := Check(comp("R", nil,
		stress.Stress{"v": q(-10, "V"), "i": q(-1e-3, "A"), "p": q(0.01, "W")},
		map[string]string{"vmax": "50", "pmax": "0.1"}))
	if len(r.Ratios) != 2 {
		t.Errorf("ratios %v, want V and P (no imax)", r.Ratios)
	}
	if x := ratio(t, r, "V/Vmax"); x.Value != 0.2 || x.Key != "v" || x.Source != "db.csv (T)" {
		t.Errorf("V/Vmax %+v", x)
	}
	if s := ratio(t, r, "V/Vmax").String(); s != "V/Vmax = 20% (10 V of 50 V)" {
		t.Errorf("String() = %q", s)
	}
}

// A diode's working voltage is its reverse voltage, not its forward voltage.
func TestDiode(t *testing.T) {
	r := Check(comp("D", nil,
		stress.Stress{"v": q(0.7, "V"), "vr": q(0, "V"), "i": q(0.18, "A")},
		map[string]string{"vmax": "100", "imax": "0.2"}))
	if x := ratio(t, r, "V/Vmax"); x.Key != "vr" || x.Value != 0 {
		t.Errorf("V/Vmax %+v, want vr = 0", x)
	}
	if x := ratio(t, r, "I/Imax"); x.Level != Warning {
		t.Errorf("I/Imax %+v, want a warning at 90%%", x)
	}
}

func TestPolarity(t *testing.T) {
	neg := stress.Stress{"v": q(-2, "V")}
	if r := Check(comp("C", []string{"alu"}, neg, map[string]string{"vmax": "25"})); r.Level() != Overstress || len(r.Issues) != 1 {
		t.Errorf("electrolytic at -2 V: %+v", r)
	}
	if r := Check(comp("C", []string{"cer", "x7r"}, neg, map[string]string{"vmax": "25"})); r.Level() != OK || len(r.Issues) != 0 {
		t.Errorf("ceramic at -2 V: %+v", r)
	}
}

func TestMetadataOverride(t *testing.T) {
	c := comp("C", nil, stress.Stress{"v": {Value: 45, Unit: "V", Source: stress.Metadata}}, map[string]string{"vmax": "50"})
	if x := ratio(t, Check(c), "V/Vmax"); x.Level != Warning || x.Stress.Source != stress.Metadata {
		t.Errorf("V/Vmax %+v", x)
	}
}

func TestCheckAt(t *testing.T) {

	ratings := map[string]string{"vmax": "75", "pmax": "0.1", "tmax": "155"}
	s := stress.Stress{"v": q(11.9, "V"), "p": q(0.0705, "W")}

	// A resistor from 70 °C: 0.1 W × (155 − 85)/(155 − 70) at 85 °C
	r := CheckAt(comp("R", nil, s, ratings), 85)
	x := ratio(t, r, "P/Pmax")
	if math.Abs(x.Rating-0.1*70/85) > 1e-12 || x.Level != Warning || r.Temp != 85 {
		t.Errorf("P/Pmax %+v at %g °C, want a warning with 82.4 mW", x, r.Temp)
	}
	if want := "db.csv (T), derated to 82.4% at 85 °C (linear from 70 °C, default for resistors, to tmax 155 °C)"; x.Source != want {
		t.Errorf("source %q, want %q", x.Source, want)
	}
	if v := ratio(t, r, "V/Vmax"); v.Rating != 75 {
		t.Errorf("V/Vmax %+v: the voltage rating is not derated", v)
	}

	// Below the rated temperature, and without a temperature: as given
	for _, temp := range []float64{60, math.NaN()} {
		if x := ratio(t, CheckAt(comp("R", nil, s, ratings), temp), "P/Pmax"); x.Rating != 0.1 || x.Level != OK {
			t.Errorf("%g °C: P/Pmax %+v", temp, x)
		}
	}

	// Other classes from 25 °C, unless trated says otherwise
	semi := map[string]string{"pmax": "0.25", "tmax": "150"}
	if x := ratio(t, CheckAt(comp("Q", nil, stress.Stress{"p": q(0.1, "W")}, semi), 85), "P/Pmax"); math.Abs(x.Rating-0.25*65/125) > 1e-12 {
		t.Errorf("transistor: P/Pmax %+v, want 0.13 W", x)
	}
	semi["trated"] = "50"
	if x := ratio(t, CheckAt(comp("Q", nil, stress.Stress{"p": q(0.1, "W")}, semi), 85), "P/Pmax"); math.Abs(x.Rating-0.25*65/100) > 1e-12 ||
		!strings.Contains(x.Source, "trated from db.csv (T)") {
		t.Errorf("trated 50 °C: P/Pmax %+v, want 0.1625 W", x)
	}

	// Without tmax the power rating stays
	if x := ratio(t, CheckAt(comp("R", nil, s, map[string]string{"pmax": "0.1"}), 85), "P/Pmax"); x.Rating != 0.1 {
		t.Errorf("no tmax: P/Pmax %+v", x)
	}

	// At or above tmax: an overstress, and no power rating left
	r = CheckAt(comp("R", nil, s, map[string]string{"pmax": "0.1", "tmax": "85"}), 85)
	if r.Level() != Overstress || len(r.Issues) != 1 || len(r.Ratios) != 0 {
		t.Errorf("at tmax: %+v", r)
	}
}

func TestNoRatings(t *testing.T) {
	if r := Check(comp("R", nil, stress.Stress{"p": q(1, "W")}, nil)); len(r.Ratios) != 0 || r.Level() != OK {
		t.Errorf("result %+v, want no ratios", r)
	}
	if r := Check(&parts.Component{Ref: "J9", Class: "J"}); len(r.Ratios) != 0 {
		t.Errorf("no stress: ratios %v", r.Ratios)
	}
}

// TestProbe: every rating of the probe circuit is respected.
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

	n := 0
	for _, r := range CheckAll(comps) {
		n += len(r.Ratios)
		if r.Level() != OK {
			t.Errorf("%s: %v %v", r.Component.Ref, r.Ratios, r.Issues)
		}
	}
	if n < 30 {
		t.Errorf("%d ratios; the probe parts have ratings for most stresses", n)
	}
}
