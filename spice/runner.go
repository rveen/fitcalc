package spice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Files in the work directory
const (
	deckFile = "fitcalc.cir"
	logFile  = "ngspice.log"
	rawFile  = "op.raw"
)

// maxReruns bounds the reruns without device vectors that ngspice cannot
// provide.
const maxReruns = 3

// Runner runs ngspice in batch mode.
type Runner struct {
	// Exe is the ngspice executable. Empty means $NGSPICE, then ngspice in
	// PATH.
	Exe string

	// Keep is the directory for the deck, the ngspice log and the raw file.
	// Empty means a temporary directory, removed afterwards.
	Keep string

	// Analysis is the analysis to run instead of the operating point: a DC
	// sweep (ReadSweep reads its raw file) or a transient analysis
	// (ReadTran); nil for the operating point.
	Analysis Analysis

	// Dtemp are the temperatures of elements above the simulation
	// temperature, in K, by element name (DeckOptions.Dtemp).
	Dtemp map[string]float64

	// Values are other values of elements, by element name
	// (DeckOptions.Values).
	Values map[string]float64
}

// plot is the name of the plot in the raw file of a run.
func (r *Runner) plot() string {
	if r.Analysis != nil {
		return r.Analysis.plot()
	}
	return "Operating Point"
}

// OP is the result of an operating point run.
type OP struct {
	Raw         []byte   // the raw file, binary format
	Version     string   // e.g. "ngspice-47"
	Warnings    []string // what the deck changed, and ngspice's warnings
	Unavailable []string // device vectors that ngspice could not provide
	Dir         string   // the directory with deck, log and raw file, if kept
	Temp        float64  // the analysis temperature ngspice reports, in °C; NaN if it does not
}

// OP runs an operating point analysis of c, or the Analysis of r, at the
// temperature of the netlist: ngspice's default, 27 °C, if it sets none.
//
// ngspice runs with -n, so that no .spiceinit file changes its behaviour.
// A device vector that ngspice cannot provide makes it write no raw file at
// all; such vectors are removed and ngspice runs again. Errors caused by
// ngspice wrap ErrSimulation.
func (r *Runner) OP(ctx context.Context, c *Circuit) (*OP, error) {
	return r.op(ctx, c, r.vectors(c), math.NaN())
}

// OPAt is OP at temp °C, whatever temperature the netlist sets.
func (r *Runner) OPAt(ctx context.Context, c *Circuit, temp float64) (*OP, error) {
	return r.op(ctx, c, r.vectors(c), temp)
}

// vectors are the device vectors to save: Vectors, and in a transient
// analysis TranVectors.
func (r *Runner) vectors(c *Circuit) []string {
	if _, ok := r.Analysis.(*Tran); ok {
		return append(Vectors(c), TranVectors(c)...)
	}
	return Vectors(c)
}

func (r *Runner) op(ctx context.Context, c *Circuit, vectors []string, temp float64) (*OP, error) {

	exe, err := r.exe()
	if err != nil {
		return nil, err
	}
	version, err := ngspiceVersion(ctx, exe)
	if err != nil {
		return nil, err
	}

	src, err := os.ReadFile(c.File)
	if err != nil {
		return nil, err
	}

	res := &OP{Version: version}
	dir := r.Keep
	if dir == "" {
		if dir, err = os.MkdirTemp("", "fitcalc-"); err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
	} else {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
		res.Dir = dir
	}
	rawPath := filepath.Join(dir, rawFile)
	logPath := filepath.Join(dir, logFile)

	for run := 0; ; run++ {

		o := DeckOptions{Vectors: vectors, Raw: rawFile, Pins: subcircuits(c), Analysis: r.Analysis,
			Dtemp: map[*Element]float64{}, Values: map[*Element]float64{}}
		for name, dt := range r.Dtemp {
			if e := c.Element(name); e != nil {
				o.Dtemp[e] = dt
			}
		}
		for name, v := range r.Values {
			if e := c.Element(name); e != nil {
				o.Values[e] = v
			}
		}
		if !math.IsNaN(temp) {
			o.Temp = &temp
		}
		deck, notes := Deck(src, c.File, o)
		if err := os.WriteFile(filepath.Join(dir, deckFile), deck, 0644); err != nil {
			return nil, err
		}
		os.Remove(rawPath)

		cmd := exec.CommandContext(ctx, exe, "-b", "-n", deckFile)
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(logPath, out, 0644); err != nil {
			return nil, err
		}
		raw, _ := os.ReadFile(rawPath)
		lg := scanLog(string(out))

		// Vectors ngspice reports as not available, and those it writes
		// with zero length
		missing := slices.DeleteFunc(append(slices.Clone(lg.unavailable), zeroLength(raw)...), func(v string) bool {
			return !slices.Contains(vectors, v)
		})
		if len(missing) > 0 && run < maxReruns {
			res.Unavailable = append(res.Unavailable, missing...)
			vectors = slices.DeleteFunc(vectors, func(v string) bool { return slices.Contains(missing, v) })
			continue
		}

		res.Warnings = append(notes, lg.warnings...)
		if err := failure(runErr, lg, raw, r.plot()); err != nil {
			where := "rerun with -k DIR to keep the ngspice log"
			if r.Keep != "" {
				where = "log: " + logPath
			}
			return res, fmt.Errorf("%w\n  (%s)", err, where)
		}
		res.Raw = raw
		res.Temp = simTemp(string(out))
		return res, nil
	}
}

// subcircuits returns the subcircuit instances of c.
func subcircuits(c *Circuit) []*Element {
	var x []*Element
	for _, e := range c.Elements {
		if e.Kind == "X" {
			x = append(x, e)
		}
	}
	return x
}

// failure returns an error wrapping ErrSimulation if the ngspice run failed:
// if its raw file does not hold the plot it should.
func failure(runErr error, lg ngspiceLog, raw []byte, plot string) error {

	var reasons []string
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		reasons = append(reasons, fmt.Sprintf("ngspice exited with status %d", ee.ExitCode()))
	} else if runErr != nil {
		return fmt.Errorf("running ngspice: %w", runErr)
	}

	if len(raw) == 0 {
		reasons = append(reasons, "no raw file written")
	} else if p := rawHeader(raw)["Plotname"]; p != plot {
		reasons = append(reasons, fmt.Sprintf("the raw file holds %q, not %q", p, plot))
	}
	if len(lg.errors) > 0 && len(reasons) == 0 {
		reasons = append(reasons, "ngspice reported errors")
	}
	if len(reasons) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(strings.Join(reasons, "; "))
	for _, s := range append(first(lg.errors, 5), first(lg.warnings, 5)...) {
		sb.WriteString("\n  " + s)
	}
	return fmt.Errorf("%w: %s", ErrSimulation, sb.String())
}

func first(s []string, n int) []string {
	return s[:min(n, len(s))]
}

// exe returns the path of the ngspice executable.
func (r *Runner) exe() (string, error) {
	name := r.Exe
	if name == "" {
		name = os.Getenv("NGSPICE")
	}
	if name == "" {
		name = "ngspice"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("ngspice executable not found (use --ngspice or $NGSPICE): %w", err)
	}
	return path, nil
}

var versionRE = regexp.MustCompile(`ngspice-\S+`)

// ngspiceVersion returns the version that exe -v reports, e.g. "ngspice-47".
func ngspiceVersion(ctx context.Context, exe string) (string, error) {
	out, err := exec.CommandContext(ctx, exe, "-v").CombinedOutput()
	if v := versionRE.FindString(string(out)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s -v does not report an ngspice version (%v): %.200s", exe, err, out)
}
