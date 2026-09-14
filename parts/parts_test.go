package parts

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoad(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bom.csv": "name, type, tags\nr1, R0603, analog\nC1, NOPE,\n",
		"db.csv":  "name, class, tmax, type, tags\nR0603, R, 155, SMD, thick\nSMD, X, 100, , smd\n",
	})
	p, err := Load([]string{filepath.Join(dir, "bom.csv"), filepath.Join(dir, "db.csv")})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Refs(); !reflect.DeepEqual(got, []string{"C1", "R1"}) {
		t.Errorf("refs %v", got)
	}
	r1 := &Component{Meta: p.Items["R1"]}
	// The nearest type wins: R0603 over its own type SMD
	if r1.Text("class") != "R" || r1.Text("tmax") != "155" {
		t.Errorf("R1 class %q tmax %q, want R, 155", r1.Text("class"), r1.Text("tmax"))
	}
	if got := r1.Source("class"); got != filepath.Join(dir, "db.csv")+" (R0603)" {
		t.Errorf("R1 class source %q", got)
	}
	if r1.Text("tags") != "analog thick smd" {
		t.Errorf("R1 tags %q", r1.Text("tags"))
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "type NOPE is not defined") {
		t.Errorf("warnings %v", p.Warnings)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{"noname.csv": "part, class\nR1, R\n"})
	if _, err := Load([]string{filepath.Join(dir, "missing.csv")}); err == nil {
		t.Error("missing file: no error")
	}
	if _, err := Load([]string{filepath.Join(dir, "noname.csv")}); err == nil || !strings.Contains(err.Error(), "no parts") {
		t.Errorf("BOM without a name or reference column: %v", err)
	}
}

func TestSortRefs(t *testing.T) {
	refs := []string{"R10", "U1", "R2", "C1", "R1A", "R1", "J9"}
	SortRefs(refs)
	if want := []string{"C1", "J9", "R1", "R1A", "R2", "R10", "U1"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("sorted %v, want %v", refs, want)
	}
}

func q(v float64, unit string) stress.Quantity {
	return stress.Quantity{Value: v, Unit: unit, Source: stress.SPICE}
}

func TestMerge(t *testing.T) {

	dir := writeFiles(t, map[string]string{
		"bom.csv": `name, type, class, tags, value, tolerance, v
R1, R0603, , , 10k, 1%,
R2, R0603, , , 4.7k, 1%,
U1, , U, analog, , ,
M1, SOT23Q, , , , ,
J1, , J, , , ,
BT1, , X, , , ,
D1, , , , , , 5
L1, LPWR, , , , ,
J9, TB2, , , , ,
`,
		"db.csv": `name, class, package, dcr, tags
R0603, R, 0603, ,
SOT23Q, Q, SOT23, ,
LPWR, L, , 0.05, power
TB2, J, , , tht
`,
	})
	p, err := Load([]string{filepath.Join(dir, "bom.csv"), filepath.Join(dir, "db.csv")})
	if err != nil {
		t.Fatal(err)
	}

	el := func(name, kind string, value float64, valueText string) *spice.Element {
		return &spice.Element{Name: name, Kind: kind, Value: value, ValueText: valueText}
	}
	c := &spice.Circuit{Elements: []*spice.Element{
		el("R1", "R", 10e3, "10k"),
		el("R2", "R", 5.1e3, "5.1k"),
		el("XU1", "X", math.NaN(), ""),
		el("M1", "M", math.NaN(), ""),
		el("J1", "J", math.NaN(), ""),
		el("VBT1", "V", 12, "12"),
		el("V1", "V", 5, "5"),
		el("D1", "D", math.NaN(), ""),
		el("L1", "L", 10e-6, "10u"),
		el("C1", "C", 1e-7, "100n"),
	}}
	stresses := map[string]stress.Stress{
		"R1":  {"v": q(1, "V"), "i": q(1e-4, "A"), "p": q(1e-4, "W")},
		"R2":  {"v": q(1, "V")},
		"XU1": {"v": q(12, "V")},
		"M1":  {"v": q(5, "V"), "i": q(1e-3, "A")},
		"J1":  {"v": q(5, "V")},
		"D1":  {"vd": q(-3, "V"), "vr": q(3, "V")},
		"L1":  {"v": q(0, "V"), "i": q(0.1, "A")},
		"C1":  {"v": q(5, "V")},
	}

	comps, simOnly, warnings := Merge(c, stresses, p)

	var refs []string
	for _, comp := range comps {
		refs = append(refs, comp.Ref)
	}
	if want := []string{"R1", "R2", "U1", "M1", "J1", "BT1", "D1", "L1", "C1", "J9"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("components %v, want %v", refs, want)
	}
	if len(simOnly) != 1 || simOnly[0].Name != "V1" {
		t.Errorf("simulation-only %v, want V1", simOnly)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %v", warnings)
	}

	by := map[string]*Component{}
	for _, comp := range comps {
		by[comp.Ref] = comp
	}
	hasWarning := func(ref, s string) bool {
		return slices.ContainsFunc(by[ref].Warnings, func(w string) bool { return strings.Contains(w, s) })
	}

	if len(by["R1"].Warnings) != 0 || by["R1"].Class != "R" {
		t.Errorf("R1: class %q, warnings %v", by["R1"].Class, by["R1"].Warnings)
	}
	if !hasWarning("R2", "value 5.1k in the netlist, 4.7k in") {
		t.Errorf("R2 warnings %v", by["R2"].Warnings)
	}
	if u1 := by["U1"]; u1.Element.Name != "XU1" || u1.Class != "U" || !reflect.DeepEqual(u1.Tags, []string{"analog"}) {
		t.Errorf("U1: %+v", u1)
	}
	if m1 := by["M1"]; m1.Class != "Q" || !reflect.DeepEqual(m1.Tags, []string{"mos"}) {
		t.Errorf("M1: class %q tags %v", m1.Class, m1.Tags)
	}
	if !hasWarning("J1", "netlist element J1 is a JFET (class Q)") {
		t.Errorf("J1 warnings %v", by["J1"].Warnings)
	}
	if bt1 := by["BT1"]; bt1.Stress != nil || !hasWarning("BT1", "VBT1 is simulated as a voltage source") {
		t.Errorf("BT1: stress %v, warnings %v", bt1.Stress, bt1.Warnings)
	}
	// A BOM v is the working voltage: the reverse voltage of a diode
	if vr := by["D1"].Stress["vr"]; vr.Value != 5 || vr.Source != stress.Metadata || !hasWarning("D1", "overrides") {
		t.Errorf("D1: vr %v, warnings %v", vr, by["D1"].Warnings)
	}
	if p := by["L1"].Stress["p"]; math.Abs(p.Value-5e-4) > 1e-15 || p.Source != stress.Derived {
		t.Errorf("L1: p %v, want i²·dcr = 5e-4 W", p)
	}
	if !hasWarning("C1", "not in the parts files") {
		t.Errorf("C1 warnings %v", by["C1"].Warnings)
	}
	if j9 := by["J9"]; j9.InNet() || j9.Class != "J" || !hasWarning("J9", "not in the netlist") {
		t.Errorf("J9: %+v", j9)
	}
	// The stresses passed in are not changed
	if _, ok := stresses["D1"]["vr"]; !ok || stresses["D1"]["vr"].Source != stress.SPICE {
		t.Error("Merge changed the stresses passed in")
	}
}

