package main

import (
	"bytes"
	"encoding/csv"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func haveNgspice() bool {
	name := os.Getenv("NGSPICE")
	if name == "" {
		name = "ngspice"
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// TestReports runs fitcalc on the probe circuit and checks both report
// formats.
func TestReports(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}

	dir := t.TempDir()
	args := []string{"-p", "../../testdata/probe.bom.csv", "-p", "../../testdata/parts.db.csv",
		"-m", "../../testdata/mission.csv", "../../testdata/probe.net"}

	md := filepath.Join(dir, "probe.md")
	var stdout, stderr bytes.Buffer
	if code := run(append([]string{"-o", md}, args...), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit status %d:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "fitcalc: 18 components") || !strings.Contains(stderr.String(), "report: "+md) ||
		!strings.Contains(stderr.String(), "; at 32, 60 and 85 °C: 0 overstressed, 1 derating warnings;") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
	b, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Reliability analysis: OP probe circuit",
		"at 27 °C",
		"| `../../testdata/probe.net` | netlist |",
		"- **Components:** 18: 17 in the netlist, 1 only in the parts files; 1 simulation-only element left out",
		"- **Without FIT:** 0",
		"- **Derating:** 0 overstressed, 0 with warnings",
		"- **With the nominal stresses in every phase:** ",
		"- **Derating at 32, 60 and 85 °C:** 0 overstressed, 1 with warnings (RC)",
		"## Temperatures",
		"| full-op | yes | 85 °C | 201 h | 85 °C |",
		// RC is at 70 % of 0.1 W, which is 82 % at 85 °C
		"- **RC**: at 85 °C (full-op): P/Pmax = ",
		"| M1 | Q |",
		"## Mission profile",
		"- `V1` (voltage source): simulation only, not a component.",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("Markdown report without %q", want)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(append([]string{"-f", "csv", "-o", "-"}, args...), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit status %d:\n%s", code, stderr.String())
	}
	records, err := csv.NewReader(&stdout).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 19 || records[0][0] != "ref" {
		t.Fatalf("%d CSV records, first %v; want a header and 18 components", len(records), records[0])
	}
	for _, rec := range records[1:] {
		if rec[14] == "" {
			t.Errorf("%s: no FIT in the CSV report", rec[0])
		}
	}
}

// TestSweep: a DC sweep of the probe circuit's supply, from 10.8 V to 13.2 V.
// RC takes 85 mW at 13.2 V, more than its rating derated for 85 °C.
func TestSweep(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--sweep", "V1 10.8 13.2 0.6", "-o", "-", "-p", "../../testdata/probe.bom.csv", "-p", "../../testdata/parts.db.csv",
		"-m", "../../testdata/mission.csv", "../../testdata/probe.net"}
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit status %d:\n%s", code, stderr.String())
	}
	md := stdout.String()
	for _, want := range []string{
		"- DC sweep (`.dc`), simulated with ",
		"- The DC sweep of V1 from 10.8 V to 13.2 V in steps of 0.6 V (5 points) replaces the single operating point.",
		"## DC sweep",
		"| R1 | 9.82 V … 12 V | 982 µA … 1.2 mA |",
		"- **RC**: at 85 °C (full-op): P/Pmax = ",
		"with V1 = 13.2 V: overstress",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report without %q", want)
		}
	}
}

// TestTran: a transient analysis of the probe circuit, which does not change,
// gives the FIT of its operating point; with a 1 kHz ripple of 1 V on the
// supply, the report has the peaks, and RC's power is its mean.
func TestTran(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	report := func(netlist string, extra ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := append(extra, "-o", "-", "-p", "../../testdata/probe.bom.csv", "-p", "../../testdata/parts.db.csv",
			"-m", "../../testdata/mission.csv", netlist)
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("%s: exit status %d:\n%s", extra, code, stderr.String())
		}
		return stdout.String()
	}
	fits := func(out string) map[string]float64 {
		t.Helper()
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		f := map[string]float64{}
		for _, rec := range records[1:] {
			x, err := strconv.ParseFloat(rec[14], 64)
			if err != nil {
				t.Fatalf("%s: FIT %q", rec[0], rec[14])
			}
			f[rec[0]] = x
		}
		return f
	}

	op := fits(report("../../testdata/probe.net", "-f", "csv"))
	tran := fits(report("../../testdata/probe.net", "-f", "csv", "--tran", "10u 1m"))
	for ref, want := range op {
		if got := tran[ref]; math.Abs(got-want) > 1e-6*want {
			t.Errorf("%s: FIT %g with the transient, %g with the operating point", ref, got, want)
		}
	}

	src, err := os.ReadFile("../../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	netlist := filepath.Join(t.TempDir(), "ripple.net")
	ripple := strings.Replace(string(src), "V1 vcc 0 12\n", "V1 vcc 0 SIN(12 1 1k)\n", 1)
	if err := os.WriteFile(netlist, []byte(ripple), 0644); err != nil {
		t.Fatal(err)
	}
	md := report(netlist, "--tran", "10u 3m 1m")
	for _, want := range []string{
		"- Transient analysis (`.tran`), simulated with ",
		"- The transient analysis from 0 to 3 ms in steps of 10 µs, recorded from 1 ms (201 time points) replaces the single operating point.",
		"## Transient",
		"| R1 | 11.8 V at ",
		" ms | 10.9 V | 1.18 mA at ",
		"| C1 | 1.17 V at ",
		"- **RC**: at 85 °C (full-op): P/Pmax = 86% (0.0705 W of 0.08235 W) with the mean power: warning (limit 80 %)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report without %q", want)
		}
	}
}

