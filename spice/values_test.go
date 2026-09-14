package spice

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeckValues: values replaced in a source, a resistor with a
// temperature, a capacitor with a value parameter; not in an expression.
func TestDeckValues(t *testing.T) {
	src := "t\nV1 a 0 dc 12\nR1 a b 1k tc1=0.01\nC1 b 0 c=100n\nR2 b 0 {rl}\n.end\n"
	el := func(name, kind string, value float64, text string, line int) *Element {
		f := strings.Fields(text)
		return &Element{Name: name, Kind: kind, Nodes: f[1:3], Value: value, ValueText: strings.Join(f[3:], " "), File: "/n.net", Line: line, Text: text}
	}
	v1 := el("V1", "V", 12, "V1 a 0 dc 12", 2)
	r1 := el("R1", "R", 1e3, "R1 a b 1k tc1=0.01", 3)
	c1 := el("C1", "C", 100e-9, "C1 b 0 c=100n", 4)
	r2 := el("R2", "R", math.NaN(), "R2 b 0 {rl}", 5)

	deck, notes := Deck([]byte(src), "/n.net", DeckOptions{Raw: "op.raw",
		Values: map[*Element]float64{v1: 12.6, r1: 1010, c1: 9.5e-8, r2: 5}, Dtemp: map[*Element]float64{r1: 3}})
	s := string(deck)
	for _, want := range []string{
		"\n* fitcalc: V1 a 0 dc 12\n",
		"\n* fitcalc: elements with a varied value or at their temperature\nV1 a 0 dc 12.6\nR1 a b 1010 tc1=0.01 dtemp=3\nC1 b 0 c=9.5e-08\n",
		"\nR2 b 0 {rl}\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("deck without %q:\n%s", want, s)
		}
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "R2: fitcalc cannot change its value {rl}") {
		t.Errorf("notes %q", notes)
	}
}

// TestRunnerValues: R1 of a divider at 15 kΩ instead of 10 kΩ.
func TestRunnerValues(t *testing.T) {
	needNgspice(t)

	file := filepath.Join(t.TempDir(), "div.net")
	if err := os.WriteFile(file, []byte("div\nV1 in 0 12\nR1 in out 10k\nR2 out 0 1k\n.end\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{Values: map[string]float64{"R1": 15e3}}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := v.Voltage("out"); !near(out, 0.75, 1e-9) {
		t.Errorf("v(out) = %g, want 0.75", out)
	}
}
