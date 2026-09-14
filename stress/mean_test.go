package stress

import "testing"

func TestMean(t *testing.T) {
	points := []map[string]Stress{
		{"R1": {"v": {1, "V", Derived, "v(a)"}, "p": {1e-3, "W", Derived, "v·i"}}, "R2": {"v": {2, "V", Derived, ""}}},
		{"R1": {"v": {3, "V", Derived, "v(a)"}, "p": {9e-3, "W", Derived, "v·i"}}},
	}
	m := MeanAll(points)
	if _, ok := m["R2"]; ok || len(m) != 1 {
		t.Errorf("means %v: R2 is missing at the second point", m)
	}
	check(t, m["R1"], "v", 2, Derived, "mean over the sweep of v(a)")
	check(t, m["R1"], "p", 5e-3, Derived, "mean over the sweep of v·i")

	lo, hi, at, ok := Range([]Stress{points[0]["R1"], points[1]["R1"]}, "p")
	if !ok || lo != 1e-3 || hi != 9e-3 || at != 1 {
		t.Errorf("Range = %g, %g, %d, %v", lo, hi, at, ok)
	}
	if _, _, _, ok := Range([]Stress{points[0]["R2"], {}}, "v"); ok {
		t.Error("Range of a quantity missing at a point: ok")
	}
}
