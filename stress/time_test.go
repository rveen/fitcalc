package stress

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

// TestTimeStats: a triangle from 0 to 2 V and back in 2 s, a constant
// negative current and power, and a quantity missing at one point.
func TestTimeStats(t *testing.T) {
	times := []float64{0, 1, 2}
	var points []Stress
	for k, v := range []float64{0, 2, 0} {
		s := Stress{
			"v": {v, "V", Derived, "v(a)"},
			"i": {-5e-3, "A", SPICE, "@r1[i]"},
			"p": {v * 1e-3, "W", Derived, "v·i"},
		}
		if k != 1 {
			s["vgs"] = Quantity{1, "V", SPICE, "@m1[vgs]"}
		}
		points = append(points, s)
	}

	st := TimeStats(times, points)
	if _, ok := st["vgs"]; ok {
		t.Error("vgs, missing at t = 1, has statistics")
	}
	v := st["v"]
	if v.Min != 0 || v.MinAt != 0 || v.Max != 2 || v.MaxAt != 1 || !approx(v.Mean, 1) || !approx(v.RMS, math.Sqrt(4.0/3)) {
		t.Errorf("v: %+v, want min 0 at 0, max 2 at 1, mean 1, RMS √(4/3)", v)
	}
	if peak, at := v.Peak(); peak != 2 || at != 1 {
		t.Errorf("v peak %g at %g, want 2 at 1", peak, at)
	}

	eff := Effective(st)
	if q := eff["v"]; !approx(q.Value, math.Sqrt(4.0/3)) || q.Source != Derived || q.Note != "RMS over the transient of v(a)" {
		t.Errorf("effective v: %+v", q)
	}
	if q := eff["i"]; !approx(q.Value, -5e-3) {
		t.Errorf("effective i = %g, want the constant -5 mA", q.Value)
	}
	if q := eff["p"]; !approx(q.Value, 1e-3) || q.Note != "mean over the transient of v·i" {
		t.Errorf("effective p: %+v, want the mean 1 mW", q)
	}

	high, low := Extremes(st)
	if high["v"].Value != 2 || low["v"].Value != 0 {
		t.Errorf("extremes of v: %g and %g, want 2 and 0", high["v"].Value, low["v"].Value)
	}
	for _, s := range []Stress{high, low} {
		if !approx(s["i"].Value, -5e-3) || !approx(s["p"].Value, 1e-3) {
			t.Errorf("extremes of i and p: %g and %g, want their effective values", s["i"].Value, s["p"].Value)
		}
	}
}

// TestTimeStatsOnePoint: a single time point gives its values.
func TestTimeStatsOnePoint(t *testing.T) {
	st := TimeStats([]float64{1e-3}, []Stress{{"v": {-3, "V", Derived, ""}}})
	if v := st["v"]; v.Mean != -3 || v.RMS != 3 || v.Effective().Value != -3 {
		t.Errorf("one point: %+v", v)
	}
	all := TimeStatsAll([]float64{0, 1}, []map[string]Stress{
		{"R1": {"v": {1, "V", Derived, ""}}, "C1": {"v": {1, "V", Derived, ""}}},
		{"R1": {"v": {3, "V", Derived, ""}}},
	})
	if _, ok := all["C1"]; ok || !approx(all["R1"]["v"].Mean, 2) {
		t.Errorf("TimeStatsAll: %+v", all)
	}
}