// TestThermal: the probe circuit on a board 25 K/W above the ambient. X1 is
// 10.5 K above the ambient, 6 K of them from the others: 54.1 FIT instead of
// 38.7. A node that looks like a reference but is no component is a warning.
func TestThermal(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	report := func(network string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := []string{"--thermal", network, "-o", "-", "-p", "../../testdata/probe.bom.csv", "-p", "../../testdata/parts.db.csv",
			"-m", "../../testdata/mission.csv", "../../testdata/probe.net"}
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("exit status %d:\n%s", code, stderr.String())
		}
		return stdout.String(), stderr.String()
	}

	md, stderr := report("../../testdata/thermal.csv")
	for _, want := range []string{
		"- Thermal network `../../testdata/thermal.csv` (section Thermal network): ",
		"## Thermal network",
		"`../../testdata/thermal.csv`: 9 thermal resistances between 9 nodes and the ambient (amb). Simulations until the temperatures agree with the power: 3 at 27 °C, 3 at 32 °C, 3 at 60 °C and 3 at 85 °C.",
		"| BOARD | AMB | 25 K/W | 12 |",
		"| X1 | U | 36 mW | 4.5 K | 5.96 K | 37.46 °C |",
		"| X1 | U |  | 6 Vᴰ | 3 mAˢ | 36 mWᴰ | 4.5 °Cᴰ | 54.1 |",
		"- **RC**: at 85 °C (full-op): P/Pmax = 92% (0.07022 W of 0.07639 W): warning (limit 80 %)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report without %q", want)
		}
	}
	if !strings.Contains(stderr, "94.7 FIT in total") {
		t.Errorf("stderr: %s", stderr)
	}

	src, err := os.ReadFile("../../testdata/thermal.csv")
	if err != nil {
		t.Fatal(err)
	}
	network := filepath.Join(t.TempDir(), "thermal.csv")
	if err := os.WriteFile(network, append(src, "Q9, board, 10\n"...), 0644); err != nil {
		t.Fatal(err)
	}
	if _, stderr := report(network); !strings.Contains(stderr, "node Q9 is not a component: it has no power") {
		t.Errorf("no warning about Q9: %s", stderr)
	}
}

// TestKiCadBOM: the KiCad BOM of the probe circuit, whose parts get their
// types from part numbers and footprints, gives the FIT of the fides BOM.
func TestKiCadBOM(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}

	report := func(format, bom string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := []string{"-f", format, "-o", "-", "-p", bom, "-p", "../../testdata/parts.db.csv",
			"-m", "../../testdata/mission.csv", "../../testdata/probe.net"}
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("%s: exit status %d:\n%s", bom, code, stderr.String())
		}
		return stdout.String(), stderr.String()
	}
	fits := func(bom string) (map[string]string, string) {
		out, stderr := report("csv", bom)
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, rec := range records[1:] {
			m[rec[0]] = rec[14]
		}
		return m, stderr
	}

	want, _ := fits("../../testdata/probe.bom.csv")
	got, stderr := fits("../../testdata/probe.kicad.csv")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FIT with the KiCad BOM\n%v\nwith the fides BOM\n%v", got, want)
	}
	for _, bad := range []string{"part numbers without a type", "no class", "not in the parts files"} {
		if strings.Contains(stderr, bad) {
			t.Errorf("stderr:\n%s", stderr)
		}
	}

	md, _ := report("markdown", "../../testdata/probe.kicad.csv")
	for _, want := range []string{
		"| `../../testdata/probe.kicad.csv` | parts (KiCad BOM) |",
		"; 3 not mounted (DNP)",
		"- Not mounted (DNP in the BOM): R4, R5, R6.",
		"| D1 | D |  | SOD123 | Diodes Inc. 1N4148W |",
		"type D1N4148 from the part number 1N4148W",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown report without %q", want)
		}
	}
}

