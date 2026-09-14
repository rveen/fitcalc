package spice

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func parseString(t *testing.T, s string) *Circuit {
	t.Helper()
	c, err := ParseReader(strings.NewReader(s), "test.net")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func names(c *Circuit) []string {
	var n []string
	for _, e := range c.Elements {
		n = append(n, e.Name)
	}
	return n
}

func sameValue(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	return math.Abs(a-b) <= 1e-12*math.Max(math.Abs(a), math.Abs(b))
}

func TestParseProbe(t *testing.T) {

	c, err := Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}

	if c.Title != "OP probe circuit: one element of each kind handled by fitcalc" {
		t.Errorf("title %q", c.Title)
	}
	want := []string{"V1", "R1", "R2", "C1", "L1", "R3", "D1", "D2", "RB", "Q1", "RC", "M1",
		"RG", "RG2", "RD", "J1", "RJ", "X1"}
	if got := names(c); !reflect.DeepEqual(got, want) {
		t.Errorf("elements %v, want %v (subcircuit elements excluded)", got, want)
	}

	for _, tc := range []struct {
		name  string
		kind  string
		nodes []string
		model string
		value float64
	}{
		{"V1", "V", []string{"vcc", "0"}, "", 12},
		{"R1", "R", []string{"vcc", "out"}, "", 10e3},
		{"C1", "C", []string{"out", "0"}, "", 100e-9},
		{"L1", "L", []string{"vcc", "nl"}, "", 10e-6},
		{"RG", "R", []string{"vcc", "g"}, "", 1e6},
		{"D1", "D", []string{"dk", "0"}, "dmod", math.NaN()},
		{"Q1", "Q", []string{"c", "b", "0"}, "qmod", math.NaN()},
		{"M1", "M", []string{"d", "g", "0", "0"}, "nmod", math.NaN()},
		{"J1", "J", []string{"dj", "0", "0"}, "jmod", math.NaN()},
		{"X1", "X", []string{"vcc", "sub1"}, "div", math.NaN()},
	} {
		e := c.Element(tc.name)
		if e == nil {
			t.Errorf("%s: not found", tc.name)
			continue
		}
		if e.Kind != tc.kind || !reflect.DeepEqual(e.Nodes, tc.nodes) || e.Model != tc.model || !sameValue(e.Value, tc.value) {
			t.Errorf("%s: kind %s nodes %v model %q value %g; want %s %v %q %g",
				tc.name, e.Kind, e.Nodes, e.Model, e.Value, tc.kind, tc.nodes, tc.model, tc.value)
		}
	}

	types := map[string]string{}
	for name, m := range c.Models {
		types[name] = m.Type
	}
	if want := map[string]string{"dmod": "d", "qmod": "npn", "nmod": "nmos", "jmod": "njf"}; !reflect.DeepEqual(types, want) {
		t.Errorf("models %v, want %v", types, want)
	}
	if s := c.Subckts["div"]; s == nil || !reflect.DeepEqual(s.Ports, []string{"a", "b"}) {
		t.Errorf("subckt div: %+v", s)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings: %v", c.Warnings)
	}
}

func TestParseSyntax(t *testing.T) {

	c := parseString(t, `title line R1 is not an element
* comment
R1 a b 1k ; inline comment
R2 a b
* a comment between a line and its continuation
+ 2k $ dollar comment
R7 a b 3k // slash comment
C1 a 0 10u ic=5
L1 a b l = 4.7u
R3 a b {rval}
R4 a b r=1meg
R5 a b RMOD l=10u w=1u
C2 a 0 '2*cval'
R6 A B 1K
.control
R99 x y 1
.endc
.end
R100 after the end
`)

	if c.Title != "title line R1 is not an element" {
		t.Errorf("title %q", c.Title)
	}
	if got, want := names(c), []string{"R1", "R2", "R7", "C1", "L1", "R3", "R4", "R5", "C2", "R6"}; !reflect.DeepEqual(got, want) {
		t.Errorf("elements %v, want %v", got, want)
	}

	for _, tc := range []struct {
		name      string
		value     float64
		valueText string
		model     string
	}{
		{"R1", 1e3, "1k", ""},
		{"R2", 2e3, "2k", ""},
		{"R7", 3e3, "3k", ""},
		{"C1", 10e-6, "10u", ""},
		{"L1", 4.7e-6, "4.7u", ""},
		{"R3", math.NaN(), "{rval}", ""},
		{"R4", 1e6, "1meg", ""},
		{"R5", math.NaN(), "", "rmod"},
		{"C2", math.NaN(), "'2*cval'", ""},
		{"R6", 1e3, "1K", ""},
	} {
		e := c.Element(tc.name)
		if !sameValue(e.Value, tc.value) || e.ValueText != tc.valueText || e.Model != tc.model {
			t.Errorf("%s: value %g %q model %q; want %g %q %q", tc.name, e.Value, e.ValueText, e.Model,
				tc.value, tc.valueText, tc.model)
		}
	}

	if e := c.Element("R2"); e.Text != "R2 a b 2k" || e.Line != 4 {
		t.Errorf("R2: text %q line %d", e.Text, e.Line)
	}
	if e := c.Element("C1"); e.Params["ic"] != "5" {
		t.Errorf("C1 params %v", e.Params)
	}
	if e := c.Element("R5"); e.Params["l"] != "10u" || e.Params["w"] != "1u" {
		t.Errorf("R5 params %v", e.Params)
	}
	if e := c.Element("R6"); !reflect.DeepEqual(e.Nodes, []string{"a", "b"}) {
		t.Errorf("R6 nodes %v, want lower case", e.Nodes)
	}
}

