package main

import (
	"bytes"
	stdcsv "encoding/csv"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/report"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/golib/csv"
)

func TestParseVary(t *testing.T) {
	got, err := parseVary("v1=5%, R1=1  C2=0.5")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]float64{"V1": 5, "R1": 1, "C2": 0.5}) {
		t.Errorf("parseVary: %v", got)
	}
	for _, bad := range []string{"", "V1", "=5%", "V1=0", "V1=100%", "V1=x"} {
		if _, err := parseVary(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// TestDraw: the values of a run depend on the seed and the run only, and
// stay within the tolerance.
func TestDraw(t *testing.T) {
	vs := []report.Variation{{Element: "R1", Nominal: 1e3, Tolerance: 1}, {Element: "V1", Nominal: 12, Tolerance: 10}}
	a, b, other := draw(vs, 1, 5), draw(vs, 1, 5), draw(vs, 1, 6)
	if !reflect.DeepEqual(a, b) || reflect.DeepEqual(a, other) || !reflect.DeepEqual(draw(vs, 1, 5), draw(vs, 1, 5)) {
		t.Errorf("run 5: %v and %v, run 6: %v", a, b, other)
	}
	var sum float64
	n := 2000
	for k := range n {
		v := draw(vs, 7, k)
		if math.Abs(v["R1"]-1e3) > 10 || math.Abs(v["V1"]-12) > 1.2 {
			t.Fatalf("run %d beyond the tolerance: %v", k, v)
		}
		sum += (v["V1"] - 12) / 0.4 // in σ
	}
	if m := sum / float64(n); math.Abs(m) > 0.1 {
		t.Errorf("mean deviation %g σ", m)
	}
}

func TestVariations(t *testing.T) {
	file := filepath.Join(t.TempDir(), "c.net")
	if err := os.WriteFile(file, []byte("t\nV1 a 0 12\nR1 a b 10k\nR2 b 0 {rl}\nC1 b 0 1u\n.end\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := spice.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	comp := func(name, tol string) *parts.Component {
		return &parts.Component{Ref: name, Element: c.Element(name), Class: name[:1],
			Meta: map[string]csv.Field{"tolerance": {Value: tol, Sources: []csv.Source{{File: "db.csv", Name: "T"}}}}}
	}
	comps := []*parts.Component{comp("R1", "1"), comp("R2", "5"), comp("C1", "0")}

	vs, _, err := variations(c, comps, nil, "--vary")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Element != "R1" || vs[0].Tolerance != 1 || vs[0].Nominal != 10e3 || !strings.Contains(vs[0].Source, "db.csv") {
		t.Errorf("from the parts files: %+v", vs)
	}

	vs, _, err = variations(c, comps, map[string]float64{"V1": 5, "R1": 2}, "--vary")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].Element != "R1" || vs[0].Tolerance != 2 || vs[0].Source != "--vary" || vs[1].Element != "V1" || vs[1].Ref != "" {
		t.Errorf("with --vary: %+v", vs)
	}

	for name, vary := range map[string]map[string]float64{
		"not an element": {"V9": 1},
		"no value":       {"R2": 1},
	} {
		if _, _, err := variations(c, comps, vary, "--vary"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestMonteCarlo: the same seed gives the same runs, another seed others;
// RC goes above its derating limit in some runs.
func TestMonteCarlo(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	analysis := func(args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(args, "-o", "-", "../../testdata"), &stdout, &stderr); code != exitOK {
			t.Fatalf("%q: exit status %d:\n%s", args, code, stderr.String())
		}
		return stdout.String(), stderr.String()
	}
	section := func(md string) string {
		_, s, _ := strings.Cut(md, "## Monte Carlo")
		s, _, _ = strings.Cut(s, "## FIDES metadata")
		return s
	}

	md, stderr := analysis("--mc", "12", "--vary", "V1=10%, RC=5%", "--seed", "3")
	for _, want := range []string{
		"- Monte Carlo (section Monte Carlo): 12 more runs of the whole analysis",
		"12 runs with seed 3. In each run",
		"| RC | RC | 2 kΩ | 5 % | --vary |",
		"| V1 |  | 12 V | 10 % | --vary |",
		"**Total FIT:** 74.9 with the nominal values; over the runs ",
		"- **RC**: Monte Carlo: above a derating limit in ",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report without %q", want)
		}
	}
	if !strings.Contains(stderr, "; Monte Carlo: 12 runs, total FIT ") {
		t.Errorf("stderr: %s", stderr)
	}
	again, _ := analysis("--mc", "12", "--vary", "V1=10%, RC=5%", "--seed", "3")
	other, _ := analysis("--mc", "12", "--vary", "V1=10%, RC=5%", "--seed", "4")
	if section(again) != section(md) {
		t.Error("the same seed gives other results")
	}
	if section(other) == section(md) {
		t.Error("another seed gives the same results")
	}

	out, _ := analysis("-f", "csv", "--mc", "4", "--vary", "V1=10%")
	records, err := stdcsv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(records) < 2 {
		t.Fatalf("CSV: %v", err)
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[h] = i
	}
	found := false
	for _, rec := range records[1:] {
		if rec[0] == "RC" {
			found = true
			if rec[col["fit_mc_mean"]] == "" || rec[col["mc_warning_share"]] == "" {
				t.Errorf("RC without Monte Carlo columns: %v", rec)
			}
		}
	}
	if !found {
		t.Error("no RC in the CSV report")
	}
}
