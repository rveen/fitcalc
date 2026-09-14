package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeProject(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const sampleProject = `netlist  = "c.net"
parts    = ["bom.csv", "db.csv"]
mission  = "m.csv"
thermal  = "/abs/thermal.csv"
sweep    = "V1 0 1 0.1"
hot      = 70.5
format   = "csv"
`

func TestLoadProject(t *testing.T) {
	dir := writeProject(t, sampleProject)
	p, err := loadProject(filepath.Join(dir, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	if p.Netlist != "c.net" || len(p.Parts) != 2 || p.Hot == nil || *p.Hot != 70.5 || p.Format != "csv" {
		t.Errorf("project %+v", p)
	}

	for name, text := range map[string]string{
		"unknown key": "netlist = \"c.net\"\nmision = \"m.csv\"\n",
		"no netlist":  "mission = \"m.csv\"\n",
		"bad TOML":    "netlist = c.net\n",
		"wrong type":  "netlist = \"c.net\"\nparts = \"bom.csv\"\n",
	} {
		dir := writeProject(t, text)
		_, err := loadProject(filepath.Join(dir, projectFile))
		if err == nil {
			t.Errorf("%s: no error", name)
		} else if name == "unknown key" && !strings.Contains(err.Error(), "unknown keys: mision") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestParseArgsProject: a project directory, its file, the current
// directory, and flags that override the project.
func TestParseArgsProject(t *testing.T) {
	dir := writeProject(t, sampleProject)
	file := filepath.Join(dir, projectFile)
	in := func(f string) string { return filepath.Join(dir, f) }

	want := config{
		netlist: in("c.net"), parts: []string{in("bom.csv"), in("db.csv")}, mission: in("m.csv"),
		thermal: "/abs/thermal.csv", sweep: "V1 0 1 0.1", hot: "70.5", format: "csv", output: in("c.csv"),
		project:     file,
		fromProject: map[string]bool{"mission": true, "thermal": true, "sweep": true, "hot": true, "format": true},
	}
	for _, args := range [][]string{{dir}, {file}} {
		got, err := parseArgs(args, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*got, want) {
			t.Errorf("%q:\ngot  %+v\nwant %+v", args, *got, want)
		}
	}

	// The command line wins; its paths are not the project's
	got, err := parseArgs([]string{"-f", "markdown", "-p", "x.csv", "--hot", "60", dir, "-o", "-"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got.format != "markdown" || !reflect.DeepEqual(got.parts, []string{"x.csv"}) || got.hot != "60" || got.output != "-" || got.mission != in("m.csv") {
		t.Errorf("with flags: %+v", *got)
	}

	// Without arguments, the project of the current directory
	t.Chdir(dir)
	got, err = parseArgs(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got.project != projectFile || got.netlist != "c.net" || got.output != "c.csv" {
		t.Errorf("in the project directory: %+v", *got)
	}
}

func TestParseArgsProjectErrors(t *testing.T) {
	for name, tc := range map[string]struct{ project, want string }{
		"bad tran":         {"netlist = \"c.net\"\ntran = \"5m\"\n", "bad tran in "},
		"sweep and tran":   {"netlist = \"c.net\"\nsweep = \"V1 0 1 0.1\"\ntran = \"1u 1m\"\n", "sweep in "},
		"bad format":       {"netlist = \"c.net\"\nformat = \"html\"\n", "format in "},
		"unknown key":      {"netlist = \"c.net\"\nparts_db = \"x\"\n", "unknown keys: parts_db"},
		"directory only":   {"", "without fitcalc.toml"},
		"missing toml arg": {"-", "no such file"},
	} {
		var args []string
		switch tc.project {
		case "":
			args = []string{t.TempDir()}
		case "-":
			args = []string{filepath.Join(t.TempDir(), "p.toml")}
		default:
			args = []string{writeProject(t, tc.project)}
		}
		_, err := parseArgs(args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// TestProject: the probe's project gives its report, with the project file
// among the inputs.
func TestProject(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-o", "-", "../../testdata"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit status %d:\n%s", code, stderr.String())
	}
	md := stdout.String()
	for _, want := range []string{
		"- **Project:** `../../testdata/fitcalc.toml`\n- **Netlist:** `../../testdata/probe.net`\n",
		"| `../../testdata/fitcalc.toml` | project | `",
		"| `../../testdata/probe.bom.csv` | parts (BOM) | `",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report without %q", want)
		}
	}
	if !strings.Contains(stderr.String(), "74.9 FIT in total") {
		t.Errorf("stderr: %s", stderr.String())
	}
}
