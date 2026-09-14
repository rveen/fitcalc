package rel

import (
	"math"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
)

func resistor(p float64) *parts.Component {
	return &parts.Component{
		Ref:     "R1",
		Element: &spice.Element{Name: "R1", Kind: "R", Value: 10e3},
		Class:   "R",
		Tags:    []string{"thick"},
		Meta:    meta(map[string]string{"package": "0603", "tmax": "155", "vmax": "75", "pmax": "0.1"}),
		Stress:  stress.Stress{"v": q(10.9, "V", stress.Derived), "p": q(p, "W", stress.Derived)},
	}
}

// TestFITPhases: the FIT per phase adds up to the FIDES FIT, and the
// stresses of a phase change only its contribution.
func TestFITPhases(t *testing.T) {
	m := mission(t) // phases: off-day, off-night, start-night, start-day, full-op, motorway
	c := []*parts.Component{resistor(0.0119)}
	none := func() [][]*parts.Component { return make([][]*parts.Component, len(m.Phases)) }

	// The same stresses in every phase: FIT
	want := FIT(c, m)[0]
	got := FITPhases(c, none(), m)[0]
	var sum float64
	for _, f := range got.Phases {
		sum += f
	}
	if len(got.Phases) != len(m.Phases) || math.Abs(got.FIT-want.FIT) > 1e-12*want.FIT || math.Abs(sum-got.FIT) > 1e-12*got.FIT {
		t.Fatalf("FITPhases %g %v, FIT %g", got.FIT, got.Phases, want.FIT)
	}
	if got.Inputs["P"].Value != 0.0119 || got.Err != nil {
		t.Errorf("inputs %v, error %v", got.Inputs, got.Err)
	}

	// More power in full-op: only its contribution grows
	phases := none()
	phases[4] = []*parts.Component{resistor(0.05)}
	hot := FITPhases(c, phases, m)[0]
	for i := range m.Phases {
		if (i == 4) != (hot.Phases[i] > got.Phases[i]) || i != 4 && hot.Phases[i] != got.Phases[i] {
			t.Errorf("phase %d: %g, with the nominal stresses %g", i, hot.Phases[i], got.Phases[i])
		}
	}

	// A phase that is not operating does not depend on the stresses
	phases = none()
	phases[0] = []*parts.Component{resistor(0.05)}
	if off := FITPhases(c, phases, m)[0]; off.FIT != got.FIT {
		t.Errorf("other stresses in off-day: %g, want %g", off.FIT, got.FIT)
	}

	// An error in one phase is the component's, with the phase
	phases = none()
	phases[4] = []*parts.Component{resistor(0.2)}
	if e := FITPhases(c, phases, m)[0]; !math.IsNaN(e.FIT) || e.Err == nil || !strings.HasPrefix(e.Err.Error(), "phase full-op: Actual power") {
		t.Errorf("overstress in full-op: %g, %v", e.FIT, e.Err)
	}
}
