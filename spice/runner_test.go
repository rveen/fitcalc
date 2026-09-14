package spice

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Integration tests: they run ngspice and are skipped without it.

func needNgspice(t *testing.T) {
	t.Helper()
	name := os.Getenv("NGSPICE")
	if name == "" {
		name = "ngspice"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("ngspice not found")
	}
}

func parseFile(t *testing.T, dir, name, content string) *Circuit {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rawVariables returns the variable names of a raw file, with the i(…) and
// v(…) wrappers of device vectors removed.
func rawVariables(raw []byte) []string {
	var names []string
	in := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case line == "Variables:":
			in = true
		case line == "Binary:" || line == "Values:":
			return names
		case in:
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			n := f[1]
			if (strings.HasPrefix(n, "i(@") || strings.HasPrefix(n, "v(@")) && strings.HasSuffix(n, ")") {
				n = n[2 : len(n)-1]
			}
			names = append(names, n)
		}
	}
	return names
}

func TestOPProbe(t *testing.T) {
	needNgspice(t)

	c, err := Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(t.TempDir(), "keep")

	op, err := (&Runner{Keep: keep}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(op.Version, "ngspice-") {
		t.Errorf("version %q", op.Version)
	}
	if h := rawHeader(op.Raw); h["Plotname"] != "Operating Point" {
		t.Errorf("raw header %v", h)
	}
	if len(op.Unavailable) != 0 {
		t.Errorf("unavailable vectors: %v", op.Unavailable)
	}
	if op.Temp != 27 {
		t.Errorf("temperature %g °C, want ngspice's default 27", op.Temp)
	}
	if op.Dir != keep {
		t.Errorf("dir %q, want %q", op.Dir, keep)
	}
	for _, f := range []string{deckFile, logFile, rawFile} {
		if _, err := os.Stat(filepath.Join(keep, f)); err != nil {
			t.Error(err)
		}
	}

	// Every saved device vector is in the raw file: the deviceParams table
	// holds for this ngspice version
	vars := rawVariables(op.Raw)
	for _, v := range Vectors(c) {
		if !contains(vars, v) {
			t.Errorf("%s not in the raw file variables %v", v, vars)
		}
	}
	for _, v := range []string{"v(out)", "i(v1)", "i(l1)"} {
		if !contains(vars, v) {
			t.Errorf("%s not in the raw file variables", v)
		}
	}
}

// TestOPAt: OPAt overrides the .temp card of the netlist, which OP keeps.
func TestOPAt(t *testing.T) {
	needNgspice(t)

	c := parseFile(t, t.TempDir(), "temp.net", "temp\nV1 a 0 1\nR1 a 0 1k\n.temp 50\n.end\n")
	for _, tc := range []struct {
		temp, want float64
	}{
		{math.NaN(), 50}, {85, 85}, {-40, -40},
	} {
		op, err := (&Runner{}).op(context.Background(), c, Vectors(c), tc.temp)
		if err != nil {
			t.Fatal(err)
		}
		if op.Temp != tc.want {
			t.Errorf("temp %g: simulated at %g °C, want %g", tc.temp, op.Temp, tc.want)
		}
	}
}

// TestOPPinCurrents: the currents into the pins of the probe circuit's
// divider X1, 2 kΩ + 2 kΩ from vcc (12 V) to ground, with its middle pin open.
func TestOPPinCurrents(t *testing.T) {
	needNgspice(t)

	c, err := Parse("../testdata/probe.net")
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
	x1 := v.Element(c.Element("X1"))
	if len(x1.Pins) != 2 || !near(x1.Pins[0], 12/4e3, 1e-9) || math.Abs(x1.Pins[1]) > 1e-12 {
		t.Errorf("X1 pin currents %v, want 3 mA and 0", x1.Pins)
	}
	if !near(x1.Nodes[1], 6, 1e-9) {
		t.Errorf("X1 middle pin at %g V, want 6", x1.Nodes[1])
	}
}

func TestOPUnavailableVector(t *testing.T) {
	needNgspice(t)

	c, err := Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{}).op(context.Background(), c, append(Vectors(c), "@r1[xyz]"), math.NaN())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(op.Unavailable, []string{"@r1[xyz]"}) {
		t.Errorf("unavailable %v", op.Unavailable)
	}
	if contains(rawVariables(op.Raw), "@r1[xyz]") || !contains(rawVariables(op.Raw), "@r1[i]") {
		t.Errorf("raw variables %v", rawVariables(op.Raw))
	}
}

// An element line that ngspice ignores loses its device vectors; the run
// still succeeds, with ngspice's warning.
func TestOPIgnoredElement(t *testing.T) {
	needNgspice(t)

	c := parseFile(t, t.TempDir(), "ignored.net", "ignored element\nV1 a 0 1\nR1 a\nR2 a 0 1k\n.end\n")
	op, err := (&Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(op.Unavailable, "@r1[i]") {
		t.Errorf("unavailable %v, want @r1[i]", op.Unavailable)
	}
	if !strings.Contains(strings.Join(op.Warnings, "\n"), "not a valid resistor instance line") {
		t.Errorf("warnings %q", op.Warnings)
	}
}

func TestOPFailures(t *testing.T) {
	needNgspice(t)

	dir := t.TempDir()
	for _, tc := range []struct {
		name, netlist, want string
	}{
		{"missing model", "missing model\nV1 a 0 1\nD1 a 0 NOSUCHMODEL\n.end\n", "exited with status 1"},
		{"voltage source loop", "vloop\nV1 a 0 1\nV2 a 0 2\nR1 a 0 1k\n.end\n", "DC solution failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := parseFile(t, dir, strings.ReplaceAll(tc.name, " ", "_")+".net", tc.netlist)
			_, err := (&Runner{}).OP(context.Background(), c)
			if !errors.Is(err, ErrSimulation) {
				t.Fatalf("error %v, want ErrSimulation", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestOPNotFound(t *testing.T) {
	c := &Circuit{File: "../testdata/probe.net"}
	_, err := (&Runner{Exe: "/nonexistent/ngspice"}).OP(context.Background(), c)
	if err == nil || errors.Is(err, ErrSimulation) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %v, want a not-found error that is not ErrSimulation", err)
	}
}
