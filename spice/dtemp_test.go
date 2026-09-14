package spice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeckDtemp: elements of the netlist file are written again with their
// temperature; those in an included file, those with their own temperature
// and subcircuit instances are not.
func TestDeckDtemp(t *testing.T) {
	src := "t\nV1 a 0 12\nR1 a b 1k\n+ tc1=0.01\nQ1 c b 0 QMOD temp=50\nX1 a b DIV\n.end\n"
	r1 := &Element{Name: "R1", Kind: "R", File: "/n.net", Line: 3, Text: "R1 a b 1k tc1=0.01", Params: map[string]string{"tc1": "0.01"}}
	q1 := &Element{Name: "Q1", Kind: "Q", File: "/n.net", Line: 5, Text: "Q1 c b 0 QMOD temp=50", Params: map[string]string{"temp": "50"}}
	x1 := &Element{Name: "X1", Kind: "X", File: "/n.net", Line: 6, Text: "X1 a b DIV"}
	d9 := &Element{Name: "D9", Kind: "D", File: "/inc.lib", Line: 2, Text: "D9 a 0 DMOD"}

	deck, notes := Deck([]byte(src), "/n.net", DeckOptions{Raw: "op.raw", Dtemp: map[*Element]float64{r1: 12.5, q1: 3, x1: 4, d9: 1}})
	s := string(deck)
	for _, want := range []string{
		"\n* fitcalc: R1 a b 1k\n* fitcalc: + tc1=0.01\n",
		"\nQ1 c b 0 QMOD temp=50\nX1 a b DIV\n",
		"\n* fitcalc: elements with a varied value or at their temperature\nR1 a b 1k tc1=0.01 dtemp=12.5\n* fitcalc: operating point analysis\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("deck without %q:\n%s", want, s)
		}
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "D9 is in an included file") || !strings.Contains(notes[1], "Q1 sets its own temperature") {
		t.Errorf("notes %q", notes)
	}
}

// TestRunnerDtemp: a resistor with a temperature coefficient, 50 K above the
// simulation temperature.
func TestRunnerDtemp(t *testing.T) {
	needNgspice(t)

	file := filepath.Join(t.TempDir(), "div.net")
	if err := os.WriteFile(file, []byte("div\nV1 in 0 12\nR1 in out 10k tc1=0.01\nR2 out 0 1k\n.end\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&Runner{Dtemp: map[string]float64{"R1": 50}}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := v.Voltage("out"); !near(out, 0.75, 1e-9) { // R1 = 15 kΩ
		t.Errorf("v(out) = %g, want 0.75", out)
	}
}