func TestParseNodes(t *testing.T) {

	c := parseString(t, `node counts
.model QN NPN
.model VD VDMOS
Q1 c b e QN
Q2 c b e s QN
Q3 c b e QN 2
Q4 c b e s QX
Q5 c b e QX 2
Q6 c b e QX off
M1 d g s b NM
M2 d g s VD
X1 a b c SUB params: w=1
V2 a 0 5
I1 a 0 dc 1m
V3 a 0 PULSE(0 5 1n 1n 1n 1u 2u)
K1 L1 L2 0.9
E1 o 0 a b 10
.end
`)

	for _, tc := range []struct {
		name  string
		nodes []string
		model string
	}{
		{"Q1", []string{"c", "b", "e"}, "qn"},
		{"Q2", []string{"c", "b", "e", "s"}, "qn"},
		{"Q3", []string{"c", "b", "e"}, "qn"},
		{"Q4", []string{"c", "b", "e", "s"}, "qx"},
		{"Q5", []string{"c", "b", "e"}, "qx"},
		{"Q6", []string{"c", "b", "e"}, "qx"},
		{"M1", []string{"d", "g", "s", "b"}, "nm"},
		{"M2", []string{"d", "g", "s"}, "vd"},
		{"X1", []string{"a", "b", "c"}, "sub"},
		{"K1", nil, ""},
		{"E1", []string{"o", "0"}, ""},
	} {
		e := c.Element(tc.name)
		if !reflect.DeepEqual(e.Nodes, tc.nodes) || e.Model != tc.model {
			t.Errorf("%s: nodes %v model %q; want %v %q", tc.name, e.Nodes, e.Model, tc.nodes, tc.model)
		}
	}

	if e := c.Element("X1"); e.Params["w"] != "1" {
		t.Errorf("X1 params %v", e.Params)
	}
	for name, v := range map[string]float64{"V2": 5, "I1": 1e-3, "V3": math.NaN()} {
		if e := c.Element(name); !sameValue(e.Value, v) {
			t.Errorf("%s: value %g, want %g", name, e.Value, v)
		}
	}
	if e := c.Element("V3"); e.ValueText != "PULSE(0 5 1n 1n 1n 1u 2u)" {
		t.Errorf("V3 value text %q", e.ValueText)
	}
	if e := c.Element("K1"); e.ValueText != "L1 L2 0.9" {
		t.Errorf("K1 value text %q", e.ValueText)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings: %v", c.Warnings)
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseIncludes(t *testing.T) {

	dir := writeFiles(t, map[string]string{
		"main.net":       "main\n.include \"inc/parts.inc\"\n.lib 'lib/models.lib' typ\nQ1 c b e s QL\n.end\n",
		"inc/parts.inc":  "R9 a b 1k\n.include ../more.inc\n",
		"more.inc":       ".model QI PNP\nC9 a 0 1n\n",
		"lib/models.lib": ".lib typ\n.model QL NPN\nR77 x y 1\n.endl\n",
	})

	c, err := Parse(filepath.Join(dir, "main.net"))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := names(c), []string{"R9", "C9", "Q1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("elements %v, want %v (elements of .lib files excluded)", got, want)
	}
	if e := c.Element("R9"); e.File != filepath.Join(dir, "inc/parts.inc") || e.Line != 1 {
		t.Errorf("R9 at %s:%d", e.File, e.Line)
	}
	if c.Models["ql"] == nil || c.Models["qi"] == nil {
		t.Errorf("models %v, want ql (from .lib) and qi (nested include)", c.Models)
	}
	// QL is known from the library, so Q1 has a substrate node
	if e := c.Element("Q1"); len(e.Nodes) != 4 || e.Model != "ql" {
		t.Errorf("Q1 nodes %v model %q", e.Nodes, e.Model)
	}
	want := []string{
		filepath.Join(dir, "inc/parts.inc"),
		filepath.Join(dir, "more.inc"),
		filepath.Join(dir, "lib/models.lib"),
	}
	if !reflect.DeepEqual(c.Includes, want) {
		t.Errorf("includes %v, want %v", c.Includes, want)
	}
}

func TestParseErrors(t *testing.T) {

	dir := writeFiles(t, map[string]string{
		"missing.net": "t\n.include nothere.inc\n",
		"cycle.net":   "t\n.include b.inc\n",
		"b.inc":       ".include b.inc\n",
	})

	for file, want := range map[string]string{
		"missing.net": "missing.net:2:",
		"cycle.net":   "include cycle",
	} {
		_, err := Parse(filepath.Join(dir, file))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want it to contain %q", file, err, want)
		}
	}

	if _, err := ParseReader(strings.NewReader("t\n+ a b\n"), "t.net"); err == nil {
		t.Error("continuation without a preceding line: no error")
	}
}

func TestParseWarnings(t *testing.T) {

	c := parseString(t, `warnings
R1 a b 1k
R1 a c 2k
P1 a b c
R2 a
1X a b
.subckt S a b
R3 a b 1
`)

	want := []string{
		"test.net:3: duplicate element R1 (first at test.net:2)",
		"test.net:4: P1: unknown element type P",
		"test.net:5: R2: 2 nodes expected, found 1",
		"test.net:6: not an element: 1X",
		"test.net:8: missing .ends",
	}
	got := append([]string(nil), c.Warnings...)
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	for i := range c {
		for j := i + 1; j < len(c); j++ {
			if c[j] < c[i] {
				c[i], c[j] = c[j], c[i]
			}
		}
	}
	return c
}

func TestParseKiCad(t *testing.T) {

	c, err := Parse("../testdata/kicad.net")
	if err != nil {
		t.Fatal(err)
	}

	if c.Title != "KiCad schematic" {
		t.Errorf("title %q", c.Title)
	}
	if got, want := names(c), []string{"XU1", "R1", "R2", "C1", "VBT1", "V1", "D1", "R3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("elements %v, want %v", got, want)
	}
	if e := c.Element("XU1"); !reflect.DeepEqual(e.Nodes, []string{"/in", "/fb", "vcc", "0", "/out"}) || e.Model != "opamp" {
		t.Errorf("XU1 nodes %v model %q", e.Nodes, e.Model)
	}
	if s := c.Subckts["opamp"]; s == nil || len(s.Ports) != 5 {
		t.Errorf("subckt opamp: %+v", s)
	}
	if e := c.Element("D1"); !reflect.DeepEqual(e.Nodes, []string{"/out", "net-_d1-a_"}) || e.Model != "1n4148" {
		t.Errorf("D1 nodes %v model %q", e.Nodes, e.Model)
	}
	if m := c.Models["1n4148"]; m == nil || m.Type != "d" {
		t.Errorf("model 1n4148: %+v", m)
	}
	if !reflect.DeepEqual(c.Includes, []string{"../testdata/models/opamp.lib"}) {
		t.Errorf("includes %v", c.Includes)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings: %v", c.Warnings)
	}

	bom := map[string]bool{"U1": true, "R1": true, "R2": true, "C1": true, "BT1": true, "D1": true, "R3": true}
	known := func(ref string) bool { return bom[ref] }
	var refs []string
	for _, e := range c.Elements {
		refs = append(refs, Ref(e.Name, known))
	}
	if want := []string{"U1", "R1", "R2", "C1", "BT1", "V1", "D1", "R3"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("refs %v, want %v", refs, want)
	}
}

func TestRef(t *testing.T) {

	bom := map[string]bool{"U1": true, "R1": true, "RR1": true}
	known := func(ref string) bool { return bom[ref] }

	for name, want := range map[string]string{
		"xu1": "U1", // KiCad prefix removed
		"R1":  "R1", // a reference itself
		"RR1": "RR1",
		"V9":  "V9", // unknown either way
		"X":   "X",
	} {
		if got := Ref(name, known); got != want {
			t.Errorf("Ref(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize(`a b=(1 2) c = 3 {x y} 'q r' Net-(D1-A) d= 4 e =5`)
	want := []string{"a", "b=(1 2)", "c=3", "{x y}", "'q r'", "Net-(D1-A)", "d=4", "e=5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokenize: %q, want %q", got, want)
	}
}
