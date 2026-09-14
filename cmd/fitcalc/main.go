// Command fitcalc computes FIDES 2022 failure rates (FIT) for the components
// of a SPICE circuit, using ngspice for the electrical stresses.
//
// Usage:
//
//	fitcalc [flags] circuit.net
//	fitcalc [flags] [DIR | project.toml]
//
// README.md describes the usage and the input and report formats.
package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/internal/version"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/report"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/thermal"
)

// Exit status
const (
	exitOK         = 0 // success, warnings allowed
	exitError      = 1 // usage or input error
	exitSimulation = 2 // ngspice failed
)

type config struct {
	netlist     string
	parts       []string // the first one is the BOM
	mission     string
	output      string // "-" is stdout
	format      string // "markdown" or "csv"
	ngspice     string
	keep        string
	hot         string          // an additional temperature for the derating check, °C; "": none
	derating    string          // derating rules CSV; "": the default rule
	sweep       string          // DC sweep, "SOURCE START STOP STEP"; "": the operating point
	tran        string          // transient analysis, "TSTEP TSTOP [TSTART [TMAX]]"; "": the operating point
	thermal     string          // thermal network CSV; "": none
	mc          int             // Monte Carlo runs; 0: none
	seed        uint64          // of the Monte Carlo draws
	vary        string          // element tolerances besides those of the parts files: "V1=5%, R1=1%"
	project     string          // the project file the settings come from; "": none
	fromProject map[string]bool // the settings taken from the project file, by long flag name
	verbose     bool
	showVersion bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {

	cfg, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		// flag has already reported its own parse errors
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(stderr, "fitcalc: %v\nRun 'fitcalc -h' for usage.\n", err)
		}
		return exitError
	}
	if cfg.showVersion {
		fmt.Fprintln(stdout, version.String())
		return exitOK
	}

	if err := checkInputs(cfg); err != nil {
		fmt.Fprintf(stderr, "fitcalc: %v\n", err)
		return exitError
	}

	log := newLogger(stderr, cfg.verbose)
	log.Debug("configuration", "netlist", cfg.netlist, "parts", cfg.parts, "mission", cfg.mission,
		"output", cfg.output, "format", cfg.format, "ngspice", cfg.ngspice, "keep", cfg.keep)
	if cfg.mission == "" {
		log.Warn("no mission profile (-m): stresses and derating only, no FIT")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	rep, err := analyze(ctx, cfg, log)
	if err == nil {
		err = write(cfg, rep, stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "fitcalc: %v\n", err)
		if errors.Is(err, spice.ErrSimulation) {
			return exitSimulation
		}
		return exitError
	}

	fmt.Fprintf(stderr, "fitcalc: %s\n", summary(cfg, rep))
	return exitOK
}

