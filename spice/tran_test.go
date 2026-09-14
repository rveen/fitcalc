package spice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTran(t *testing.T) {
	tr, err := ParseTran("10u 3ms 1m")
	if err != nil {
		t.Fatal(err)
	}
	if !near(tr.Step, 10e-6, 1e-12) || !near(tr.Stop, 3e-3, 1e-12) || !near(tr.Start, 1e-3, 1e-12) || tr.Max != 0 || !near(tr.Duration(), 2e-3, 1e-12) {
		t.Errorf("transient %+v", tr)
	}
	if got := tr.command(); got != "tran 1e-05 0.003 0.001" {
		t.Errorf("command %q", got)
	}
	if got := tr.String(); got != "from 0 to 3 ms in steps of 10 µs, recorded from 1 ms" {
		t.Errorf("String() = %q", got)
	}
	if got := tr.Label(1.25e-3); got != "t = 1.25 ms" {
		t.Errorf("label %q", got)
	}

	tr, err = ParseTran("1u 1m 0 0.5u")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.command(); got != "tran 1e-06 0.001 0 5e-07" {
		t.Errorf("command %q", got)
	}
	if got := tr.String(); got != "from 0 to 1 ms in steps of 1 µs, time steps of at most 500 ns" {
		t.Errorf("String() = %q", got)
	}
	if tr, err := ParseTran("1n 2u"); err != nil || tr.command() != "tran 1e-09 2e-06" {
		t.Errorf("1n 2u: %+v, %v", tr, err)
	}

	for _, bad := range []string{"10u", "10u 1m 0 1u 5", "0 5m", "-1u 5m", "10u 1m 2m", "10u 1m 1m", "1u 1m 0 -1u", "x 1m", "1u 1"} {
		if _, err := ParseTran(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestTime(t *testing.T) {
	for x, want := range map[float64]string{0: "0 s", 2: "2 s", 1e-3: "1 ms", 1.25e-3: "1.25 ms", 10e-6: "10 µs", 5e-7: "500 ns", 3e-13: "0.3 ps", -2e-3: "-2 ms"} {
		if got := Time(x); got != want {
			t.Errorf("Time(%g) = %q, want %q", x, got, want)
		}
	}
}

// TestDeckTran: a transient analysis instead of the operating point.
func TestDeckTran(t *testing.T) {
	tr, err := ParseTran("10u 5m 1m")
	if err != nil {
		t.Fatal(err)
	}
	deck, _ := Deck([]byte("t\nV1 a 0 SIN(0 1 1k)\nR1 a 0 1k\n.tran 1u 1m\n.end\n"), "/n.net", DeckOptions{Raw: "op.raw", Analysis: tr})
	s := string(deck)
	if !strings.Contains(s, "\n* fitcalc: .tran 1u 1m\n") || !strings.Contains(s, "\n* fitcalc: transient analysis\n.control\n") ||
		!strings.Contains(s, "\ntran 1e-05 0.005 0.001\nwrite op.raw\n") || strings.Contains(s, "\nop\n") {
		t.Errorf("deck:\n%s", s)
	}
}

// TestOPTran: a transient analysis of an RC low-pass with a square wave,
// recorded from 1 ms.
func TestOPTran(t *testing.T) {
	needNgspice(t)

	file := filepath.Join(t.TempDir(), "rc.net")
	net := "rc\nV1 in 0 PULSE(0 5 0 1u 1u 0.5m 1m)\nR1 in out 1k\nC1 out 0 100n\n.end\n"
	if err := os.WriteFile(file, []byte(net), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseTran("10u 3m 1m")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{Analysis: tr}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	times, points, err := ReadTran(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(times) < 200 || !near(times[0], 1e-3, 1e-9) || !near(times[len(times)-1], 3e-3, 1e-9) {
		t.Fatalf("%d time points from %g to %g s, want 200 or more from 1 ms to 3 ms", len(times), times[0], times[len(times)-1])
	}

	// The capacitor current is saved, and is the resistor's
	var peak float64
	for k, v := range points {
		c1 := v.Element(c.Element("C1"))
		i, ok := c1.Device["i"]
		if !ok {
			t.Fatalf("t = %g: no capacitor current", times[k])
		}
		if r := v["@r1[i]"]; !near(i, r, 1e-9) {
			t.Errorf("t = %g: @c1[i] = %g, @r1[i] = %g", times[k], i, r)
		}
		peak = max(peak, i)
	}
	if peak < 4e-3 {
		t.Errorf("largest capacitor current %g A, want about 5 mA", peak)
	}

	if _, _, err := ReadTran(mustOP(t, c)); err == nil {
		t.Error("ReadTran of an operating point: no error")
	}
	if _, _, err := ReadSweep(op.Raw); err == nil {
		t.Error("ReadSweep of a transient: no error")
	}
}
