package spice

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

func TestDeck(t *testing.T) {

	src := `title
R1 a 0 1k
.tran 1n 1u
* a comment between a card and its continuation
+ 0 1n
.control
run
.endc
.include "sub/x.inc" ; models
.lib lib.lib typ
.lib typ
.endl
.end
R2 after the end
`
	vectors := []string{"@r1[i]", "@r1[p]", "@d1[id]", "@q1[ic]", "@q1[ib]", "@q1[ie]", "@q1[p]", "@m1[id]", "@m1[vgs]", "@m1[vds]"}
	deck, notes := Deck([]byte(src), "/base/net.net", DeckOptions{Vectors: vectors, Raw: "op.raw"})

	want := `title
R1 a 0 1k
* fitcalc: .tran 1n 1u
* a comment between a card and its continuation
* fitcalc: + 0 1n
* fitcalc: .control
* fitcalc: run
* fitcalc: .endc
.include "/base/sub/x.inc"
.lib "/base/lib.lib" typ
.lib typ
.endl
* fitcalc: operating point analysis
.control
set filetype=binary
save all
save @r1[i] @r1[p] @d1[id] @q1[ic] @q1[ib] @q1[ie] @q1[p] @m1[id]
save @m1[vgs] @m1[vds]
op
write op.raw
quit
.endc
.end
`
	if string(deck) != want {
		t.Errorf("deck:\n%s\nwant:\n%s", deck, want)
	}

	wantNotes := []string{
		"/base/net.net:6: .control block removed",
		"/base/net.net: analysis and output cards commented out: .tran (line 3)",
	}
	if !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("notes %q, want %q", notes, wantNotes)
	}
}

func TestDeckNoEndCRLF(t *testing.T) {
	deck, _ := Deck([]byte("t\r\nR1 a 0 1\r\n"), "/n.net", DeckOptions{Raw: "op.raw"})
	if strings.Contains(string(deck), "\r") {
		t.Error("deck contains CR")
	}
	if !strings.HasPrefix(string(deck), "t\nR1 a 0 1\n* fitcalc: operating point analysis\n") ||
		!strings.HasSuffix(string(deck), "\n.end\n") {
		t.Errorf("deck:\n%s", deck)
	}
}

// TestDeckTemp: the temperature is set in the control block, just before
// the analysis; the netlist's .temp card stays.
func TestDeckTemp(t *testing.T) {
	temp := 85.0
	deck, _ := Deck([]byte("t\nR1 a 0 1\n.temp 50\n.end\n"), "/n.net", DeckOptions{Raw: "op.raw", Temp: &temp})
	if s := string(deck); !strings.Contains(s, "\n.temp 50\n") || !strings.Contains(s, "\noption temp=85\nop\n") {
		t.Errorf("deck:\n%s", deck)
	}
}

// TestDeckSweep: a DC sweep instead of the operating point.
func TestDeckSweep(t *testing.T) {
	w, err := ParseSweep("V1 10 14 1")
	if err != nil {
		t.Fatal(err)
	}
	deck, _ := Deck([]byte("t\nV1 a 0 12\nR1 a 0 1k\n.dc V1 0 5 1\n.end\n"), "/n.net", DeckOptions{Raw: "op.raw", Analysis: w})
	s := string(deck)
	if !strings.Contains(s, "\n* fitcalc: .dc V1 0 5 1\n") || !strings.Contains(s, "\n* fitcalc: DC sweep\n.control\n") ||
		!strings.Contains(s, "\ndc v1 10 14 1\nwrite op.raw\n") || strings.Contains(s, "\nop\n") {
		t.Errorf("deck:\n%s", deck)
	}
}

// TestDeckPins: a subcircuit instance of the netlist file is written again,
// its pins through zero-volt sources; one from an include file is not.
func TestDeckPins(t *testing.T) {
	src := "t\nXU1 in out\n+ vcc 0 opamp gain=2\nXU2 a b sub\nR1 a 0 1k\n.end\n"
	pins := []*Element{
		{Name: "XU1", Kind: "X", Nodes: []string{"in", "out", "vcc", "0"}, File: "/n.net", Line: 2, Text: "XU1 in out vcc 0 opamp gain=2"},
		{Name: "XU2", Kind: "X", Nodes: []string{"a", "b"}, File: "/inc.lib", Line: 4, Text: "XU2 a b sub"},
	}
	deck, _ := Deck([]byte(src), "/n.net", DeckOptions{Raw: "op.raw", Pins: pins})
	for _, want := range []string{
		"\n* fitcalc: XU1 in out\n* fitcalc: + vcc 0 opamp gain=2\nXU2 a b sub\n",
		"\n* fitcalc: subcircuit instances, with their pin currents measured\n" +
			"XU1 fitcalc_xu1_1 fitcalc_xu1_2 fitcalc_xu1_3 fitcalc_xu1_4 opamp gain=2\n" +
			"vfitcalc_xu1_1 in fitcalc_xu1_1 0\nvfitcalc_xu1_2 out fitcalc_xu1_2 0\n" +
			"vfitcalc_xu1_3 vcc fitcalc_xu1_3 0\nvfitcalc_xu1_4 0 fitcalc_xu1_4 0\n" +
			"* fitcalc: operating point analysis\n",
	} {
		if !strings.Contains(string(deck), want) {
			t.Errorf("deck without\n%s\ndeck:\n%s", want, deck)
		}
	}
	if got := PinCurrent("XU1", 0); got != "i(vfitcalc_xu1_1)" {
		t.Errorf("PinCurrent = %q", got)
	}
}