// analyze runs the pipeline: netlist → ngspice .OP (or DC sweep, or
// transient) → stresses → parts → FIDES → derating.
func analyze(ctx context.Context, cfg *config, log *slog.Logger) (*report.Report, error) {

	rep := &report.Report{
		Netlist: cfg.netlist,
		Date:    time.Now().UTC(),
		Fitcalc: version.Fitcalc(),
		Fides:   version.Fides(),
		Temp:    math.NaN(),
	}
	warn := func(w string) {
		rep.Warnings = append(rep.Warnings, w)
		log.Warn(w)
	}
	input := func(role, path string) error {
		in, err := report.NewInput(role, path)
		rep.Inputs = append(rep.Inputs, in)
		return err
	}

	if cfg.project != "" {
		rep.Project = cfg.project
		if err := input("project", cfg.project); err != nil {
			return nil, err
		}
	}

	c, err := spice.Parse(cfg.netlist)
	if err != nil {
		return nil, err
	}
	rep.Title = c.Title
	for _, w := range c.Warnings {
		warn(w)
	}
	log.Debug("netlist", "title", c.Title, "elements", len(c.Elements), "models", len(c.Models),
		"subcircuits", len(c.Subckts), "includes", c.Includes)

	// The other inputs are read before ngspice runs, so that errors in them
	// show at once
	if err := input("netlist", cfg.netlist); err != nil {
		return nil, err
	}
	for _, f := range c.Includes {
		if err := input("include", f); err != nil {
			return nil, err
		}
	}
	var p *parts.Parts
	if len(cfg.parts) > 0 {
		for i, f := range cfg.parts {
			role := "parts"
			if i == 0 {
				role = "parts (BOM)"
			}
			if err := input(role, f); err != nil {
				return nil, err
			}
		}
		if p, err = parts.Load(cfg.parts); err != nil {
			return nil, err
		}
		for _, w := range p.Warnings {
			warn(w)
		}
		rep.DNP = p.DNP
		if p.Format == parts.FormatKiCad {
			for i := range rep.Inputs {
				if rep.Inputs[i].Role == "parts (BOM)" {
					rep.Inputs[i].Role = "parts (KiCad BOM)"
				}
			}
		}
	}
	if cfg.mission != "" {
		if err := input("mission", cfg.mission); err != nil {
			return nil, err
		}
		if rep.Mission, err = rel.LoadMission(cfg.mission); err != nil {
			return nil, err
		}
	}
	if cfg.derating != "" {
		if err := input("derating rules", cfg.derating); err != nil {
			return nil, err
		}
		if rep.Rules, err = derating.LoadRules(cfg.derating); err != nil {
			return nil, err
		}
	}
	if cfg.thermal != "" {
		if err := input("thermal network", cfg.thermal); err != nil {
			return nil, err
		}
		if rep.Thermal, err = thermal.Load(cfg.thermal); err != nil {
			return nil, err
		}
	}

	var an spice.Analysis // nil: the operating point
	switch {
	case cfg.sweep != "":
		if rep.Sweep, err = sweepOf(c, cfg.sweep); err != nil {
			return nil, err
		}
		an = rep.Sweep
	case cfg.tran != "":
		if rep.Tran, err = spice.ParseTran(cfg.tran); err != nil {
			return nil, err
		}
		an = rep.Tran
	}

	nom, nomThermal, err := simulateThermal(ctx, cfg, c, an, math.NaN(), cfg.keep, rep.Thermal, p, nil)
	if err != nil {
		return nil, err
	}
	op := nom.op
	rep.Ngspice, rep.Temp = op.Version, op.Temp
	for _, w := range nom.warnings {
		warn(w)
	}
	if len(op.Unavailable) > 0 {
		warn("device vectors not available from ngspice: " + strings.Join(op.Unavailable, " "))
	}
	log.Debug("ngspice", "version", op.Version, "raw bytes", len(op.Raw), "kept in", op.Dir)

	comps, simOnly, warnings := parts.Merge(c, nom.stress, p)
	for _, w := range warnings {
		warn(w)
	}
	rep.SimOnly = simOnly
	nominal := nom.components(c, p, comps)
	nominal.comps = comps
	rep.SweepPoints, rep.TranPoints = nom.labels, nom.times

	// The temperatures in the thermal network
	addThermal := func(st *stressed, run *report.ThermalRun, temp float64, name string) {
		if run == nil {
			return
		}
		st.heat(rep.Thermal, run.Solution)
		run.Temp, run.Name = temp, name
		rep.ThermalRuns = append(rep.ThermalRuns, *run)
		if !run.Converged {
			warn(fmt.Sprintf("thermal network at %s: the temperatures still change by %.3g K after %d simulations: possible thermal runaway",
				name, run.Change, run.Simulations))
		}
	}
	if rep.Thermal != nil {
		for _, w := range thermalNodes(rep.Thermal, comps) {
			warn(w)
		}
		name := "the nominal temperature"
		if !math.IsNaN(rep.Temp) {
			name = fmt.Sprintf("%g °C (nominal)", rep.Temp)
		}
		addThermal(nominal, nomThermal, rep.Temp, name)
	}

	// The simulations at the temperatures of the operating mission phases,
	// and at --hot
	var atCorner []*stressed // by rep.Corners
	for _, k := range corners(cfg, rep.Mission) {
		if k.Temp == rep.Temp {
			rep.Corners = append(rep.Corners, k)
			atCorner = append(atCorner, nominal)
			continue
		}
		keep := cfg.keep
		if keep != "" {
			keep = filepath.Join(keep, fmt.Sprintf("%gC", k.Temp))
		}
		s, run, err := simulateThermal(ctx, cfg, c, an, k.Temp, keep, rep.Thermal, p, nil)
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			warn(fmt.Sprintf("simulation at %s not done, the nominal stresses apply: %v", k.Name(), err))
			continue
		}
		// Only what the nominal run has not reported already
		for _, w := range s.warnings {
			if !slices.Contains(rep.Warnings, w) {
				warn(fmt.Sprintf("at %g °C: %s", k.Temp, w))
			}
		}
		st := s.components(c, p, comps)
		addThermal(st, run, k.Temp, k.Name())
		rep.Corners = append(rep.Corners, k)
		atCorner = append(atCorner, st)
	}

	// The FIT, with the stresses of each operating phase at its temperature
	var results, nominalFIT []*rel.Result
	if m := rep.Mission; m != nil {
		results = rel.FIT(comps, m)
		if phases, ok := phaseComponents(m, rep.Corners, atCorner); ok {
			nominalFIT, results = results, rel.FITPhases(comps, phases, m)
		}
	}

	for i, comp := range comps {
		row := report.Row{Component: comp, Derating: nominal.check(rep.Rules, i, math.NaN())}
		for k := range nominal.points {
			row.Points = append(row.Points, nominal.points[k][i])
		}
		if nominal.stats != nil {
			row.Stats = nominal.stats[i]
		}
		if results != nil {
			row.FIT = results[i]
		}
		if nominalFIT != nil {
			row.NominalFIT = nominalFIT[i]
		}
		for k, corner := range rep.Corners {
			a := atCorner[k]
			row.Corners = append(row.Corners, report.CornerResult{
				Corner: corner, Component: a.comps[i], Derating: a.check(rep.Rules, i, corner.Temp)})
		}
		rep.Rows = append(rep.Rows, row)
	}

	if cfg.mc > 0 {
		if err := monteCarlo(ctx, cfg, c, p, an, comps, rep, warn); err != nil {
			return nil, err
		}
	}

	for _, row := range rep.Rows {
		comp := row.Component
		for _, w := range report.Warnings(row) {
			log.Warn(comp.Ref + ": " + w)
		}
		args := []any{"ref", comp.Ref, "class", comp.Class, "tags", comp.Tags}
		for _, n := range comp.Stress.Names() {
			q := comp.Stress[n]
			args = append(args, n, fmt.Sprintf("%.4g %s [%s]", q.Value, q.Unit, q.Source.Mark()))
		}
		if row.FIT != nil {
			args = append(args, "fit", row.FIT.FIT)
		}
		if row.NominalFIT != nil {
			args = append(args, "fit_nominal", row.NominalFIT.FIT)
		}
		log.Debug("component", args...)

		for _, k := range row.Corners {
			args := []any{"ref", comp.Ref, "temp", k.Corner.Temp}
			for _, n := range k.Component.Stress.Names() {
				q := k.Component.Stress[n]
				args = append(args, n, fmt.Sprintf("%.4g %s [%s]", q.Value, q.Unit, q.Source.Mark()))
			}
			log.Debug("component at temperature", args...)
		}
	}

	return rep, nil
}

