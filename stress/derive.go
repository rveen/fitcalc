package stress

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rveen/fitcalc/spice"
)

// powerTolerance is the relative difference allowed between the power
// fitcalc derives (V·I) and the power ngspice reports.
const powerTolerance = 1e-6

// powerFloor is the absolute difference, in W, allowed besides: a switched-off
// transistor has leakage currents that ngspice counts in its power and the
// derived power does not, far below anything that matters for reliability.
const powerFloor = 1e-9

// Derive returns the stress of an element from its operating point, and
// warnings about quantities that could not be derived or do not agree with
// ngspice. c provides the port names of subcircuits.
//
// Elements that are not reliability items (sources, controlled sources,
// couplings, switches, lines) have no stress: Derive returns nil.
func Derive(c *spice.Circuit, op spice.ElementOP) (Stress, []string) {

	d := &deriver{op: op, e: op.Element, s: Stress{}, name: strings.ToLower(op.Element.Name)}

	switch d.e.Kind {

	case "R":
		d.diff("v", 0, 1)
		d.device("i", "i", "A")
		d.power("v", "i")

	case "C":
		d.diff("v", 0, 1)
		if _, ok := op.Device["i"]; ok { // in a transient analysis
			d.device("i", "i", "A")
		}

	case "L":
		d.diff("v", 0, 1)
		if math.IsNaN(op.Branch) {
			d.missing("i", "i("+d.name+")")
		} else {
			d.s["i"] = Quantity{op.Branch, "A", SPICE, "i(" + d.name + ")"}
		}

	case "D":
		d.diff("vd", 0, 1)
		d.device("id", "id", "A")
		if q, ok := d.s["vd"]; ok {
			d.s["vr"] = Quantity{math.Max(-q.Value, 0), "V", Derived, "max(−vd, 0)"}
		}
		d.power("vd", "id")
		d.alias("v", "vd")
		d.alias("i", "id")

	case "Q":
		d.diff("vce", 0, 2)
		d.diff("vbe", 1, 2)
		d.junctions(c)
		d.device("ic", "ic", "A")
		d.device("ib", "ib", "A")
		d.device("ie", "ie", "A")
		d.power("vce", "ic", "vbe", "ib")
		d.alias("v", "vce")
		d.alias("i", "ic")

	case "M":
		d.device("vds", "vds", "V")
		d.device("vgs", "vgs", "V")
		d.device("id", "id", "A")
		d.power("vds", "id")
		d.alias("v", "vds")
		d.alias("i", "id")

	case "J":
		d.diff("vds", 0, 2)
		d.device("vgs", "vgs", "V")
		d.device("id", "id", "A")
		d.power("vds", "id")
		d.alias("v", "vds")
		d.alias("i", "id")

	case "X":
		d.pins(c)

	default:
		return nil, nil
	}

	return d.s, d.warnings
}

// DeriveAll derives the stresses of all elements of c. Elements without
// stress are left out.
func DeriveAll(c *spice.Circuit, v spice.Values) (map[string]Stress, []string) {
	all := map[string]Stress{}
	var warnings []string
	for _, e := range c.Elements {
		s, w := Derive(c, v.Element(e))
		warnings = append(warnings, w...)
		if s != nil {
			all[e.Name] = s
		}
	}
	return all, warnings
}

type deriver struct {
	op       spice.ElementOP
	e        *spice.Element
	name     string // element name in lower case, as ngspice names it
	s        Stress
	warnings []string
}

func (d *deriver) warn(format string, a ...any) {
	d.warnings = append(d.warnings, d.e.Name+": "+fmt.Sprintf(format, a...))
}

func (d *deriver) missing(q, vector string) {
	d.warn("%s not available (%s missing from the results)", q, vector)
}

// voltage returns the voltage of node i and the name it has in notes.
func (d *deriver) voltage(i int) (float64, string, bool) {
	if i >= len(d.op.Nodes) {
		return math.NaN(), "", false
	}
	return d.op.Nodes[i], d.e.Nodes[i], !math.IsNaN(d.op.Nodes[i])
}

// diff sets q to the voltage between nodes a and b.
func (d *deriver) diff(q string, a, b int) {
	va, na, oka := d.voltage(a)
	vb, nb, okb := d.voltage(b)
	switch {
	case !oka && na != "":
		d.missing(q, "v("+na+")")
		return
	case !okb && nb != "":
		d.missing(q, "v("+nb+")")
		return
	case !oka || !okb:
		d.warn("%s not available (too few nodes)", q)
		return
	}

	note := "v(" + na + ") − v(" + nb + ")"
	switch {
	case nb == "0":
		note = "v(" + na + ")"
	case na == "0":
		note = "−v(" + nb + ")"
	}
	d.s[q] = Quantity{va - vb, "V", Derived, note}
}

