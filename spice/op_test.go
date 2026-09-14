package spice

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestReadOPZeroLength(t *testing.T) {
	raw := "Title: t\nDate: d\nPlotname: Operating Point\nFlags: real\nNo. Variables: 2\nNo. Points: 1\n" +
		"Variables:\n\t0\tv(a)\tvoltage\n\t1\tv(@r1[xyz])\tvoltage dims=0\nValues:\n 0\t1.5\n\t0.0\n"
	v, err := ReadOP([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || v["v(a)"] != 1.5 {
		t.Errorf("values %v, want only v(a) = 1.5", v)
	}
}

// TestReadOPNoCommand: ngspice 43 writes no Command: line, and its binary
// values are f64 all the same.
func TestReadOPNoCommand(t *testing.T) {
	hdr := "Title: t\nDate: d\nPlotname: Operating Point\nFlags: real\nNo. Variables: 3\nNo. Points: 1\n" +
		"Variables:\n\t0\tv(a)\tvoltage\n\t1\tv(b)\tvoltage\n\t2\ti(v1)\tcurrent\nBinary:\n"
	raw := []byte(hdr)
	for _, x := range []float64{12, 5.5, -1e-3} {
		raw = binary.LittleEndian.AppendUint64(raw, math.Float64bits(x))
	}
	v, err := ReadOP(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v["v(a)"] != 12 || v["v(b)"] != 5.5 || v["i(v1)"] != -1e-3 {
		t.Errorf("values %v, want v(a) = 12, v(b) = 5.5, i(v1) = -0.001", v)
	}
}

func TestReadOPNotOP(t *testing.T) {
	raw := "Title: t\nPlotname: Transient Analysis\nFlags: real\nNo. Variables: 2\nNo. Points: 1\n" +
		"Variables:\n\t0\ttime\ttime\n\t1\tv(a)\tvoltage\nValues:\n 0\t0\n\t1\n"
	if _, err := ReadOP([]byte(raw)); !errors.Is(err, ErrSimulation) {
		t.Errorf("error %v, want ErrSimulation", err)
	}
	if _, err := ReadOP([]byte("garbage")); !errors.Is(err, ErrSimulation) {
		t.Errorf("error %v, want ErrSimulation", err)
	}
}

func runOP(t *testing.T, file string) (*Circuit, Values) {
	t.Helper()
	needNgspice(t)
	c, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return c, v
}

// TestElementOPProbe checks the operating point of every kind in the probe
// circuit, including ngspice's power parameters against V·I.
func TestElementOPProbe(t *testing.T) {

	c, v := runOP(t, "../testdata/probe.net")

	for _, e := range c.Elements {
		if op := v.Element(e); len(op.Missing) > 0 {
			t.Errorf("%s: missing %v", e.Name, op.Missing)
		}
	}

	r1 := v.Element(c.Element("R1"))
	if r1.Nodes[0] != 12 {
		t.Errorf("R1: v(vcc) = %g, want 12", r1.Nodes[0])
	}
	if i := (r1.Nodes[0] - r1.Nodes[1]) / 10e3; !near(r1.Device["i"], i, 1e-9) {
		t.Errorf("R1: i = %g, want (V1-V2)/R = %g", r1.Device["i"], i)
	}

	// ngspice's own power equals V·I for R, Q, M and J
	for _, tc := range []struct {
		name string
		vi   func(op ElementOP) float64
	}{
		{"R1", func(op ElementOP) float64 { return (op.Nodes[0] - op.Nodes[1]) * op.Device["i"] }},
		{"Q1", func(op ElementOP) float64 {
			return (op.Nodes[0]-op.Nodes[2])*op.Device["ic"] + (op.Nodes[1]-op.Nodes[2])*op.Device["ib"]
		}},
		{"M1", func(op ElementOP) float64 { return op.Device["vds"] * op.Device["id"] }},
		{"J1", func(op ElementOP) float64 { return (op.Nodes[0] - op.Nodes[2]) * op.Device["id"] }},
	} {
		op := v.Element(c.Element(tc.name))
		if p := tc.vi(op); !near(op.Device["p"], p, 1e-6) {
			t.Errorf("%s: @p = %g, V·I = %g", tc.name, op.Device["p"], p)
		}
	}

	// MOSFET vds is the node voltage difference
	m1 := v.Element(c.Element("M1"))
	if !near(m1.Device["vds"], m1.Nodes[0]-m1.Nodes[2], 1e-9) {
		t.Errorf("M1: vds = %g, v(d)-v(s) = %g", m1.Device["vds"], m1.Nodes[0]-m1.Nodes[2])
	}

	// Branch currents of the source and the inductor: the inductor carries
	// the current of R3 and D1
	if b := v.Element(c.Element("V1")).Branch; math.IsNaN(b) || b >= 0 {
		t.Errorf("V1: branch current %g, want negative (the source delivers)", b)
	}
	l1, r3 := v.Element(c.Element("L1")), v.Element(c.Element("R3"))
	if !near(l1.Branch, r3.Device["i"], 1e-9) {
		t.Errorf("L1: i = %g, R3: i = %g", l1.Branch, r3.Device["i"])
	}

	// Subcircuit instance: pin voltages only
	if x1 := v.Element(c.Element("X1")); len(x1.Nodes) != 2 || len(x1.Device) != 0 {
		t.Errorf("X1: %+v", x1)
	}
}

func TestElementOPKiCad(t *testing.T) {

	c, v := runOP(t, "../testdata/kicad.net")

	for _, e := range c.Elements {
		if op := v.Element(e); len(op.Missing) > 0 {
			t.Errorf("%s: missing %v", e.Name, op.Missing)
		}
	}
	xu1 := v.Element(c.Element("XU1"))
	if xu1.Nodes[2] != 12 || xu1.Nodes[3] != 0 {
		t.Errorf("XU1: supply pins %g, %g; want 12, 0", xu1.Nodes[2], xu1.Nodes[3])
	}
	d1 := v.Element(c.Element("D1"))
	if !strings.Contains(strings.Join(c.Element("D1").Nodes, " "), "net-_d1-a_") || d1.Device["id"] <= 0 {
		t.Errorf("D1: nodes %v, id %g", c.Element("D1").Nodes, d1.Device["id"])
	}
}

func near(a, b, rel float64) bool {
	return a == b || math.Abs(a-b) <= rel*math.Max(math.Abs(a), math.Abs(b))
}