// refLike matches a component reference: R1, XU12.
var refLike = regexp.MustCompile(`^[A-Z]+[0-9]+[A-Z]?$`)

// thermalNodes returns warnings about the nodes of the thermal network that
// look like references but are not components: they are passive nodes.
func thermalNodes(net *thermal.Network, comps []*parts.Component) []string {
	refs := map[string]bool{}
	for _, x := range comps {
		refs[strings.ToUpper(x.Ref)] = true
	}
	var w []string
	for _, n := range net.Nodes {
		if refLike.MatchString(n) && !refs[n] {
			w = append(w, fmt.Sprintf("%s: node %s is not a component: it has no power", net.File, n))
		}
	}
	return w
}

// corners returns the ambient temperatures to simulate besides the nominal
// one, in increasing order: those of the operating mission phases, and the
// one set with --hot.
func corners(cfg *config, m *rel.Mission) []report.Corner {
	var ks []report.Corner
	at := func(temp float64) int {
		for i := range ks {
			if ks[i].Temp == temp {
				return i
			}
		}
		ks = append(ks, report.Corner{Temp: temp})
		return len(ks) - 1
	}
	if m != nil {
		for _, ph := range m.Phases {
			if ph.On {
				i := at(ph.Tamb)
				ks[i].Phases = append(ks[i].Phases, ph.Name)
			}
		}
	}
	if temp, err := strconv.ParseFloat(cfg.hot, 64); err == nil {
		ks[at(temp)].Hot = true
	}
	slices.SortFunc(ks, func(a, b report.Corner) int { return cmp.Compare(a.Temp, b.Temp) })
	return ks
}

// write writes the report to the output file, or to stdout.
func write(cfg *config, rep *report.Report, stdout io.Writer) error {

	var b bytes.Buffer
	var err error
	if cfg.format == "csv" {
		err = report.CSV(&b, rep)
	} else {
		err = report.Markdown(&b, rep)
	}
	if err != nil {
		return err
	}

	if cfg.output == "-" {
		_, err = stdout.Write(b.Bytes())
		return err
	}
	return os.WriteFile(cfg.output, b.Bytes(), 0644)
}

