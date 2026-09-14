package rel

import (
	"math"
	"testing"

	"github.com/rveen/fitcalc/parts"
)

// TestThermalResistance: the FIDES default of the package, for an IC family
// written without its pin count too.
func TestThermalResistance(t *testing.T) {
	soic8 := 400 * math.Pow(8, -0.58) * 1.15 // FIDES 2022, p. 119
	for _, tc := range []struct {
		pkg, npins string
		want       float64
	}{
		{"SOIC8", "", soic8},
		{"SOIC", "8", soic8},
		{"SOT23", "", 443},
		{"QFN32", "", 0}, // no default: its formula needs the package area
	} {
		fields := map[string]string{"package": tc.pkg}
		if tc.npins != "" {
			fields["npins"] = tc.npins
		}
		r := &Result{Component: &parts.Component{Ref: "U1", Class: "U", Meta: meta(fields)}}
		if got, _ := r.thermalResistance(); math.Abs(got-tc.want) > 1e-9*tc.want {
			t.Errorf("package %s, npins %q: Rth %g K/W, want %g", tc.pkg, tc.npins, got, tc.want)
		}
	}
}