// device sets q to a device parameter from the results.
func (d *deriver) device(q, param, unit string) {
	vector := "@" + d.name + "[" + param + "]"
	x, ok := d.op.Device[param]
	if !ok {
		d.missing(q, vector)
		return
	}
	d.s[q] = Quantity{x, unit, SPICE, vector}
}

// power sets p to the sum of the products of pairs of quantities (v, i, v,
// i, …), and compares it with the power ngspice reports, if any.
func (d *deriver) power(pairs ...string) {

	var p float64
	var terms []string
	for k := 0; k < len(pairs); k += 2 {
		v, okv := d.s[pairs[k]]
		i, oki := d.s[pairs[k+1]]
		if !okv || !oki {
			d.warn("p not available (%s or %s missing)", pairs[k], pairs[k+1])
			return
		}
		p += v.Value * i.Value
		terms = append(terms, pairs[k]+"·"+pairs[k+1])
	}
	note := strings.Join(terms, " + ")

	if sp, ok := d.op.Device["p"]; ok {
		if math.Abs(p-sp) > powerTolerance*math.Max(math.Abs(p), math.Abs(sp))+powerFloor {
			d.warn("power %s = %.4g W, but ngspice reports @%s[p] = %.4g W", note, p, d.name, sp)
		} else {
			note += ", as ngspice's @" + d.name + "[p]"
		}
	}
	d.s["p"] = Quantity{p, "W", Derived, note}
}

// alias sets q to the same quantity as from.
func (d *deriver) alias(q, from string) {
	if x, ok := d.s[from]; ok {
		d.s[q] = x
	}
}

// junctions sets vcb, the collector-base voltage of a bipolar transistor,
// and veb, the reverse voltage of its emitter-base junction: max(−vbe, 0)
// for an NPN transistor, max(vbe, 0) for a PNP one.
func (d *deriver) junctions(c *spice.Circuit) {
	vce, okce := d.s["vce"]
	vbe, okbe := d.s["vbe"]
	if !okce || !okbe {
		return
	}
	d.s["vcb"] = Quantity{vce.Value - vbe.Value, "V", Derived, "vce − vbe"}
	if c != nil && c.Models[d.e.Model] != nil && c.Models[d.e.Model].Type == "pnp" {
		d.s["veb"] = Quantity{math.Max(vbe.Value, 0), "V", Derived, "max(vbe, 0), PNP"}
	} else {
		d.s["veb"] = Quantity{math.Max(-vbe.Value, 0), "V", Derived, "max(−vbe, 0)"}
	}
}

// pins sets v:<port> to the voltage of each pin of a subcircuit instance,
// and v to the largest voltage between two pins. Where the deck measured the
// pin currents, it also sets i:<port> to the current into each pin, i to the
// largest one, and p to the power the instance takes: the sum of v·i over its
// pins.
func (d *deriver) pins(c *spice.Circuit) {

	var ports []string
	if c != nil {
		if sub := c.Subckts[d.e.Model]; sub != nil && len(sub.Ports) == len(d.e.Nodes) {
			ports = sub.Ports
		}
	}
	measured := len(d.e.Nodes) > 0 && len(d.op.Pins) == len(d.e.Nodes) && !slices.ContainsFunc(d.op.Pins, math.IsNaN)

	lo, hi := math.Inf(1), math.Inf(-1)
	var nlo, nhi, nmax string
	var p, imax float64
	complete := true
	for i := range d.e.Nodes {
		port := fmt.Sprintf("pin%d", i+1)
		if ports != nil {
			port = ports[i]
		}
		v, node, ok := d.voltage(i)
		if !ok {
			d.missing("v:"+port, "v("+node+")")
			complete = false
			continue
		}
		d.s["v:"+port] = Quantity{v, "V", SPICE, "v(" + node + ")"}
		if v < lo {
			lo, nlo = v, port
		}
		if v > hi {
			hi, nhi = v, port
		}
		if measured {
			cur := d.op.Pins[i]
			d.s["i:"+port] = Quantity{cur, "A", SPICE, spice.PinCurrent(d.e.Name, i)}
			p += v * cur
			if nmax == "" || math.Abs(cur) > math.Abs(imax) {
				imax, nmax = cur, port
			}
		}
	}
	if nlo != "" && nhi != nlo {
		d.s["v"] = Quantity{hi - lo, "V", Derived, "largest voltage between two pins: v:" + nhi + " − v:" + nlo}
	}
	if measured && complete {
		d.s["i"] = Quantity{imax, "A", SPICE, "largest pin current: i:" + nmax}
		d.s["p"] = Quantity{p, "W", Derived, "Σ v·i over the pins"}
	}
}