// summary is the line fitcalc prints when it is done.
func summary(cfg *config, rep *report.Report) string {
	s := fmt.Sprintf("%d components", len(rep.Rows))
	if rep.Mission != nil {
		total, missing := rep.Total()
		s += fmt.Sprintf(", %.3g FIT in total", total)
		if missing > 0 {
			s += fmt.Sprintf(" (%d without FIT)", missing)
		}
	}
	if over, warn := rep.DeratingCounts(); over > 0 || warn > 0 {
		s += fmt.Sprintf(", %d overstressed, %d derating warnings", over, warn)
	}
	if len(rep.Corners) > 0 {
		over, warn := rep.CornerDeratingCounts()
		s += fmt.Sprintf("; at %s: %d overstressed, %d derating warnings", report.Temps(rep.Corners), over, warn)
	}
	if mc := rep.MonteCarlo; mc != nil {
		s += fmt.Sprintf("; Monte Carlo: %d runs", mc.Runs)
		if lo, hi, ok := mc.TotalRange(); ok {
			s += fmt.Sprintf(", total FIT %.3g to %.3g (5 %% to 95 %%)", lo, hi)
		}
	}
	where := cfg.output
	if where == "-" {
		where = "stdout"
	}
	return s + "; report: " + where
}

// usageError is a command line error that is not a flag parse error.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usageErrorf(format string, a ...any) error {
	return usageError{fmt.Sprintf(format, a...)}
}

// fileList is a repeatable string flag.
type fileList []string

func (l *fileList) String() string { return strings.Join(*l, ",") }

func (l *fileList) Set(s string) error {
	*l = append(*l, s)
	return nil
}

// longFlags are the long names of the short flags.
var longFlags = map[string]string{
	"p": "parts", "m": "mission", "o": "output", "f": "format", "k": "keep", "d": "derating", "v": "verbose",
}

func parseArgs(args []string, stderr io.Writer) (*config, error) {

	cfg := &config{format: "markdown"}

	fs := flag.NewFlagSet("fitcalc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(fs.Output()) }

	// Short and long forms share the variable
	for _, name := range []string{"p", "parts"} {
		fs.Var((*fileList)(&cfg.parts), name, "")
	}
	for _, name := range []string{"m", "mission"} {
		fs.StringVar(&cfg.mission, name, "", "")
	}
	for _, name := range []string{"o", "output"} {
		fs.StringVar(&cfg.output, name, "", "")
	}
	for _, name := range []string{"f", "format"} {
		fs.StringVar(&cfg.format, name, cfg.format, "")
	}
	for _, name := range []string{"k", "keep"} {
		fs.StringVar(&cfg.keep, name, "", "")
	}
	for _, name := range []string{"d", "derating"} {
		fs.StringVar(&cfg.derating, name, "", "")
	}
	for _, name := range []string{"v", "verbose"} {
		fs.BoolVar(&cfg.verbose, name, false, "")
	}
	fs.StringVar(&cfg.ngspice, "ngspice", "", "")
	fs.StringVar(&cfg.hot, "hot", "", "")
	fs.StringVar(&cfg.sweep, "sweep", "", "")
	fs.StringVar(&cfg.tran, "tran", "", "")
	fs.StringVar(&cfg.thermal, "thermal", "", "")
	fs.IntVar(&cfg.mc, "mc", 0, "")
	fs.Uint64Var(&cfg.seed, "seed", 0, "")
	fs.StringVar(&cfg.vary, "vary", "", "")
	fs.BoolVar(&cfg.showVersion, "version", false, "")

	// Flags may also follow the netlist: parse again after each positional
	// argument.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}

	if cfg.showVersion {
		return cfg, nil
	}

	if len(positional) > 1 {
		return nil, usageErrorf("one netlist or project expected, got %d: %s", len(positional), strings.Join(positional, " "))
	}
	file, err := projectOf(positional)
	switch {
	case err != nil:
		return nil, usageErrorf("%v", err)
	case file != "":
		p, err := loadProject(file)
		if err != nil {
			return nil, err
		}
		// The flags the command line sets, by their long names
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) {
			name := f.Name
			if long, ok := longFlags[name]; ok {
				name = long
			}
			set[name] = true
		})
		p.apply(cfg, file, set)
	case len(positional) == 0:
		return nil, usageErrorf("no netlist given, and no %s in the current directory", projectFile)
	default:
		cfg.netlist = positional[0]
	}

	switch cfg.format {
	case "markdown", "csv":
	default:
		return nil, usageErrorf("unknown format %q in %s: use markdown or csv", cfg.format, cfg.origin("format"))
	}

	if cfg.hot != "" {
		if _, err := strconv.ParseFloat(cfg.hot, 64); err != nil {
			return nil, usageErrorf("bad %s %q: a temperature in °C", cfg.origin("hot"), cfg.hot)
		}
	}
	if cfg.sweep != "" {
		if _, err := spice.ParseSweep(cfg.sweep); err != nil {
			return nil, usageErrorf("bad %s: %v", cfg.origin("sweep"), err)
		}
	}
	if cfg.tran != "" {
		if cfg.sweep != "" {
			return nil, usageErrorf("%s and %s exclude each other", cfg.origin("sweep"), cfg.origin("tran"))
		}
		if _, err := spice.ParseTran(cfg.tran); err != nil {
			return nil, usageErrorf("bad %s: %v", cfg.origin("tran"), err)
		}
	}
	if cfg.mc < 0 || cfg.mc > maxRuns {
		return nil, usageErrorf("bad %s %d: from 1 to %d runs", cfg.origin("mc"), cfg.mc, maxRuns)
	}
	if cfg.vary != "" {
		if _, err := parseVary(cfg.vary); err != nil {
			return nil, usageErrorf("bad %s: %v", cfg.origin("vary"), err)
		}
	}
	if cfg.mc == 0 && (cfg.vary != "" || cfg.seed != 0) {
		return nil, usageErrorf("%s and %s need %s", cfg.origin("vary"), cfg.origin("seed"), cfg.origin("mc"))
	}

	if cfg.output == "" {
		cfg.output = defaultOutput(cfg.netlist, cfg.format)
	}

	return cfg, nil
}