// TestDeckKiCad compares the deck of testdata/kicad.net with a golden file,
// in which the absolute path of testdata is replaced by TESTDATA.
func TestDeckKiCad(t *testing.T) {

	file := "../testdata/kicad.net"
	c, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	deck, notes := Deck(src, file, DeckOptions{Vectors: Vectors(c), Raw: "op.raw", Pins: subcircuits(c)})

	abs, err := filepath.Abs("../testdata")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(string(deck), abs, "TESTDATA")

	golden := "../testdata/golden/kicad.deck"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("deck:\n%s\nwant:\n%s", got, want)
	}

	wantNotes := []string{file + ": analysis and output cards commented out: .save (line 3), .probe (line 4), .op (line 5)"}
	if !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("notes %q, want %q", notes, wantNotes)
	}
}

func TestVectors(t *testing.T) {
	c, err := Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	v := Vectors(c)
	// 9 resistors × 2, 2 diodes × 1, 1 BJT × 4, 1 MOSFET × 4, 1 JFET × 3
	if len(v) != 31 {
		t.Errorf("%d vectors, want 31: %v", len(v), v)
	}
	for _, want := range []string{"@r1[i]", "@r1[p]", "@d2[id]", "@q1[ie]", "@m1[vds]", "@j1[vgs]"} {
		if !contains(v, want) {
			t.Errorf("%s missing from %v", want, v)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestScanLog(t *testing.T) {

	lg := scanLog(`Note: No compatibility mode selected!
Warning: singular matrix:  check node b
Note: Starting dynamic gmin stepping
Warning: singular matrix:  check node b
Warning from checkvalid: vector @R1[xyz] is not available or has zero length.
Error during 'write': no writable vector found.
DC solution failed -
ngspice-47 done
`)
	if want := []string{"Error during 'write': no writable vector found.", "DC solution failed -"}; !reflect.DeepEqual(lg.errors, want) {
		t.Errorf("errors %q, want %q", lg.errors, want)
	}
	if want := []string{
		"ngspice: Warning: singular matrix:  check node b (2 times)",
		"ngspice: Warning from checkvalid: vector @R1[xyz] is not available or has zero length.",
	}; !reflect.DeepEqual(lg.warnings, want) {
		t.Errorf("warnings %q, want %q", lg.warnings, want)
	}
	if want := []string{"@r1[xyz]"}; !reflect.DeepEqual(lg.unavailable, want) {
		t.Errorf("unavailable %q, want %q", lg.unavailable, want)
	}
}

func TestZeroLength(t *testing.T) {
	raw := "Title: t\nNo. Variables: 3\nVariables:\n\t0\tv(a)\tvoltage\n\t1\ti(@r1[i])\tcurrent\n" +
		"\t2\tv(@R1[xyz])\tvoltage dims=0\nValues:\n 0\t1\n"
	if got := zeroLength([]byte(raw)); !reflect.DeepEqual(got, []string{"@r1[xyz]"}) {
		t.Errorf("zeroLength = %q, want [@r1[xyz]]", got)
	}
}

func TestSimTemp(t *testing.T) {
	if got := simTemp("Circuit: x\nDoing analysis at TEMP = 27.000000 and TNOM = 27.000000\n"); got != 27 {
		t.Errorf("simTemp = %g, want 27", got)
	}
	if got := simTemp("no analysis"); got == got {
		t.Errorf("simTemp = %g, want NaN", got)
	}
}

func TestRawHeader(t *testing.T) {
	h := rawHeader([]byte("Title: t\nPlotname: Operating Point\nFlags: real\nVariables:\n\t0\tv(a)\tvoltage\nBinary:\n\x00\x01"))
	if h["Plotname"] != "Operating Point" || h["Flags"] != "real" || h["Title"] != "t" {
		t.Errorf("header %v", h)
	}
}
