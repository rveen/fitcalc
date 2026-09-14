package rel

import (
	"math"
	"testing"

	"github.com/rveen/fides"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/stress"
)

// TestLocalAmbient: a component heated by 10 K by the others has the FIT of
// operating phases 10 K warmer, in FIT and in FITPhases.
func TestLocalAmbient(t *testing.T) {
	m := mission(t)
	warm := &fides.Mission{Ttotal: m.Ttotal}
	for _, ph := range m.Phases {
		x := *ph
		if x.On {
			x.Tamb += 10
			x.Tmax += 10
		}
		warm.Phases = append(warm.Phases, &x)
	}

	cold := resistor(0.0119)
	heated := resistor(0.0119)
	heated.Stress["ta"] = q(10, "°C", stress.Derived)

	want := FIT([]*parts.Component{cold}, warm)[0].FIT
	got := FIT([]*parts.Component{heated}, m)[0]
	if math.Abs(got.FIT-want) > 1e-12*want || got.Inputs["Ta"].Value != 10 {
		t.Errorf("FIT %g with ta = 10 K, want %g; inputs %v", got.FIT, want, got.Inputs)
	}
	if base := FIT([]*parts.Component{cold}, m)[0].FIT; !(want > base) {
		t.Errorf("FIT %g 10 K warmer, not above %g", want, base)
	}

	phases := make([][]*parts.Component, len(m.Phases))
	for i := range phases {
		phases[i] = []*parts.Component{heated}
	}
	if p := FITPhases([]*parts.Component{cold}, phases, m)[0]; math.Abs(p.FIT-want) > 1e-12*want {
		t.Errorf("FITPhases %g, want %g", p.FIT, want)
	}
	for _, ph := range m.Phases {
		if ph.Tamb > 85 {
			t.Fatal("the mission was changed")
		}
	}
}
