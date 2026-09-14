package spice

import (
	"context"
	"testing"
)

func TestParseSweep(t *testing.T) {
	w, err := ParseSweep("v1 10.8 13.2 0.2")
	if err != nil {
		t.Fatal(err)
	}
	if w.Source != "V1" || w.Start != 10.8 || w.Stop != 13.2 || w.Step != 0.2 || w.Points() != 13 || w.Unit() != "V" {
		t.Errorf("sweep %+v, %d points", w, w.Points())
	}
	if got := w.command(); got != "dc v1 10.8 13.2 0.2" {
		t.Errorf("command %q", got)
	}
	if got := w.Label(13.2); got != "V1 = 13.2 V" {
		t.Errorf("label %q", got)
	}
	if got := w.String(); got != "V1 from 10.8 V to 13.2 V in steps of 0.2 V (13 points)" {
		t.Errorf("String() = %q", got)
	}

	// Downwards, with SPICE values
	if w, err := ParseSweep("I1 10m 0 -1m"); err != nil || w.Points() != 11 || w.Unit() != "A" {
		t.Errorf("I1 10m 0 -1m: %+v, %v", w, err)
	}

	for _, bad := range []string{"V1 0 1", "R1 0 1 0.1", "V1 0 1 0", "V1 0 1 -0.1", "V1 0 1 x", "V1 0 1000 0.1"} {
		if _, err := ParseSweep(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// TestOPSweep: a DC sweep of the probe circuit's supply.
func TestOPSweep(t *testing.T) {
	needNgspice(t)

	c, err := Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	w, err := ParseSweep("V1 10 14 1")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{Analysis: w}).OPAt(context.Background(), c, 60)
	if err != nil {
		t.Fatal(err)
	}
	if op.Temp != 60 {
		t.Errorf("simulated at %g °C, want 60", op.Temp)
	}
	axis, points, err := ReadSweep(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(axis) != 5 || len(points) != 5 {
		t.Fatalf("%d points, want 5", len(points))
	}
	for k, v := range points {
		if axis[k] != float64(10+k) {
			t.Errorf("point %d at %g V", k, axis[k])
		}
		if vcc, _ := v.Voltage("vcc"); vcc != axis[k] {
			t.Errorf("point %d: v(vcc) = %g, want %g", k, vcc, axis[k])
		}
		if x1 := v.Element(c.Element("X1")); !near(x1.Pins[0], axis[k]/4e3, 1e-9) {
			t.Errorf("point %d: X1 pin current %g, want %g", k, x1.Pins[0], axis[k]/4e3)
		}
		if _, ok := v["@r1[i]"]; !ok {
			t.Errorf("point %d: no @r1[i]", k)
		}
	}

	// The operating point is not a sweep, nor the other way round
	if _, _, err := ReadSweep(mustOP(t, c)); err == nil {
		t.Error("ReadSweep of an operating point: no error")
	}
	if _, err := ReadOP(op.Raw); err == nil {
		t.Error("ReadOP of a sweep: no error")
	}
}

func mustOP(t *testing.T, c *Circuit) []byte {
	t.Helper()
	op, err := (&Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return op.Raw
}
