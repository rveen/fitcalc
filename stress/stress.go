package stress

import (
	"math"
	"sort"
)

// Stress is the set of stress quantities of one component, by name.
//
// v, i and p are the main voltage, current and power of any component:
// resistor and capacitor voltage, diode forward voltage (vd), transistor vce
// or vds. The others are device-specific: vr (diode reverse voltage), vce,
// vbe, ic, ib, ie, vds, vgs, id, and v:<port> for the pins of a subcircuit
// instance.
type Stress map[string]Quantity

// Value returns the value of a quantity, or NaN if there is none.
func (s Stress) Value(name string) float64 {
	if q, ok := s[name]; ok {
		return q.Value
	}
	return math.NaN()
}

// Names returns the quantity names in report order: v, i, p, then the others
// sorted.
func (s Stress) Names() []string {
	var names, rest []string
	for _, n := range []string{"v", "i", "p"} {
		if _, ok := s[n]; ok {
			names = append(names, n)
		}
	}
	for n := range s {
		if n != "v" && n != "i" && n != "p" {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(names, rest...)
}