func TestMergeNoParts(t *testing.T) {
	c := &spice.Circuit{Elements: []*spice.Element{
		{Name: "R1", Kind: "R"}, {Name: "X1", Kind: "X"}, {Name: "V1", Kind: "V"},
	}}
	stresses := map[string]stress.Stress{"R1": {"v": q(1, "V")}, "X1": {"v": q(1, "V")}}

	comps, simOnly, warnings := Merge(c, stresses, nil)
	if len(comps) != 2 || len(simOnly) != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "no parts files") {
		t.Fatalf("components %d, simulation-only %d, warnings %v", len(comps), len(simOnly), warnings)
	}
	if comps[0].Class != "R" || len(comps[0].Warnings) != 0 {
		t.Errorf("R1: class %q warnings %v", comps[0].Class, comps[0].Warnings)
	}
	if comps[1].Class != "" || len(comps[1].Warnings) != 1 || !strings.Contains(comps[1].Warnings[0], "no class") {
		t.Errorf("X1: class %q warnings %v", comps[1].Class, comps[1].Warnings)
	}
}

// TestMergeProbe runs ngspice on the probe circuit with its parts files.
func TestMergeProbe(t *testing.T) {
	name := os.Getenv("NGSPICE")
	if name == "" {
		name = "ngspice"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("ngspice not found")
	}

	c, err := spice.Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&spice.Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	values, err := spice.ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	stresses, _ := stress.DeriveAll(c, values)

	p, err := Load([]string{"../testdata/probe.bom.csv", "../testdata/parts.db.csv"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("parts warnings %v", p.Warnings)
	}

	comps, simOnly, warnings := Merge(c, stresses, p)
	if len(comps) != 18 || len(simOnly) != 1 || len(warnings) != 0 {
		t.Fatalf("components %d (want 17 + J9), simulation-only %d, warnings %v", len(comps), len(simOnly), warnings)
	}
	for _, comp := range comps {
		want := 0
		if comp.Ref == "J9" {
			want = 1 // not in the netlist
		}
		if len(comp.Warnings) != want || comp.Class == "" {
			t.Errorf("%s: class %q, warnings %v", comp.Ref, comp.Class, comp.Warnings)
		}
	}
	last := comps[len(comps)-1]
	if last.Ref != "J9" || last.Class != "J" {
		t.Errorf("last component %s class %s, want the connector J9", last.Ref, last.Class)
	}
	for _, comp := range comps {
		switch comp.Ref {
		case "M1":
			if !slices.Contains(comp.Tags, "mos") {
				t.Errorf("M1 tags %v", comp.Tags)
			}
		case "L1":
			i := comp.Stress["i"].Value
			if p := comp.Stress["p"].Value; math.Abs(p-i*i*0.05) > 1e-15 {
				t.Errorf("L1: p = %g, want i²·dcr = %g", p, i*i*0.05)
			}
		}
	}
}