func TestParseArgs(t *testing.T) {

	for _, tc := range []struct {
		name string
		args []string
		want config
	}{
		{
			name: "defaults",
			args: []string{"circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md"},
		},
		{
			name: "csv default output next to the netlist",
			args: []string{"-f", "csv", "dir/circuit.cir"},
			want: config{netlist: "dir/circuit.cir", format: "csv", output: "dir/circuit.csv"},
		},
		{
			name: "short and long forms, repeated parts, flags after the netlist",
			args: []string{"-p", "bom.csv", "circuit.net", "--parts", "db.csv", "--mission", "m.csv",
				"-o", "-", "--ngspice", "/usr/bin/ngspice", "-k", "keep", "-v"},
			want: config{netlist: "circuit.net", parts: []string{"bom.csv", "db.csv"}, mission: "m.csv",
				output: "-", format: "markdown", ngspice: "/usr/bin/ngspice", keep: "keep", verbose: true},
		},
		{
			name: "DC sweep",
			args: []string{"--sweep", "V1 10 14 1", "circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", sweep: "V1 10 14 1"},
		},
		{
			name: "transient",
			args: []string{"--tran", "10u 5m 1m", "circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", tran: "10u 5m 1m"},
		},
		{
			name: "Monte Carlo",
			args: []string{"--mc", "10", "--vary", "V1=5%", "--seed", "3", "circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", mc: 10, vary: "V1=5%", seed: 3},
		},
		{
			name: "thermal network",
			args: []string{"--thermal", "thermal.csv", "circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", thermal: "thermal.csv"},
		},
		{
			name: "derating rules",
			args: []string{"-d", "rules.csv", "circuit.net", "--derating", "other.csv"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", derating: "other.csv"},
		},
		{
			name: "additional derating temperature",
			args: []string{"--hot", "70", "circuit.net"},
			want: config{netlist: "circuit.net", format: "markdown", output: "circuit.md", hot: "70"},
		},
		{
			name: "version needs no netlist",
			args: []string{"--version"},
			want: config{format: "markdown", showVersion: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", *got, tc.want)
			}
		})
	}
}

func TestParseArgsErrors(t *testing.T) {

	for _, args := range [][]string{
		{},
		{"a.net", "b.net"},
		{"-f", "html", "a.net"},
		{"--unknown", "a.net"},
		{"a.net", "-p"},
		{"--hot", "warm", "a.net"},
		{"--sweep", "V1 1 2", "a.net"},
		{"--sweep", "R1 0 1 0.1", "a.net"},
		{"--tran", "5m", "a.net"},
		{"--vary", "V1=5%", "a.net"},
		{"--seed", "3", "a.net"},
		{"--mc", "-1", "a.net"},
		{"--mc", "20000", "a.net"},
		{"--mc", "5", "--vary", "V1", "a.net"},
		{"--tran", "10u 5m", "--sweep", "V1 0 1 0.1", "a.net"},
	} {
		if _, err := parseArgs(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parseArgs(%q): no error", args)
		}
	}
}

func TestRun(t *testing.T) {

	dir := t.TempDir()
	netlist := filepath.Join(dir, "circuit.net")
	if err := os.WriteFile(netlist, []byte("title\nV1 1 0 5\nR1 1 0 1k\n.end\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The whole pipeline needs ngspice
	pipeline := exitOK
	if !haveNgspice() {
		pipeline = exitError
	}

	for _, tc := range []struct {
		name   string
		args   []string
		code   int
		stdout string // expected prefix
		stderr string // expected substring
	}{
		{"help", []string{"-h"}, exitOK, "", "Usage: fitcalc"},
		{"version", []string{"--version"}, exitOK, "fitcalc ", ""},
		{"no netlist", []string{}, exitError, "", "no netlist given"},
		{"missing netlist", []string{filepath.Join(dir, "missing.net")}, exitError, "", "missing.net"},
		{"missing parts file", []string{"-p", filepath.Join(dir, "bom.csv"), netlist}, exitError, "", "bom.csv"},
		{"report overwrites input", []string{"-o", netlist, netlist}, exitError, "", "would overwrite"},
		{"no mission warns", []string{"-o", "-", netlist}, pipeline, "", "no mission profile"},
		{"sweep of a missing source", []string{"--sweep", "V9 0 1 0.1", "-o", "-", netlist}, exitError, "", "V9 is not a voltage or current source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.code {
				t.Errorf("exit status %d, want %d; stderr:\n%s", code, tc.code, stderr.String())
			}
			if !strings.HasPrefix(stdout.String(), tc.stdout) {
				t.Errorf("stdout %q, want prefix %q", stdout.String(), tc.stdout)
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr %q, want it to contain %q", stderr.String(), tc.stderr)
			}
		})
	}
}
