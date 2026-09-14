package spice

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	rawfile "github.com/rveen/logb/spice"
)

// Values are the results of an operating point analysis, by lower-case vector
// name: "v(out)", "i(v1)", "@r1[i]", "@m1[vgs]".
type Values map[string]float64

// ReadOP reads the raw file of an operating point run. Variables that ngspice
// wrote with zero length (dims=0) are left out: their 0 is not a result.
//
// The dialect is always ngspice (f64 values): the reader would detect it from
// a "Command: ngspice-…" header line, which ngspice 43 and older do not write,
// and would then read the values as LTspice's f32.
func ReadOP(data []byte) (Values, error) {

	r, err := readRaw(data)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(r.Plotname, "Operating Point") || r.Points != 1 {
		return nil, fmt.Errorf("%w: the raw file holds %q with %d points, not an operating point",
			ErrSimulation, r.Plotname, r.Points)
	}
	return values(r, r.Layout(), 0), nil
}

// ReadSweep reads the raw file of a DC sweep (ReadOP): the values of the
// swept source, and the results at each of them.
func ReadSweep(data []byte) ([]float64, []Values, error) {
	return readPoints(data, (*Sweep).plot(nil), "a DC sweep")
}

// ReadTran reads the raw file of a transient analysis (ReadOP): the time
// points, and the results at each of them.
func ReadTran(data []byte) ([]float64, []Values, error) {
	return readPoints(data, (*Tran).plot(nil), "a transient analysis")
}

// readPoints reads the raw file of an analysis with plot name plot: the
// values of its first variable, the sweep or time, and the results at each
// of them.
func readPoints(data []byte, plot, what string) ([]float64, []Values, error) {

	r, err := readRaw(data)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(r.Plotname, plot) || r.Points < 1 {
		return nil, nil, fmt.Errorf("%w: the raw file holds %q with %d points, not %s",
			ErrSimulation, r.Plotname, r.Points, what)
	}

	l := r.Layout()
	var axis []float64
	var points []Values
	for i := 0; i < r.Points; i++ {
		axis = append(axis, r.Value(l, i, 0))
		points = append(points, values(r, l, i))
	}
	return axis, points, nil
}

func readRaw(data []byte) (*rawfile.Raw, error) {
	r, err := rawfile.ReadRawOptions(bytes.NewReader(data), rawfile.ReadOptions{Dialect: rawfile.DialectNgspice})
	if err != nil {
		return nil, fmt.Errorf("%w: reading the raw file: %v", ErrSimulation, err)
	}
	return r, nil
}

// values returns the variables of point i, without those ngspice wrote with
// zero length.
func values(r *rawfile.Raw, l rawfile.Layout, i int) Values {
	v := Values{}
	for k, x := range r.Vars {
		if strings.Contains(x.Type, "dims=0") {
			continue
		}
		v[deviceVector(x.Name)] = r.Value(l, i, k)
	}
	return v
}

// Voltage returns the voltage of a node. Node 0 is ground, and so is gnd if
// ngspice did not report it as a node of its own.
func (v Values) Voltage(node string) (float64, bool) {
	node = strings.ToLower(node)
	if node == "0" {
		return 0, true
	}
	if x, ok := v["v("+node+")"]; ok {
		return x, true
	}
	if node == "gnd" {
		return 0, true
	}
	return math.NaN(), false
}

// ElementOP is what the operating point says about one element.
type ElementOP struct {
	Element *Element
	Nodes   []float64          // node voltages, in the order of Element.Nodes; NaN if missing
	Device  map[string]float64 // device parameters: "i", "p", "id", "ic", "vgs", …
	Branch  float64            // branch current of a voltage source or inductor; NaN otherwise
	Pins    []float64          // subcircuit instance: the current into each pin (PinCurrent); NaN if not measured
	Missing []string           // expected vectors that are not in the results
}

// Element returns the operating point of element e: its node voltages, the
// device parameters saved for its kind, and the branch current of a voltage
// source or inductor.
func (v Values) Element(e *Element) ElementOP {

	op := ElementOP{Element: e, Device: map[string]float64{}, Branch: math.NaN()}

	for _, n := range e.Nodes {
		x, ok := v.Voltage(n)
		if !ok {
			op.Missing = append(op.Missing, "v("+n+")")
		}
		op.Nodes = append(op.Nodes, x)
	}

	name := strings.ToLower(e.Name)
	for _, p := range deviceParams[e.Kind] {
		vec := "@" + name + "[" + p + "]"
		if x, ok := v[vec]; ok {
			op.Device[p] = x
		} else {
			op.Missing = append(op.Missing, vec)
		}
	}
	for _, p := range tranParams[e.Kind] {
		if x, ok := v["@"+name+"["+p+"]"]; ok {
			op.Device[p] = x
		}
	}

	if e.Kind == "X" {
		for k := range e.Nodes {
			x, ok := v[PinCurrent(e.Name, k)]
			if !ok {
				x = math.NaN()
			}
			op.Pins = append(op.Pins, x)
		}
	}

	if e.Kind == "V" || e.Kind == "L" {
		vec := "i(" + name + ")"
		if x, ok := v[vec]; ok {
			op.Branch = x
		} else {
			op.Missing = append(op.Missing, vec)
		}
	}

	return op
}