// defaultOutput is the netlist path with the extension of the report format:
// dir/circuit.net gives dir/circuit.md.
func defaultOutput(netlist, format string) string {
	ext := ".md"
	if format == "csv" {
		ext = ".csv"
	}
	return strings.TrimSuffix(netlist, filepath.Ext(netlist)) + ext
}

// checkInputs verifies that the input files exist and that the report does
// not overwrite any of them.
func checkInputs(cfg *config) error {

	inputs := append([]string{cfg.netlist}, cfg.parts...)
	for _, f := range []string{cfg.mission, cfg.derating, cfg.thermal} {
		if f != "" {
			inputs = append(inputs, f)
		}
	}

	for _, f := range inputs {
		fi, err := os.Stat(f)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file", f)
		}
		if cfg.output != "-" && sameFile(f, cfg.output) {
			return fmt.Errorf("the report %s would overwrite the input file %s", cfg.output, f)
		}
	}
	return nil
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// newLogger writes diagnostics to stderr: warnings always, debug messages
// with -v.
func newLogger(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: fitcalc [flags] circuit.net
       fitcalc [flags] [DIR | project.toml]

Computes FIDES 2022 failure rates (FIT) for the components of a SPICE circuit,
using ngspice for the DC operating point, a DC sweep or a transient analysis.

A project file (DIR/fitcalc.toml, or ./fitcalc.toml without arguments) holds
the netlist and the settings of the long flags, with paths relative to it;
flags on the command line override it.

Flags:
  -p, --parts FILE     parts metadata CSV; repeatable, the first one is the BOM
                       (fides CSV or KiCad BOM export)
  -m, --mission FILE   FIDES mission profile CSV; without it no FIT is computed
  -o, --output FILE    report file (default: netlist name + .md or .csv; - for stdout)
  -f, --format FORMAT  report format: markdown (default) or csv
      --ngspice PATH   ngspice executable (default: $NGSPICE, then PATH)
  -k, --keep DIR       keep the generated deck, ngspice log and raw file in DIR
                       (those at other temperatures in DIR/85C, …)
      --hot TEMP       also check the derating at ambient temperature TEMP in °C
      --sweep "SOURCE START STOP STEP"
                       DC sweep of a voltage or current source instead of the
                       operating point: mean stresses for the FIT, the largest
                       for the derating
      --tran "TSTEP TSTOP [TSTART [TMAX]]"
                       transient analysis instead of the operating point,
                       recorded from TSTART: RMS stresses and mean power for
                       the FIT; peak voltages, RMS currents and mean power for
                       the derating
      --thermal FILE   thermal network CSV (from, to, rth in K/W): the
                       temperature of each component from the power of all,
                       simulated with each element at its temperature
      --mc N           Monte Carlo: N more runs of the whole analysis with the
                       values of R, C and L drawn within their tolerance
      --vary "NAME=TOL%, …"
                       with --mc: also vary these elements, such as sources
      --seed N         with --mc: the seed of the draws (default 0)
  -d, --derating FILE  derating rules CSV (default: 80 % of every rating)
  -v, --verbose        diagnostic output on stderr
      --version        print version information and exit
  -h, --help           show this help

Exit status: 0 success (warnings allowed), 1 usage or input error,
2 simulation failure.
`)
}
